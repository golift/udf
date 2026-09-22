package udf

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// SectorSize is the standard UDF sector size in bytes.
const SectorSize = 2048

// Udf represents a parsed UDF filesystem image.
type Udf struct {
	r         io.ReaderAt
	blockSize uint32
	pvd       *PrimaryVolumeDescriptor
	pd        *PartitionDescriptor
	pds       []*PartitionDescriptor
	lvd       *LogicalVolumeDescriptor
	fsd       *FileSetDescriptor
	rootFE    *FileEntry
	partMaps  []partMap
}

// Errors returned by this package.
var (
	ErrNoPartition          = errors.New("no partition descriptor found")
	ErrNoLogicalVolume      = errors.New("no logical volume descriptor found")
	ErrNoFileEntry          = errors.New("no file entry provided and no root file entry")
	ErrNoAllocDescriptors   = errors.New("file entry has no allocation descriptors")
	ErrBadAnchorTag         = errors.New("unexpected anchor volume pointer tag")
	ErrShortRead            = errors.New("short read")
	ErrUnsupportedPartition = errors.New("unsupported UDF partition map")
	errVDSNotTerminated     = errors.New("volume descriptor sequence is not terminated")
	errBadBlockSize         = errors.New("unsupported logical block size")
	errBadFIDTag            = errors.New("unexpected file identifier tag")
	errDirectoryTooLarge    = errors.New("directory exceeds size limit")
	errBadSeek              = errors.New("invalid seek")
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
	size := uint64(u.sectorSize()) * sectorsCount
	buf := make([]byte, size)

	n, err := u.r.ReadAt(buf, int64(u.sectorSize())*int64(sectorNumber))
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

	fdBuf, err := u.readFileContents(fe)
	if err != nil {
		return nil, err
	}

	return u.parseDirEntries(fdBuf, uint64(len(fdBuf)))
}

// Revision reports the UDF revision from the logical volume domain identifier.
// UDF 2.50 is 0x0250 and UDF 2.60 is 0x0260. Zero means the image omitted it.
func (u *Udf) Revision() uint16 {
	if u.lvd == nil {
		return 0
	}

	return rlU16(u.lvd.DomainIdentifier.IdentifierSuffix[:])
}

func (u *Udf) sectorSize() uint32 {
	if u.blockSize == 0 {
		return SectorSize
	}

	return u.blockSize
}

func (u *Udf) parseDirEntries(fdBuf []byte, fdLen uint64) ([]File, error) {
	var result []File

	fdOff := uint64(0)
	for fdOff < fdLen {
		if fdOff+38 > uint64(len(fdBuf)) {
			break
		}

		if rlU16(fdBuf[fdOff:]) == 0 {
			break
		}

		fid, err := newFileIdentifierDescriptor(fdBuf[fdOff:])
		if err != nil {
			return result, fmt.Errorf("parsing file identifier at offset %d: %w", fdOff, err)
		}

		if fid.Descriptor.TagIdentifier != descriptorFileIdentifier {
			return result, fmt.Errorf("file identifier tag %d at offset %d: %w",
				fid.Descriptor.TagIdentifier, fdOff, errBadFIDTag)
		}

		keep := fid.FileIdentifier != "" &&
			fid.FileCharacteristics&fidCharDeleted == 0 &&
			fid.FileCharacteristics&fidCharParent == 0
		if keep {
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
	u.blockSize = SectorSize

	err := u.readVolumeDescriptors()
	if err != nil {
		return err
	}

	if u.lvd == nil {
		return ErrNoLogicalVolume
	}

	if u.lvd.LogicalBlockSize != SectorSize {
		return fmt.Errorf("logical block size %d: only %d-byte blocks are supported: %w",
			u.lvd.LogicalBlockSize, SectorSize, errBadBlockSize)
	}

	err = u.bindPartitions()
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

	start := uint64(anchorDesc.MainVolumeDescriptorSeq.Location)
	count := uint64(anchorDesc.MainVolumeDescriptorSeq.DataLength()) / uint64(u.sectorSize())

	if count == 0 {
		count = 16
	}

	if count > 64 {
		count = 64
	}

	for i := range count {
		done, err := u.parseVolumeDescriptor(start + i)
		if err != nil {
			return err
		}

		if done {
			return nil
		}
	}

	return errVDSNotTerminated
}

func (u *Udf) readAnchor() (*AnchorVolumeDescriptorPointer, error) {
	var lastErr error

	for _, sector := range u.anchorSectors() {
		anchorDesc, err := u.anchorAt(sector)
		if err != nil {
			lastErr = err
			continue
		}

		return anchorDesc, nil
	}

	if lastErr == nil {
		lastErr = ErrBadAnchorTag
	}

	return nil, lastErr
}

func (u *Udf) anchorSectors() []uint64 {
	sectors := []uint64{256}

	size, ok := readerSize(u.r)
	if !ok || size < int64(u.sectorSize()) {
		return sectors
	}

	last := uint64(size/int64(u.sectorSize())) - 1
	if last != 256 {
		sectors = append(sectors, last)
	}

	if last > 256 {
		sectors = append(sectors, last-256)
	}

	return sectors
}

func (u *Udf) anchorAt(sector uint64) (*AnchorVolumeDescriptorPointer, error) {
	anchorBuf, err := u.ReadSector(sector)
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

func readerSize(r io.ReaderAt) (int64, bool) {
	if s, ok := r.(interface{ Size() int64 }); ok {
		return s.Size(), true
	}

	file, ok := r.(*os.File)
	if !ok {
		return 0, false
	}

	info, err := file.Stat()
	if err != nil {
		return 0, false
	}

	return info.Size(), true
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
		var pd *PartitionDescriptor

		pd, err = newPartitionDescriptor(desc.data)
		if err == nil {
			u.pd = pd
			u.pds = append(u.pds, pd)
		}
	case descriptorLogicalVolume:
		u.lvd, err = newLogicalVolumeDescriptor(desc.data)
	}

	return false, err
}

func (u *Udf) readRootEntry() error {
	if u.lvd == nil {
		return ErrNoLogicalVolume
	}

	fsd := u.lvd.LogicalVolumeContentsUse

	fsdBuf, err := u.readPartitionBytes(fsd.Partition, uint32(fsd.Location), u.blockSize)
	if err != nil {
		return fmt.Errorf("reading file set descriptor: %w", err)
	}

	u.fsd, err = newFileSetDescriptor(fsdBuf)
	if err != nil {
		return err
	}

	if u.fsd.Descriptor.TagIdentifier != descriptorFileSet {
		return fmt.Errorf("file set tag %d: %w", u.fsd.Descriptor.TagIdentifier, ErrNoFileEntry)
	}

	root := u.fsd.RootDirectoryICB

	u.rootFE, err = u.readFileEntry(root.Partition, uint32(root.Location))
	if err != nil {
		return fmt.Errorf("reading root file entry: %w", err)
	}

	return nil
}
