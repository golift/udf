package udf

import (
	"errors"
	"fmt"
	"io"
)

// SectorSize is the standard UDF sector size in bytes.
const SectorSize = 2048

// Udf represents a parsed UDF filesystem image.
type Udf struct {
	r      io.ReaderAt
	pvd    *PrimaryVolumeDescriptor
	pd     *PartitionDescriptor
	lvd    *LogicalVolumeDescriptor
	fsd    *FileSetDescriptor
	rootFE *FileEntry
}

// Errors returned by this package.
var (
	ErrNoPartition        = errors.New("no partition descriptor found")
	ErrNoLogicalVolume    = errors.New("no logical volume descriptor found")
	ErrNoFileEntry        = errors.New("no file entry provided and no root file entry")
	ErrNoAllocDescriptors = errors.New("file entry has no allocation descriptors")
	ErrBadAnchorTag       = errors.New("unexpected anchor volume pointer tag")
	ErrShortRead          = errors.New("short read")
)

// ErrNilReader is returned when a nil io.ReaderAt is passed to NewUdfFromReader.
var ErrNilReader = errors.New("nil reader")

// NewUdfFromReader creates a new Udf from an io.ReaderAt, parsing the image immediately.
func NewUdfFromReader(r io.ReaderAt) (*Udf, error) {
	if r == nil {
		return nil, ErrNilReader
	}

	u := &Udf{r: r}

	err := u.init()
	if err != nil {
		return nil, err
	}

	return u, nil
}

// PartitionStart returns the starting sector of the partition.
func (u *Udf) PartitionStart() (uint64, error) {
	if u.pd == nil {
		return 0, ErrNoPartition
	}

	return uint64(u.pd.PartitionStartingLocation), nil
}

// GetReader returns the underlying io.ReaderAt.
func (u *Udf) GetReader() io.ReaderAt {
	return u.r
}

// ReadSectors reads consecutive sectors from the image.
func (u *Udf) ReadSectors(sectorNumber, sectorsCount uint64) ([]byte, error) {
	size := SectorSize * sectorsCount
	buf := make([]byte, size)

	n, err := u.r.ReadAt(buf, int64(SectorSize*sectorNumber))
	if err != nil {
		return nil, fmt.Errorf("reading sectors at %d: %w", sectorNumber, err)
	}

	if uint64(n) != size {
		return nil, fmt.Errorf("sector %d: got %d bytes, want %d: %w", sectorNumber, n, size, ErrShortRead)
	}

	return buf, nil
}

// ReadSector reads a single sector from the image.
func (u *Udf) ReadSector(sectorNumber uint64) ([]byte, error) {
	return u.ReadSectors(sectorNumber, 1)
}

// ReadDir reads a directory. Pass nil for the root directory.
func (u *Udf) ReadDir(fe *FileEntry) ([]File, error) {
	if fe == nil {
		fe = u.rootFE
	}

	if fe == nil {
		return nil, ErrNoFileEntry
	}

	fdBuf, fdLen, err := u.readDirData(fe)
	if err != nil {
		return nil, err
	}

	return u.parseDirEntries(fdBuf, fdLen)
}

func (u *Udf) readDirData(fe *FileEntry) ([]byte, uint64, error) {
	ps, err := u.PartitionStart()
	if err != nil {
		return nil, 0, err
	}

	if len(fe.AllocationDescriptors) == 0 {
		return nil, 0, ErrNoAllocDescriptors
	}

	adPos := fe.AllocationDescriptors[0]
	fdLen := uint64(adPos.Length)

	fdBuf, err := u.ReadSectors(ps+uint64(adPos.Location), (fdLen+SectorSize-1)/SectorSize)
	if err != nil {
		return nil, 0, fmt.Errorf("reading directory data: %w", err)
	}

	return fdBuf, fdLen, nil
}

func (u *Udf) parseDirEntries(fdBuf []byte, fdLen uint64) ([]File, error) {
	var result []File

	fdOff := uint64(0)
	for fdOff < fdLen {
		if fdOff+38 > uint64(len(fdBuf)) {
			break
		}

		fid, err := newFileIdentifierDescriptor(fdBuf[fdOff:])
		if err != nil {
			return result, fmt.Errorf("parsing file identifier at offset %d: %w", fdOff, err)
		}

		if fid.FileIdentifier != "" {
			result = append(result, File{Udf: u, Fid: fid})
		}

		fidLen := fid.Len()
		if fidLen == 0 {
			break
		}

		fdOff += fidLen
	}

	return result, nil
}

func (u *Udf) init() error {
	err := u.readVolumeDescriptors()
	if err != nil {
		return err
	}

	return u.readRootEntry()
}

func (u *Udf) readVolumeDescriptors() error {
	anchorDesc, err := u.readAnchor()
	if err != nil {
		return err
	}

	for sector := uint64(anchorDesc.MainVolumeDescriptorSeq.Location); ; sector++ {
		done, err := u.parseVolumeDescriptor(sector)
		if err != nil {
			return err
		}

		if done {
			break
		}
	}

	return nil
}

func (u *Udf) readAnchor() (*AnchorVolumeDescriptorPointer, error) {
	anchorBuf, err := u.ReadSector(256)
	if err != nil {
		return nil, fmt.Errorf("reading anchor descriptor: %w", err)
	}

	anchorDesc, err := newAnchorVolumeDescriptorPointer(anchorBuf)
	if err != nil {
		return nil, fmt.Errorf("parsing anchor descriptor: %w", err)
	}

	if anchorDesc.Descriptor.TagIdentifier != descriptorAnchorVolumePointer {
		return nil, fmt.Errorf("%w: expected %d, got %d",
			ErrBadAnchorTag, descriptorAnchorVolumePointer, anchorDesc.Descriptor.TagIdentifier)
	}

	return anchorDesc, nil
}

func (u *Udf) parseVolumeDescriptor(sector uint64) (bool, error) {
	buf, err := u.ReadSector(sector)
	if err != nil {
		return false, fmt.Errorf("reading volume descriptor at sector %d: %w", sector, err)
	}

	desc, err := newDescriptor(buf)
	if err != nil {
		return false, err
	}

	if desc.TagIdentifier == descriptorTerminating {
		return true, nil
	}

	switch desc.TagIdentifier {
	case descriptorPrimaryVolume:
		u.pvd, err = newPrimaryVolumeDescriptor(desc.data)
	case descriptorPartition:
		u.pd, err = newPartitionDescriptor(desc.data)
	case descriptorLogicalVolume:
		u.lvd, err = newLogicalVolumeDescriptor(desc.data)
	}

	return false, err
}

func (u *Udf) readRootEntry() error {
	partitionStart, err := u.PartitionStart()
	if err != nil {
		return err
	}

	if u.lvd == nil {
		return ErrNoLogicalVolume
	}

	fsdBuf, err := u.ReadSector(partitionStart + u.lvd.LogicalVolumeContentsUse.Location)
	if err != nil {
		return fmt.Errorf("reading file set descriptor: %w", err)
	}

	u.fsd, err = newFileSetDescriptor(fsdBuf)
	if err != nil {
		return err
	}

	rootBuf, err := u.ReadSector(partitionStart + u.fsd.RootDirectoryICB.Location)
	if err != nil {
		return fmt.Errorf("reading root file entry: %w", err)
	}

	u.rootFE, err = newFileEntry(rootBuf)

	return err
}
