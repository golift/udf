package udf

// Extent is an 8-byte short allocation descriptor.
type Extent struct {
	Length   uint32
	Location uint32
}

// NewExtent parses an Extent from a byte slice.
// Returns a zero Extent if the slice is too short.
func NewExtent(b []byte) Extent {
	if len(b) < 8 {
		return Extent{}
	}

	return Extent{
		Length:   rlU32(b[0:]),
		Location: rlU32(b[4:]),
	}
}

// ExtentLong is a 16-byte long allocation descriptor.
type ExtentLong struct {
	Length   uint32
	Location uint64
}

// NewExtentLong parses an ExtentLong from a byte slice.
// Returns a zero ExtentLong if the slice is too short.
func NewExtentLong(b []byte) ExtentLong {
	if len(b) < 10 {
		return ExtentLong{}
	}

	return ExtentLong{
		Length:   rlU32(b[0:]),
		Location: rlU48(b[4:]),
	}
}
