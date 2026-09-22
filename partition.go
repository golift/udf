package udf

import "fmt"

const (
	partPhysical partKind = 1
	partMeta     partKind = 2

	idMetadata = "*UDF Metadata Partition"
)

type partKind uint8

// partMap is one logical volume partition map, indexed by partition reference number.
type partMap struct {
	kind       partKind
	volSeq     uint16
	number     uint16
	start      uint32
	length     uint32
	metaFile   uint32
	metaMirror uint32
	metaBitmap uint32
	runs       []byteRun
}

func parsePartitionMaps(b []byte, tableLen, count uint32) ([]partMap, []PartitionMap, error) {
	if tableLen > uint32(len(b)) {
		return nil, nil, fmt.Errorf("partition map table: %w", ErrBufferTooShort)
	}

	var (
		maps  []partMap
		type1 []PartitionMap
		off   uint32
	)

	for range count {
		if off+2 > tableLen {
			return nil, nil, fmt.Errorf("partition map: %w", ErrBufferTooShort)
		}

		length := uint32(b[off+1])
		if length < 2 || off+length > tableLen {
			return nil, nil, fmt.Errorf("partition map length %d: %w", length, ErrBufferTooShort)
		}

		pm, err := parseOneMap(b[off : off+length])
		if err != nil {
			return nil, nil, err
		}

		if pm.kind == partPhysical {
			type1 = append(type1, PartitionMap{
				PartitionMapType:     b[off],
				PartitionMapLength:   b[off+1],
				VolumeSequenceNumber: pm.volSeq,
				PartitionNumber:      pm.number,
			})
		}

		maps = append(maps, pm)
		off += length
	}

	return maps, type1, nil
}

func parseOneMap(b []byte) (partMap, error) {
	switch b[0] {
	case 1:
		if len(b) < 6 {
			return partMap{}, fmt.Errorf("type 1 partition map: %w", ErrBufferTooShort)
		}

		var decoded PartitionMap

		decoded.fromBytes(b)

		return partMap{
			kind:   partPhysical,
			volSeq: decoded.VolumeSequenceNumber,
			number: decoded.PartitionNumber,
		}, nil
	case 2:
		return parseType2Map(b)
	default:
		return partMap{}, fmt.Errorf("partition map type %d: %w", b[0], ErrUnsupportedPartition)
	}
}

func parseType2Map(b []byte) (partMap, error) {
	if len(b) < 36 {
		return partMap{}, fmt.Errorf("type 2 partition map: %w", ErrBufferTooShort)
	}

	name := NewEntityID(b[4:]).Name()
	if name != idMetadata {
		if name == "" {
			name = "type 2"
		}

		return partMap{}, fmt.Errorf("%s: %w", name, ErrUnsupportedPartition)
	}

	if len(b) < 64 {
		return partMap{}, fmt.Errorf("metadata partition map: %w", ErrBufferTooShort)
	}

	return partMap{
		kind:       partMeta,
		volSeq:     rlU16(b[36:]),
		number:     rlU16(b[38:]),
		metaFile:   rlU32(b[40:]),
		metaMirror: rlU32(b[44:]),
		metaBitmap: rlU32(b[48:]),
	}, nil
}

func (u *Udf) bindPartitions() error {
	u.partMaps = append([]partMap(nil), u.lvd.partMaps...)

	for i := range u.partMaps {
		if u.partMaps[i].kind != partPhysical {
			continue
		}

		pd := u.partitionDesc(u.partMaps[i].number)
		if pd == nil {
			return fmt.Errorf("partition %d: %w", u.partMaps[i].number, ErrNoPartition)
		}

		u.partMaps[i].start = pd.PartitionStartingLocation
		u.partMaps[i].length = pd.PartitionLength
	}

	for i := range u.partMaps {
		if u.partMaps[i].kind != partMeta {
			continue
		}

		err := u.loadMetadata(i)
		if err != nil {
			return err
		}
	}

	return nil
}

func (u *Udf) partitionDesc(number uint16) *PartitionDescriptor {
	var found *PartitionDescriptor

	for _, pd := range u.pds {
		if pd.PartitionNumber == number {
			found = pd
		}
	}

	return found
}

func (u *Udf) physicalRef(number uint16) (uint16, bool) {
	for i := range u.partMaps {
		if u.partMaps[i].kind == partPhysical && u.partMaps[i].number == number {
			return uint16(i), true
		}
	}

	return 0, false
}

func (u *Udf) loadMetadata(idx int) error {
	meta := &u.partMaps[idx]

	phys, ok := u.physicalRef(meta.number)
	if !ok {
		return fmt.Errorf("metadata partition %d: %w", meta.number, ErrNoPartition)
	}

	fe, err := u.openMetadataFile(phys, meta.metaFile, meta.metaMirror)
	if err != nil {
		return err
	}

	runs, err := u.buildRuns(fe)
	if err != nil {
		return fmt.Errorf("metadata file extents: %w", err)
	}

	meta.runs = runs

	return nil
}

func (u *Udf) openMetadataFile(phys uint16, fileLoc, mirrorLoc uint32) (*FileEntry, error) {
	fe, err := u.metadataFileAt(phys, fileLoc)
	if err == nil {
		return fe, nil
	}

	mirror, mirrorErr := u.metadataFileAt(phys, mirrorLoc)
	if mirrorErr == nil {
		return mirror, nil
	}

	if fileLoc == locNotRecorded {
		err = mirrorErr
	}

	return nil, fmt.Errorf("metadata file: %w", err)
}

func (u *Udf) metadataFileAt(part uint16, lbn uint32) (*FileEntry, error) {
	if lbn == locNotRecorded {
		return nil, ErrNoFileEntry
	}

	return u.readFileEntry(part, lbn)
}

// translateLogical maps a byte offset inside a partition to an image offset.
// n is the number of bytes that are physically contiguous from that offset,
// capped at max.
func (u *Udf) translateLogical(part uint16, logicalOff, limit int64) (int64, int64, error) {
	if limit <= 0 {
		return 0, 0, ErrShortRead
	}

	if int(part) >= len(u.partMaps) {
		return 0, 0, fmt.Errorf("partition reference %d: %w", part, ErrNoPartition)
	}

	m := &u.partMaps[part]
	if m.kind == partPhysical {
		return int64(m.start)*int64(u.blockSize) + logicalOff, limit, nil
	}

	if len(m.runs) == 0 {
		return 0, 0, fmt.Errorf("metadata partition %d is not loaded: %w", part, ErrNoPartition)
	}

	for _, run := range m.runs {
		end := run.fileOff + run.length
		if logicalOff < run.fileOff || logicalOff >= end {
			continue
		}

		n := min(end-logicalOff, limit)

		if run.hole {
			return 0, 0, fmt.Errorf("metadata block %d is unrecorded: %w",
				logicalOff/int64(u.blockSize), ErrNoAllocDescriptors)
		}

		return run.phys + (logicalOff - run.fileOff), n, nil
	}

	return 0, 0, fmt.Errorf("metadata block %d: %w", logicalOff/int64(u.blockSize), ErrNoAllocDescriptors)
}
