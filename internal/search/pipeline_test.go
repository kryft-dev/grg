package search

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
)

type mockSearchReader struct {
	objects  map[string]*gitengine.Object
	failWith map[string]error // OIDs whose ReadObject returns the given error
}

func newMockSearchReader() *mockSearchReader {
	return &mockSearchReader{
		objects:  make(map[string]*gitengine.Object),
		failWith: make(map[string]error),
	}
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
	if err, ok := m.failWith[oid]; ok {
		return nil, err
	}
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

// A blob the reader cannot load is a soft failure: it is delivered on resultsCh
// as a *BlobReadError so callers can warn, but never on errCh (regression #10).
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
	var bre *BlobReadError
	if !errors.As(results[0].Error, &bre) {
		t.Fatalf("expected *BlobReadError on result, got %v", results[0].Error)
	}
	if bre.OID != "nonexistent_oid" || bre.Path != "missing.txt" {
		t.Errorf("unexpected BlobReadError fields: %+v", bre)
	}
	if !errors.Is(bre, gitengine.ErrObjectNotFound) {
		t.Errorf("expected BlobReadError to wrap ErrObjectNotFound, got %v", bre.Err)
	}

	if err := <-errCh; err != nil {
		t.Errorf("blob read failure must not be fatal, got error on errCh: %v", err)
	}
}

// One unreadable blob must not abort the search: every other blob still yields
// its matches and the failing OID is reported as a soft *BlobReadError.
func TestPipelineSkipsUnreadableBlob(t *testing.T) {
	tests := []struct {
		name    string
		readErr error
	}{
		{name: "object not found", readErr: gitengine.ErrObjectNotFound},
		{name: "corrupt object", readErr: fmt.Errorf("%w: bad zlib stream", gitengine.ErrCorruptObject)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newMockSearchReader()
			good1 := reader.putBlob([]byte("needle in first blob\n"))
			bad := reader.putBlob([]byte("needle in unreadable blob\n"))
			good2 := reader.putBlob([]byte("needle in third blob\n"))
			reader.failWith[bad] = tt.readErr

			occurrences := []model.BlobOccurrence{
				{BlobOID: good1, Path: "a.txt", CommitSHA: "c1", Mode: 0100644},
				{BlobOID: bad, Path: "gone.txt", CommitSHA: "c1", Mode: 0100644},
				{BlobOID: bad, Path: "gone-renamed.txt", CommitSHA: "c2", Mode: 0100644},
				{BlobOID: good2, Path: "b.txt", CommitSHA: "c2", Mode: 0100644},
			}

			cfg := &model.Config{Pattern: "needle"}
			matcher, err := NewMatcher(cfg)
			if err != nil {
				t.Fatalf("NewMatcher failed: %v", err)
			}

			results, err := NewPipeline(reader, matcher, cfg).Execute(occurrences)
			if err != nil {
				t.Fatalf("pipeline must not fail on an unreadable blob, got: %v", err)
			}

			byOID := make(map[string]*BlobResult, len(results))
			for _, r := range results {
				byOID[r.BlobOID] = r
			}
			for _, oid := range []string{good1, good2} {
				res, ok := byOID[oid]
				if !ok {
					t.Fatalf("missing result for readable blob %s", oid)
				}
				if res.Error != nil || len(res.Matches) != 1 {
					t.Errorf("readable blob %s: want 1 match and no error, got %d matches, err=%v", oid, len(res.Matches), res.Error)
				}
			}

			res, ok := byOID[bad]
			if !ok {
				t.Fatalf("expected a result carrying the read failure for %s", bad)
			}
			var bre *BlobReadError
			if !errors.As(res.Error, &bre) {
				t.Fatalf("expected *BlobReadError, got %v", res.Error)
			}
			if bre.OID != bad {
				t.Errorf("BlobReadError.OID = %s, want %s", bre.OID, bad)
			}
			if bre.Path != "gone.txt" {
				t.Errorf("BlobReadError.Path = %q, want first occurrence path %q", bre.Path, "gone.txt")
			}
			if !errors.Is(bre, tt.readErr) {
				t.Errorf("BlobReadError should wrap %v, got %v", tt.readErr, bre.Err)
			}
			if len(res.Matches) != 0 {
				t.Errorf("skipped blob must contribute no matches, got %d", len(res.Matches))
			}
		})
	}
}

// countingReader records how many blobs the pipeline actually read, and panics
// on selected OIDs to stand in for a corrupt object that trips a bug in inflate
// or delta reconstruction.
type countingReader struct {
	*mockSearchReader
	reads   atomic.Int64
	panicOn map[string]bool // filled in before ExecuteContext, never written after
}

func newCountingReader() *countingReader {
	return &countingReader{
		mockSearchReader: newMockSearchReader(),
		panicOn:          make(map[string]bool),
	}
}

func (c *countingReader) ReadObject(oid string) (*gitengine.Object, error) {
	c.reads.Add(1)
	if c.panicOn[oid] {
		panic("simulated corrupt object " + oid)
	}
	return c.mockSearchReader.ReadObject(oid)
}

// matchingOccurrences stores n distinct blobs that all match the pattern
// "needle" and returns one occurrence for each, in blob order.
func matchingOccurrences(r *countingReader, n int) []model.BlobOccurrence {
	occurrences := make([]model.BlobOccurrence, 0, n)
	for i := range n {
		oid := r.putBlob([]byte(fmt.Sprintf("needle in blob %d\n", i)))
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("file_%d.txt", i),
			CommitSHA: "c1",
			Mode:      0100644,
		})
	}
	return occurrences
}

// -q asks for the first hit only. The pipeline must abandon the remaining blobs
// instead of pulling all of them through the queue, and its early stop must not
// be reported as a cancellation error.
func TestPipelineQuietStopsAfterFirstMatch(t *testing.T) {
	t.Run("single worker reads only the first blob", func(t *testing.T) {
		reader := newCountingReader()
		occurrences := matchingOccurrences(reader, 200)

		cfg := &model.Config{Pattern: "needle", Quiet: true}
		matcher, err := NewMatcher(cfg)
		if err != nil {
			t.Fatalf("NewMatcher failed: %v", err)
		}
		p := NewPipeline(reader, matcher, cfg)
		p.workers = 1 // one worker, so the first task alone decides the run

		resultsCh, errCh := p.ExecuteContext(context.Background(), occurrences)
		var results []*BlobResult
		for res := range resultsCh {
			results = append(results, res)
		}
		if err := <-errCh; err != nil {
			t.Fatalf("quiet early stop must not be an error, got %v", err)
		}
		if len(results) != 1 {
			t.Errorf("got %d results, want 1: -q reports a single hit", len(results))
		}
		if got := reader.reads.Load(); got != 1 {
			t.Errorf("read %d blobs, want exactly 1: -q must stop at the first match", got)
		}
	})

	t.Run("concurrent workers stop well before the queue is drained", func(t *testing.T) {
		reader := newCountingReader()
		occurrences := matchingOccurrences(reader, 2000)

		cfg := &model.Config{Pattern: "needle", Quiet: true}
		matcher, err := NewMatcher(cfg)
		if err != nil {
			t.Fatalf("NewMatcher failed: %v", err)
		}

		resultsCh, errCh := NewPipeline(reader, matcher, cfg).ExecuteContext(context.Background(), occurrences)
		results := 0
		for range resultsCh {
			results++
		}
		if err := <-errCh; err != nil {
			t.Fatalf("quiet early stop must not be an error, got %v", err)
		}
		if results == 0 {
			t.Errorf("expected at least the one hit -q asks for")
		}
		// Cancellation bounds the reads to the tasks already buffered or in
		// flight; without it every one of the 2000 blobs is pulled from the queue.
		if got, limit := reader.reads.Load(), int64(len(occurrences)/2); got > limit {
			t.Errorf("read %d of %d blobs, want at most %d: -q must cancel, not drain", got, len(occurrences), limit)
		}
	})
}

// A fatal error abandons the run. The pipeline must stop at once instead of
// searching every remaining blob and only revealing the failure when resultsCh
// closes, and the failure it reports must be the error itself, not the
// cancellation that error triggered.
func TestPipelineFatalErrorStopsRunEarly(t *testing.T) {
	reader := newCountingReader()
	occurrences := matchingOccurrences(reader, 300)
	reader.panicOn[occurrences[0].BlobOID] = true

	cfg := &model.Config{Pattern: "needle"}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}
	p := NewPipeline(reader, matcher, cfg)
	p.workers = 1 // one worker, so the failing blob is the first task

	resultsCh, errCh := p.ExecuteContext(context.Background(), occurrences)
	for range resultsCh {
	}

	err = <-errCh
	if err == nil {
		t.Fatalf("a fatal error must be reported on errCh")
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("stopping the run must not mask the fatal error as a cancellation: %v", err)
	}
	if got := reader.reads.Load(); got != 1 {
		t.Errorf("read %d of %d blobs, want exactly 1: a fatal error must stop the run", got, len(occurrences))
	}
}

// The pipeline inflates and reconstructs bytes from an arbitrary .git
// directory, so a panic must become an ordinary fatal error naming the offending
// blob instead of killing the process (here, the test binary) and skipping every
// deferred cleanup and the exit-code contract.
func TestPipelinePanicBecomesFatalError(t *testing.T) {
	reader := newCountingReader()
	good := reader.putBlob([]byte("needle in a readable blob\n"))
	poison := reader.putBlob([]byte("needle in a poisoned blob\n"))
	reader.panicOn[poison] = true

	occurrences := []model.BlobOccurrence{
		{BlobOID: good, Path: "good.txt", CommitSHA: "c1", Mode: 0100644},
		{BlobOID: poison, Path: "poison.txt", CommitSHA: "c1", Mode: 0100644},
	}

	cfg := &model.Config{Pattern: "needle"}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}
	p := NewPipeline(reader, matcher, cfg)
	p.workers = 1 // one worker, so the readable blob is searched before the panic

	resultsCh, errCh := p.ExecuteContext(context.Background(), occurrences)
	byOID := make(map[string]*BlobResult, len(occurrences))
	for res := range resultsCh {
		byOID[res.BlobOID] = res
	}

	err = <-errCh
	if err == nil {
		t.Fatalf("a panicking blob must surface as a fatal error on errCh")
	}
	if !strings.Contains(err.Error(), poison) {
		t.Errorf("fatal error must name the offending blob %s, got: %v", poison, err)
	}

	res, ok := byOID[poison]
	if !ok {
		t.Fatalf("expected a result carrying the panic for %s", poison)
	}
	if res.Error == nil || !strings.Contains(res.Error.Error(), "panic") {
		t.Errorf("result for the poisoned blob must carry the recovered panic, got %v", res.Error)
	}
	if isBlobReadError(res.Error) {
		t.Errorf("a panic is fatal, not a soft per-blob read failure: %v", res.Error)
	}

	res, ok = byOID[good]
	if !ok {
		t.Fatalf("the blob searched before the panic must still be delivered")
	}
	if len(res.Matches) != 1 {
		t.Errorf("got %d matches for the readable blob, want 1", len(res.Matches))
	}
}

// A cancellation arriving once every result has been handed to the caller must
// not turn a complete run into a failure: nothing was cut short, so exit codes
// derived from errCh must stay clean even if the signal lands microseconds after
// the last worker finished.
func TestPipelineCancelAfterCompletion(t *testing.T) {
	reader := newCountingReader()
	occurrences := matchingOccurrences(reader, 64)

	cfg := &model.Config{Pattern: "needle"}
	matcher, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}
	p := NewPipeline(reader, matcher, cfg)

	// Repeat so the cancel lands at varying points of the shutdown sequence.
	for range 30 {
		func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			resultsCh, errCh := p.ExecuteContext(ctx, occurrences)

			// Take every expected result, then cancel while the workers and the
			// closer may still be winding down.
			for got := 0; got < len(occurrences); got++ {
				res, ok := <-resultsCh
				if !ok {
					t.Fatalf("resultsCh closed after %d of %d results", got, len(occurrences))
				}
				if res.Error != nil {
					t.Fatalf("unexpected result error: %v", res.Error)
				}
			}
			cancel()

			for range resultsCh {
				t.Errorf("got more results than the %d blobs searched", len(occurrences))
			}
			if err := <-errCh; err != nil {
				t.Fatalf("cancel after the full result set must leave errCh empty, got %v", err)
			}
		}()
	}
}
