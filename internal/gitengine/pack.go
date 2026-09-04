package gitengine

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/klauspost/compress/zlib"
)

var (
	packMagic = []byte{'P', 'A', 'C', 'K'}
	// ErrPackInvalid indicates a malformed or unsupported packfile.
	ErrPackInvalid = errors.New("invalid packfile")
)

const maxDeltaDepth = 50

// PackReader provides thread-safe random read access to a Git .pack file using its .idx index.
type PackReader struct {
	packPath string
	file     *os.File
	fileSize int64
	idx      *PackIndex
	resolver ObjectReader
	zlibPool sync.Pool
}

// OpenPackfile opens a Git .pack file and its associated .idx file.
func OpenPackfile(packPath, idxPath string) (*PackReader, error) {
	idx, err := OpenPackIndex(idxPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open pack index %s: %w", idxPath, err)
	}

	f, err := os.Open(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open packfile %s: %w", packPath, err)
	}

	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to stat packfile %s: %w", packPath, err)
	}

	// Verify pack header: 4 bytes 'PACK', 4 bytes version (2)
	hdr := make([]byte, 12)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w: failed to read pack header: %v", ErrPackInvalid, err)
	}

	if string(hdr[:4]) != string(packMagic) {
		_ = f.Close()
		return nil, fmt.Errorf("%w: invalid pack magic %q", ErrPackInvalid, string(hdr[:4]))
	}

	return &PackReader{
		packPath: packPath,
		file:     f,
		fileSize: fi.Size(),
		idx:      idx,
	}, nil
}

// OpenPackfiles searches for and opens all packfiles in commonGitDir/objects/pack.
func OpenPackfiles(commonGitDir string) ([]*PackReader, error) {
	packDir := filepath.Join(commonGitDir, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var readers []*PackReader
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "pack-") && strings.HasSuffix(name, ".idx") {
			baseName := strings.TrimSuffix(name, ".idx")
			packName := baseName + ".pack"
			idxPath := filepath.Join(packDir, name)
			packPath := filepath.Join(packDir, packName)

			if _, err := os.Stat(packPath); err == nil {
				reader, err := OpenPackfile(packPath, idxPath)
				if err == nil {
					readers = append(readers, reader)
				}
			}
		}
	}
	return readers, nil
}

// SetResolver sets an ObjectReader to resolve external base objects for OBJ_REF_DELTA.
func (p *PackReader) SetResolver(resolver ObjectReader) {
	p.resolver = resolver
}

// HasObject checks if the pack index contains the given OID.
func (p *PackReader) HasObject(oid string) bool {
	return p.idx.HasObject(oid)
}

// ReadObject reads and decompresses the object corresponding to oid.
func (p *PackReader) ReadObject(oid string) (*Object, error) {
	offset, err := p.idx.FindOffset(oid)
	if err != nil {
		return nil, err
	}

	obj, err := p.readObjectAt(offset, 0)
	if err != nil {
		return nil, err
	}
	obj.OID = oid
	return obj, nil
}

// readObjectAt decodes an object at the given packfile offset, recursively resolving deltas.
func (p *PackReader) readObjectAt(offset int64, depth int) (*Object, error) {
	if depth > maxDeltaDepth {
		return nil, fmt.Errorf("%w: delta chain exceeds maximum depth of %d", ErrCorruptObject, maxDeltaDepth)
	}

	if offset < 0 || offset >= p.fileSize {
		return nil, fmt.Errorf("%w: offset %d out of bounds (packfile size %d)", ErrCorruptObject, offset, p.fileSize)
	}

	// 1. Read object header using a stack buffer to eliminate 1-byte allocations and multiple pread syscalls
	var hdrBuf [64]byte
	avail := p.fileSize - offset
	toRead := len(hdrBuf)
	if int64(toRead) > avail {
		toRead = int(avail)
	}
	if toRead == 0 {
		return nil, fmt.Errorf("%w: unexpected EOF reading header at offset %d", ErrCorruptObject, offset)
	}

	n, err := p.file.ReadAt(hdrBuf[:toRead], offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: failed to read object header at %d: %v", ErrCorruptObject, offset, err)
	}
	if n == 0 {
		return nil, fmt.Errorf("%w: empty read at offset %d", ErrCorruptObject, offset)
	}

	pos := 0
	b := hdrBuf[pos]
	pos++

	objType := ObjectType((b >> 4) & 7)
	size := int64(b & 0x0f)
	shift := uint(4)

	for (b & 0x80) != 0 {
		if shift >= 64 {
			return nil, fmt.Errorf("%w: varint shift overflow at offset %d", ErrCorruptObject, offset)
		}
		if pos >= n {
			return nil, fmt.Errorf("%w: truncated object size in header at %d", ErrCorruptObject, offset)
		}
		b = hdrBuf[pos]
		pos++
		size |= int64(b&0x7f) << shift
		shift += 7
	}

	if size < 0 || size > MaxObjectSize {
		return nil, fmt.Errorf("%w: invalid object size %d at offset %d", ErrCorruptObject, size, offset)
	}

	switch objType {
	case TypeCommit, TypeTree, TypeBlob, TypeTag:
		dataOffset := offset + int64(pos)
		sr := io.NewSectionReader(p.file, dataOffset, p.fileSize-dataOffset)
		data, err := p.decompressZlib(sr, size)
		if err != nil {
			return nil, err
		}
		return &Object{
			Type: objType,
			Size: size,
			Data: data,
		}, nil

	case TypeOfsDelta:
		if pos >= n {
			return nil, fmt.Errorf("%w: truncated ofs_delta offset at %d", ErrCorruptObject, offset)
		}
		b = hdrBuf[pos]
		pos++
		offsetDelta := int64(b & 0x7f)
		shiftCount := 0
		for (b & 0x80) != 0 {
			shiftCount++
			if shiftCount > 10 {
				return nil, fmt.Errorf("%w: ofs_delta offset shift overflow at %d", ErrCorruptObject, offset)
			}
			if pos >= n {
				return nil, fmt.Errorf("%w: truncated ofs_delta offset at %d", ErrCorruptObject, offset)
			}
			b = hdrBuf[pos]
			pos++
			offsetDelta = ((offsetDelta + 1) << 7) | int64(b&0x7f)
		}
		baseOffset := offset - offsetDelta
		if baseOffset < 0 || baseOffset >= offset {
			return nil, fmt.Errorf("%w: invalid base offset %d", ErrCorruptObject, baseOffset)
		}

		dataOffset := offset + int64(pos)
		sr := io.NewSectionReader(p.file, dataOffset, p.fileSize-dataOffset)
		deltaBytes, err := p.decompressZlib(sr, size)
		if err != nil {
			return nil, err
		}

		baseObj, err := p.readObjectAt(baseOffset, depth+1)
		if err != nil {
			return nil, fmt.Errorf("failed to read base object for ofs_delta: %w", err)
		}

		deltaBuf := GetDeltaBuffer()
		targetData, err := ApplyDeltaWithBuffer(*deltaBuf, baseObj.Data, deltaBytes)
		if baseObj.poolBuf != nil {
			PutDeltaBuffer(baseObj.poolBuf)
			baseObj.poolBuf = nil
		}
		if err != nil {
			PutDeltaBuffer(deltaBuf)
			return nil, fmt.Errorf("failed to apply ofs_delta: %w", err)
		}

		if depth > 0 {
			return &Object{
				Type:    baseObj.Type,
				Size:    int64(len(targetData)),
				Data:    targetData,
				poolBuf: deltaBuf,
			}, nil
		}

		finalData := append([]byte(nil), targetData...)
		PutDeltaBuffer(deltaBuf)
		return &Object{
			Type: baseObj.Type,
			Size: int64(len(finalData)),
			Data: finalData,
		}, nil

	case TypeRefDelta:
		shaLen := p.idx.hashLen
		if pos+shaLen > n {
			return nil, fmt.Errorf("%w: truncated base SHA in ref_delta at %d", ErrCorruptObject, offset)
		}
		baseOID := hex.EncodeToString(hdrBuf[pos : pos+shaLen])
		pos += shaLen

		dataOffset := offset + int64(pos)
		sr := io.NewSectionReader(p.file, dataOffset, p.fileSize-dataOffset)
		deltaBytes, err := p.decompressZlib(sr, size)
		if err != nil {
			return nil, err
		}

		var baseObj *Object
		if p.resolver != nil {
			baseObj, err = p.resolver.ReadObject(baseOID)
		} else {
			baseObj, err = p.ReadObject(baseOID)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve base object %s for ref_delta: %w", baseOID, err)
		}

		deltaBuf := GetDeltaBuffer()
		targetData, err := ApplyDeltaWithBuffer(*deltaBuf, baseObj.Data, deltaBytes)
		if baseObj.poolBuf != nil {
			PutDeltaBuffer(baseObj.poolBuf)
			baseObj.poolBuf = nil
		}
		if err != nil {
			PutDeltaBuffer(deltaBuf)
			return nil, fmt.Errorf("failed to apply ref_delta: %w", err)
		}

		if depth > 0 {
			return &Object{
				Type:    baseObj.Type,
				Size:    int64(len(targetData)),
				Data:    targetData,
				poolBuf: deltaBuf,
			}, nil
		}

		finalData := append([]byte(nil), targetData...)
		PutDeltaBuffer(deltaBuf)
		return &Object{
			Type: baseObj.Type,
			Size: int64(len(finalData)),
			Data: finalData,
		}, nil

	default:
		return nil, fmt.Errorf("%w: unsupported pack object type %d", ErrCorruptObject, objType)
	}
}

// decompressZlib reads and decompresses size bytes using a pooled or new zlib reader.
func (p *PackReader) decompressZlib(r io.Reader, size int64) ([]byte, error) {
	if size < 0 || size > MaxObjectSize {
		return nil, fmt.Errorf("%w: object size %d exceeds limit", ErrCorruptObject, size)
	}

	var zReader io.ReadCloser
	if pooled := p.zlibPool.Get(); pooled != nil {
		if zr, ok := pooled.(zlib.Resetter); ok {
			if resetErr := zr.Reset(r, nil); resetErr == nil {
				if rc, ok := pooled.(io.ReadCloser); ok {
					zReader = rc
				}
			}
		}
	}

	if zReader == nil {
		var zErr error
		zReader, zErr = zlib.NewReader(r)
		if zErr != nil {
			return nil, fmt.Errorf("%w: failed to init zlib decompressor: %v", ErrCorruptObject, zErr)
		}
	}
	defer func() {
		_ = zReader.Close()
		p.zlibPool.Put(zReader)
	}()

	buf := make([]byte, size)
	limited := io.LimitReader(zReader, size)
	if _, err := io.ReadFull(limited, buf); err != nil {
		return nil, fmt.Errorf("%w: failed reading decompressed pack object: %v", ErrCorruptObject, err)
	}
	return buf, nil
}

// Close closes the underlying packfile.
func (p *PackReader) Close() error {
	if p.file != nil {
		return p.file.Close()
	}
	return nil
}
