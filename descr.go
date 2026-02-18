package udf

import (
	"errors"
	"fmt"
	"time"
)

// Descriptor tag identifiers per ECMA-167.
const (
	descriptorPrimaryVolume       = 0x1
	descriptorAnchorVolumePointer = 0x2
	descriptorPartition           = 0x5
	descriptorLogicalVolume       = 0x6
	descriptorTerminating         = 0x8
	descriptorFileSet             = 0x100
	descriptorFileIdentifier      = 0x101
	descriptorFileEntry           = 0x105
)

// ErrBufferTooShort is returned when a byte slice is too short for parsing.
var ErrBufferTooShort = errors.New("buffer too short")

// Descriptor is a UDF descriptor tag (ECMA-167 §7.2).
type Descriptor struct {
	TagIdentifier       uint16
	DescriptorVersion   uint16
	TagChecksum         uint8
	TagSerialNumber     uint16
	DescriptorCRC       uint16
	DescriptorCRCLength uint16
	TagLocation         uint32
	data                []byte
}

// Data returns a copy of the descriptor payload (after the 16-byte tag).
func (d *Descriptor) Data() []byte {
	if len(d.data) <= 16 {
		return nil
	}

	buf := make([]byte, len(d.data)-16)
	copy(buf, d.data[16:])

	return buf
}

func (d *Descriptor) fromBytes(b []byte) error {
	if len(b) < 16 {
		return fmt.Errorf("descriptor tag: %w", ErrBufferTooShort)
	}

	d.TagIdentifier = rlU16(b[0:])
	d.DescriptorVersion = rlU16(b[2:])
	d.TagChecksum = rU8(b[4:])
	d.TagSerialNumber = rlU16(b[6:])
	d.DescriptorCRC = rlU16(b[8:])
	d.DescriptorCRCLength = rlU16(b[10:])
	d.TagLocation = rlU32(b[12:])
	d.data = b

	return nil
}

func newDescriptor(b []byte) (*Descriptor, error) {
	d := &Descriptor{}

	err := d.fromBytes(b)
	if err != nil {
		return nil, err
	}

	return d, nil
}

// AnchorVolumeDescriptorPointer is ECMA-167 §10.2.
type AnchorVolumeDescriptorPointer struct {
	Descriptor                 Descriptor
	MainVolumeDescriptorSeq    Extent
	ReserveVolumeDescriptorSeq Extent
}

func newAnchorVolumeDescriptorPointer(b []byte) (*AnchorVolumeDescriptorPointer, error) {
	if len(b) < 32 {
		return nil, fmt.Errorf("anchor volume descriptor: %w", ErrBufferTooShort)
	}

	ad := &AnchorVolumeDescriptorPointer{}

	err := ad.Descriptor.fromBytes(b)
	if err != nil {
		return nil, err
	}

	ad.MainVolumeDescriptorSeq = NewExtent(b[16:])
	ad.ReserveVolumeDescriptorSeq = NewExtent(b[24:])

	return ad, nil
}

// PrimaryVolumeDescriptor is ECMA-167 §10.1.
type PrimaryVolumeDescriptor struct {
	Descriptor                                  Descriptor
	VolumeDescriptorSequenceNumber              uint32
	PrimaryVolumeDescriptorNumber               uint32
	VolumeIdentifier                            string
	VolumeSequenceNumber                        uint16
	MaximumVolumeSequenceNumber                 uint16
	InterchangeLevel                            uint16
	MaximumInterchangeLevel                     uint16
	CharacterSetList                            uint32
	MaximumCharacterSetList                     uint32
	VolumeSetIdentifier                         string
	VolumeAbstract                              Extent
	VolumeCopyrightNoticeExtent                 Extent
	ApplicationIdentifier                       EntityID
	RecordingDateTime                           time.Time
	ImplementationIdentifier                    EntityID
	ImplementationUse                           []byte
	PredecessorVolumeDescriptorSequenceLocation uint32
	Flags                                       uint16
}

func newPrimaryVolumeDescriptor(b []byte) (*PrimaryVolumeDescriptor, error) {
	if len(b) < 490 {
		return nil, fmt.Errorf("primary volume descriptor: %w", ErrBufferTooShort)
	}

	pvd := &PrimaryVolumeDescriptor{}

	err := pvd.Descriptor.fromBytes(b)
	if err != nil {
		return nil, err
	}

	pvd.VolumeDescriptorSequenceNumber = rlU32(b[16:])
	pvd.PrimaryVolumeDescriptorNumber = rlU32(b[20:])
	pvd.VolumeIdentifier = rDstring(b[24:], 32)
	pvd.VolumeSequenceNumber = rlU16(b[56:])
	pvd.MaximumVolumeSequenceNumber = rlU16(b[58:])
	pvd.InterchangeLevel = rlU16(b[60:])
	pvd.MaximumInterchangeLevel = rlU16(b[62:])
	pvd.CharacterSetList = rlU32(b[64:])
	pvd.MaximumCharacterSetList = rlU32(b[68:])
	pvd.VolumeSetIdentifier = rDstring(b[72:], 128)
	pvd.VolumeAbstract = NewExtent(b[328:])
	pvd.VolumeCopyrightNoticeExtent = NewExtent(b[336:])
	pvd.ApplicationIdentifier = NewEntityID(b[344:])
	pvd.RecordingDateTime = rTimestamp(b[376:])
	pvd.ImplementationIdentifier = NewEntityID(b[388:])
	pvd.ImplementationUse = b[420:484]
	pvd.PredecessorVolumeDescriptorSequenceLocation = rlU32(b[484:])
	pvd.Flags = rlU16(b[488:])

	return pvd, nil
}

// PartitionDescriptor is ECMA-167 §10.5.
type PartitionDescriptor struct {
	Descriptor                     Descriptor
	VolumeDescriptorSequenceNumber uint32
	PartitionFlags                 uint16
	PartitionNumber                uint16
	PartitionContents              EntityID
	PartitionContentsUse           []byte
	AccessType                     uint32
	PartitionStartingLocation      uint32
	PartitionLength                uint32
	ImplementationIdentifier       EntityID
	ImplementationUse              []byte
}

func newPartitionDescriptor(b []byte) (*PartitionDescriptor, error) {
	if len(b) < 356 {
		return nil, fmt.Errorf("partition descriptor: %w", ErrBufferTooShort)
	}

	pd := &PartitionDescriptor{}

	err := pd.Descriptor.fromBytes(b)
	if err != nil {
		return nil, err
	}

	pd.VolumeDescriptorSequenceNumber = rlU32(b[16:])
	pd.PartitionFlags = rlU16(b[20:])
	pd.PartitionNumber = rlU16(b[22:])
	pd.PartitionContents = NewEntityID(b[24:])
	pd.PartitionContentsUse = b[56:184]
	pd.AccessType = rlU32(b[184:])
	pd.PartitionStartingLocation = rlU32(b[188:])
	pd.PartitionLength = rlU32(b[192:])
	pd.ImplementationIdentifier = NewEntityID(b[196:])
	pd.ImplementationUse = b[228:356]

	return pd, nil
}

// PartitionMap is ECMA-167 §10.7 Type 1 Partition Map.
type PartitionMap struct {
	PartitionMapType     uint8
	PartitionMapLength   uint8
	VolumeSequenceNumber uint16
	PartitionNumber      uint16
}

func (pm *PartitionMap) fromBytes(b []byte) {
	if len(b) < 6 {
		return
	}
	// UDF is little-endian; the original code used big-endian here by mistake.
	pm.PartitionMapType = rU8(b[0:])
	pm.PartitionMapLength = rU8(b[1:])
	pm.VolumeSequenceNumber = rlU16(b[2:])
	pm.PartitionNumber = rlU16(b[4:])
}

// LogicalVolumeDescriptor is ECMA-167 §10.6.
type LogicalVolumeDescriptor struct {
	Descriptor                     Descriptor
	VolumeDescriptorSequenceNumber uint32
	LogicalVolumeIdentifier        string
	LogicalBlockSize               uint32
	DomainIdentifier               EntityID
	LogicalVolumeContentsUse       ExtentLong
	MapTableLength                 uint32
	NumberOfPartitionMaps          uint32
	ImplementationIdentifier       EntityID
	ImplementationUse              []byte
	IntegritySequenceExtent        Extent
	PartitionMaps                  []PartitionMap
}

func newLogicalVolumeDescriptor(b []byte) (*LogicalVolumeDescriptor, error) {
	if len(b) < 440 {
		return nil, fmt.Errorf("logical volume descriptor: %w", ErrBufferTooShort)
	}

	lvd := &LogicalVolumeDescriptor{}

	err := lvd.Descriptor.fromBytes(b)
	if err != nil {
		return nil, err
	}

	lvd.VolumeDescriptorSequenceNumber = rlU32(b[16:])
	lvd.LogicalVolumeIdentifier = rDstring(b[84:], 128)
	lvd.LogicalBlockSize = rlU32(b[212:])
	lvd.DomainIdentifier = NewEntityID(b[216:])
	lvd.LogicalVolumeContentsUse = NewExtentLong(b[248:])
	lvd.MapTableLength = rlU32(b[264:])
	lvd.NumberOfPartitionMaps = rlU32(b[268:])
	lvd.ImplementationIdentifier = NewEntityID(b[272:])
	lvd.ImplementationUse = b[304:432]
	lvd.IntegritySequenceExtent = NewExtent(b[432:])

	lvd.PartitionMaps = make([]PartitionMap, lvd.NumberOfPartitionMaps)
	for i := range lvd.PartitionMaps {
		offset := 440 + i*6
		if offset+6 <= len(b) {
			lvd.PartitionMaps[i].fromBytes(b[offset:])
		}
	}

	return lvd, nil
}

// FileSetDescriptor is ECMA-167 §14.1.
type FileSetDescriptor struct {
	Descriptor              Descriptor
	RecordingDateTime       time.Time
	InterchangeLevel        uint16
	MaximumInterchangeLevel uint16
	CharacterSetList        uint32
	MaximumCharacterSetList uint32
	FileSetNumber           uint32
	FileSetDescriptorNumber uint32
	LogicalVolumeIdentifier string
	FileSetIdentifier       string
	CopyrightFileIdentifier string
	AbstractFileIdentifier  string
	RootDirectoryICB        ExtentLong
	DomainIdentifier        EntityID
	NextExtent              ExtentLong
}

func newFileSetDescriptor(b []byte) (*FileSetDescriptor, error) {
	if len(b) < 464 {
		return nil, fmt.Errorf("file set descriptor: %w", ErrBufferTooShort)
	}

	fsd := &FileSetDescriptor{}

	err := fsd.Descriptor.fromBytes(b)
	if err != nil {
		return nil, err
	}

	fsd.RecordingDateTime = rTimestamp(b[16:])
	fsd.InterchangeLevel = rlU16(b[28:])
	fsd.MaximumInterchangeLevel = rlU16(b[30:])
	fsd.CharacterSetList = rlU32(b[32:])
	fsd.MaximumCharacterSetList = rlU32(b[36:])
	fsd.FileSetNumber = rlU32(b[40:])
	fsd.FileSetDescriptorNumber = rlU32(b[44:])
	fsd.LogicalVolumeIdentifier = rDstring(b[112:], 128)
	fsd.FileSetIdentifier = rDstring(b[304:], 32)
	fsd.CopyrightFileIdentifier = rDstring(b[336:], 32)
	fsd.AbstractFileIdentifier = rDstring(b[368:], 32)
	fsd.RootDirectoryICB = NewExtentLong(b[400:])
	fsd.DomainIdentifier = NewEntityID(b[416:])
	fsd.NextExtent = NewExtentLong(b[448:])

	return fsd, nil
}

// FileIdentifierDescriptor is ECMA-167 §14.4.
type FileIdentifierDescriptor struct {
	Descriptor                Descriptor
	FileVersionNumber         uint16
	FileCharacteristics       uint8
	LengthOfFileIdentifier    uint8
	ICB                       ExtentLong
	LengthOfImplementationUse uint16
	ImplementationUse         EntityID
	FileIdentifier            string
}

// Len returns the padded length of this FID on disk.
func (fid *FileIdentifierDescriptor) Len() uint64 {
	l := 38 + uint64(fid.LengthOfImplementationUse) + uint64(fid.LengthOfFileIdentifier)
	return 4 * ((l + 3) / 4) // round up to 4-byte boundary
}

func newFileIdentifierDescriptor(b []byte) (*FileIdentifierDescriptor, error) {
	if len(b) < 38 {
		return nil, fmt.Errorf("file identifier descriptor: %w", ErrBufferTooShort)
	}

	fid := &FileIdentifierDescriptor{}

	err := fid.Descriptor.fromBytes(b)
	if err != nil {
		return nil, err
	}

	fid.FileVersionNumber = rlU16(b[16:])
	fid.FileCharacteristics = rU8(b[18:])
	fid.LengthOfFileIdentifier = rU8(b[19:])
	fid.ICB = NewExtentLong(b[20:])
	fid.LengthOfImplementationUse = rlU16(b[36:])

	if fid.LengthOfImplementationUse >= 32 && len(b) >= 38+32 {
		fid.ImplementationUse = NewEntityID(b[38:])
	}

	// Use uint32 arithmetic to avoid uint8 truncation on identStart.
	identStart := uint32(38) + uint32(fid.LengthOfImplementationUse)
	identEnd := identStart + uint32(fid.LengthOfFileIdentifier)

	if identEnd <= uint32(len(b)) && fid.LengthOfFileIdentifier > 0 {
		fid.FileIdentifier = rDcharacters(b[identStart:identEnd])
	}

	return fid, nil
}

// FileEntry is ECMA-167 §14.9.
type FileEntry struct {
	Descriptor                    Descriptor
	ICBTag                        *ICBTag
	UID                           uint32
	GID                           uint32
	Permissions                   uint32
	FileLinkCount                 uint16
	RecordFormat                  uint8
	RecordDisplayAttributes       uint8
	RecordLength                  uint32
	InformationLength             uint64
	LogicalBlocksRecorded         uint64
	AccessTime                    time.Time
	ModificationTime              time.Time
	AttributeTime                 time.Time
	Checkpoint                    uint32
	ExtendedAttributeICB          ExtentLong
	ImplementationIdentifier      EntityID
	UniqueID                      uint64
	LengthOfExtendedAttributes    uint32
	LengthOfAllocationDescriptors uint32
	ExtendedAttributes            []byte
	AllocationDescriptors         []Extent
}

func newFileEntry(b []byte) (*FileEntry, error) {
	if len(b) < 176 {
		return nil, fmt.Errorf("file entry: %w", ErrBufferTooShort)
	}

	fe := &FileEntry{}

	err := fe.Descriptor.fromBytes(b)
	if err != nil {
		return nil, err
	}

	fe.ICBTag = NewICBTag(b[16:])
	fe.UID = rlU32(b[36:])
	fe.GID = rlU32(b[40:])
	fe.Permissions = rlU32(b[44:])
	fe.FileLinkCount = rlU16(b[48:])
	fe.RecordFormat = rU8(b[50:])
	fe.RecordDisplayAttributes = rU8(b[51:])
	fe.RecordLength = rlU32(b[52:])
	fe.InformationLength = rlU64(b[56:])
	fe.LogicalBlocksRecorded = rlU64(b[64:])
	fe.AccessTime = rTimestamp(b[72:])
	fe.ModificationTime = rTimestamp(b[84:])
	fe.AttributeTime = rTimestamp(b[96:])
	fe.Checkpoint = rlU32(b[108:])
	fe.ExtendedAttributeICB = NewExtentLong(b[112:])
	fe.ImplementationIdentifier = NewEntityID(b[128:])
	fe.UniqueID = rlU64(b[160:])
	fe.LengthOfExtendedAttributes = rlU32(b[168:])
	fe.LengthOfAllocationDescriptors = rlU32(b[172:])

	allocDescStart := 176 + fe.LengthOfExtendedAttributes
	if uint32(len(b)) >= allocDescStart {
		fe.ExtendedAttributes = b[176:allocDescStart]
	}

	numDescriptors := fe.LengthOfAllocationDescriptors / 8
	fe.AllocationDescriptors = make([]Extent, numDescriptors)

	for i := range fe.AllocationDescriptors {
		offset := allocDescStart + uint32(i)*8
		if offset+8 <= uint32(len(b)) {
			fe.AllocationDescriptors[i] = NewExtent(b[offset:])
		}
	}

	return fe, nil
}
