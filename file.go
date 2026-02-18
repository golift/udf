package udf

import (
	"fmt"
	"io"
	"os"
	"time"
)

// File represents a file or directory in a UDF filesystem.
type File struct {
	Udf               *Udf
	Fid               *FileIdentifierDescriptor
	fe                *FileEntry
	fileEntryPosition uint64
}

// GetFileEntryPosition returns the sector position of this file's entry.
func (f *File) GetFileEntryPosition() int64 {
	return int64(f.fileEntryPosition)
}

// GetFileOffset returns the byte offset of this file's data in the image.
func (f *File) GetFileOffset() (int64, error) {
	fe, err := f.FileEntry()
	if err != nil {
		return 0, err
	}

	if len(fe.AllocationDescriptors) == 0 {
		return 0, ErrNoAllocDescriptors
	}

	ps, err := f.Udf.PartitionStart()
	if err != nil {
		return 0, err
	}

	return SectorSize * (int64(fe.AllocationDescriptors[0].Location) + int64(ps)), nil
}

// FileEntry returns the parsed FileEntry, loading it on first access.
func (f *File) FileEntry() (*FileEntry, error) {
	if f.fe != nil {
		return f.fe, nil
	}

	ps, err := f.Udf.PartitionStart()
	if err != nil {
		return nil, err
	}

	f.fileEntryPosition = f.Fid.ICB.Location

	buf, err := f.Udf.ReadSector(ps + f.fileEntryPosition)
	if err != nil {
		return nil, fmt.Errorf("reading file entry: %w", err)
	}

	f.fe, err = newFileEntry(buf)
	if err != nil {
		return nil, err
	}

	return f.fe, nil
}

// NewReader returns a SectionReader for this file's data.
func (f *File) NewReader() (*io.SectionReader, error) {
	offset, err := f.GetFileOffset()
	if err != nil {
		return nil, err
	}

	return io.NewSectionReader(f.Udf.r, offset, f.Size()), nil
}

// Name returns the file's name.
func (f *File) Name() string {
	return f.Fid.FileIdentifier
}

// Mode returns the file's permission bits.
func (f *File) Mode() os.FileMode {
	fe, err := f.FileEntry()
	if err != nil {
		// FileEntry failed; use FileCharacteristics bit 1 for directory check
		// instead of IsDir() which would call FileEntry() again and fail.
		if f.Fid != nil && f.Fid.FileCharacteristics&0x02 != 0 {
			return os.ModeDir | 0o755
		}

		return 0o644
	}

	var mode os.FileMode

	perms := os.FileMode(fe.Permissions)
	mode |= ((perms >> 0) & 7) << 0
	mode |= ((perms >> 5) & 7) << 3
	mode |= ((perms >> 10) & 7) << 6

	if f.IsDir() {
		mode |= os.ModeDir
	}

	return mode
}

// Size returns the file's size in bytes.
func (f *File) Size() int64 {
	fe, err := f.FileEntry()
	if err != nil {
		return 0
	}

	return int64(fe.InformationLength)
}

// ModTime returns the file's modification time.
func (f *File) ModTime() time.Time {
	fe, err := f.FileEntry()
	if err != nil {
		return time.Time{}
	}

	return fe.ModificationTime
}

// IsDir returns true if the file is a directory.
func (f *File) IsDir() bool {
	fe, err := f.FileEntry()
	if err != nil {
		return false
	}

	if fe.ICBTag == nil {
		return false
	}

	return fe.ICBTag.FileType == 4
}

// Sys returns the underlying FileIdentifierDescriptor.
func (f *File) Sys() any {
	return f.Fid
}

// ReadDir returns the directory entries if this file is a directory.
func (f *File) ReadDir() ([]File, error) {
	fe, err := f.FileEntry()
	if err != nil {
		return nil, err
	}

	return f.Udf.ReadDir(fe)
}
