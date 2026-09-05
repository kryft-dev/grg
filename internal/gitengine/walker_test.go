package gitengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kryft-dev/grg/internal/model"
)

func TestHistoryWalker(t *testing.T) {
	reader := newMockReader()

	// Blobs
	blobA1 := reader.put(TypeBlob, []byte("Blob A version 1"))
	blobB1 := reader.put(TypeBlob, []byte("Blob B version 1"))
	blobA2 := reader.put(TypeBlob, []byte("Blob A version 2"))

	// Tree 1 (initial commit):
	// hello.txt -> blobA1
	// src/helper.go -> blobB1
	subTree1 := reader.put(TypeTree, buildTreePayload([]TreeEntry{
		{Mode: 0100644, Name: "helper.go", OID: blobB1},
	}))
	rootTree1 := reader.put(TypeTree, buildTreePayload([]TreeEntry{
		{Mode: 0100644, Name: "hello.txt", OID: blobA1},
		{Mode: 0040000, Name: "src", OID: subTree1},
	}))

	// Commit 1 (initial)
	commit1Raw := fmt.Sprintf(`tree %s
author Alice <alice@example.com> 1600000000 +0000
committer Alice <alice@example.com> 1600000000 +0000

Initial commit
`, rootTree1)
	commit1SHA := reader.put(TypeCommit, []byte(commit1Raw))

	// Tree 2 (second commit):
	// hello.txt -> blobA2 (modified)
	// src/helper.go -> blobB1 (unchanged! Same subtree OID subTree1)
	rootTree2 := reader.put(TypeTree, buildTreePayload([]TreeEntry{
		{Mode: 0100644, Name: "hello.txt", OID: blobA2},
		{Mode: 0040000, Name: "src", OID: subTree1},
	}))

	// Commit 2
	commit2Raw := fmt.Sprintf(`tree %s
parent %s
author Bob <bob@example.com> 1600000100 +0000
committer Bob <bob@example.com> 1600000100 +0000

Update hello.txt
`, rootTree2, commit1SHA)
	commit2SHA := reader.put(TypeCommit, []byte(commit2Raw))

	// Mock git dir with HEAD pointing to commit2
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	_ = os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644)
	_ = os.WriteFile(filepath.Join(gitDir, "refs", "heads", "main"), []byte(commit2SHA+"\n"), 0644)

	repo := &RepoInfo{
		WorkTree:     tmpDir,
		GitDir:       gitDir,
		CommonGitDir: gitDir,
	}

	// 1. Walk entire history without filters
	cfg := &model.Config{}
	walker := NewHistoryWalker(repo, reader, cfg, nil)

	var occurrences []model.BlobOccurrence
	err := walker.Walk(context.Background(), func(occ model.BlobOccurrence) error {
		occurrences = append(occurrences, occ)
		return nil
	})
	if err != nil {
		t.Fatalf("walker.Walk failed: %v", err)
	}

	// In commit 2 (with tree diff), only hello.txt (blobA2) was modified!
	// (src subtree was identical so it was pruned!)
	// In commit 1 (root commit), hello.txt (blobA1) and src/helper.go (blobB1) are introduced.
	// Total occurrences: 3
	if len(occurrences) != 3 {
		t.Fatalf("expected 3 occurrences, got %d: %+v", len(occurrences), occurrences)
	}

	foundA2 := false
	foundA1 := false
	foundB1 := false
	for _, occ := range occurrences {
		if occ.BlobOID == blobA2 && occ.CommitSHA == commit2SHA && occ.Path == "hello.txt" {
			foundA2 = true
		}
		if occ.BlobOID == blobA1 && occ.CommitSHA == commit1SHA && occ.Path == "hello.txt" {
			foundA1 = true
		}
		if occ.BlobOID == blobB1 && occ.CommitSHA == commit1SHA && occ.Path == "src/helper.go" {
			foundB1 = true
		}
	}
	if !foundA2 || !foundA1 || !foundB1 {
		t.Errorf("missing expected occurrence: foundA2=%v, foundA1=%v, foundB1=%v", foundA2, foundA1, foundB1)
	}

	// 2. Author filter: only author Alice
	cfgAlice := &model.Config{
		Author: "Alice",
	}
	walkerAlice := NewHistoryWalker(repo, reader, cfgAlice, nil)
	var aliceOcc []model.BlobOccurrence
	_ = walkerAlice.Walk(context.Background(), func(occ model.BlobOccurrence) error {
		aliceOcc = append(aliceOcc, occ)
		return nil
	})
	// Only commit 1 by Alice should be visited
	for _, occ := range aliceOcc {
		if occ.CommitSHA != commit1SHA {
			t.Errorf("unexpected commit in Alice filtered walk: %s", occ.CommitSHA)
		}
	}

	// 3. Path filter: only *.go files
	walkerGo := NewHistoryWalker(repo, reader, cfg, func(path string) bool {
		return filepath.Ext(path) == ".go"
	})
	var goOcc []model.BlobOccurrence
	_ = walkerGo.Walk(context.Background(), func(occ model.BlobOccurrence) error {
		goOcc = append(goOcc, occ)
		return nil
	})
	if len(goOcc) != 1 || goOcc[0].Path != "src/helper.go" {
		t.Errorf("expected only src/helper.go, got: %+v", goOcc)
	}

	// 4. RevRange: commit1SHA..commit2SHA
	cfgRange := &model.Config{
		RevRange: commit1SHA + ".." + commit2SHA,
	}
	walkerRange := NewHistoryWalker(repo, reader, cfgRange, nil)
	var rangeOcc []model.BlobOccurrence
	_ = walkerRange.Walk(context.Background(), func(occ model.BlobOccurrence) error {
		rangeOcc = append(rangeOcc, occ)
		return nil
	})
	// Range commit1..commit2 includes commit 2 only
	if len(rangeOcc) != 1 || rangeOcc[0].BlobOID != blobA2 {
		t.Errorf("expected only blobA2 in commit1..commit2, got: %+v", rangeOcc)
	}
}

func TestHistoryWalkerDateFilters(t *testing.T) {
	d1, err := ParseFilterDate("2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("ParseFilterDate failed: %v", err)
	}
	if d1.Year() != 2026 {
		t.Errorf("expected 2026, got %d", d1.Year())
	}

	meta := &model.CommitMetadata{
		Date:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Author: "John <john@test.com>",
	}
	cfg := &model.Config{
		Since: "2026-06-01",
	}
	sinceTime, _ := ParseFilterDate(cfg.Since)
	if MatchesCommitFilters(meta, cfg, nil, nil, sinceTime, time.Time{}) {
		t.Errorf("expected commit before sinceTime to be rejected")
	}
}

// countingReader wraps an ObjectReader, counting reads and optionally cancelling a
// context once a given number of reads has been served. It lets a test assert on the
// I/O the walker actually performed rather than only on the error it returned.
type countingReader struct {
	inner  ObjectReader
	reads  int
	tripAt int
	cancel context.CancelFunc
}

func (c *countingReader) ReadObject(oid string) (*Object, error) {
	c.reads++
	if c.tripAt > 0 && c.reads == c.tripAt && c.cancel != nil {
		c.cancel()
	}
	return c.inner.ReadObject(oid)
}

func (c *countingReader) HasObject(oid string) bool { return c.inner.HasObject(oid) }

func (c *countingReader) Close() error { return c.inner.Close() }

// buildLinearHistory creates n chained commits, each replacing f.txt with a fresh blob,
// so every commit contributes exactly one blob occurrence. Returns the repo and the tip SHA.
func buildLinearHistory(t *testing.T, reader *mockObjectReader, n int) (*RepoInfo, string) {
	t.Helper()

	prevSHA := ""
	for i := 0; i < n; i++ {
		blobOID := reader.put(TypeBlob, []byte(fmt.Sprintf("version %d", i)))
		treeOID := reader.put(TypeTree, buildTreePayload([]TreeEntry{
			{Mode: 0100644, Name: "f.txt", OID: blobOID},
		}))

		var raw string
		if prevSHA == "" {
			raw = fmt.Sprintf("tree %s\nauthor A <a@e.com> %d +0000\ncommitter A <a@e.com> %d +0000\n\ncommit %d\n",
				treeOID, 1600000000+i, 1600000000+i, i)
		} else {
			raw = fmt.Sprintf("tree %s\nparent %s\nauthor A <a@e.com> %d +0000\ncommitter A <a@e.com> %d +0000\n\ncommit %d\n",
				treeOID, prevSHA, 1600000000+i, 1600000000+i, i)
		}
		prevSHA = reader.put(TypeCommit, []byte(raw))
	}

	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "heads", "main"), []byte(prevSHA+"\n"), 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}

	return &RepoInfo{WorkTree: tmpDir, GitDir: gitDir, CommonGitDir: gitDir}, prevSHA
}

// Cancellation during commit-DAG collection must abort the walk. This phase reads a
// commit object per DAG node and emits nothing, so before ctx was threaded through
// collectOrderedCommits it was entirely deaf to cancellation.
func TestHistoryWalkerCancelDuringCommitCollection(t *testing.T) {
	const numCommits = 400

	mock := newMockReader()
	repo, _ := buildLinearHistory(t, mock, numCommits)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &countingReader{inner: mock, tripAt: 20, cancel: cancel}
	walker := NewHistoryWalker(repo, reader, &model.Config{}, nil)

	occurrences := 0
	err := walker.Walk(ctx, func(occ model.BlobOccurrence) error {
		occurrences++
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if occurrences != 0 {
		t.Fatalf("cancellation happened during commit collection, before any emission, but %d occurrences were emitted", occurrences)
	}
	// The collection loop pops one commit per iteration and reads one object per pop,
	// so an abort at the loop head must stop within a handful of reads of the trip point.
	if reader.reads > reader.tripAt+5 {
		t.Fatalf("walk kept reading after cancellation: %d reads (cancelled at %d) out of %d commits",
			reader.reads, reader.tripAt, numCommits)
	}
}

// Cancellation once the walk has begun emitting must stop promptly. The callback used
// to be the only cancellation point, and it returns nil here, so the walker itself has
// to observe the cancelled context.
func TestHistoryWalkerCancelMidWalk(t *testing.T) {
	const numCommits = 400

	mock := newMockReader()
	repo, _ := buildLinearHistory(t, mock, numCommits)

	// Baseline: a full, uncancelled walk, to know what "draining the whole DAG" costs.
	baseline := &countingReader{inner: mock}
	baselineOcc := 0
	if err := NewHistoryWalker(repo, baseline, &model.Config{}, nil).Walk(
		context.Background(),
		func(occ model.BlobOccurrence) error {
			baselineOcc++
			return nil
		},
	); err != nil {
		t.Fatalf("baseline walk failed: %v", err)
	}
	if baselineOcc != numCommits {
		t.Fatalf("baseline walk emitted %d occurrences, want %d", baselineOcc, numCommits)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &countingReader{inner: mock}
	walker := NewHistoryWalker(repo, reader, &model.Config{}, nil)

	occurrences := 0
	readsAtCancel := 0
	err := walker.Walk(ctx, func(occ model.BlobOccurrence) error {
		occurrences++
		if occurrences == 1 {
			cancel()
			readsAtCancel = reader.reads
		}
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if occurrences != 1 {
		t.Fatalf("expected the walk to stop after the occurrence that cancelled it, got %d occurrences", occurrences)
	}
	if after := reader.reads - readsAtCancel; after > 5 {
		t.Fatalf("walk performed %d object reads after cancellation (a full drain costs %d)", after, baseline.reads)
	}
	if reader.reads >= baseline.reads {
		t.Fatalf("cancelled walk read %d objects, no better than the full walk's %d", reader.reads, baseline.reads)
	}
}

// The exclude side of an A..B rev-range is a second unbounded commit walk. It must
// abort on cancellation instead of draining the whole ancestry of A.
func TestTraverseExcludeCancellation(t *testing.T) {
	const numCommits = 400

	mock := newMockReader()
	repo, tipSHA := buildLinearHistory(t, mock, numCommits)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &countingReader{inner: mock, tripAt: 10, cancel: cancel}
	walker := NewHistoryWalker(repo, reader, &model.Config{}, nil)

	excluded := make(map[string]bool)
	err := walker.traverseExclude(ctx, tipSHA, excluded)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if len(excluded) >= numCommits {
		t.Fatalf("traverseExclude drained the whole ancestry (%d commits) despite cancellation", len(excluded))
	}
	if reader.reads > reader.tripAt+5 {
		t.Fatalf("traverseExclude kept reading after cancellation: %d reads (cancelled at %d)", reader.reads, reader.tripAt)
	}
}

// Tree traversal must also honour cancellation: a root commit with no parent walks the
// full tree with the callback as its only former cancellation point.
func TestTraverseTreeCancellation(t *testing.T) {
	reader := newMockReader()

	var entries []TreeEntry
	for i := 0; i < 64; i++ {
		entries = append(entries, TreeEntry{
			Mode: 0100644,
			Name: fmt.Sprintf("f%02d.txt", i),
			OID:  reader.put(TypeBlob, []byte(fmt.Sprintf("blob %d", i))),
		})
	}
	rootOID := reader.put(TypeTree, buildTreePayload(entries))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	visited := 0
	err := TraverseTree(ctx, reader, rootOID, func(path string, entry TreeEntry) error {
		visited++
		if visited == 3 {
			cancel()
		}
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if visited != 3 {
		t.Fatalf("expected traversal to stop at the entry that cancelled it, visited %d of %d", visited, len(entries))
	}
}
