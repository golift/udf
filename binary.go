package udf

import (
	"encoding/binary"
	"time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func rU8(b []byte) uint8 {
	return b[0]
}

func rlU64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
func rlU32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func rlU16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

func rlU48(b []byte) uint64 {
	var buf [8]byte
	copy(buf[:], b[:6])

	return rlU64(buf[:])
}

func rDstring(b []byte, fieldlen int) string {
	if fieldlen == 0 {
		return ""
	}

	length := min(int(b[fieldlen-1]), fieldlen-1)

	if length == 0 {
		return ""
	}

	return string(b[:length])
}

func rDcharacters(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	switch b[0] {
	case 8:
		s, _, err := transform.Bytes(charmap.Windows1252.NewDecoder(), b[1:])
		if err != nil {
			return ""
		}

		return string(s)
	case 16:
		s, _, err := transform.Bytes(unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder(), b[1:])
		if err != nil {
			return ""
		}

		return string(s)
	default:
		return ""
	}
}

func rTimestamp(b []byte) time.Time {
	if len(b) < 12 {
		return time.Time{}
	}

	year := int(rlU16(b[2:]))
	month := time.Month(b[4])
	day := int(b[5])
	hour := int(b[6])
	minute := int(b[7])
	second := int(b[8])

	// UDF timestamps store absolute year, month, day.
	return time.Date(year, month, day, hour, minute, second, 0, time.UTC)
}
