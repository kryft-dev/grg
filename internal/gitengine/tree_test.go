package gitengine

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

type mockObjectReader struct {
	objects map[string]*Object
}

func newMockReader() *mockObjectReader {
	return &mockObjectReader{objects: make(map[string]*Object)}
}

func (m *mockObjectReader) put(t ObjectType, data []byte) string {
	hdr := fmt.Sprintf("%s %d\x00", t.String(), len(data))
	full := append([]byte(hdr), data...)
	h := sha1.Sum(full)
	oid := hex.EncodeToString(h[:])
	m.objects[oid] = &Object{
		OID:  oid,
		Type: t,
		Size: int64(len(data)),
		Data: data,
	}
	return oid
}

func (m *mockObjectReader) ReadObject(oid string) (*Object, error) {
	if obj, ok := m.objects[oid]; ok {
		return obj, nil
	}
	return nil, ErrObjectNotFound
}

func (m *mockObjectReader) HasObject(oid string) bool {
	_, ok := m.objects[oid]
	return ok
}

func (m *mockObjectReader) Close() error {
	return nil
}

func buildTreePayload(entries []TreeEntry) []byte {
	var buf bytes.Buffer
	for _, e := range entries {
		buf.WriteString(fmt.Sprintf("%o %s\x00", e.Mode, e.Name))
		shaBytes, _ := hex.DecodeString(e.OID)
		buf.Write(shaBytes)
	}
	return buf.Bytes()
}

func TestParseTreeAndTraverse(t *testing.T) {
	reader := newMockReader()

	// 1. Create blobs
	file1OID := reader.put(TypeBlob, []byte("package main\n"))
	file2OID := reader.put(TypeBlob, []byte("console.log('hi');\n"))

	// 2. Create subtree containing file2
	subEntries := []TreeEntry{
		{Mode: 0100644, Name: "script.js", OID: file2OID},
	}
	subPayload := buildTreePayload(subEntries)
	subTreeOID := reader.put(TypeTree, subPayload)

	// 3. Create root tree containing file1 and subtree
	rootEntries := []TreeEntry{
		{Mode: 0100644, Name: "main.go", OID: file1OID},
		{Mode: 0040000, Name: "src", OID: subTreeOID},
	}
	rootPayload := buildTreePayload(rootEntries)
	rootTreeOID := reader.put(TypeTree, rootPayload)

	// Test ParseTree directly
	parsed, err := ParseTree(rootPayload)
	if err != nil {
		t.Fatalf("ParseTree failed: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(parsed))
	}
	if parsed[0].Name != "main.go" || !parsed[0].IsBlob() {
		t.Errorf("wrong entry 0: %+v", parsed[0])
	}
	if parsed[1].Name != "src" || !parsed[1].IsTree() {
		t.Errorf("wrong entry 1: %+v", parsed[1])
	}

	// Test TraverseTree
	visited := make(map[string]string)
	err = TraverseTree(reader, rootTreeOID, func(path string, entry TreeEntry) error {
		visited[path] = entry.OID
		return nil
	})
	if err != nil {
		t.Fatalf("TraverseTree failed: %v", err)
	}

	if visited["main.go"] != file1OID {
		t.Errorf("expected main.go at %s, got %s", file1OID, visited["main.go"])
	}
	if visited["src/script.js"] != file2OID {
		t.Errorf("expected src/script.js at %s, got %s", file2OID, visited["src/script.js"])
	}

	// Test ErrSkipDir
	visitedSkipped := make(map[string]string)
	err = TraverseTree(reader, rootTreeOID, func(path string, entry TreeEntry) error {
		visitedSkipped[path] = entry.OID
		if entry.IsTree() && entry.Name == "src" {
			return ErrSkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TraverseTree with ErrSkipDir failed: %v", err)
	}
	if _, ok := visitedSkipped["src/script.js"]; ok {
		t.Errorf("src/script.js should have been skipped!")
	}
}

func TestParseTreeErrors(t *testing.T) {
	// Missing space
	_, err := ParseTree([]byte("100644file\x0012345678901234567890"))
	if !errors.Is(err, ErrInvalidTree) {
		t.Errorf("expected ErrInvalidTree, got %v", err)
	}

	// Missing null terminator
	_, err = ParseTree([]byte("100644 file12345678901234567890"))
	if !errors.Is(err, ErrInvalidTree) {
		t.Errorf("expected ErrInvalidTree, got %v", err)
	}

	// Truncated SHA
	_, err = ParseTree([]byte("100644 file\x00short"))
	if !errors.Is(err, ErrInvalidTree) {
		t.Errorf("expected ErrInvalidTree, got %v", err)
	}
}
