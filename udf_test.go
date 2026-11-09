package udf_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"golift.io/udf"
)

func TestNewUdfFromReader_EmptyInput(t *testing.T) {
	t.Parallel()

	_, err := udf.NewUdfFromReader(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error from empty reader, got nil")
	}
}

func TestNewUdfFromReader_GarbageInput(t *testing.T) {
	t.Parallel()

	garbage := make([]byte, 1024*1024) // 1 MiB of zeros

	_, err := udf.NewUdfFromReader(bytes.NewReader(garbage))
	if err == nil {
		t.Fatal("expected error from garbage input, got nil")
	}
}

func TestNewUdfFromReader_SmallInput(t *testing.T) {
	t.Parallel()

	small := make([]byte, 100)

	_, err := udf.NewUdfFromReader(bytes.NewReader(small))
	if err == nil {
		t.Fatal("expected error from small input, got nil")
	}
}

func TestICBTagParsing(t *testing.T) {
	t.Parallel()

	// Build a 20-byte ICB tag buffer with known values.
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint32(buf[0:], 42)    // PriorRecordedNumberOfDirectEntries
	binary.LittleEndian.PutUint16(buf[4:], 4)     // StrategyType
	binary.LittleEndian.PutUint16(buf[6:], 100)   // StrategyParameter
	binary.LittleEndian.PutUint16(buf[8:], 1)     // MaximumNumberOfEntries
	buf[10] = 0                                   // Reserved
	buf[11] = 4                                   // FileType (directory)
	binary.LittleEndian.PutUint32(buf[12:], 50)   // ParentICBLocation block number
	binary.LittleEndian.PutUint16(buf[16:], 0)    // ParentICBLocation partition ref
	binary.LittleEndian.PutUint16(buf[18:], 0x21) // Flags

	tag := udf.NewICBTag(buf)
	if tag == nil {
		t.Fatal("NewICBTag returned nil")
	}

	if tag.PriorRecordedNumberOfDirectEntries != 42 {
		t.Errorf("PriorRecordedNumberOfDirectEntries = %d, want 42", tag.PriorRecordedNumberOfDirectEntries)
	}

	if tag.StrategyType != 4 {
		t.Errorf("StrategyType = %d, want 4", tag.StrategyType)
	}

	if tag.StrategyParameter != 100 {
		t.Errorf("StrategyParameter = %d, want 100", tag.StrategyParameter)
	}

	if tag.MaximumNumberOfEntries != 1 {
		t.Errorf("MaximumNumberOfEntries = %d, want 1", tag.MaximumNumberOfEntries)
	}

	if tag.FileType != 4 {
		t.Errorf("FileType = %d, want 4", tag.FileType)
	}

	if tag.Flags != 0x21 {
		t.Errorf("Flags = %d, want 0x21", tag.Flags)
	}
}

func TestICBTagTooShort(t *testing.T) {
	t.Parallel()

	tag := udf.NewICBTag(make([]byte, 10))
	if tag != nil {
		t.Error("expected nil from short buffer")
	}
}

func TestNewExtentTooShort(t *testing.T) {
	t.Parallel()

	ext := udf.NewExtent(make([]byte, 4))
	if ext.Length != 0 || ext.Location != 0 {
		t.Error("expected zero extent from short buffer")
	}
}

func TestNewExtentLongTooShort(t *testing.T) {
	t.Parallel()

	ext := udf.NewExtentLong(make([]byte, 4))
	if ext.Length != 0 || ext.Location != 0 {
		t.Error("expected zero extent long from short buffer")
	}
}

func TestNewExtentLongPartition(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:], 100)
	binary.LittleEndian.PutUint32(buf[4:], 50)
	binary.LittleEndian.PutUint16(buf[8:], 1)

	ext := udf.NewExtentLong(buf)
	if ext.Length != 100 || ext.Location != 50 || ext.Partition != 1 {
		t.Errorf("extent = %+v, want length 100 location 50 partition 1", ext)
	}
}

func TestNewEntityIDTooShort(t *testing.T) {
	t.Parallel()

	eid := udf.NewEntityID(make([]byte, 10))
	if eid.Flags != 0 {
		t.Error("expected zero entity ID from short buffer")
	}
}

func TestReadSectorErrors(t *testing.T) {
	t.Parallel()

	// Reader with only 1 sector (not enough for anchor at sector 256).
	small := make([]byte, udf.SectorSize)

	_, err := udf.NewUdfFromReader(bytes.NewReader(small))
	if err == nil {
		t.Error("expected error reading beyond end of reader")
	}
}

func TestPartitionStartNoPartition(t *testing.T) {
	t.Parallel()

	// A garbage reader large enough to read sector 256 but with invalid data.
	// NewUdfFromReader will fail, so we test the error message.
	garbage := make([]byte, udf.SectorSize*257)

	_, err := udf.NewUdfFromReader(bytes.NewReader(garbage))
	if err == nil {
		t.Error("expected error with invalid UDF image")
	}

	if !strings.Contains(err.Error(), "anchor") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVolumeIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		primary     []byte
		logical     []byte
		wantPrimary string
		wantLogical string
		primaryErr  error
		logicalErr  error
	}{
		{name: "empty"},
		{
			name:        "8-bit",
			primary:     []byte{8, 'D', 'V', 'D', 0xE9},
			logical:     []byte{8, 'M', 'O', 'V', 'I', 'E'},
			wantPrimary: "DVDé",
			wantLogical: "MOVIE",
		},
		{
			name:        "16-bit",
			primary:     []byte{16, 0x65, 0xe5, 0x67, 0x2c},
			logical:     []byte{16, 0xd8, 0x3d, 0xde, 0x80},
			wantPrimary: "\u65E5\u672C",
			wantLogical: "🚀",
		},
		{
			name:        "literal replacement character",
			primary:     []byte{16, 0xff, 0xfd},
			logical:     []byte{16, 0xff, 0xfd},
			wantPrimary: "\uFFFD",
			wantLogical: "\uFFFD",
		},
		{
			name:       "unknown compression",
			primary:    []byte{7, 'A'},
			logical:    []byte{7, 'A'},
			primaryErr: udf.ErrUnsupportedDStringCompression,
			logicalErr: udf.ErrUnsupportedDStringCompression,
		},
		{
			name:       "odd 16-bit string",
			primary:    []byte{16, 0, 'A', 0},
			logical:    []byte{16, 0, 'A', 0},
			primaryErr: udf.ErrOddDString16Length,
			logicalErr: udf.ErrOddDString16Length,
		},
		{
			name:       "invalid surrogate",
			primary:    []byte{16, 0xd8, 0x00},
			logical:    []byte{16, 0xd8, 0x00},
			primaryErr: udf.ErrInvalidSurrogatePair,
			logicalErr: udf.ErrInvalidSurrogatePair,
		},
		{
			name:       "unpaired low surrogate",
			primary:    []byte{16, 0xdc, 0x00},
			logical:    []byte{16, 0xdc, 0x00},
			primaryErr: udf.ErrInvalidSurrogatePair,
			logicalErr: udf.ErrInvalidSurrogatePair,
		},
		{
			name:       "invalid surrogate successor",
			primary:    []byte{16, 0xd8, 0x00, 0, 'A'},
			logical:    []byte{16, 0xd8, 0x00, 0, 'A'},
			primaryErr: udf.ErrInvalidSurrogatePair,
			logicalErr: udf.ErrInvalidSurrogatePair,
		},
		{
			name:       "low surrogate before high surrogate",
			primary:    []byte{16, 0xdc, 0x00, 0xd8, 0x00},
			logical:    []byte{16, 0xdc, 0x00, 0xd8, 0x00},
			primaryErr: udf.ErrInvalidSurrogatePair,
			logicalErr: udf.ErrInvalidSurrogatePair,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			image := buildPhysicalImage()
			writeVolumeDString(image[vdsSector*sectorSize+24:vdsSector*sectorSize+56], tt.primary)
			writeVolumeDString(image[(vdsSector+2)*sectorSize+84:(vdsSector+2)*sectorSize+212], tt.logical)
			volume, err := udf.NewUdfFromReader(bytes.NewReader(image))
			if err != nil {
				t.Fatal(err)
			}
			primary, err := volume.VolumeIdentifier()
			if !errors.Is(err, tt.primaryErr) || primary != tt.wantPrimary {
				t.Errorf("VolumeIdentifier() = %q, %v; want %q, error=%v", primary, err, tt.wantPrimary, tt.primaryErr)
			}
			logical, err := volume.LogicalVolumeIdentifier()
			if !errors.Is(err, tt.logicalErr) || logical != tt.wantLogical {
				t.Errorf("LogicalVolumeIdentifier() = %q, %v; want %q, error=%v", logical, err, tt.wantLogical, tt.logicalErr)
			}
		})
	}
}

func TestVolumeIdentifierErrors(t *testing.T) {
	t.Parallel()

	image := buildPhysicalImage()
	binary.LittleEndian.PutUint16(image[vdsSector*sectorSize:], 0)
	volume, err := udf.NewUdfFromReader(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	_, err = volume.VolumeIdentifier()
	if !errors.Is(err, udf.ErrNoPrimaryVolume) {
		t.Errorf("missing primary descriptor: %v", err)
	}
	_, err = new(udf.Udf).LogicalVolumeIdentifier()
	if !errors.Is(err, udf.ErrNoLogicalVolume) {
		t.Errorf("missing logical descriptor: %v", err)
	}

	image = buildPhysicalImage()
	image[vdsSector*sectorSize+55] = 32
	volume, err = udf.NewUdfFromReader(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	_, err = volume.VolumeIdentifier()
	if !errors.Is(err, udf.ErrInvalidDStringLength) {
		t.Errorf("expected invalid primary d-string length: %v", err)
	}
}

func writeVolumeDString(field, encoded []byte) {
	clear(field)
	copy(field, encoded)
	field[len(field)-1] = byte(len(encoded))
}
