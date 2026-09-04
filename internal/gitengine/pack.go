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

	sr := io.NewSectionReader(p.file, offset, p.fileSize-offset)

	// 1. Read object header
	b := make([]byte, 1)
	if _, err := sr.Read(b); err != nil {
		return nil, fmt.Errorf("%w: failed to read object header: %v", ErrCorruptObject, err)
	}

	objType := ObjectType((b[0] >> 4) & 7)
	size := int64(b[0] & 0x0f)
	shift := uint(4)

	for (b[0] & 0x80) != 0 {
		if _, err := sr.Read(b); err != nil {
			return nil, fmt.Errorf("%w: truncated object size in header: %v", ErrCorruptObject, err)
		}
		size |= int64(b[0]&0x7f) << shift
		shift += 7
	}

	switch objType {
	case TypeCommit, TypeTree, TypeBlob, TypeTag:
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
		// Read negative offset to base object
		if _, err := sr.Read(b); err != nil {
			return nil, fmt.Errorf("%w: failed to read ofs_delta offset: %v", ErrCorruptObject, err)
		}
		offsetDelta := int64(b[0] & 0x7f)
		for (b[0] & 0x80) != 0 {
			if _, err := sr.Read(b); err != nil {
				return nil, fmt.Errorf("%w: truncated ofs_delta offset: %v", ErrCorruptObject, err)
			}
			offsetDelta = ((offsetDelta + 1) << 7) | int64(b[0]&0x7f)
		}
		baseOffset := offset - offsetDelta
		if baseOffset < 0 || baseOffset >= offset {
			return nil, fmt.Errorf("%w: invalid base offset %d", ErrCorruptObject, baseOffset)
		}

		deltaBytes, err := p.decompressZlib(sr, size)
		if err != nil {
			return nil, err
		}

		baseObj, err := p.readObjectAt(baseOffset, depth+1)
		if err != nil {
			return nil, fmt.Errorf("failed to read base object for ofs_delta: %w", err)
		}

		targetData, err := ApplyDelta(baseObj.Data, deltaBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to apply ofs_delta: %w", err)
		}

		return &Object{
			Type: baseObj.Type,
			Size: int64(len(targetData)),
			Data: targetData,
		}, nil

	case TypeRefDelta:
		shaBuf := make([]byte, p.idx.hashLen)
		if _, err := io.ReadFull(sr, shaBuf); err != nil {
			return nil, fmt.Errorf("%w: truncated base SHA in ref_delta: %v", ErrCorruptObject, err)
		}
		baseOID := hex.EncodeToString(shaBuf)

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

		targetData, err := ApplyDelta(baseObj.Data, deltaBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to apply ref_delta: %w", err)
		}

		return &Object{
			Type: baseObj.Type,
			Size: int64(len(targetData)),
			Data: targetData,
		}, nil

	default:
		return nil, fmt.Errorf("%w: unsupported pack object type %d", ErrCorruptObject, objType)
	}
}

// decompressZlib reads and decompresses size bytes using a pooled or new zlib reader.
func (p *PackReader) decompressZlib(r io.Reader, size int64) ([]byte, error) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to init zlib decompressor: %v", ErrCorruptObject, err)
	}
	defer zr.Close()

	buf := make([]byte, size)
	if _, err := io.ReadFull(zr, buf); err != nil {
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
