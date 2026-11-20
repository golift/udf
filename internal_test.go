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

func TestTimestampTimeZone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  uint16
		off  int
		loc  *time.Location
	}{
		{name: "utc plus 330", typ: 1, off: 330, loc: time.FixedZone("", 330*60)},
		{name: "utc minus 60", typ: 1, off: -60, loc: time.FixedZone("", -60*60)},
		{name: "unspecified offset", typ: 1, off: -2047, loc: time.UTC},
		{name: "type 0", typ: 0, off: 330, loc: time.UTC},
		{name: "offset too large", typ: 1, off: 1441, loc: time.UTC},
		{name: "offset too small", typ: 1, off: -1441, loc: time.UTC},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ts := rTimestamp(timestampBytes(test.typ, test.off))
			want := time.Date(2024, 3, 15, 10, 30, 45, 0, test.loc)

			if !ts.Equal(want) {
				t.Errorf("rTimestamp = %v, want %v", ts, want)
			}

			_, gotOff := ts.Zone()
			_, wantOff := want.Zone()

			if gotOff != wantOff {
				t.Errorf("zone offset = %d, want %d", gotOff, wantOff)
			}
		})
	}
}

// timestampBytes builds a 12-byte ECMA-167 timestamp. typ is the 4-bit type
// and off is the signed offset from UTC in minutes, stored in 12 bits.
func timestampBytes(typ uint16, off int) []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint16(buf, (typ<<12)|(uint16(off)&0x0FFF))
	binary.LittleEndian.PutUint16(buf[2:], 2024)
	buf[4] = 3
	buf[5] = 15
	buf[6] = 10
	buf[7] = 30
	buf[8] = 45

	return buf
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
