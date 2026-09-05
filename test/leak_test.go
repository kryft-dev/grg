package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
	"github.com/kryft-dev/grg/internal/output"
	"github.com/kryft-dev/grg/internal/search"
)

// verifyNoGoroutineLeaks polls runtime.Stack for up to 1 second to confirm zero leaked grg goroutines.
func verifyNoGoroutineLeaks(t *testing.T) {
	t.Helper()

	if lingering := awaitNoGRGGoroutines(1 * time.Second); len(lingering) > 0 {
		t.Fatalf("detected %d leaked goroutines:\n%s", len(lingering), strings.Join(lingering, "\n\n"))
	}
}

// requireNoLingeringGoroutines asserts that no grg goroutine survives what, and is
// meant to be called mid-test while the caller still holds an uncancelled context.
// It proves the component under test unwound the pipeline itself instead of being
// rescued by the test's deferred cancel.
func requireNoLingeringGoroutines(t *testing.T, what string) {
	t.Helper()

	if lingering := awaitNoGRGGoroutines(1 * time.Second); len(lingering) > 0 {
		t.Fatalf("%s left %d goroutines live:\n%s", what, len(lingering), strings.Join(lingering, "\n\n"))
	}
}

// awaitNoGRGGoroutines polls for up to timeout and returns the grg goroutines that
// were still running when it gave up, or nil once there are none. Polling absorbs
// the short window in which a correctly unwinding pipeline is still tearing down.
func awaitNoGRGGoroutines(timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for {
		lingering := getLingeringGRGGoroutines()
		if len(lingering) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return lingering
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// getLingeringGRGGoroutines inspects a complete stack dump and returns any active grg goroutines.
func getLingeringGRGGoroutines() []string {
	return grgGoroutinesInDump(fullStackDump())
}

// fullStackDump returns a stack dump of every goroutine, complete rather than truncated.
//
// runtime.Stack truncates silently: it fills the buffer, returns len(buf), and
// reports no error, so a fixed buffer turns the biggest leak into the shortest
// report - exactly when the detector is needed most. n < len(buf) is the only
// available proof that the whole dump was written, because a return of exactly
// len(buf) is ambiguous (a dump that happened to end on the boundary looks
// identical to a truncated one), so the loop doubles the buffer and retries until
// the result is strictly shorter than it. go.uber.org/goleak does this for us, but
// grg depends only on github.com/klauspost/compress and this is not worth a
// dependency.
func fullStackDump() string {
	buf := make([]byte, 64*1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// grgGoroutinesInDump splits a stack dump into its per-goroutine sections and
// returns those running grg code, excluding the goroutine performing the check.
func grgGoroutinesInDump(raw string) []string {
	var leaked []string
	sections := strings.Split(raw, "\n\n")

	for _, sec := range sections {
		sec = strings.TrimSpace(sec)
		if sec == "" {
			continue
		}

		// Only look for grg package goroutines
		if !strings.Contains(sec, "github.com/kryft-dev/grg") {
			continue
		}

		// Exclude the current goroutine running the check or test runner
		if strings.Contains(sec, "getLingeringGRGGoroutines") ||
			strings.Contains(sec, "grgGoroutinesInDump") ||
			strings.Contains(sec, "fullStackDump") ||
			strings.Contains(sec, "awaitNoGRGGoroutines") ||
			strings.Contains(sec, "verifyNoGoroutineLeaks") ||
			strings.Contains(sec, "requireNoLingeringGoroutines") ||
			strings.Contains(sec, "testing.(*T).Run") ||
			strings.Contains(sec, "testing.tRunner") {
			continue
		}

		leaked = append(leaked, sec)
	}

	return leaked
}

type memoryObjectReader struct {
	objects map[string]*gitengine.Object
}

func (m *memoryObjectReader) ReadObject(oid string) (*gitengine.Object, error) {
	if obj, ok := m.objects[oid]; ok {
		return obj, nil
	}
	return nil, fmt.Errorf("object %s not found", oid)
}

func (m *memoryObjectReader) HasObject(oid string) bool {
	_, ok := m.objects[oid]
	return ok
}

func (m *memoryObjectReader) Close() error {
	return nil
}

// TestGoroutineLeak_SearchPipeline_Normal verifies no goroutines leak after standard pipeline search.
func TestGoroutineLeak_SearchPipeline_Normal(t *testing.T) {
	defer verifyNoGoroutineLeaks(t)

	objects := make(map[string]*gitengine.Object)
	var occurrences []model.BlobOccurrence

	for i := 0; i < 50; i++ {
		oid := fmt.Sprintf("%040d", i)
		data := fmt.Sprintf("Line 1 in object %d\nMATCH_TARGET_LEAK_TEST\nLine 3 in object %d\n", i, i)
		objects[oid] = &gitengine.Object{
			OID:  oid,
			Type: gitengine.TypeBlob,
			Size: int64(len(data)),
			Data: []byte(data),
		}
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("dir/file_%d.txt", i),
			CommitSHA: "abcdef0123456789abcdef0123456789abcdef01",
		})
	}

	reader := &memoryObjectReader{objects: objects}
	cfg := &model.Config{
		Pattern:       "MATCH_TARGET_LEAK_TEST",
		CaseSensitive: true,
	}

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("failed creating matcher: %v", err)
	}

	pipeline := search.NewPipeline(reader, matcher, cfg)
	results, err := pipeline.Execute(occurrences)
	if err != nil {
		t.Fatalf("pipeline Execute failed: %v", err)
	}

	if len(results) != 50 {
		t.Fatalf("expected 50 results, got %d", len(results))
	}
}

// TestGoroutineLeak_SearchPipeline_ContextCancellation verifies no goroutines leak when context is cancelled.
func TestGoroutineLeak_SearchPipeline_ContextCancellation(t *testing.T) {
	defer verifyNoGoroutineLeaks(t)

	objects := make(map[string]*gitengine.Object)
	var occurrences []model.BlobOccurrence

	for i := 0; i < 200; i++ {
		oid := fmt.Sprintf("%040d", i)
		data := fmt.Sprintf("Line 1 in object %d\nCANCEL_MATCH_TOKEN\nLine 3 in object %d\n", i, i)
		objects[oid] = &gitengine.Object{
			OID:  oid,
			Type: gitengine.TypeBlob,
			Size: int64(len(data)),
			Data: []byte(data),
		}
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("cancel/file_%d.txt", i),
			CommitSHA: "1234567890abcdef1234567890abcdef12345678",
		})
	}

	reader := &memoryObjectReader{objects: objects}
	cfg := &model.Config{
		Pattern:       "CANCEL_MATCH_TOKEN",
		CaseSensitive: true,
	}

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("failed creating matcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pipeline := search.NewPipeline(reader, matcher, cfg)
	resultsCh, errCh := pipeline.ExecuteContext(ctx, occurrences)

	// Read a few results and then cancel context
	count := 0
	for res := range resultsCh {
		if res != nil {
			count++
			if count >= 5 {
				cancel()
				break
			}
		}
	}

	// Drain remaining channels after cancellation
	for range resultsCh {
	}
	for range errCh {
	}
}

// TestGoroutineLeak_SearchPipeline_QuietMode verifies early termination in quiet mode does not leak workers.
func TestGoroutineLeak_SearchPipeline_QuietMode(t *testing.T) {
	defer verifyNoGoroutineLeaks(t)

	objects := make(map[string]*gitengine.Object)
	var occurrences []model.BlobOccurrence

	// 100 tasks; matches in the first few will cause quiet early-stop
	for i := 0; i < 100; i++ {
		oid := fmt.Sprintf("%040d", i)
		data := "Line 1\nEARLY_QUIET_MATCH_TOKEN\nLine 3\n"
		objects[oid] = &gitengine.Object{
			OID:  oid,
			Type: gitengine.TypeBlob,
			Size: int64(len(data)),
			Data: []byte(data),
		}
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("dir/quiet_%d.txt", i),
			CommitSHA: "1122334455667788990011223344556677889900",
		})
	}

	reader := &memoryObjectReader{objects: objects}
	cfg := &model.Config{
		Pattern:       "EARLY_QUIET_MATCH_TOKEN",
		CaseSensitive: true,
		Quiet:         true,
	}

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("failed creating matcher: %v", err)
	}

	pipeline := search.NewPipeline(reader, matcher, cfg)
	results, err := pipeline.Execute(occurrences)
	if err != nil {
		t.Fatalf("pipeline Execute failed in quiet mode: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected at least 1 match in quiet mode, got 0")
	}
}

// TestGoroutineLeak_SearchPipeline_EmptyOccurrences verifies no goroutines are created or leaked on empty input.
func TestGoroutineLeak_SearchPipeline_EmptyOccurrences(t *testing.T) {
	defer verifyNoGoroutineLeaks(t)

	reader := &memoryObjectReader{objects: make(map[string]*gitengine.Object)}
	cfg := &model.Config{
		Pattern: "SOME_PATTERN",
	}

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("failed creating matcher: %v", err)
	}

	pipeline := search.NewPipeline(reader, matcher, cfg)
	results, err := pipeline.Execute(nil)
	if err != nil {
		t.Fatalf("execute failed on empty occurrences: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results on nil occurrences, got %d", len(results))
	}
}

// TestGoroutineLeak_SearchPipeline_BinaryObjects verifies mixed text/binary blobs don't leak goroutines.
func TestGoroutineLeak_SearchPipeline_BinaryObjects(t *testing.T) {
	defer verifyNoGoroutineLeaks(t)

	objects := make(map[string]*gitengine.Object)
	var occurrences []model.BlobOccurrence

	for i := 0; i < 30; i++ {
		oid := fmt.Sprintf("%040d", i)
		var data []byte
		if i%2 == 0 {
			// Binary payload with null byte
			data = []byte(fmt.Sprintf("binary\x00header\x00TOKEN_BINARY_TEST\x00payload_%d", i))
		} else {
			// Normal text payload
			data = []byte("text line 1\nTOKEN_BINARY_TEST match\ntext line 2\n")
		}

		objects[oid] = &gitengine.Object{
			OID:  oid,
			Type: gitengine.TypeBlob,
			Size: int64(len(data)),
			Data: data,
		}
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("file_%d.bin", i),
			CommitSHA: "aabbccddeeff00112233445566778899aabbccdd",
		})
	}

	reader := &memoryObjectReader{objects: objects}
	cfg := &model.Config{
		Pattern:       "TOKEN_BINARY_TEST",
		CaseSensitive: true,
	}

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("failed creating matcher: %v", err)
	}

	pipeline := search.NewPipeline(reader, matcher, cfg)
	results, err := pipeline.Execute(occurrences)
	if err != nil {
		t.Fatalf("pipeline Execute failed: %v", err)
	}

	if len(results) != 30 {
		t.Fatalf("expected 30 results for mixed binary/text, got %d", len(results))
	}
}

// TestGoroutineLeak_EndToEndLifecycle verifies that executing the complete search lifecycle in-process leaves zero goroutines.
func TestGoroutineLeak_EndToEndLifecycle(t *testing.T) {
	defer verifyNoGoroutineLeaks(t)

	tempDir := t.TempDir()

	// Create test git repo with real commits
	gitRun := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v, out: %s", args, err, out)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "commit.gpgsign", "false")

	filePath := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("LIFECYCLE_LEAK_TARGET_STRING\nSecond line\n"), 0644); err != nil {
		t.Fatalf("failed writing file: %v", err)
	}
	gitRun("add", ".")
	gitRun("commit", "-m", "Initial test commit")

	// Execute full in-process grg search pipeline
	repoInfo, err := gitengine.DiscoverFrom(tempDir)
	if err != nil {
		t.Fatalf("DiscoverFrom failed: %v", err)
	}

	repoReader, err := gitengine.NewRepositoryReader(repoInfo)
	if err != nil {
		t.Fatalf("NewRepositoryReader failed: %v", err)
	}
	defer repoReader.Close()

	cfg := &model.Config{
		Pattern:       "LIFECYCLE_LEAK_TARGET_STRING",
		CaseSensitive: true,
		LineNumber:    true,
	}

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}

	walker := gitengine.NewHistoryWalker(repoInfo, repoReader, cfg, nil)
	var occurrences []model.BlobOccurrence
	err = walker.Walk(context.Background(), func(occ model.BlobOccurrence) error {
		occurrences = append(occurrences, occ)
		return nil
	})
	if err != nil {
		t.Fatalf("walker.Walk failed: %v", err)
	}

	if len(occurrences) == 0 {
		t.Fatalf("expected at least 1 blob occurrence, got 0")
	}

	pipeline := search.NewPipeline(repoReader, matcher, cfg)
	results, err := pipeline.Execute(occurrences)
	if err != nil {
		t.Fatalf("pipeline.Execute failed: %v", err)
	}

	agg := aggregator.New(cfg)
	aggregated := agg.Aggregate(results)

	var outBuf bytes.Buffer
	formatter := output.NewFormatter(cfg)
	if err := formatter.Format(context.Background(), &outBuf, aggregated); err != nil {
		t.Fatalf("formatter.Format failed: %v", err)
	}

	if !strings.Contains(outBuf.String(), "LIFECYCLE_LEAK_TARGET_STRING") {
		t.Fatalf("expected search output to contain match token, got:\n%s", outBuf.String())
	}
}

// pipelineResultBuffer mirrors the capacity search.Pipeline gives its results
// channel: max(32, NumCPU*4). A caller that walks away from the pipeline only
// wedges its workers once more results are outstanding than that buffer can hold,
// so an abandonment test with fewer blobs than this proves nothing.
func pipelineResultBuffer() int {
	if n := runtime.NumCPU() * 4; n > 32 {
		return n
	}
	return 32
}

// matchingBlobs builds n distinct in-memory blobs that all contain token, plus one
// occurrence per blob in dispatch order. Every blob matches, so the pipeline
// publishes a result for every task and a test can reason exactly about how many
// results are outstanding.
func matchingBlobs(n int, token string) (map[string]*gitengine.Object, []model.BlobOccurrence) {
	objects := make(map[string]*gitengine.Object, n)
	occurrences := make([]model.BlobOccurrence, 0, n)

	for i := range n {
		oid := fmt.Sprintf("%040d", i)
		data := fmt.Sprintf("line 1 of blob %d\n%s\nline 3 of blob %d\n", i, token, i)
		objects[oid] = &gitengine.Object{
			OID:  oid,
			Type: gitengine.TypeBlob,
			Size: int64(len(data)),
			Data: []byte(data),
		}
		occurrences = append(occurrences, model.BlobOccurrence{
			BlobOID:   oid,
			Path:      fmt.Sprintf("dir/file_%d.txt", i),
			CommitSHA: "abcdef0123456789abcdef0123456789abcdef01",
		})
	}

	return objects, occurrences
}

// scriptedObjectReader serves blobs from memory, counts reads, and runs an optional
// hook before each one so a test can block, cancel, or panic at a chosen point in
// the run. objects is never written after construction, so concurrent reads by the
// pipeline's workers are safe.
type scriptedObjectReader struct {
	objects map[string]*gitengine.Object
	reads   atomic.Int64
	before  func(oid string, read int64)
}

func (r *scriptedObjectReader) ReadObject(oid string) (*gitengine.Object, error) {
	read := r.reads.Add(1)
	if r.before != nil {
		r.before(oid, read)
	}
	if obj, ok := r.objects[oid]; ok {
		return obj, nil
	}
	return nil, fmt.Errorf("object %s not found", oid)
}

func (r *scriptedObjectReader) HasObject(oid string) bool {
	_, ok := r.objects[oid]
	return ok
}

func (r *scriptedObjectReader) Close() error {
	return nil
}

// newLeakTestPipeline wires a pipeline over reader for a case-sensitive search for
// pattern, with the supplied config tweaks already applied.
func newLeakTestPipeline(t *testing.T, reader gitengine.ObjectReader, cfg *model.Config) *search.Pipeline {
	t.Helper()

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("failed creating matcher: %v", err)
	}
	return search.NewPipeline(reader, matcher, cfg)
}

// waitFor polls cond for up to 5 seconds so a test can wait for a concurrent
// precondition - a filled channel buffer, a read count - instead of sleeping and
// hoping it was long enough.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if cond() {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// parkDeep recurses depth frames and then parks until stop is closed, so the
// goroutine contributes a deep section to every stack dump taken in the meantime.
// Go neither inlines a recursive call nor eliminates tail calls, so the frames are
// genuinely live.
func parkDeep(depth int, parked *sync.WaitGroup, stop <-chan struct{}) {
	if depth > 0 {
		parkDeep(depth-1, parked, stop)
		return
	}
	parked.Done()
	<-stop
}

// TestLeakDetector_GrowsBufferUntilDumpFits proves the growable dump is not
// cosmetic. With enough parked goroutines the dump outgrows the fixed 128 KiB
// buffer the detector used to allocate, and runtime.Stack says nothing about it:
// the fixed-size view then underreports a population this test knows exactly,
// while the growable dump sees all of it.
func TestLeakDetector_GrowsBufferUntilDumpFits(t *testing.T) {
	const (
		parkedGoroutines = 64
		stackDepth       = 64
	)

	var parked, exited sync.WaitGroup
	stop := make(chan struct{})

	// Registered first, so it runs last: the padding goroutines must be gone before
	// the leak check, or they are indistinguishable from a leak.
	defer verifyNoGoroutineLeaks(t)
	defer func() {
		close(stop)
		exited.Wait()
	}()

	parked.Add(parkedGoroutines)
	exited.Add(parkedGoroutines)
	for range parkedGoroutines {
		go func() {
			defer exited.Done()
			parkDeep(stackDepth, &parked, stop)
		}()
	}
	parked.Wait()

	// The buffer the detector used to allocate. runtime.Stack reports no error on
	// overflow, so a return of exactly len(buf) is the only hint that it truncated.
	fixed := make([]byte, 128*1024)
	n := runtime.Stack(fixed, true)
	if n != len(fixed) {
		t.Fatalf("dump of %d parked goroutines fit in %d bytes (n=%d): raise parkedGoroutines or stackDepth so this test still exercises truncation",
			parkedGoroutines, len(fixed), n)
	}

	truncated := grgGoroutinesInDump(string(fixed[:n]))
	complete := grgGoroutinesInDump(fullStackDump())

	if len(truncated) >= parkedGoroutines {
		t.Fatalf("fixed 128 KiB dump reported %d grg goroutines; expected it to miss some of the %d parked ones",
			len(truncated), parkedGoroutines)
	}
	if len(complete) < parkedGoroutines {
		t.Fatalf("growable dump reported %d grg goroutines; expected at least the %d parked ones",
			len(complete), parkedGoroutines)
	}

	t.Logf("fixed 128 KiB dump saw %d of %d parked goroutines; growable dump saw %d",
		len(truncated), parkedGoroutines, len(complete))
}

// TestGoroutineLeak_SearchPipeline_AbandonWithoutDraining covers the shape that
// actually wedges the pipeline and that every other test here misses: a caller that
// takes one result, walks away without draining, and only cancels afterwards. Once
// more results are outstanding than the results buffer holds, the workers are
// parked on `resultsCh <- res`, whose only escape is ctx.Done - so a late cancel has
// to be enough to unwind the whole run.
func TestGoroutineLeak_SearchPipeline_AbandonWithoutDraining(t *testing.T) {
	bufSize := pipelineResultBuffer()
	objects, occurrences := matchingBlobs(bufSize*4, "ABANDON_MATCH_TOKEN")
	if len(occurrences) <= bufSize {
		t.Fatalf("precondition: need more blobs than the %d-result buffer to leave workers blocked on a send, got %d",
			bufSize, len(occurrences))
	}

	reader := &scriptedObjectReader{objects: objects}
	pipeline := newLeakTestPipeline(t, reader, &model.Config{
		Pattern:       "ABANDON_MATCH_TOKEN",
		CaseSensitive: true,
	})

	defer verifyNoGoroutineLeaks(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultsCh, _ := pipeline.ExecuteContext(ctx, occurrences)

	// Exactly one result, then abandon the channels: no drain, and no cancel yet.
	if res := <-resultsCh; res == nil {
		t.Fatalf("expected a result from the pipeline, got nil")
	}

	// Wait until the run is genuinely wedged - more blobs read than the buffer can
	// hold means workers are parked on their sends. Cancelling before that would
	// exercise nothing.
	waitFor(t, fmt.Sprintf("the pipeline to read more than its %d-result buffer", bufSize), func() bool {
		return reader.reads.Load() > int64(bufSize)
	})

	wedged := getLingeringGRGGoroutines()
	if len(wedged) == 0 {
		t.Fatalf("expected the abandoned pipeline to still hold goroutines before cancel")
	}
	t.Logf("abandoned pipeline holds %d live grg goroutines (%d blobs read) before cancel",
		len(wedged), reader.reads.Load())

	// The deferred verifyNoGoroutineLeaks now asserts that this alone frees every
	// worker, the dispatcher, and the closer, with nobody draining the channels.
	cancel()
}

// TestGoroutineLeak_SearchPipeline_FatalErrorAbandonsRun covers the fatal-error
// path: the first fatal result must cancel the pipeline's own derived context,
// abandoning the remaining blobs instead of searching them all, and must leave
// nothing running.
//
// A reader error becomes a soft *search.BlobReadError, so a recovered panic is the
// only fatal result the pipeline can currently produce; that is what drives the
// path here. Reads of the other blobs park until the test has taken the fatal error
// off errCh, which makes the abandonment assertion exact instead of a race: no
// worker can deliver anything while the fatal error is in flight.
func TestGoroutineLeak_SearchPipeline_FatalErrorAbandonsRun(t *testing.T) {
	bufSize := pipelineResultBuffer()
	total := bufSize * 4
	objects, occurrences := matchingBlobs(total, "FATAL_MATCH_TOKEN")
	poison := occurrences[0].BlobOID

	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	reader := &scriptedObjectReader{
		objects: objects,
		before: func(oid string, _ int64) {
			if oid == poison {
				panic("poisoned blob exploded")
			}
			<-release
		},
	}
	pipeline := newLeakTestPipeline(t, reader, &model.Config{
		Pattern:       "FATAL_MATCH_TOKEN",
		CaseSensitive: true,
	})

	defer verifyNoGoroutineLeaks(t)
	// Runs before the leak check on every path, including t.Fatalf, so parked
	// workers can never be mistaken for a leak.
	defer releaseOnce()

	resultsCh, errCh := pipeline.ExecuteContext(context.Background(), occurrences)

	err := <-errCh
	if err == nil {
		t.Fatalf("expected the fatal blob to be reported on errCh")
	}
	if !strings.Contains(err.Error(), "panic searching blob") {
		t.Fatalf("expected a contained panic on errCh, got %v", err)
	}

	releaseOnce()

	delivered := 0
	for range resultsCh {
		delivered++
	}
	if delivered == 0 {
		t.Fatalf("expected the fatal result to be delivered on resultsCh")
	}
	if delivered >= total {
		t.Fatalf("pipeline delivered all %d results after a fatal error; expected it to abandon the run", delivered)
	}
	if reads := reader.reads.Load(); reads >= int64(total) {
		t.Fatalf("pipeline read all %d blobs after a fatal error; expected dispatch to stop", reads)
	}
	t.Logf("fatal error abandoned the run after %d of %d blobs, %d results delivered",
		reader.reads.Load(), total, delivered)
}

// TestGoroutineLeak_SearchPipeline_PanickingTask asserts panic containment: a blob
// whose read panics surfaces as an ordinary error from Execute - naming the blob and
// the panic value - instead of killing the test binary, and leaves no goroutine
// behind.
func TestGoroutineLeak_SearchPipeline_PanickingTask(t *testing.T) {
	defer verifyNoGoroutineLeaks(t)

	objects, occurrences := matchingBlobs(8, "PANIC_MATCH_TOKEN")
	poison := occurrences[3].BlobOID

	reader := &scriptedObjectReader{
		objects: objects,
		before: func(oid string, _ int64) {
			if oid == poison {
				panic("blob 3 exploded")
			}
		},
	}
	pipeline := newLeakTestPipeline(t, reader, &model.Config{
		Pattern:       "PANIC_MATCH_TOKEN",
		CaseSensitive: true,
	})

	results, err := pipeline.Execute(occurrences)
	if err == nil {
		t.Fatalf("expected the panicking blob to surface as an error, got %d results", len(results))
	}
	if results != nil {
		t.Fatalf("expected no results alongside a fatal error, got %d", len(results))
	}
	for _, want := range []string{"panic searching blob", poison, "blob 3 exploded"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to mention %q, got %v", want, err)
		}
	}
}

// TestGoroutineLeak_AggregateChannel_RealPipelineFatalError leak-checks the
// streaming aggregator against a real pipeline instead of hand-built channels,
// which is all internal/aggregator's own tests ever feed it.
//
// The poisoned blob is dispatched first but parks until the test releases it, so by
// the time it panics the results buffer is full and the other workers are blocked
// on `resultsCh <- res`. The panicking worker cannot reach the cancel that would
// unwind the run, because its own result send is blocked behind that full buffer -
// so AggregateChannel draining resultsCh on its early return is the only thing that
// can free the pipeline. That is the behaviour under test.
func TestGoroutineLeak_AggregateChannel_RealPipelineFatalError(t *testing.T) {
	workers := runtime.NumCPU()
	if workers < 2 {
		t.Skip("needs at least 2 workers: one to park on the poisoned blob while the others fill the results buffer")
	}

	bufSize := pipelineResultBuffer()
	total := bufSize * 8
	objects, occurrences := matchingBlobs(total, "AGG_FATAL_TOKEN")
	poison := occurrences[0].BlobOID

	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	reader := &scriptedObjectReader{
		objects: objects,
		before: func(oid string, _ int64) {
			if oid == poison {
				<-release
				panic("poisoned blob exploded")
			}
		},
	}
	cfg := &model.Config{Pattern: "AGG_FATAL_TOKEN", CaseSensitive: true}
	pipeline := newLeakTestPipeline(t, reader, cfg)

	defer verifyNoGoroutineLeaks(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer releaseOnce()

	resultsCh, errCh := pipeline.ExecuteContext(ctx, occurrences)

	// One read for the parked poison, bufSize buffered results, and one blocked send
	// per remaining worker: at that point the pipeline cannot advance on its own.
	waitFor(t, "the pipeline to fill its results buffer and block its workers", func() bool {
		return reader.reads.Load() >= int64(bufSize+workers)
	})
	releaseOnce()

	out, err := aggregator.New(cfg).AggregateChannel(ctx, resultsCh, errCh)
	if err == nil {
		t.Fatalf("expected the fatal blob to abort aggregation, got %+v", out)
	}
	if out != nil {
		t.Fatalf("expected no aggregated results alongside an error, got %+v", out)
	}
	if !strings.Contains(err.Error(), "panic searching blob") {
		t.Fatalf("expected the pipeline's fatal error, got %v", err)
	}

	// Checked before the deferred cancel, so a rescued pipeline cannot pass for a
	// self-unwinding one.
	requireNoLingeringGoroutines(t, "AggregateChannel returning on a fatal error")
}

// TestGoroutineLeak_AggregateChannel_RealPipelineCancelled is the other early
// return out of AggregateChannel: the shared context is cancelled mid-run. The
// cancellation must surface as an error - a cancelled run that reports success is
// worse than one that reports failure - and the pipeline behind it must unwind
// without the test having to rescue it.
func TestGoroutineLeak_AggregateChannel_RealPipelineCancelled(t *testing.T) {
	bufSize := pipelineResultBuffer()
	total := bufSize * 4
	objects, occurrences := matchingBlobs(total, "AGG_CANCEL_TOKEN")

	defer verifyNoGoroutineLeaks(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel from inside the reader on a fixed read index: the pipeline and the
	// aggregator share ctx, so this is exactly the mid-run cancellation the
	// streaming contract is written for, triggered deterministically rather than by
	// a timer.
	const cancelAfterReads = 4
	reader := &scriptedObjectReader{
		objects: objects,
		before: func(_ string, read int64) {
			if read == cancelAfterReads {
				cancel()
			}
		},
	}
	cfg := &model.Config{Pattern: "AGG_CANCEL_TOKEN", CaseSensitive: true}
	pipeline := newLeakTestPipeline(t, reader, cfg)

	resultsCh, errCh := pipeline.ExecuteContext(ctx, occurrences)

	out, err := aggregator.New(cfg).AggregateChannel(ctx, resultsCh, errCh)
	if err == nil {
		t.Fatalf("expected cancellation to be reported, got %+v", out)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if out != nil {
		t.Fatalf("expected no aggregated results alongside a cancellation, got %+v", out)
	}
	if reads := reader.reads.Load(); reads >= int64(total) {
		t.Fatalf("pipeline read all %d blobs after cancellation; expected it to stop early", reads)
	}

	requireNoLingeringGoroutines(t, "AggregateChannel returning on cancellation")
}
