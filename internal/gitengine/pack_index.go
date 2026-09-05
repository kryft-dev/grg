package gitengine

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
)

// idxV2Magic is the 4-byte header magic of a .idx v2 file ("\xfftOc").
const idxV2Magic = "\xff\x74\x4f\x63"

// ErrIdxInvalid indicates an invalid or unsupported .idx file.
var ErrIdxInvalid = errors.New("invalid pack index file")

// PackIndex represents a parsed Git packfile .idx v2 file.
type PackIndex struct {
	fanout    [256]uint32
	shaTable  []byte
	offsets   []uint32
	offsets64 []uint64
	count     int
	hashLen   int
}

// OpenPackIndex opens and parses a packfile .idx v2 file from disk.
func OpenPackIndex(path string) (*PackIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read pack index %s: %w", path, err)
	}

	return ParsePackIndex(data)
}

// ParsePackIndex parses the raw bytes of a .idx v2 file.
func ParsePackIndex(data []byte) (*PackIndex, error) {
	if len(data) < 8+1024+40 {
		return nil, fmt.Errorf("%w: file too short (%d bytes)", ErrIdxInvalid, len(data))
	}

	// 1. Verify magic and version
	if string(data[:4]) != idxV2Magic {
		return nil, fmt.Errorf("%w: invalid header magic %x", ErrIdxInvalid, data[:4])
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != 2 {
		return nil, fmt.Errorf("%w: unsupported index version %d (only v2 is supported)", ErrIdxInvalid, version)
	}

	idx := &PackIndex{
		hashLen: 20, // Default SHA-1 (standard Git)
	}

	// 2. Read 256-entry fanout table
	pos := 8
	for i := 0; i < 256; i++ {
		idx.fanout[i] = binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4
	}
	for i := 1; i < 256; i++ {
		if idx.fanout[i] < idx.fanout[i-1] {
			return nil, fmt.Errorf("%w: non-monotonic fanout table at %d", ErrIdxInvalid, i)
		}
	}
	idx.count = int(idx.fanout[255])
	if idx.count < 0 {
		return nil, fmt.Errorf("%w: negative object count %d in fanout", ErrIdxInvalid, idx.count)
	}

	// Validate data length can hold: 8 (header) + 1024 (fanout) + count*(hashLen + 4 + 4) + 40 (trailer)
	minRequiredLen := int64(8+1024) + int64(idx.count)*int64(idx.hashLen+4+4) + 40
	if int64(len(data)) < minRequiredLen {
		return nil, fmt.Errorf("%w: truncated pack index for %d objects (need %d bytes, got %d)", ErrIdxInvalid, idx.count, minRequiredLen, len(data))
	}

	if idx.count == 0 {
		return idx, nil
	}

	// 3. Object names (SHAs)
	shaBytesLen := idx.count * idx.hashLen
	if pos+shaBytesLen > len(data) {
		return nil, fmt.Errorf("%w: truncated SHA table", ErrIdxInvalid)
	}
	idx.shaTable = data[pos : pos+shaBytesLen]
	pos += shaBytesLen

	// 4. CRC32 table (count * 4 bytes)
	crcBytesLen := idx.count * 4
	if pos+crcBytesLen > len(data) {
		return nil, fmt.Errorf("%w: truncated CRC table", ErrIdxInvalid)
	}
	pos += crcBytesLen

	// 5. 4-byte offset table (count * 4 bytes)
	offsetBytesLen := idx.count * 4
	if pos+offsetBytesLen > len(data) {
		return nil, fmt.Errorf("%w: truncated offset table", ErrIdxInvalid)
	}
	idx.offsets = make([]uint32, idx.count)
	for i := 0; i < idx.count; i++ {
		idx.offsets[i] = binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4
	}

	// 6. Optional 8-byte offset table for large packfiles (>2GB)
	// Remaining bytes before 40-byte trailer checksums are 8-byte offsets
	remBytes := len(data) - pos - 40
	if remBytes > 0 {
		if remBytes%8 != 0 {
			return nil, fmt.Errorf("%w: malformed 64-bit offset table length %d", ErrIdxInvalid, remBytes)
		}
		num64 := remBytes / 8
		idx.offsets64 = make([]uint64, num64)
		for i := 0; i < num64; i++ {
			idx.offsets64[i] = binary.BigEndian.Uint64(data[pos : pos+8])
			pos += 8
		}
	}

	return idx, nil
}

// FindOffset looks up the packfile byte offset for the given OID.
func (idx *PackIndex) FindOffset(oid string) (int64, error) {
	if idx.count == 0 {
		return -1, ErrObjectNotFound
	}

	if len(oid) != idx.hashLen*2 {
		return -1, fmt.Errorf("invalid OID length %d for pack index lookup", len(oid))
	}

	var shaBuf [32]byte
	sha := shaBuf[:idx.hashLen]
	if _, err := hex.Decode(sha, []byte(oid)); err != nil {
		return -1, fmt.Errorf("invalid OID %q for pack index lookup: %w", oid, err)
	}

	firstByte := int(sha[0])
	var low int
	if firstByte > 0 {
		low = int(idx.fanout[firstByte-1])
	}
	high := int(idx.fanout[firstByte])

	// Binary search in the SHA table slice
	for low < high {
		mid := int(uint(low+high) >> 1)
		midSHA := idx.shaTable[mid*idx.hashLen : (mid+1)*idx.hashLen]
		cmp := bytes.Compare(midSHA, sha)
		if cmp < 0 {
			low = mid + 1
		} else if cmp > 0 {
			high = mid
		} else {
			// Found matching SHA at mid
			rawOffset := idx.offsets[mid]
			if (rawOffset & 0x80000000) == 0 {
				return int64(rawOffset), nil
			}

			// 64-bit offset lookup
			idx64 := int(rawOffset & 0x7fffffff)
			if idx64 >= len(idx.offsets64) {
				return -1, fmt.Errorf("%w: invalid 64-bit offset index %d", ErrIdxInvalid, idx64)
			}
			off64 := idx.offsets64[idx64]
			if off64 > math.MaxInt64 {
				return -1, fmt.Errorf("%w: 64-bit offset %d exceeds max int64", ErrIdxInvalid, off64)
			}
			return int64(off64), nil
		}
	}

	return -1, ErrObjectNotFound
}

// HasObject checks if the OID exists in this index.
func (idx *PackIndex) HasObject(oid string) bool {
	_, err := idx.FindOffset(oid)
	return err == nil
}

// Count returns the number of objects indexed in this pack.
func (idx *PackIndex) Count() int {
	return idx.count
}
