package udf

// ICBTag represents an ICB Tag (ECMA-167 Part 4 §14.6).
// The structure is 20 bytes:
//
//	Offset  Size  Field
//	0       4     PriorRecordedNumberOfDirectEntries
//	4       2     StrategyType
//	6       2     StrategyParameter
//	8       2     MaximumNumberOfEntries
//	10      1     Reserved
//	11      1     FileType
//	12      6     ParentICBLocation (lb_addr: 4-byte block + 2-byte partition)
//	18      2     Flags
type ICBTag struct {
	PriorRecordedNumberOfDirectEntries uint32
	StrategyType                       uint16
	StrategyParameter                  uint16
	MaximumNumberOfEntries             uint16
	FileType                           uint8
	ParentICBLocation                  uint64
	Flags                              uint16
}

// NewICBTag parses an ICBTag from a byte slice.
// Returns nil if the slice is too short.
func NewICBTag(b []byte) *ICBTag {
	if len(b) < 20 {
		return nil
	}

	return &ICBTag{
		PriorRecordedNumberOfDirectEntries: rlU32(b[0:]),
		StrategyType:                       rlU16(b[4:]),
		StrategyParameter:                  rlU16(b[6:]),
		MaximumNumberOfEntries:             rlU16(b[8:]),
		FileType:                           rU8(b[11:]),
		ParentICBLocation:                  rlU48(b[12:]),
		Flags:                              rlU16(b[18:]),
	}
}
