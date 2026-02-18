package udf

// EntityID represents a UDF Entity Identifier (ECMA-167 §7.4).
type EntityID struct {
	Flags            uint8
	Identifier       [23]byte
	IdentifierSuffix [8]byte
}

// NewEntityID parses an EntityID from a byte slice.
// Returns a zero EntityID if the slice is too short.
func NewEntityID(b []byte) EntityID {
	if len(b) < 32 {
		return EntityID{}
	}

	e := EntityID{Flags: b[0]}
	copy(e.Identifier[:], b[1:24])
	copy(e.IdentifierSuffix[:], b[24:32])

	return e
}
