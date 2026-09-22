package udf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// byteRun is a contiguous span of file bytes, either stored in the image or a hole.
type byteRun struct {
	fileOff int64
	phys    int64
	length  int64
	hole    bool
}

const (
	maxDirectoryBytes = 32 << 20
	maxFileEntryBytes = 1 << 20
	maxAllocDescBytes = 1 << 20
	allocExtentHeader = 24
)

func (u *Udf) readFileEntry(part uint16, lbn uint32) (*FileEntry, error) {
	buf, err := u.readPartitionBytes(part, lbn, u.blockSize)
	if err != nil {
		return nil, fmt.Errorf("reading file entry at partition %d block %d: %w", part, lbn, err)
	}

	total, err := fileEntryTotal(buf)
	if err != nil {
		return nil, fmt.Errorf("file entry at partition %d block %d: %w", part, lbn, err)
	}

	if total > u.blockSize {
		buf, err = u.readPartitionBytes(part, lbn, total)
		if err != nil {
			return nil, fmt.Errorf("reading file entry at partition %d block %d: %w", part, lbn, err)
		}
	}

	fe, err := parseFileEntry(buf, part)
	if err != nil {
		return nil, fmt.Errorf("file entry at partition %d block %d: %w", part, lbn, err)
	}

	return fe, nil
}

func fileEntryTotal(b []byte) (uint32, error) {
	if len(b) < 16 {
		return 0, fmt.Errorf("file entry: %w", ErrBufferTooShort)
	}

	var header uint32

	switch rlU16(b) {
	case descriptorFileEntry:
		header = fileEntryHeader
	case descriptorExtendedFileEntry:
		header = extendedFileEntryHeader
	default:
		return 0, fmt.Errorf("tag %d: %w", rlU16(b), ErrNoFileEntry)
	}

	if uint32(len(b)) < header {
		return 0, fmt.Errorf("file entry: %w", ErrBufferTooShort)
	}

	lea := rlU32(b[header-8:])
	lad := rlU32(b[header-4:])

	total, ok := within(maxFileEntryBytes, header, lea)
	if !ok {
		return 0, errFileEntryTooLarge
	}

	total, ok = within(maxFileEntryBytes, total, lad)
	if !ok {
		return 0, errFileEntryTooLarge
	}

	return total, nil
}

func (u *Udf) readPartitionBytes(part uint16, lbn, n uint32) ([]byte, error) {
	buf := make([]byte, n)

	var filled int64

	logical := int64(lbn) * int64(u.blockSize)

	for filled < int64(n) {
		phys, contig, err := u.translateLogical(part, logical+filled, int64(n)-filled)
		if err != nil {
			return nil, err
		}

		_, err = io.ReadFull(io.NewSectionReader(u.r, phys, contig), buf[filled:filled+contig])
		if err != nil {
			return nil, fmt.Errorf("reading partition %d block %d: %w", part, lbn, err)
		}

		filled += contig
	}

	return buf, nil
}

func (u *Udf) readFileContents(fe *FileEntry) ([]byte, error) {
	if fe.InformationLength > maxDirectoryBytes {
		return nil, fmt.Errorf("directory of %d bytes exceeds %d: %w",
			fe.InformationLength, maxDirectoryBytes, errDirectoryTooLarge)
	}

	r, err := u.open(fe)
	if err != nil {
		return nil, err
	}

	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	return buf, nil
}

func (u *Udf) open(fe *FileEntry) (*io.SectionReader, error) {
	if len(fe.embedded) > 0 {
		n := len(fe.embedded)
		if fe.InformationLength < uint64(n) {
			n = int(fe.InformationLength)
		}

		return io.NewSectionReader(bytes.NewReader(fe.embedded[:n]), 0, int64(n)), nil
	}

	runs, err := u.buildRuns(fe)
	if err != nil {
		return nil, err
	}

	er := &extentReader{r: u.r, size: int64(fe.InformationLength), runs: runs}

	return io.NewSectionReader(er, 0, er.size), nil
}

func (u *Udf) buildRuns(fe *FileEntry) ([]byteRun, error) {
	if fe == nil {
		return nil, ErrNoFileEntry
	}

	runs, _, err := u.runsFromAds(fe.AllocationDescriptors, adTypeOf(fe), 0, 0)

	return runs, err
}

func (u *Udf) runsFromAds(ads []Extent, kind uint8, fileOff int64, depth int) ([]byteRun, int64, error) {
	if depth > maxAllocDepth {
		return nil, 0, fmt.Errorf("allocation descriptor chain: %w", errAllocChain)
	}

	var runs []byteRun

	for _, ad := range ads {
		n := ad.DataLength()
		if n == 0 {
			continue
		}

		var err error

		switch ad.ExtentType() {
		case ExtentNextDescriptors:
			next, nextErr := u.readNextAds(ad, kind)
			if nextErr != nil {
				return nil, 0, nextErr
			}

			var more []byteRun

			more, fileOff, err = u.runsFromAds(next, kind, fileOff, depth+1)
			runs = append(runs, more...)
		case ExtentRecorded:
			runs, fileOff, err = u.consumeRecorded(runs, fileOff, ad.Partition, ad.Location, n)
		default:
			runs = append(runs, byteRun{fileOff: fileOff, length: int64(n), hole: true})
			fileOff += int64(n)
		}

		if err != nil {
			return nil, 0, err
		}
	}

	return runs, fileOff, nil
}

func (u *Udf) readNextAds(ad Extent, kind uint8) ([]Extent, error) {
	n := ad.DataLength()
	if n < allocExtentHeader {
		return nil, fmt.Errorf("allocation extent: %w", ErrBufferTooShort)
	}

	header, err := u.readPartitionBytes(ad.Partition, ad.Location, allocExtentHeader)
	if err != nil {
		return nil, fmt.Errorf("reading allocation extent: %w", err)
	}

	if rlU16(header) != descriptorAllocExtent {
		return nil, fmt.Errorf("allocation extent tag %d: %w", rlU16(header), ErrNoAllocDescriptors)
	}

	adLen := rlU32(header[20:])
	if adLen > maxAllocDescBytes || adLen > n-allocExtentHeader {
		return nil, fmt.Errorf("allocation extent descriptors %d: %w", adLen, errAllocTooLarge)
	}

	if adLen == 0 {
		return nil, nil
	}

	buf, err := u.readPartitionBytes(ad.Partition, ad.Location, allocExtentHeader+adLen)
	if err != nil {
		return nil, fmt.Errorf("reading allocation extent: %w", err)
	}

	return parseAllocBytes(buf[allocExtentHeader:], kind, ad.Partition)
}

func (u *Udf) consumeRecorded(runs []byteRun, fileOff int64, part uint16, lbn, n uint32) ([]byteRun, int64, error) {
	left := int64(n)
	logical := int64(lbn) * int64(u.blockSize)

	for left > 0 {
		phys, contig, err := u.translateLogical(part, logical, left)
		if err != nil {
			return nil, 0, err
		}

		runs = append(runs, byteRun{fileOff: fileOff, phys: phys, length: contig})
		fileOff += contig
		logical += contig
		left -= contig
	}

	return runs, fileOff, nil
}

var errAllocChain = errors.New("allocation descriptor chain too deep")

// extentReader reads a file across allocation extents, including holes.
type extentReader struct {
	r    io.ReaderAt
	size int64
	runs []byteRun
}

func (r *extentReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative read offset %d: %w", off, errBadSeek)
	}

	if off >= r.size {
		return 0, io.EOF
	}

	if int64(len(p)) > r.size-off {
		p = p[:r.size-off]

		n, err := r.readFrom(p, off)
		if err == nil {
			err = io.EOF
		}

		return n, err
	}

	return r.readFrom(p, off)
}

func (r *extentReader) readFrom(p []byte, off int64) (int, error) {
	dst := 0

	for dst < len(p) {
		pos := off + int64(dst)

		run, ok := runAt(r.runs, pos)
		if !ok {
			return dst, io.ErrUnexpectedEOF
		}

		n := min(int(run.length-(pos-run.fileOff)), len(p)-dst)

		if run.hole {
			clear(p[dst : dst+n])
			dst += n

			continue
		}

		got, err := r.r.ReadAt(p[dst:dst+n], run.phys+(pos-run.fileOff))
		dst += got

		if err != nil {
			return dst, fmt.Errorf("reading file data: %w", err)
		}

		if got < n {
			return dst, fmt.Errorf("reading file data: %w", io.ErrUnexpectedEOF)
		}
	}

	return dst, nil
}

func runAt(runs []byteRun, off int64) (byteRun, bool) {
	for _, run := range runs {
		if off >= run.fileOff && off < run.fileOff+run.length {
			return run, true
		}
	}

	return byteRun{}, false
}
