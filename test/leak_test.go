package test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

	deadline := time.Now().Add(1 * time.Second)
	var lingering []string

	for time.Now().Before(deadline) {
		lingering = getLingeringGRGGoroutines()
		if len(lingering) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(lingering) > 0 {
		t.Fatalf("detected %d leaked goroutines:\n%s", len(lingering), strings.Join(lingering, "\n\n"))
	}
}

// getLingeringGRGGoroutines inspects runtime stack traces and returns any active grg goroutines.
func getLingeringGRGGoroutines() []string {
	buf := make([]byte, 128*1024)
	n := runtime.Stack(buf, true)
	raw := string(buf[:n])

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
			strings.Contains(sec, "verifyNoGoroutineLeaks") ||
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
