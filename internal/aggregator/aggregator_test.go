package aggregator

import (
	"testing"
	"time"

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
