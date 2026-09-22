package udf_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"golift.io/udf"
)

const (
	sectorSize   = 2048
	partStart    = 400
	vdsSector    = 32
	imageSectors = 512

	extNext uint32 = 3 << 30
	extHole uint32 = 2 << 30
)

func TestPhysicalImage(t *testing.T) {
	t.Parallel()

	image, err := udf.NewUdfFromReader(bytes.NewReader(buildPhysicalImage()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if image.Revision() != 0x0201 {
		t.Fatalf("revision = %#x, want 0x0201", image.Revision())
	}

	files := readRoot(t, image)
	if got := readFile(t, files["note.txt"]); got != "hello" {
		t.Fatalf("note.txt = %q", got)
	}

	if got := readFile(t, files["cont.txt"]); got != "cont" {
		t.Fatalf("cont.txt = %q, want continuation extent", got)
	}
}

func TestMetadataPartition(t *testing.T) {
	t.Parallel()

	for _, rev := range []uint16{0x0250, 0x0260} {
		t.Run(revisionName(rev), func(t *testing.T) {
			t.Parallel()

			image, err := udf.NewUdfFromReader(bytes.NewReader(buildMetadataImage(rev, 0, 0xFFFFFFFF)))
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			if image.Revision() != rev {
				t.Fatalf("revision = %#x, want %#x", image.Revision(), rev)
			}

			assertMetadataTree(t, image)
		})
	}
}

func TestMetadataMirror(t *testing.T) {
	t.Parallel()

	// Primary metadata file location is "not recorded"; the mirror holds the file.
	image, err := udf.NewUdfFromReader(bytes.NewReader(buildMetadataImage(0x0260, 0xFFFFFFFF, 0)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	assertMetadataTree(t, image)
}

func TestVirtualPartitionRejected(t *testing.T) {
	t.Parallel()

	img := buildMetadataImage(0x0250, 0, 0xFFFFFFFF)
	ident := 34*sectorSize + 440 + 6 + 4 + 1

	for i := range 23 {
		img[ident+i] = 0
	}

	copy(img[ident:], "*UDF Virtual Partition")

	_, err := udf.NewUdfFromReader(bytes.NewReader(img))
	if !errors.Is(err, udf.ErrUnsupportedPartition) {
		t.Fatalf("error = %v, want unsupported partition", err)
	}
}

func assertMetadataTree(t *testing.T, image *udf.Udf) {
	t.Helper()

	files := readRoot(t, image)
	assertContents(t, files["hello.txt"], "hello")
	assertContents(t, files["split.bin"], "ABCDEFGH")
	assertContents(t, files["embed.dat"], "xyz")
	assertContents(t, files["gap.bin"], "AB\x00\x00CD")
	assertBDMV(t, files["BDMV"])
	assertSplitSeek(t, files["split.bin"])
}

func assertContents(t *testing.T, file *udf.File, want string) {
	t.Helper()

	if got := readFile(t, file); got != want {
		t.Fatalf("%s = %q, want %q", file.Name(), got, want)
	}
}

func assertBDMV(t *testing.T, dir *udf.File) {
	t.Helper()

	if dir == nil || !dir.IsDir() {
		t.Fatal("BDMV directory missing")
	}

	children, err := dir.ReadDir()
	if err != nil {
		t.Fatalf("BDMV: %v", err)
	}

	var index *udf.File

	for i := range children {
		if children[i].Name() == "index.bdmv" {
			index = &children[i]
		}
	}

	if index == nil {
		t.Fatal("index.bdmv missing")
	}

	assertContents(t, index, "BD")
}

func assertSplitSeek(t *testing.T, file *udf.File) {
	t.Helper()

	reader, err := file.NewReader()
	if err != nil {
		t.Fatal(err)
	}

	_, err = reader.Seek(4, io.SeekStart)
	if err != nil {
		t.Fatal(err)
	}

	tail := make([]byte, 4)

	_, err = io.ReadFull(reader, tail)
	if err != nil {
		t.Fatal(err)
	}

	if string(tail) != "EFGH" {
		t.Fatalf("split.bin seek = %q", tail)
	}
}

func readRoot(t *testing.T, image *udf.Udf) map[string]*udf.File {
	t.Helper()

	entries, err := image.ReadDir(nil)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	files := make(map[string]*udf.File, len(entries))
	for i := range entries {
		files[entries[i].Name()] = &entries[i]
	}

	return files
}

func readFile(t *testing.T, file *udf.File) string {
	t.Helper()

	if file == nil {
		t.Fatal("missing file")
	}

	reader, err := file.NewReader()
	if err != nil {
		t.Fatalf("%s: %v", file.Name(), err)
	}

	buf, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("%s: %v", file.Name(), err)
	}

	return string(buf)
}

func revisionName(rev uint16) string {
	if rev == 0x0260 {
		return "udf-2.60"
	}

	return "udf-2.50"
}

func buildPhysicalImage() []byte {
	img := make([]byte, imageSectors*sectorSize)
	writeVolume(img, 0x0201, 0, 0, type1Map())

	fids := appendFID(nil, "", 1, 0, 0x08)
	fids = appendFID(fids, "note.txt", 3, 0, 0)
	fids = appendFID(fids, "cont.txt", 5, 0, 0)

	placePart(img, 0, writeFSD(1, 0))
	placePart(img, 1, writeFE(4, 0, uint64(len(fids)), []ext{{length: uint32(len(fids)), lbn: 2}}))
	placePart(img, 2, payloadSector(fids))
	placePart(img, 3, writeFE(5, 0, 5, []ext{{length: 5, lbn: 4}}))
	placePart(img, 4, payloadSector([]byte("hello")))
	placePart(img, 5, writeFE(5, 0, 4, []ext{{length: 32 | extNext, lbn: 6}}))
	placePart(img, 6, writeAED(4, 7))
	placePart(img, 7, payloadSector([]byte("cont")))

	return img
}

func buildMetadataImage(rev uint16, fileLoc, mirrorLoc uint32) []byte {
	img := make([]byte, imageSectors*sectorSize)
	writeVolume(img, rev, 0, 1, append(type1Map(), metaMap(fileLoc, mirrorLoc)...))

	root := appendFID(nil, "", 1, 1, 0x08)
	root = appendFID(root, "hello.txt", 3, 1, 0)
	root = appendFID(root, "split.bin", 4, 1, 0)
	root = appendFID(root, "embed.dat", 5, 1, 0)
	root = appendFID(root, "gap.bin", 6, 1, 0)
	root = appendFID(root, "BDMV", 7, 1, 0x02)

	bdmv := appendFID(nil, "", 1, 1, 0x08)
	bdmv = appendFID(bdmv, "index.bdmv", 9, 1, 0)

	// Metadata file body is fragmented: block 0, then blocks 1-9 elsewhere.
	metaFE := writeEFE(250, 0, 10*uint64(sectorSize), []ext{
		{length: sectorSize, lbn: 1},
		{length: 9 * sectorSize, lbn: 10},
	})

	placePart(img, 0, metaFE)
	placePart(img, 1, writeFSD(1, 1))
	placePart(img, 10, writeEFE(4, 0, uint64(len(root)), []ext{{length: uint32(len(root)), lbn: 2}}))
	placePart(img, 11, payloadSector(root))
	placePart(img, 12, writeEFE(5, 1, 5, []ext{{length: 5, lbn: 20, part: 0}}))
	placePart(img, 13, writeEFE(5, 1, 8, []ext{
		{length: 4, lbn: 21, part: 0},
		{length: 4, lbn: 30, part: 0},
	}))
	placePart(img, 14, writeEFE(5, 3, 3, []ext{{embed: []byte("xyz")}}))
	placePart(img, 15, writeEFE(5, 1, 6, []ext{
		{length: 2, lbn: 24, part: 0},
		{length: 2 | extHole, lbn: 0, part: 0},
		{length: 2, lbn: 25, part: 0},
	}))
	placePart(img, 16, writeEFE(4, 0, uint64(len(bdmv)), []ext{{length: uint32(len(bdmv)), lbn: 8}}))
	placePart(img, 17, payloadSector(bdmv))
	placePart(img, 18, writeEFE(5, 1, 2, []ext{{length: 2, lbn: 22, part: 0}}))
	placePart(img, 20, payloadSector([]byte("hello")))
	placePart(img, 21, payloadSector([]byte("ABCD")))
	placePart(img, 22, payloadSector([]byte("BD")))
	placePart(img, 24, payloadSector([]byte("AB")))
	placePart(img, 25, payloadSector([]byte("CD")))
	placePart(img, 30, payloadSector([]byte("EFGH")))

	return img
}

type ext struct {
	length uint32
	lbn    uint32
	part   uint16
	embed  []byte
}

func writeVolume(img []byte, rev uint16, fsdLBN uint32, fsdPart uint16, maps []byte) {
	place(img, vdsSector, writePVD())
	place(img, vdsSector+1, writePD())
	place(img, vdsSector+2, writeLVD(rev, fsdLBN, fsdPart, maps))
	place(img, vdsSector+3, writeTagSector(8))
	place(img, 256, writeAVDP())
}

func writePVD() []byte {
	sec := writeTagSector(1)
	copy(sec[24:], "TEST")
	sec[55] = 4

	return sec
}

func writePD() []byte {
	sec := writeTagSector(5)
	binary.LittleEndian.PutUint16(sec[22:], 0)
	binary.LittleEndian.PutUint32(sec[188:], partStart)
	binary.LittleEndian.PutUint32(sec[192:], 80)

	return sec
}

func writeLVD(rev uint16, fsdLBN uint32, fsdPart uint16, maps []byte) []byte {
	sec := writeTagSector(6)
	binary.LittleEndian.PutUint32(sec[212:], sectorSize)
	copy(sec[217:], "*OSTA UDF Compliant")
	binary.LittleEndian.PutUint16(sec[240:], rev)
	putLongAD(sec[248:], sectorSize, fsdLBN, fsdPart)
	binary.LittleEndian.PutUint32(sec[264:], uint32(len(maps)))
	binary.LittleEndian.PutUint32(sec[268:], 1)

	if len(maps) > 6 {
		binary.LittleEndian.PutUint32(sec[268:], 2)
	}

	copy(sec[440:], maps)

	return sec
}

func writeAVDP() []byte {
	sec := writeTagSector(2)
	putShortAD(sec[16:], 4*sectorSize, vdsSector)

	return sec
}

func writeFSD(rootLBN uint32, rootPart uint16) []byte {
	sec := writeTagSector(0x100)
	putLongAD(sec[400:], sectorSize, rootLBN, rootPart)

	return sec
}

func writeFE(fileType byte, flags uint16, size uint64, ads []ext) []byte {
	sec := writeTagSector(0x105)
	putICB(sec, fileType, flags)
	binary.LittleEndian.PutUint64(sec[56:], size)
	binary.LittleEndian.PutUint32(sec[172:], uint32(8*len(ads)))

	for i, ad := range ads {
		putShortAD(sec[176+8*i:], ad.length, ad.lbn)
	}

	return sec
}

func writeEFE(fileType byte, flags uint16, size uint64, ads []ext) []byte {
	sec := writeTagSector(0x10A)
	putICB(sec, fileType, flags)
	binary.LittleEndian.PutUint64(sec[56:], size)
	binary.LittleEndian.PutUint64(sec[64:], size)

	if flags&7 == 3 {
		binary.LittleEndian.PutUint32(sec[212:], uint32(len(ads[0].embed)))
		copy(sec[216:], ads[0].embed)

		return sec
	}

	stride := 8
	if flags&7 == 1 {
		stride = 16
	}

	binary.LittleEndian.PutUint32(sec[212:], uint32(stride*len(ads)))

	for i, ad := range ads {
		off := 216 + stride*i
		if stride == 16 {
			putLongAD(sec[off:], ad.length, ad.lbn, ad.part)
			continue
		}

		putShortAD(sec[off:], ad.length, ad.lbn)
	}

	return sec
}

func writeAED(length, lbn uint32) []byte {
	sec := writeTagSector(0x102)
	binary.LittleEndian.PutUint32(sec[20:], 8)
	putShortAD(sec[24:], length, lbn)

	return sec
}

func writeTagSector(id uint16) []byte {
	sec := make([]byte, sectorSize)
	binary.LittleEndian.PutUint16(sec[0:], id)
	binary.LittleEndian.PutUint16(sec[2:], 3)

	return sec
}

func putICB(sec []byte, fileType byte, flags uint16) {
	binary.LittleEndian.PutUint16(sec[20:], 4)
	sec[27] = fileType
	binary.LittleEndian.PutUint16(sec[34:], flags)
}

func putShortAD(b []byte, length, lbn uint32) {
	binary.LittleEndian.PutUint32(b[0:], length)
	binary.LittleEndian.PutUint32(b[4:], lbn)
}

func putLongAD(b []byte, length, lbn uint32, part uint16) {
	binary.LittleEndian.PutUint32(b[0:], length)
	binary.LittleEndian.PutUint32(b[4:], lbn)
	binary.LittleEndian.PutUint16(b[8:], part)
}

func type1Map() []byte {
	buf := make([]byte, 6)
	buf[0] = 1
	buf[1] = 6
	binary.LittleEndian.PutUint16(buf[2:], 1)

	return buf
}

func metaMap(fileLoc, mirrorLoc uint32) []byte {
	buf := make([]byte, 64)
	buf[0] = 2
	buf[1] = 64
	copy(buf[5:], "*UDF Metadata Partition")
	binary.LittleEndian.PutUint16(buf[36:], 1)
	binary.LittleEndian.PutUint32(buf[40:], fileLoc)
	binary.LittleEndian.PutUint32(buf[44:], mirrorLoc)
	binary.LittleEndian.PutUint32(buf[48:], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(buf[52:], 32)
	binary.LittleEndian.PutUint16(buf[56:], 1)

	return buf
}

func appendFID(dst []byte, name string, lbn uint32, part uint16, flags byte) []byte {
	identLen := 0
	if name != "" {
		identLen = 1 + len(name)
	}

	padded := (38 + identLen + 3) &^ 3
	rec := make([]byte, padded)
	binary.LittleEndian.PutUint16(rec[0:], 0x101)
	binary.LittleEndian.PutUint16(rec[2:], 3)
	binary.LittleEndian.PutUint16(rec[16:], 1)
	rec[18] = flags
	rec[19] = byte(identLen)
	putLongAD(rec[20:], sectorSize, lbn, part)

	if identLen > 0 {
		rec[38] = 8
		copy(rec[39:], name)
	}

	return append(dst, rec...)
}

func payloadSector(payload []byte) []byte {
	sec := make([]byte, sectorSize)
	copy(sec, payload)

	return sec
}

func place(img []byte, sector int, sec []byte) {
	copy(img[sector*sectorSize:], sec)
}

func placePart(img []byte, lbn int, sec []byte) {
	place(img, partStart+lbn, sec)
}
