package search

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
)

type mockSearchReader struct {
	objects map[string]*gitengine.Object
}

func newMockSearchReader() *mockSearchReader {
	return &mockSearchReader{objects: make(map[string]*gitengine.Object)}
}

func (m *mockSearchReader) putBlob(content []byte) string {
	hdr := fmt.Sprintf("blob %d\x00", len(content))
	full := append([]byte(hdr), content...)
	h := sha1.Sum(full)
	oid := hex.EncodeToString(h[:])
	m.objects[oid] = &gitengine.Object{
		OID:  oid,
		Type: gitengine.TypeBlob,
		Size: int64(len(content)),
		Data: content,
	}
	return oid
}

func (m *mockSearchReader) ReadObject(oid string) (*gitengine.Object, error) {
	if obj, ok := m.objects[oid]; ok {
		return obj, nil
	}
	return nil, gitengine.ErrObjectNotFound
}

func (m *mockSearchReader) HasObject(oid string) bool {
	_, ok := m.objects[oid]
	return ok
}

func (m *mockSearchReader) Close() error {
	return nil
}

func TestPipelineDeduplicationAndProvenance(t *testing.T) {
	reader := newMockSearchReader()

	blob1Content := []byte("package main\n\nfunc Search() string {\n\treturn \"FOUND_ME\"\n}\n")
	blob1OID := reader.putBlob(blob1Content)

	blob2Content := []byte("readme content\nFOUND_ME is here too\n")
	blob2OID := reader.putBlob(blob2Content)

	blob3Binary := []byte("binary data \x00\x01\x02 FOUND_ME")
	blob3OID := reader.putBlob(blob3Binary)

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// Blob 1 appears in 3 occurrences across different commits and paths
	occurrences := []model.BlobOccurrence{
		{BlobOID: blob1OID, Path: "main.go", CommitSHA: "commit1", CommitDate: t1, Mode: 0100644},
		{BlobOID: blob1OID, Path: "cmd/main.go", CommitSHA: "commit2", CommitDate: t2, Mode: 0100644},
		{BlobOID: blob1OID, Path: "old/main.go", CommitSHA: "commit3", CommitDate: t3, Mode: 0100644},
		{BlobOID: blob2OID, Path: "README.md", CommitSHA: "commit1", CommitDate: t1, Mode: 0100644},
		{BlobOID: blob3OID, Path: "asset.bin", CommitSHA: "commit1", CommitDate: t1, Mode: 0100644},
	}

	cfg := &model.Config{
		Pattern: "FOUND_ME",
	}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}

	pipeline := NewPipeline(reader, matcher, cfg)
	results, err := pipeline.Execute(occurrences)
	if err != nil {
		t.Fatalf("pipeline.Execute failed: %v", err)
	}

	// Should have results for blob1 (text match), blob2 (text match), and blob3 (marked binary)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Find result for blob1
	var res1 *BlobResult
	for _, r := range results {
		if r.BlobOID == blob1OID {
			res1 = r
			break
		}
	}
	if res1 == nil {
		t.Fatalf("blob1 result not found")
	}

	// Verify deduplication and provenance association:
	// Blob 1 had 3 occurrences, all 3 occurrences must be preserved on res1!
	if len(res1.Occurrences) != 3 {
		t.Errorf("expected 3 occurrences attached to blob1 result, got %d", len(res1.Occurrences))
	}
	if len(res1.Matches) != 1 {
		t.Fatalf("expected 1 match in blob1, got %d", len(res1.Matches))
	}
	if res1.Matches[0].LineNum != 4 {
		t.Errorf("expected match on line 4, got %d", res1.Matches[0].LineNum)
	}

	// Verify binary blob result
	var res3 *BlobResult
	for _, r := range results {
		if r.BlobOID == blob3OID {
			res3 = r
			break
		}
	}
	if res3 == nil {
		t.Fatalf("blob3 result not found")
	}
	if !res3.IsBinary {
		t.Errorf("expected blob3 to be marked binary")
	}
}

func TestPipelineWithContext(t *testing.T) {
	reader := newMockSearchReader()

	blobContent := []byte("line 1\nline 2\nKEYWORD target\nline 4\nline 5\n")
	blobOID := reader.putBlob(blobContent)

	occurrences := []model.BlobOccurrence{
		{BlobOID: blobOID, Path: "file.txt", CommitSHA: "c1", Mode: 0100644},
	}

	cfg := &model.Config{
		Pattern:       "KEYWORD",
		BeforeContext: 1,
		AfterContext:  1,
	}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}

	pipeline := NewPipeline(reader, matcher, cfg)
	results, err := pipeline.Execute(occurrences)
	if err != nil {
		t.Fatalf("pipeline.Execute failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if len(res.ContextGroups) != 1 {
		t.Fatalf("expected 1 context group, got %d", len(res.ContextGroups))
	}
	group := res.ContextGroups[0]
	if len(group.Lines) != 3 {
		t.Fatalf("expected 3 lines in context group (before, match, after), got %d", len(group.Lines))
	}
	if group.Lines[0].LineNum != 2 || group.Lines[1].LineNum != 3 || group.Lines[2].LineNum != 4 {
		t.Errorf("wrong context line numbers: %+v", group.Lines)
	}
}
