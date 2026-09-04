package gitengine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/klauspost/compress/zlib"
)

// LooseReader provides read access to loose Git objects located in .git/objects/??/*
type LooseReader struct {
	objectsDir string
	zlibPool   sync.Pool
}

// NewLooseReader creates a LooseReader for the given common git directory.
func NewLooseReader(commonGitDir string) *LooseReader {
	return &LooseReader{
		objectsDir: filepath.Join(commonGitDir, "objects"),
	}
}

// objectPath returns the filesystem path for a loose Git object by OID.
func (r *LooseReader) objectPath(oid string) string {
	if len(oid) < 4 {
		return ""
	}
	return filepath.Join(r.objectsDir, oid[:2], oid[2:])
}

// HasObject returns true if the loose object file exists on disk.
func (r *LooseReader) HasObject(oid string) bool {
	path := r.objectPath(oid)
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// ReadObject reads, decompresses, and parses a loose Git object by OID.
func (r *LooseReader) ReadObject(oid string) (*Object, error) {
	path := r.objectPath(oid)
	if path == "" {
		return nil, fmt.Errorf("%w: invalid OID %q", ErrObjectNotFound, oid)
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, oid)
		}
		return nil, fmt.Errorf("failed to open loose object %s: %w", oid, err)
	}
	defer f.Close()

	var zReader io.ReadCloser
	if pooled := r.zlibPool.Get(); pooled != nil {
		zr := pooled.(zlib.Resetter)
		if resetErr := zr.Reset(f, nil); resetErr == nil {
			zReader = pooled.(io.ReadCloser)
		}
	}

	if zReader == nil {
		var zErr error
		zReader, zErr = zlib.NewReader(f)
		if zErr != nil {
			return nil, fmt.Errorf("%w: failed to create zlib reader for %s: %v", ErrCorruptObject, oid, zErr)
		}
	}
	defer func() {
		_ = zReader.Close()
		r.zlibPool.Put(zReader)
	}()

	br := bufio.NewReader(zReader)

	// Loose object header format: "<type> <size>\0"
	typeBytes, err := br.ReadBytes(' ')
	if err != nil {
		return nil, fmt.Errorf("%w: malformed loose object header for %s: %v", ErrCorruptObject, oid, err)
	}
	typeStr := string(typeBytes[:len(typeBytes)-1])
	objType := ParseObjectType(typeStr)
	if objType == TypeInvalid {
		return nil, fmt.Errorf("%w: unknown loose object type %q for %s", ErrCorruptObject, typeStr, oid)
	}

	sizeBytes, err := br.ReadBytes(0)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed loose object size for %s: %v", ErrCorruptObject, oid, err)
	}
	sizeStr := string(sizeBytes[:len(sizeBytes)-1])
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil || size < 0 {
		return nil, fmt.Errorf("%w: invalid loose object size %q for %s: %v", ErrCorruptObject, sizeStr, oid, err)
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(br, payload); err != nil {
		return nil, fmt.Errorf("%w: unexpected EOF reading loose object data for %s: %v", ErrCorruptObject, oid, err)
	}

	return &Object{
		OID:  oid,
		Type: objType,
		Size: size,
		Data: payload,
	}, nil
}

// Close releases any resources associated with LooseReader.
func (r *LooseReader) Close() error {
	return nil
}
