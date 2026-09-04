package gitengine

import (
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
	err := walker.Walk(func(occ model.BlobOccurrence) error {
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
	_ = walkerAlice.Walk(func(occ model.BlobOccurrence) error {
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
	_ = walkerGo.Walk(func(occ model.BlobOccurrence) error {
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
	_ = walkerRange.Walk(func(occ model.BlobOccurrence) error {
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
