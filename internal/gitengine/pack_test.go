package gitengine

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/klauspost/compress/zlib"
)

type testPackObj struct {
	sha     [20]byte
	objType ObjectType
	raw     []byte // uncompressed payload for regular objects or delta instructions
	isOfs   bool
	baseOff int64
}

func buildTestPackAndIdx(objects []testPackObj) (packData []byte, idxData []byte) {
	var packBuf bytes.Buffer

	// 1. Pack header: PACK + version(2) + count
	packBuf.WriteString("PACK")
	_ = binary.Write(&packBuf, binary.BigEndian, uint32(2))
	_ = binary.Write(&packBuf, binary.BigEndian, uint32(len(objects)))

	type recordedObj struct {
		sha    [20]byte
		offset int64
		crc    uint32
	}
	recorded := make([]recordedObj, len(objects))

	for i, obj := range objects {
		offset := int64(packBuf.Len())
		startOff := packBuf.Len()

		// Write header
		size := int64(len(obj.raw))
		b0 := byte((obj.objType & 7) << 4)
		b0 |= byte(size & 0x0f)
		size >>= 4
		if size > 0 {
			b0 |= 0x80
		}
		packBuf.WriteByte(b0)
		for size > 0 {
			b := byte(size & 0x7f)
			size >>= 7
			if size > 0 {
				b |= 0x80
			}
			packBuf.WriteByte(b)
		}

		if obj.isOfs {
			// Write ofs delta offset
			deltaOff := offset - obj.baseOff
			var enc []byte
			enc = append(enc, byte(deltaOff&0x7f))
			deltaOff >>= 7
			for deltaOff > 0 {
				deltaOff--
				enc = append(enc, byte(0x80|(deltaOff&0x7f)))
				deltaOff >>= 7
			}
			// Reverse
			for j := len(enc) - 1; j >= 0; j-- {
				packBuf.WriteByte(enc[j])
			}
		}

		// Write zlib compressed payload
		zw := zlib.NewWriter(&packBuf)
		_, _ = zw.Write(obj.raw)
		_ = zw.Close()

		endOff := packBuf.Len()
		crc := crc32.ChecksumIEEE(packBuf.Bytes()[startOff:endOff])

		recorded[i] = recordedObj{
			sha:    obj.sha,
			offset: offset,
			crc:    crc,
		}
	}

	// Packfile SHA-1 trailer
	packSHA := sha1.Sum(packBuf.Bytes())
	packBuf.Write(packSHA[:])
	packData = packBuf.Bytes()

	// 2. Build .idx v2
	// Sort recorded objects by SHA
	sort.Slice(recorded, func(i, j int) bool {
		return bytes.Compare(recorded[i].sha[:], recorded[j].sha[:]) < 0
	})

	var idxBuf bytes.Buffer
	idxBuf.Write(idxV2Magic)
	_ = binary.Write(&idxBuf, binary.BigEndian, uint32(2))

	// Fanout table
	var fanout [256]uint32
	for _, rec := range recorded {
		b := rec.sha[0]
		fanout[b]++
	}
	// Cumulative
	for i := 1; i < 256; i++ {
		fanout[i] += fanout[i-1]
	}
	for i := 0; i < 256; i++ {
		_ = binary.Write(&idxBuf, binary.BigEndian, fanout[i])
	}

	// SHA table
	for _, rec := range recorded {
		idxBuf.Write(rec.sha[:])
	}

	// CRC table
	for _, rec := range recorded {
		_ = binary.Write(&idxBuf, binary.BigEndian, rec.crc)
	}

	// Offset table
	for _, rec := range recorded {
		_ = binary.Write(&idxBuf, binary.BigEndian, uint32(rec.offset))
	}

	// Checksums
	idxBuf.Write(packSHA[:]) // packfile checksum
	idxSHA := sha1.Sum(idxBuf.Bytes())
	idxBuf.Write(idxSHA[:]) // idx checksum
	idxData = idxBuf.Bytes()

	return packData, idxData
}

func TestPackReaderAndOfsDelta(t *testing.T) {
	// Base blob
	baseContent := []byte("Initial Base Line In Repo.")
	baseHeader := append([]byte("blob 26\x00"), baseContent...)
	baseSHA := sha1.Sum(baseHeader)

	// Delta blob: replaces "Base" with "Super" -> "Initial Super Line In Repo."
	targetContent := []byte("Initial Super Line In Repo.")
	targetHeader := append([]byte("blob 27\x00"), targetContent...)
	targetSHA := sha1.Sum(targetHeader)

	var deltaPayload []byte
	deltaPayload = append(deltaPayload, encodeLEB128(len(baseContent))...)
	deltaPayload = append(deltaPayload, encodeLEB128(len(targetContent))...)
	// Copy "Initial " (8 bytes)
	deltaPayload = append(deltaPayload, 0x80|0x10|0x01, 0, 8)
	// Insert "Super" (5 bytes)
	deltaPayload = append(deltaPayload, 5)
	deltaPayload = append(deltaPayload, []byte("Super")...)
	// Copy " Line In Repo." (14 bytes starting at offset 12 in base)
	deltaPayload = append(deltaPayload, 0x80|0x10|0x01, 12, 14)

	objs := []testPackObj{
		{
			sha:     baseSHA,
			objType: TypeBlob,
			raw:     baseContent,
		},
		{
			sha:     targetSHA,
			objType: TypeOfsDelta,
			raw:     deltaPayload,
			isOfs:   true,
			baseOff: 12, // Base object begins right after 12-byte header
		},
	}

	packData, idxData := buildTestPackAndIdx(objs)

	tmpDir := t.TempDir()
	packDir := filepath.Join(tmpDir, "objects", "pack")
	if err := os.MkdirAll(packDir, 0755); err != nil {
		t.Fatalf("failed to create pack dir: %v", err)
	}

	packPath := filepath.Join(packDir, "pack-test.pack")
	idxPath := filepath.Join(packDir, "pack-test.idx")
	if err := os.WriteFile(packPath, packData, 0644); err != nil {
		t.Fatalf("failed to write pack: %v", err)
	}
	if err := os.WriteFile(idxPath, idxData, 0644); err != nil {
		t.Fatalf("failed to write idx: %v", err)
	}

	pr, err := OpenPackfile(packPath, idxPath)
	if err != nil {
		t.Fatalf("OpenPackfile failed: %v", err)
	}
	defer pr.Close()

	baseOID := hex.EncodeToString(baseSHA[:])
	targetOID := hex.EncodeToString(targetSHA[:])

	// 1. Check HasObject
	if !pr.HasObject(baseOID) {
		t.Fatalf("expected HasObject(%s) = true", baseOID)
	}
	if !pr.HasObject(targetOID) {
		t.Fatalf("expected HasObject(%s) = true", targetOID)
	}

	// 2. Read base object
	baseObj, err := pr.ReadObject(baseOID)
	if err != nil {
		t.Fatalf("ReadObject base failed: %v", err)
	}
	if baseObj.Type != TypeBlob {
		t.Errorf("expected TypeBlob, got %v", baseObj.Type)
	}
	if !bytes.Equal(baseObj.Data, baseContent) {
		t.Errorf("expected %q, got %q", baseContent, baseObj.Data)
	}

	// 3. Read delta object (resolving ofs_delta)
	targetObj, err := pr.ReadObject(targetOID)
	if err != nil {
		t.Fatalf("ReadObject delta failed: %v", err)
	}
	if targetObj.Type != TypeBlob {
		t.Errorf("expected TypeBlob, got %v", targetObj.Type)
	}
	if !bytes.Equal(targetObj.Data, targetContent) {
		t.Errorf("expected %q, got %q", targetContent, targetObj.Data)
	}

	// 4. Test RepositoryReader
	repoInfo := &RepoInfo{
		CommonGitDir: tmpDir,
	}
	repoReader, err := NewRepositoryReader(repoInfo)
	if err != nil {
		t.Fatalf("NewRepositoryReader failed: %v", err)
	}
	defer repoReader.Close()

	if !repoReader.HasObject(targetOID) {
		t.Errorf("repoReader.HasObject(%s) should be true", targetOID)
	}
	objFromRepo, err := repoReader.ReadObject(targetOID)
	if err != nil {
		t.Fatalf("repoReader.ReadObject failed: %v", err)
	}
	if !bytes.Equal(objFromRepo.Data, targetContent) {
		t.Errorf("repoReader returned wrong data: %q", objFromRepo.Data)
	}
}
