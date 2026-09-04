package search

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"runtime"
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

func TestPipelineExecuteContext_Streaming(t *testing.T) {
	reader := newMockSearchReader()
	blob1 := reader.putBlob([]byte("target match in blob 1\n"))
	blob2 := reader.putBlob([]byte("target match in blob 2\n"))
	blob3 := reader.putBlob([]byte("non-matching content\n"))

	occurrences := []model.BlobOccurrence{
		{BlobOID: blob1, Path: "f1.txt", CommitSHA: "c1", Mode: 0100644},
		{BlobOID: blob2, Path: "f2.txt", CommitSHA: "c2", Mode: 0100644},
		{BlobOID: blob3, Path: "f3.txt", CommitSHA: "c3", Mode: 0100644},
	}

	cfg := &model.Config{Pattern: "target"}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}

	pipeline := NewPipeline(reader, matcher, cfg)
	resultsCh, errCh := pipeline.ExecuteContext(context.Background(), occurrences)

	var streamResults []*BlobResult
	for res := range resultsCh {
		streamResults = append(streamResults, res)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}

	if len(streamResults) != 2 {
		t.Fatalf("expected 2 matching results from stream, got %d", len(streamResults))
	}
}

func TestPipelineExecuteContext_CancelledContext(t *testing.T) {
	reader := newMockSearchReader()
	var occurrences []model.BlobOccurrence
	for i := 0; i < 200; i++ {
		oid := reader.putBlob([]byte(fmt.Sprintf("line %d with needle in blob\n", i)))
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("file_%d.txt", i),
			CommitSHA: "c1",
			Mode:      0100644,
		})
	}

	cfg := &model.Config{Pattern: "needle"}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	pipeline := NewPipeline(reader, matcher, cfg)
	resultsCh, errCh := pipeline.ExecuteContext(ctx, occurrences)

	// Read one result, then cancel context immediately
	<-resultsCh
	cancel()

	// Drain remaining results to ensure pipeline shuts down cleanly without deadlocking
	count := 1
	for range resultsCh {
		count++
	}

	err = <-errCh
	if err == nil && count < len(occurrences) {
		// Context was cancelled mid-flight
		t.Logf("Pipeline stopped after reading %d of %d items", count, len(occurrences))
	}
}

func TestPipelineExecuteContext_ZeroGoroutineLeak(t *testing.T) {
	reader := newMockSearchReader()
	var occurrences []model.BlobOccurrence
	for i := 0; i < 300; i++ {
		oid := reader.putBlob([]byte(fmt.Sprintf("content with search_key for item %d\n", i)))
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("path/file_%d.txt", i),
			CommitSHA: "c1",
			Mode:      0100644,
		})
	}

	cfg := &model.Config{Pattern: "search_key"}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}

	// Give runtime a chance to stabilize
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	pipeline := NewPipeline(reader, matcher, cfg)
	resultsCh, errCh := pipeline.ExecuteContext(ctx, occurrences)

	// Cancel after reading a few results
	for i := 0; i < 5; i++ {
		<-resultsCh
	}
	cancel()

	// Drain results and errors
	for range resultsCh {
	}
	<-errCh

	// Verify all workers, dispatchers, and closers exit
	var finalGoroutines int
	for attempt := 0; attempt < 50; attempt++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		finalGoroutines = runtime.NumGoroutine()
		if finalGoroutines <= baseGoroutines+1 {
			break
		}
	}

	if finalGoroutines > baseGoroutines+2 {
		t.Errorf("goroutine leak detected: baseline=%d, final=%d", baseGoroutines, finalGoroutines)
	}
}

func TestPipelineExecuteContext_EarlyCancelled(t *testing.T) {
	reader := newMockSearchReader()
	oid := reader.putBlob([]byte("data"))
	occurrences := []model.BlobOccurrence{{BlobOID: oid, Path: "file.txt"}}

	cfg := &model.Config{Pattern: "data"}
	matcher, _ := NewMatcher(cfg)
	pipeline := NewPipeline(reader, matcher, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	resultsCh, errCh := pipeline.ExecuteContext(ctx, occurrences)
	for range resultsCh {
		t.Errorf("expected no results from early-cancelled pipeline")
	}

	err := <-errCh
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPipelineExecuteContext_Empty(t *testing.T) {
	reader := newMockSearchReader()
	cfg := &model.Config{Pattern: "pattern"}
	matcher, _ := NewMatcher(cfg)
	pipeline := NewPipeline(reader, matcher, cfg)

	resultsCh, errCh := pipeline.ExecuteContext(context.Background(), nil)
	for range resultsCh {
		t.Errorf("expected no results for empty occurrences")
	}
	err, ok := <-errCh
	if ok && err != nil {
		t.Errorf("expected no error for empty occurrences, got %v", err)
	}
}

func TestPipelineExecuteContext_ReaderError(t *testing.T) {
	reader := newMockSearchReader() // missing blob
	occurrences := []model.BlobOccurrence{
		{BlobOID: "nonexistent_oid", Path: "missing.txt", CommitSHA: "c1", Mode: 0100644},
	}

	cfg := &model.Config{Pattern: "pattern"}
	matcher, _ := NewMatcher(cfg)
	pipeline := NewPipeline(reader, matcher, cfg)

	resultsCh, errCh := pipeline.ExecuteContext(context.Background(), occurrences)
	var results []*BlobResult
	for res := range resultsCh {
		results = append(results, res)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result with error, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Errorf("expected blob reading error on result")
	}

	err := <-errCh
	if err == nil {
		t.Errorf("expected error propagated to errCh")
	}
}

