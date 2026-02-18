package udf

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestTimestamp(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 12)
	binary.LittleEndian.PutUint16(buf[2:], 2024) // Year
	buf[4] = 3                                   // Month (March)
	buf[5] = 15                                  // Day
	buf[6] = 10                                  // Hour
	buf[7] = 30                                  // Minute
	buf[8] = 45                                  // Second

	ts := rTimestamp(buf)
	expected := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)

	if !ts.Equal(expected) {
		t.Errorf("rTimestamp = %v, want %v", ts, expected)
	}
}

func TestTimestampTooShort(t *testing.T) {
	t.Parallel()

	ts := rTimestamp(make([]byte, 5))
	if !ts.IsZero() {
		t.Errorf("expected zero time from short buffer, got %v", ts)
	}
}

func TestDcharactersWindows1252(t *testing.T) {
	t.Parallel()

	input := append([]byte{8}, []byte("hello")...)
	result := rDcharacters(input)

	if result != "hello" {
		t.Errorf("rDcharacters(latin1 hello) = %q, want %q", result, "hello")
	}
}

func TestDcharactersUTF16(t *testing.T) {
	t.Parallel()

	input := []byte{16, 0x00, 0x41, 0x00, 0x42} // "AB" in UTF-16BE
	result := rDcharacters(input)

	if result != "AB" {
		t.Errorf("rDcharacters(utf16 AB) = %q, want %q", result, "AB")
	}
}

func TestDcharactersEmpty(t *testing.T) {
	t.Parallel()

	if result := rDcharacters(nil); result != "" {
		t.Errorf("rDcharacters(nil) = %q, want empty", result)
	}

	if result := rDcharacters([]byte{}); result != "" {
		t.Errorf("rDcharacters(empty) = %q, want empty", result)
	}
}

func TestDcharactersUnknownCompression(t *testing.T) {
	t.Parallel()

	result := rDcharacters([]byte{99, 'h', 'i'})
	if result != "" {
		t.Errorf("rDcharacters(unknown) = %q, want empty", result)
	}
}

func TestDstring(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 32)
	copy(buf, "TESTVOL")
	buf[31] = 7 // length byte

	result := rDstring(buf, 32)
	if result != "TESTVOL" {
		t.Errorf("rDstring = %q, want %q", result, "TESTVOL")
	}
}

func TestDstringEmpty(t *testing.T) {
	t.Parallel()

	if result := rDstring(nil, 0); result != "" {
		t.Errorf("rDstring(nil, 0) = %q, want empty", result)
	}
}

func TestDstringLengthOverflow(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 8)
	copy(buf, "ABC")
	buf[7] = 255 // bogus length

	result := rDstring(buf, 8)
	// Should clamp to fieldlen-1 = 7.
	if len(result) > 7 {
		t.Errorf("rDstring length overflow: got len %d", len(result))
	}
}

func TestDescriptorDataCopy(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 32)
	for i := range buf {
		buf[i] = byte(i)
	}

	desc := &Descriptor{}

	err := desc.fromBytes(buf)
	if err != nil {
		t.Fatalf("fromBytes: %v", err)
	}

	data := desc.Data()
	if len(data) != 16 {
		t.Fatalf("Data() len = %d, want 16", len(data))
	}

	for i, val := range data {
		if val != byte(i+16) {
			t.Errorf("Data()[%d] = %d, want %d", i, val, i+16)
		}
	}
}
