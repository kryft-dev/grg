package gitengine

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zlib"
)

func writeLooseObject(t *testing.T, objectsDir string, objType ObjectType, content []byte) string {
	header := fmt.Sprintf("%s %d\x00", objType.String(), len(content))
	fullData := append([]byte(header), content...)

	h := sha1.Sum(fullData)
	oid := hex.EncodeToString(h[:])

	objDir := filepath.Join(objectsDir, oid[:2])
	if err := os.MkdirAll(objDir, 0755); err != nil {
		t.Fatalf("failed to create object dir: %v", err)
	}

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(fullData); err != nil {
		t.Fatalf("failed to compress object: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zlib writer: %v", err)
	}

	objFile := filepath.Join(objDir, oid[2:])
	if err := os.WriteFile(objFile, compressed.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write loose object: %v", err)
	}

	return oid
}

func TestLooseReader(t *testing.T) {
	tmpDir := t.TempDir()
	objectsDir := filepath.Join(tmpDir, "objects")

	content := []byte("Hello, Git Loose Object!")
	blobOID := writeLooseObject(t, objectsDir, TypeBlob, content)

	reader := NewLooseReader(tmpDir)
	defer reader.Close()

	if !reader.HasObject(blobOID) {
		t.Fatalf("expected HasObject(%q) to be true", blobOID)
	}

	obj, err := reader.ReadObject(blobOID)
	if err != nil {
		t.Fatalf("failed to read loose object: %v", err)
	}

	if obj.Type != TypeBlob {
		t.Errorf("expected type %v, got %v", TypeBlob, obj.Type)
	}
	if obj.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), obj.Size)
	}
	if !bytes.Equal(obj.Data, content) {
		t.Errorf("expected data %q, got %q", content, obj.Data)
	}

	// Non-existent object
	nonExistentOID := "0123456789abcdef0123456789abcdef01234567"
	if reader.HasObject(nonExistentOID) {
		t.Errorf("expected HasObject to be false for non-existent OID")
	}
	_, err = reader.ReadObject(nonExistentOID)
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound, got %v", err)
	}

	// Corrupt object file
	corruptOID := "abcdef1234567890abcdef1234567890abcdef12"
	cDir := filepath.Join(objectsDir, corruptOID[:2])
	_ = os.MkdirAll(cDir, 0755)
	_ = os.WriteFile(filepath.Join(cDir, corruptOID[2:]), []byte("not zlib data"), 0644)

	_, err = reader.ReadObject(corruptOID)
	if !errors.Is(err, ErrCorruptObject) {
		t.Errorf("expected ErrCorruptObject for invalid zlib data, got %v", err)
	}
}
