package aggregator

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
	"github.com/kryft-dev/grg/internal/search"
)

func TestAggregator_DeduplicationIntroducingCommit(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)

	// Blob appears in 3 commits at the same path
	res := &search.BlobResult{
		BlobOID: "blob1",
		Matches: []model.SearchMatch{
			{LineNum: 1, LineText: "func main()"},
		},
		Occurrences: []model.BlobOccurrence{
			{Path: "main.go", CommitSHA: "sha3", CommitDate: t3, CommitAuthor: "Alice"},
			{Path: "main.go", CommitSHA: "sha1", CommitDate: t1, CommitAuthor: "Bob"},
			{Path: "main.go", CommitSHA: "sha2", CommitDate: t2, CommitAuthor: "Charlie"},
		},
	}

	cfg := &model.Config{ExpandCommits: false}
	agg := New(cfg)
	out := agg.Aggregate([]*search.BlobResult{res})

	if len(out.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(out.Files))
	}
	f := out.Files[0]
	if f.Path != "main.go" {
		t.Errorf("expected path main.go, got %s", f.Path)
	}
	if len(f.Commits) != 1 {
		t.Fatalf("expected 1 introducing commit, got %d", len(f.Commits))
	}
	if f.Commits[0].CommitSHA != "sha1" {
		t.Errorf("expected introducing commit sha1, got %s", f.Commits[0].CommitSHA)
	}
	if f.Commits[0].Author != "Bob" {
		t.Errorf("expected author Bob, got %s", f.Commits[0].Author)
	}
}

func TestAggregator_ExpandCommits(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	res := &search.BlobResult{
		BlobOID: "blob1",
		Matches: []model.SearchMatch{
			{LineNum: 1, LineText: "foo"},
		},
		Occurrences: []model.BlobOccurrence{
			{Path: "main.go", CommitSHA: "sha1", CommitDate: t1},
			{Path: "main.go", CommitSHA: "sha2", CommitDate: t2},
		},
	}

	cfg := &model.Config{ExpandCommits: true}
	agg := New(cfg)
	out := agg.Aggregate([]*search.BlobResult{res})

	if len(out.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(out.Files))
	}
	f := out.Files[0]
	if len(f.Commits) != 2 {
		t.Fatalf("expected 2 commits with expand-commits, got %d", len(f.Commits))
	}
	// Sorted newest first: sha2 then sha1
	if f.Commits[0].CommitSHA != "sha2" || f.Commits[1].CommitSHA != "sha1" {
		t.Errorf("expected commits [sha2, sha1], got [%s, %s]", f.Commits[0].CommitSHA, f.Commits[1].CommitSHA)
	}
}

func TestAggregator_SortingNewestFirst(t *testing.T) {
	tOld := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tNew := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	res1 := &search.BlobResult{
		BlobOID: "blobOld",
		Matches: []model.SearchMatch{{LineNum: 1, LineText: "old"}},
		Occurrences: []model.BlobOccurrence{
			{Path: "old_file.go", CommitSHA: "shaOld", CommitDate: tOld},
		},
	}
	res2 := &search.BlobResult{
		BlobOID: "blobNew",
		Matches: []model.SearchMatch{{LineNum: 1, LineText: "new"}},
		Occurrences: []model.BlobOccurrence{
			{Path: "new_file.go", CommitSHA: "shaNew", CommitDate: tNew},
		},
	}

	cfg := &model.Config{Unordered: false}
	agg := New(cfg)
	out := agg.Aggregate([]*search.BlobResult{res1, res2})

	if len(out.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(out.Files))
	}
	// Newest file should come first
	if out.Files[0].Path != "new_file.go" {
		t.Errorf("expected first file to be new_file.go, got %s", out.Files[0].Path)
	}
	if out.Files[1].Path != "old_file.go" {
		t.Errorf("expected second file to be old_file.go, got %s", out.Files[1].Path)
	}
}

func TestAggregator_Unordered(t *testing.T) {
	tOld := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tNew := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	res1 := &search.BlobResult{
		BlobOID: "blobOld",
		Matches: []model.SearchMatch{{LineNum: 1, LineText: "old"}},
		Occurrences: []model.BlobOccurrence{
			{Path: "old_file.go", CommitSHA: "shaOld", CommitDate: tOld},
		},
	}
	res2 := &search.BlobResult{
		BlobOID: "blobNew",
		Matches: []model.SearchMatch{{LineNum: 1, LineText: "new"}},
		Occurrences: []model.BlobOccurrence{
			{Path: "new_file.go", CommitSHA: "shaNew", CommitDate: tNew},
		},
	}

	cfg := &model.Config{Unordered: true}
	agg := New(cfg)
	out := agg.Aggregate([]*search.BlobResult{res1, res2})

	// Order should be preserved as encountered
	if out.Files[0].Path != "old_file.go" {
		t.Errorf("expected first file old_file.go in unordered mode, got %s", out.Files[0].Path)
	}
	if out.Files[1].Path != "new_file.go" {
		t.Errorf("expected second file new_file.go in unordered mode, got %s", out.Files[1].Path)
	}
}

func TestAggregator_BinaryResults(t *testing.T) {
	res := &search.BlobResult{
		BlobOID:  "blobBin",
		IsBinary: true,
		Occurrences: []model.BlobOccurrence{
			{Path: "image.png", CommitSHA: "shaBin", CommitDate: time.Now()},
		},
	}

	agg := New(&model.Config{})
	out := agg.Aggregate([]*search.BlobResult{res})

	if !out.HasMatches() {
		t.Errorf("expected HasMatches to be true for binary match")
	}
	if out.TotalMatches != 1 {
		t.Errorf("expected TotalMatches 1, got %d", out.TotalMatches)
	}
	if len(out.Files) != 1 || !out.Files[0].Commits[0].IsBinary {
		t.Errorf("expected 1 file with binary commit")
	}
}

func TestAggregator_DefensiveCopy(t *testing.T) {
	submatches := []model.Submatch{{Start: 0, End: 4}}
	matches := []model.SearchMatch{
		{LineNum: 1, LineText: "test line", Submatches: submatches},
	}
	res := &search.BlobResult{
		BlobOID: "blob1",
		Matches: matches,
		Occurrences: []model.BlobOccurrence{
			{Path: "main.go", CommitSHA: "c1", CommitDate: time.Now()},
		},
	}

	agg := New(&model.Config{})
	out := agg.Aggregate([]*search.BlobResult{res})

	if len(out.Files) != 1 || len(out.Files[0].Commits) != 1 {
		t.Fatalf("expected 1 file and 1 commit")
	}

	// Mutate original slice
	matches[0].LineText = "MUTATED"
	submatches[0].Start = 999

	// Verify aggregated matches were defensively copied and remain unaffected
	aggregatedMatch := out.Files[0].Commits[0].Matches[0]
	if aggregatedMatch.LineText != "test line" {
		t.Errorf("defensive copy failed: expected 'test line', got %q", aggregatedMatch.LineText)
	}
	if aggregatedMatch.Submatches[0].Start != 0 {
		t.Errorf("defensive submatches copy failed: expected 0, got %d", aggregatedMatch.Submatches[0].Start)
	}
}

func TestAggregator_AggregateChannel_Success(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	res1 := &search.BlobResult{
		BlobOID: "blob1",
		Matches: []model.SearchMatch{{LineNum: 10, LineText: "func Hello()"}},
		Occurrences: []model.BlobOccurrence{
			{Path: "hello.go", CommitSHA: "c1", CommitDate: t1},
		},
	}
	res2 := &search.BlobResult{
		BlobOID: "blob2",
		Matches: []model.SearchMatch{{LineNum: 20, LineText: "func World()"}},
		Occurrences: []model.BlobOccurrence{
			{Path: "world.go", CommitSHA: "c2", CommitDate: t2},
		},
	}

	agg := New(&model.Config{})

	// Compare channel results with slice Aggregate results
	expected := agg.Aggregate([]*search.BlobResult{res1, res2})

	resultsCh := make(chan *search.BlobResult, 2)
	errCh := make(chan error, 1)

	resultsCh <- res1
	resultsCh <- res2
	close(resultsCh)
	// A producer that has finished closes both of its channels; aggregation only
	// finalizes once errCh is closed, so that a late error is not lost.
	close(errCh)

	actual, err := agg.AggregateChannel(context.Background(), resultsCh, errCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if actual.TotalMatches != expected.TotalMatches {
		t.Errorf("expected %d total matches, got %d", expected.TotalMatches, actual.TotalMatches)
	}
	if len(actual.Files) != len(expected.Files) {
		t.Fatalf("expected %d files, got %d", len(expected.Files), len(actual.Files))
	}
	for i := range actual.Files {
		if actual.Files[i].Path != expected.Files[i].Path {
			t.Errorf("file %d path mismatch: expected %s, got %s", i, expected.Files[i].Path, actual.Files[i].Path)
		}
	}
}

func TestAggregator_AggregateChannel_Cancellation(t *testing.T) {
	agg := New(&model.Config{})
	resultsCh := make(chan *search.BlobResult)
	errCh := make(chan error)
	// A producer that has already stopped streaming but never reports on errCh:
	// cancellation is the only way out, and the closed results channel is what lets
	// the drain on the way out terminate.
	close(resultsCh)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	var out *AggregatedResults
	var err error
	runWithin(t, 5*time.Second, func() { out, err = agg.AggregateChannel(ctx, resultsCh, errCh) })
	if out != nil {
		t.Errorf("expected nil results on cancelled context, got %+v", out)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestAggregator_AggregateChannel_Error(t *testing.T) {
	agg := New(&model.Config{})
	resultsCh := make(chan *search.BlobResult, 1)
	errCh := make(chan error, 1)

	expectedErr := errors.New("pipeline failed")
	errCh <- expectedErr
	close(resultsCh)

	var out *AggregatedResults
	var err error
	runWithin(t, 5*time.Second, func() { out, err = agg.AggregateChannel(context.Background(), resultsCh, errCh) })
	if out != nil {
		t.Errorf("expected nil results on error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestAggregator_AggregateChannel_BlobError(t *testing.T) {
	agg := New(&model.Config{})
	resultsCh := make(chan *search.BlobResult, 1)
	errCh := make(chan error)

	blobErr := errors.New("blob decompression error")
	resultsCh <- &search.BlobResult{Error: blobErr}
	close(resultsCh)

	var out *AggregatedResults
	var err error
	runWithin(t, 5*time.Second, func() { out, err = agg.AggregateChannel(context.Background(), resultsCh, errCh) })
	if out != nil {
		t.Errorf("expected nil results on blob error")
	}
	if !errors.Is(err, blobErr) {
		t.Errorf("expected %v, got %v", blobErr, err)
	}
}

// A *search.BlobReadError is a soft failure: the result is skipped and the
// remaining results still aggregate (regression #10).
func TestAggregator_AggregateChannel_SkipsBlobReadError(t *testing.T) {
	agg := New(&model.Config{})
	resultsCh := make(chan *search.BlobResult, 3)
	errCh := make(chan error)

	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	resultsCh <- &search.BlobResult{
		BlobOID:     "good1",
		Matches:     []model.SearchMatch{{LineNum: 1, LineText: "needle"}},
		Occurrences: []model.BlobOccurrence{{Path: "a.txt", CommitSHA: "c1", CommitDate: t1}},
	}
	resultsCh <- &search.BlobResult{
		BlobOID:     "bad",
		Occurrences: []model.BlobOccurrence{{Path: "gone.txt", CommitSHA: "c1", CommitDate: t1}},
		Error:       &search.BlobReadError{OID: "bad", Path: "gone.txt", Err: gitengine.ErrObjectNotFound},
	}
	resultsCh <- &search.BlobResult{
		BlobOID:     "good2",
		Matches:     []model.SearchMatch{{LineNum: 2, LineText: "needle again"}},
		Occurrences: []model.BlobOccurrence{{Path: "b.txt", CommitSHA: "c1", CommitDate: t1}},
	}
	close(resultsCh)
	close(errCh)

	out, err := agg.AggregateChannel(context.Background(), resultsCh, errCh)
	if err != nil {
		t.Fatalf("unreadable blob must not abort aggregation, got: %v", err)
	}
	if out == nil || out.TotalFiles != 2 || out.TotalMatches != 2 {
		t.Fatalf("expected 2 files / 2 matches from the readable blobs, got %+v", out)
	}
	for _, f := range out.Files {
		if f.Path == "gone.txt" {
			t.Errorf("skipped blob must not appear in aggregated files")
		}
	}
}

// runWithin runs fn on another goroutine and fails if it has not returned by the
// deadline, so a regression that blocks forever surfaces as a test failure instead of
// hanging the whole binary.
func runWithin(t *testing.T, d time.Duration, fn func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("AggregateChannel did not return within %s", d)
	}
}

// requireGoroutineBaseline polls the goroutine count back down to baseline, the way
// test/leak_test.go polls for leaks, and fails if a producer is still parked.
func requireGoroutineBaseline(t *testing.T, baseline int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= baseline {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count did not return to baseline %d (still %d): the producer is still blocked on an abandoned results channel", baseline, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// requireDrained asserts that resultsCh was consumed to close before AggregateChannel
// returned, i.e. that the producer was released synchronously rather than abandoned.
func requireDrained(t *testing.T, resultsCh <-chan *search.BlobResult) {
	t.Helper()

	select {
	case _, ok := <-resultsCh:
		if ok {
			t.Error("results channel still carries buffered results: producer was abandoned mid-stream")
		}
	default:
		t.Error("results channel was neither drained nor closed before returning")
	}
}

// stubObjectReader serves blobs from memory so a real search.Pipeline can be driven
// without a repository on disk.
type stubObjectReader struct {
	objects map[string]*gitengine.Object
}

func (s *stubObjectReader) ReadObject(oid string) (*gitengine.Object, error) {
	if obj, ok := s.objects[oid]; ok {
		return obj, nil
	}
	return nil, gitengine.ErrObjectNotFound
}

func (s *stubObjectReader) HasObject(oid string) bool {
	_, ok := s.objects[oid]
	return ok
}

func (s *stubObjectReader) Close() error { return nil }

// newStreamingPipeline builds a real pipeline over n in-memory matching blobs. n is
// chosen by the caller to exceed the pipeline's bounded results buffer so its workers
// must block on their sends, which is what makes abandoning the channel fatal.
func newStreamingPipeline(t *testing.T, n int) (*search.Pipeline, []model.BlobOccurrence, *model.Config) {
	t.Helper()

	objects := make(map[string]*gitengine.Object, n)
	occurrences := make([]model.BlobOccurrence, 0, n)
	commitDate := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	for i := range n {
		oid := fmt.Sprintf("%040d", i)
		data := fmt.Sprintf("first line\nNEEDLE_AGG %d\nlast line\n", i)
		objects[oid] = &gitengine.Object{
			OID:  oid,
			Type: gitengine.TypeBlob,
			Size: int64(len(data)),
			Data: []byte(data),
		}
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:    oid,
			Path:       fmt.Sprintf("dir/file_%d.txt", i),
			CommitSHA:  fmt.Sprintf("%040x", i),
			CommitDate: commitDate,
		})
	}

	cfg := &model.Config{Pattern: "NEEDLE_AGG", CaseSensitive: true}
	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("failed creating matcher: %v", err)
	}

	return search.NewPipeline(&stubObjectReader{objects: objects}, matcher, cfg), occurrences, cfg
}

// Returning early must not abandon the producer. A real pipeline blocks its workers on
// a bounded results channel, so a consumer that walks away without draining wedges the
// worker pool, its dispatcher, and the object store they hold forever (regression #15).
func TestAggregator_AggregateChannel_ReleasesProducerOnEarlyReturn(t *testing.T) {
	const blobs = 256 // well above max(32, NumCPU*4), the pipeline's results buffer

	t.Run("producer error", func(t *testing.T) {
		pipeline, occurrences, cfg := newStreamingPipeline(t, blobs)
		baseline := runtime.NumGoroutine()

		// The pipeline keeps its own context: it is not cancelled when the consumer
		// gives up, so only draining can unblock it.
		resultsCh, _ := pipeline.ExecuteContext(context.Background(), occurrences)

		fatal := errors.New("repository closed under the search")
		errCh := make(chan error, 1)
		errCh <- fatal
		close(errCh)

		var out *AggregatedResults
		var err error
		runWithin(t, 30*time.Second, func() {
			out, err = New(cfg).AggregateChannel(context.Background(), resultsCh, errCh)
		})

		if !errors.Is(err, fatal) {
			t.Fatalf("expected %v, got %v", fatal, err)
		}
		if out != nil {
			t.Errorf("expected nil results alongside the error, got %+v", out)
		}
		requireDrained(t, resultsCh)
		requireGoroutineBaseline(t, baseline)
	})

	t.Run("consumer cancellation", func(t *testing.T) {
		pipeline, occurrences, cfg := newStreamingPipeline(t, blobs)
		baseline := runtime.NumGoroutine()

		resultsCh, errCh := pipeline.ExecuteContext(context.Background(), occurrences)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var err error
		runWithin(t, 30*time.Second, func() {
			_, err = New(cfg).AggregateChannel(ctx, resultsCh, errCh)
		})

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		requireDrained(t, resultsCh)
		requireGoroutineBaseline(t, baseline)
	})
}

// selectCountingContext counts how often its Done channel is requested. AggregateChannel
// evaluates ctx.Done() exactly once per pass through its select, which turns "the loop
// busy-polls an exhausted channel" into an observable number.
type selectCountingContext struct {
	context.Context
	done   chan struct{}
	passes atomic.Int64
}

func newSelectCountingContext() *selectCountingContext {
	return &selectCountingContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *selectCountingContext) Done() <-chan struct{} {
	c.passes.Add(1)
	return c.done
}

// A producer is free to close errCh before it has finished streaming results. A receive
// on a closed channel is ready forever, so the exhausted arm has to be disabled or the
// loop burns a core until the producer catches up (regression #15).
func TestAggregator_AggregateChannel_ClosedErrChIsNotPolled(t *testing.T) {
	const (
		results = 5
		gap     = 20 * time.Millisecond
	)

	resultsCh := make(chan *search.BlobResult)
	errCh := make(chan error)
	close(errCh) // producer reports "no failures" up front, then keeps streaming

	commitDate := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	go func() {
		defer close(resultsCh)
		for i := range results {
			time.Sleep(gap)
			resultsCh <- &search.BlobResult{
				BlobOID:     fmt.Sprintf("blob%d", i),
				Matches:     []model.SearchMatch{{LineNum: 1, LineText: "needle"}},
				Occurrences: []model.BlobOccurrence{{Path: fmt.Sprintf("f%d.txt", i), CommitSHA: "c1", CommitDate: commitDate}},
			}
		}
	}()

	ctx := newSelectCountingContext()
	agg := New(&model.Config{})

	var out *AggregatedResults
	var err error
	runWithin(t, 30*time.Second, func() { out, err = agg.AggregateChannel(ctx, resultsCh, errCh) })

	if err != nil {
		t.Fatalf("a closed error channel reports success, got: %v", err)
	}
	if out == nil || out.TotalFiles != results {
		t.Fatalf("expected %d files aggregated after errCh closed, got %+v", results, out)
	}
	// One pass per delivered result plus a handful for the two closes; a spinning loop
	// racks up millions over the same 100ms of streaming.
	if passes := ctx.passes.Load(); passes > 1000 {
		t.Errorf("select was re-entered %d times for %d results: the closed error channel is being polled", passes, results)
	}
}

// Nothing obliges a producer to publish its terminal error before closing resultsCh, so
// an error that arrives afterwards must still be surfaced rather than reported as a
// successful search (regression #15).
func TestAggregator_AggregateChannel_ErrorAfterResultsClosed(t *testing.T) {
	agg := New(&model.Config{})
	resultsCh := make(chan *search.BlobResult, 1)
	errCh := make(chan error)

	commitDate := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	resultsCh <- &search.BlobResult{
		BlobOID:     "blob1",
		Matches:     []model.SearchMatch{{LineNum: 1, LineText: "needle"}},
		Occurrences: []model.BlobOccurrence{{Path: "a.txt", CommitSHA: "c1", CommitDate: commitDate}},
	}
	close(resultsCh)

	fatal := errors.New("history walk aborted")
	go func() {
		defer close(errCh)
		// Ordered the other way round from the pipeline: results first, error second.
		time.Sleep(20 * time.Millisecond)
		errCh <- fatal
	}()

	var out *AggregatedResults
	var err error
	runWithin(t, 30*time.Second, func() { out, err = agg.AggregateChannel(context.Background(), resultsCh, errCh) })

	if !errors.Is(err, fatal) {
		t.Fatalf("error published after resultsCh closed must be surfaced, got err=%v out=%+v", err, out)
	}
	if out != nil {
		t.Errorf("expected nil results alongside the error, got %+v", out)
	}
}
