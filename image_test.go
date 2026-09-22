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

	extADBytes = 20
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
	assertContents(t, files["note.txt"], "hello")
	assertContents(t, files["cont.txt"], "cont")
	assertContents(t, files["span.txt"], "span")
	assertContents(t, files["xcont.txt"], "xcon")
	assertContents(t, files["xhole.txt"], "AB\x00\x00CD")
	assertOffset(t, files["cont.txt"], int64((partStart+7)*sectorSize))
	assertOpenError(t, files["over.txt"])
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

func TestVirtualPartitionRead(t *testing.T) {
	t.Parallel()

	image, err := udf.NewUdfFromReader(bytes.NewReader(buildMetadataVolume(0x0260, 0, 0xFFFFFFFF, true)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	files := readRoot(t, image)
	assertContents(t, files["hello.txt"], "hello")

	_, err = files["vat.bin"].NewReader()
	if !errors.Is(err, udf.ErrUnsupportedPartition) {
		t.Fatalf("vat.bin: %v", err)
	}
}

func TestBadFileSetTag(t *testing.T) {
	t.Parallel()

	img := buildPhysicalImage()
	binary.LittleEndian.PutUint16(img[partStart*sectorSize:], 0x105)

	_, err := udf.NewUdfFromReader(bytes.NewReader(img))
	if err == nil || errors.Is(err, udf.ErrNoFileEntry) {
		t.Fatalf("error = %v, want a file set tag error", err)
	}
}

func TestWrappedFileEntryLength(t *testing.T) {
	t.Parallel()

	img := buildPhysicalImage()
	binary.LittleEndian.PutUint32(img[(partStart+3)*sectorSize+168:], 0xFFFFFFF0)

	image := openImage(t, img)

	_, err := readRoot(t, image)["note.txt"].NewReader()
	if err == nil {
		t.Fatal("wrapped extended-attribute length opened")
	}
}

func TestShortFileIsUnexpectedEOF(t *testing.T) {
	t.Parallel()

	img := buildPhysicalImage()
	binary.LittleEndian.PutUint64(img[(partStart+3)*sectorSize+56:], 100)

	reader := openNamed(t, img, "note.txt")
	buf, err := io.ReadAll(reader)

	if string(buf) != "hello" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read %q err=%v, want hello and unexpected EOF", buf, err)
	}
}

func TestTruncatedImageKeepsPartialRead(t *testing.T) {
	t.Parallel()

	img := buildPhysicalImage()
	img = img[:(partStart+4)*sectorSize+2]

	reader := openNamed(t, img, "note.txt")
	buf, err := io.ReadAll(reader)

	if string(buf) != "he" || err == nil {
		t.Fatalf("read %q err=%v, want partial hello", buf, err)
	}
}

func TestHugeAllocationExtent(t *testing.T) {
	t.Parallel()

	img := buildPhysicalImage()
	binary.LittleEndian.PutUint32(img[(partStart+6)*sectorSize+20:], 1<<28)

	image := openImage(t, img)

	_, err := readRoot(t, image)["cont.txt"].NewReader()
	if err == nil {
		t.Fatal("huge allocation extent opened")
	}
}

func TestCompressedExtentRejected(t *testing.T) {
	t.Parallel()

	img := buildMetadataImage(0x0250, 0, 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(img[(partStart+26)*sectorSize+220:], 1)

	image := openImage(t, img)

	_, err := readRoot(t, image)["ext.bin"].NewReader()
	if err == nil {
		t.Fatal("compressed extent opened")
	}
}

func openImage(t *testing.T, img []byte) *udf.Udf {
	t.Helper()

	image, err := udf.NewUdfFromReader(bytes.NewReader(img))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	return image
}

func openNamed(t *testing.T, img []byte, name string) io.Reader {
	t.Helper()

	file := readRoot(t, openImage(t, img))[name]
	if file == nil {
		t.Fatalf("missing %s", name)
	}

	reader, err := file.NewReader()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	return reader
}

func assertMetadataTree(t *testing.T, image *udf.Udf) {
	t.Helper()

	files := readRoot(t, image)
	assertContents(t, files["hello.txt"], "hello")
	assertContents(t, files["split.bin"], "ABCDEFGH")
	assertContents(t, files["embed.dat"], "xyz")
	assertContents(t, files["gap.bin"], "AB\x00\x00CD")
	assertContents(t, files["ext.bin"], "EX")
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

	if reader.Size() != 8 {
		t.Fatalf("split.bin size = %d, want 8", reader.Size())
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

func assertOffset(t *testing.T, file *udf.File, want int64) {
	t.Helper()

	got, err := file.GetFileOffset()
	if err != nil {
		t.Fatalf("offset: %v", err)
	}

	if got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
}

func assertOpenError(t *testing.T, file *udf.File) {
	t.Helper()

	if file == nil {
		t.Fatal("missing file")
	}

	_, err := file.NewReader()
	if err == nil {
		t.Fatalf("%s opened", file.Name())
	}
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
	fids = appendFID(fids, "span.txt", 9, 0, 0)
	fids = appendFID(fids, "over.txt", 8, 0, 0)
	fids = appendFID(fids, "xcont.txt", 12, 0, 0)
	fids = appendFID(fids, "xhole.txt", 15, 0, 0)

	placePart(img, 0, writeFSD(1, 0))
	placePart(img, 1, writeFE(4, uint64(len(fids)), []ext{{length: uint32(len(fids)), lbn: 2}}))
	placePart(img, 2, payloadSector(fids))
	placePart(img, 3, writeFE(5, 5, []ext{{length: 5, lbn: 4}}))
	placePart(img, 4, payloadSector([]byte("hello")))
	placePart(img, 5, writeFE(5, 4, []ext{{length: 32 | extNext, lbn: 6}}))
	placePart(img, 6, writeAED(4, 7))
	placePart(img, 7, payloadSector([]byte("cont")))
	placePart(img, 8, writeFE(5, 4, []ext{{length: 4, lbn: 80}}))
	placePart(img, 9, writeSpanningFE(11))
	placePart(img, 11, payloadSector([]byte("span")))
	placePart(img, 12, writeEFE(5, 2, 4, []ext{{length: 44 | extNext, lbn: 13, part: 0}}))
	placePart(img, 13, writeExtAED(4, 14, 0))
	placePart(img, 14, payloadSector([]byte("xcon")))
	placePart(img, 15, writeEFE(5, 2, 6, []ext{
		{length: 2, lbn: 16, part: 0},
		{length: 2 | extHole, lbn: 0, part: 0},
		{length: 2, lbn: 17, part: 0},
	}))
	placePart(img, 16, payloadSector([]byte("AB")))
	placePart(img, 17, payloadSector([]byte("CD")))

	return img
}

func buildMetadataImage(rev uint16, fileLoc, mirrorLoc uint32) []byte {
	return buildMetadataVolume(rev, fileLoc, mirrorLoc, false)
}

func buildMetadataVolume(rev uint16, fileLoc, mirrorLoc uint32, withVAT bool) []byte {
	img := make([]byte, imageSectors*sectorSize)

	maps := append(type1Map(), metaMap(fileLoc, mirrorLoc)...)
	if withVAT {
		maps = append(maps, virtualMap()...)
	}

	writeVolume(img, rev, 0, 1, maps)

	root, bdmv := metadataDirectories(withVAT)
	placeMetadata(img, root, bdmv, withVAT)

	return img
}

func metadataDirectories(withVAT bool) ([]byte, []byte) {
	root := appendFID(nil, "", 1, 1, 0x08)
	root = appendFID(root, "hello.txt", 3, 1, 0)
	root = appendFID(root, "split.bin", 4, 1, 0)
	root = appendFID(root, "embed.dat", 5, 1, 0)
	root = appendFID(root, "gap.bin", 6, 1, 0)
	root = appendFID(root, "ext.bin", 10, 1, 0)
	root = appendFID(root, "BDMV", 7, 1, 0x02)

	if withVAT {
		root = appendFID(root, "vat.bin", 11, 1, 0)
	}

	bdmv := appendFID(nil, "", 1, 1, 0x08)
	bdmv = appendFID(bdmv, "index.bdmv", 9, 1, 0)

	return root, bdmv
}

func placeMetadata(img, root, bdmv []byte, withVAT bool) {
	metaFE := writeEFE(250, 0, 12*uint64(sectorSize), []ext{
		{length: sectorSize, lbn: 1},
		{length: 9 * sectorSize, lbn: 10},
		{length: 2 * sectorSize, lbn: 26},
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
	placePart(img, 26, writeEFE(5, 2, 2, []ext{{length: 2, lbn: 28, part: 0}}))
	placePart(img, 28, payloadSector([]byte("EX")))
	placePart(img, 30, payloadSector([]byte("EFGH")))

	if withVAT {
		placePart(img, 27, writeEFE(5, 1, 3, []ext{{length: 3, lbn: 0, part: 2}}))
	}
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
	binary.LittleEndian.PutUint32(sec[268:], countMaps(maps))

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

func writeFE(fileType byte, size uint64, ads []ext) []byte {
	sec := writeTagSector(0x105)
	putICB(sec, fileType, 0)
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

	stride := adStride(flags)
	binary.LittleEndian.PutUint32(sec[212:], uint32(stride*len(ads)))

	for i, ad := range ads {
		off := 216 + stride*i
		putAD(sec[off:], stride, ad)
	}

	return sec
}

func adStride(flags uint16) int {
	switch flags & 7 {
	case 1:
		return 16
	case 2:
		return extADBytes
	default:
		return 8
	}
}

func putAD(b []byte, stride int, ad ext) {
	switch stride {
	case 16:
		putLongAD(b, ad.length, ad.lbn, ad.part)
	case extADBytes:
		putExtAD(b, ad.length, ad.lbn, ad.part)
	default:
		putShortAD(b, ad.length, ad.lbn)
	}
}

func writeSpanningFE(dataLBN uint32) []byte {
	const lea = sectorSize

	buf := make([]byte, 176+lea+8)
	binary.LittleEndian.PutUint16(buf[0:], 0x105)
	binary.LittleEndian.PutUint16(buf[2:], 3)
	putICB(buf, 5, 0)
	binary.LittleEndian.PutUint64(buf[56:], 4)
	binary.LittleEndian.PutUint32(buf[168:], lea)
	binary.LittleEndian.PutUint32(buf[172:], 8)
	putShortAD(buf[176+lea:], 4, dataLBN)

	return buf
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

func putExtAD(b []byte, length, lbn uint32, part uint16) {
	data := length & 0x3FFFFFFF
	recorded := data
	info := data

	switch length >> 30 {
	case 1, 2:
		recorded = 0
	case 3:
		recorded = 0
		info = 0
	}

	binary.LittleEndian.PutUint32(b[0:], length)
	binary.LittleEndian.PutUint32(b[4:], recorded)
	binary.LittleEndian.PutUint32(b[8:], info)
	binary.LittleEndian.PutUint32(b[12:], lbn)
	binary.LittleEndian.PutUint16(b[16:], part)
}

func writeExtAED(length, lbn uint32, part uint16) []byte {
	sec := writeTagSector(0x102)
	binary.LittleEndian.PutUint32(sec[20:], extADBytes)
	putExtAD(sec[24:], length, lbn, part)

	return sec
}

func countMaps(maps []byte) uint32 {
	var n, off uint32

	for off+2 <= uint32(len(maps)) {
		length := uint32(maps[off+1])
		if length < 2 || off+length > uint32(len(maps)) {
			break
		}

		n++
		off += length
	}

	return n
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

func virtualMap() []byte {
	buf := make([]byte, 64)
	buf[0] = 2
	buf[1] = 64
	copy(buf[5:], "*UDF Virtual Partition")
	binary.LittleEndian.PutUint16(buf[36:], 1)

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
