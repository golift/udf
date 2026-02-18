package udf_test

import (
	"bytes"
	"encoding/binary"
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
