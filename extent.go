package udf

// Extent length types from ECMA-167 4/14.14.1.1. The type occupies the top
// two bits of Length.
const (
	ExtentRecorded        uint32 = 0
	ExtentAllocated       uint32 = 1
	ExtentUnallocated     uint32 = 2
	ExtentNextDescriptors uint32 = 3
)

// Extent is a short allocation descriptor (ECMA-167 4/14.14.1).
// Partition is the partition reference number the block belongs to.
// Short descriptors inherit the file entry's partition.
type Extent struct {
	Length    uint32
	Location  uint32
	Partition uint16
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

// DataLength returns the byte length of the extent, without the type bits.
func (e Extent) DataLength() uint32 {
	return e.Length & 0x3FFFFFFF
}

// ExtentType returns the ECMA-167 extent type (0 recorded, 1 allocated hole,
// 2 unallocated hole, 3 next allocation-descriptor extent).
func (e Extent) ExtentType() uint32 {
	return e.Length >> 30
}

// ExtentLong is a long allocation descriptor (ECMA-167 4/14.14.2).
// Location is the logical block number. Partition is the partition
// reference number from the descriptor's lb_addr.
type ExtentLong struct {
	Length    uint32
	Location  uint64
	Partition uint16
}

// NewExtentLong parses an ExtentLong from a byte slice.
// Returns a zero ExtentLong if the slice is too short.
func NewExtentLong(b []byte) ExtentLong {
	if len(b) < 10 {
		return ExtentLong{}
	}

	return ExtentLong{
		Length:    rlU32(b[0:]),
		Location:  uint64(rlU32(b[4:])),
		Partition: rlU16(b[8:]),
	}
}
