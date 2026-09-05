package integration

import (
	"strings"
	"testing"
	"time"
)

// TestFilter_SinceAndUntil verifies filtering commits by date boundaries.
func TestFilter_SinceAndUntil(t *testing.T) {
	repo := NewTestRepo(t)

	dEarly := time.Date(2023, 1, 15, 12, 0, 0, 0, time.UTC)
	dMid := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
	dLate := time.Date(2023, 11, 15, 12, 0, 0, 0, time.UTC)

	repo.CommitWithOptions(CommitOptions{
		Message:    "Early commit",
		Files:      map[string]string{"early.txt": "PHASE_EARLY_TOKEN\n"},
		AuthorDate: dEarly,
	})

	repo.CommitWithOptions(CommitOptions{
		Message:    "Mid commit",
		Files:      map[string]string{"mid.txt": "PHASE_MID_TOKEN\n"},
		AuthorDate: dMid,
	})

	repo.CommitWithOptions(CommitOptions{
		Message:    "Late commit",
		Files:      map[string]string{"late.txt": "PHASE_LATE_TOKEN\n"},
		AuthorDate: dLate,
	})

	// 1. --since "2023-05-01": matches mid and late, excludes early
	resSince := repo.RunSuccess("--color=never", "--since", "2023-05-01", "PHASE_")
	if !strings.Contains(resSince.Stdout, "PHASE_MID_TOKEN") || !strings.Contains(resSince.Stdout, "PHASE_LATE_TOKEN") {
		t.Errorf("expected mid and late tokens in --since output, got:\n%s", resSince.Stdout)
	}
	if strings.Contains(resSince.Stdout, "PHASE_EARLY_TOKEN") {
		t.Errorf("did not expect early token in --since output, got:\n%s", resSince.Stdout)
	}

	// 2. --until "2023-07-01": matches early and mid, excludes late
	resUntil := repo.RunSuccess("--color=never", "--until", "2023-07-01", "PHASE_")
	if !strings.Contains(resUntil.Stdout, "PHASE_EARLY_TOKEN") || !strings.Contains(resUntil.Stdout, "PHASE_MID_TOKEN") {
		t.Errorf("expected early and mid tokens in --until output, got:\n%s", resUntil.Stdout)
	}
	if strings.Contains(resUntil.Stdout, "PHASE_LATE_TOKEN") {
		t.Errorf("did not expect late token in --until output, got:\n%s", resUntil.Stdout)
	}

	// 3. --since "2023-05-01" --until "2023-07-01": matches only mid
	resBounded := repo.RunSuccess("--color=never", "--since", "2023-05-01", "--until", "2023-07-01", "PHASE_")
	if !strings.Contains(resBounded.Stdout, "PHASE_MID_TOKEN") {
		t.Errorf("expected mid token in bounded date search, got:\n%s", resBounded.Stdout)
	}
	if strings.Contains(resBounded.Stdout, "PHASE_EARLY_TOKEN") || strings.Contains(resBounded.Stdout, "PHASE_LATE_TOKEN") {
		t.Errorf("only mid token should match bounded range, got:\n%s", resBounded.Stdout)
	}
}

// TestFilter_AuthorAndCommitter verifies filtering commits by author and committer regex.
func TestFilter_AuthorAndCommitter(t *testing.T) {
	repo := NewTestRepo(t)

	repo.CommitWithOptions(CommitOptions{
		Message:       "Alice's work",
		Files:         map[string]string{"alice.txt": "ALICE_WORK_PAYLOAD\n"},
		AuthorName:    "Alice Wonder",
		AuthorEmail:   "alice@wonderland.org",
		CommitterName: "Alice Wonder",
	})

	repo.CommitWithOptions(CommitOptions{
		Message:        "Bob's feature landed by Bot",
		Files:          map[string]string{"bob.txt": "BOB_WORK_PAYLOAD\n"},
		AuthorName:     "Bob Builder",
		AuthorEmail:    "bob@buildit.com",
		CommitterName:  "CI Deployment Bot",
		CommitterEmail: "bot@ci.example.com",
	})

	repo.CommitWithOptions(CommitOptions{
		Message:       "Charlie's fix",
		Files:         map[string]string{"charlie.txt": "CHARLIE_WORK_PAYLOAD\n"},
		AuthorName:    "Charlie Root",
		AuthorEmail:   "charlie@root.net",
		CommitterName: "Charlie Root",
	})

	// Author filter: --author "Alice"
	resAlice := repo.RunSuccess("--color=never", "--author", "Alice", "ALICE_WORK_PAYLOAD")
	if !strings.Contains(resAlice.Stdout, "alice.txt") {
		t.Errorf("expected alice.txt in --author Alice output, got:\n%s", resAlice.Stdout)
	}

	// Author filter excluding non-matching commit
	repo.RunNoMatch("--author", "Alice", "BOB_WORK_PAYLOAD")

	// Author email regex filter
	resBobEmail := repo.RunSuccess("--color=never", "--author", "buildit\\.com", "BOB_WORK_PAYLOAD")
	if !strings.Contains(resBobEmail.Stdout, "bob.txt") {
		t.Errorf("expected bob.txt in --author email output, got:\n%s", resBobEmail.Stdout)
	}

	// Committer filter: --committer "CI Deployment Bot"
	resBot := repo.RunSuccess("--color=never", "--committer", "Deployment Bot", "BOB_WORK_PAYLOAD")
	if !strings.Contains(resBot.Stdout, "bob.txt") {
		t.Errorf("expected bob.txt in --committer output, got:\n%s", resBot.Stdout)
	}

	// Committer filter excluding other committers
	repo.RunNoMatch("--committer", "Deployment Bot", "ALICE_WORK_PAYLOAD")
}

// TestFilter_Glob verifies file path filtering with glob patterns including negations.
func TestFilter_Glob(t *testing.T) {
	repo := NewTestRepo(t)

	repo.Commit("Add diverse files", map[string]string{
		"src/main.go":          "COMMON_GLOB_NEEDLE\n",
		"src/main_test.go":     "COMMON_GLOB_NEEDLE\n",
		"docs/guide.md":        "COMMON_GLOB_NEEDLE\n",
		"config/settings.json": "COMMON_GLOB_NEEDLE\n",
	})

	// 1. Inclusion glob: -g "*.go"
	resGo := repo.RunSuccess("--color=never", "-g", "*.go", "COMMON_GLOB_NEEDLE")
	if !strings.Contains(resGo.Stdout, "src/main.go") || !strings.Contains(resGo.Stdout, "src/main_test.go") {
		t.Errorf("expected Go files in -g *.go output, got:\n%s", resGo.Stdout)
	}
	if strings.Contains(resGo.Stdout, "docs/guide.md") || strings.Contains(resGo.Stdout, "config/settings.json") {
		t.Errorf("did not expect non-Go files in -g *.go output, got:\n%s", resGo.Stdout)
	}

	// 2. Exclusion glob: -g "!*_test.go"
	resNoTest := repo.RunSuccess("--color=never", "-g", "!*_test.go", "COMMON_GLOB_NEEDLE")
	if strings.Contains(resNoTest.Stdout, "src/main_test.go") {
		t.Errorf("did not expect main_test.go in negated glob output, got:\n%s", resNoTest.Stdout)
	}
	if !strings.Contains(resNoTest.Stdout, "src/main.go") || !strings.Contains(resNoTest.Stdout, "docs/guide.md") {
		t.Errorf("expected non-test files in negated glob output, got:\n%s", resNoTest.Stdout)
	}

	// 3. Combined inclusion + exclusion: -g "*.go" -g "!*_test.go"
	resCombined := repo.RunSuccess("--color=never", "-g", "*.go", "-g", "!*_test.go", "COMMON_GLOB_NEEDLE")
	if !strings.Contains(resCombined.Stdout, "src/main.go") {
		t.Errorf("expected src/main.go in combined glob output, got:\n%s", resCombined.Stdout)
	}
	if strings.Contains(resCombined.Stdout, "src/main_test.go") ||
		strings.Contains(resCombined.Stdout, "docs/guide.md") ||
		strings.Contains(resCombined.Stdout, "config/settings.json") {
		t.Errorf("expected only src/main.go in combined glob output, got:\n%s", resCombined.Stdout)
	}
}

// TestFilter_Type verifies filtering files by predefined file types (-t/--type).
func TestFilter_Type(t *testing.T) {
	repo := NewTestRepo(t)

	repo.Commit("Add multi-type tree", map[string]string{
		"server.go":   "TYPE_FILTER_PAYLOAD\n",
		"README.md":   "TYPE_FILTER_PAYLOAD\n",
		"config.json": "TYPE_FILTER_PAYLOAD\n",
		"style.css":   "TYPE_FILTER_PAYLOAD\n",
	})

	// Type go
	resGo := repo.RunSuccess("--color=never", "-t", "go", "TYPE_FILTER_PAYLOAD")
	if !strings.Contains(resGo.Stdout, "server.go") {
		t.Errorf("expected server.go in -t go output, got:\n%s", resGo.Stdout)
	}
	if strings.Contains(resGo.Stdout, "README.md") || strings.Contains(resGo.Stdout, "config.json") {
		t.Errorf("unexpected non-Go file in -t go output, got:\n%s", resGo.Stdout)
	}

	// Type md / markdown
	resMd := repo.RunSuccess("--color=never", "-t", "md", "TYPE_FILTER_PAYLOAD")
	if !strings.Contains(resMd.Stdout, "README.md") {
		t.Errorf("expected README.md in -t md output, got:\n%s", resMd.Stdout)
	}
	if strings.Contains(resMd.Stdout, "server.go") {
		t.Errorf("unexpected server.go in -t md output, got:\n%s", resMd.Stdout)
	}

	// Type json
	resJSON := repo.RunSuccess("--color=never", "-t", "json", "TYPE_FILTER_PAYLOAD")
	if !strings.Contains(resJSON.Stdout, "config.json") {
		t.Errorf("expected config.json in -t json output, got:\n%s", resJSON.Stdout)
	}
	if strings.Contains(resJSON.Stdout, "server.go") || strings.Contains(resJSON.Stdout, "README.md") {
		t.Errorf("unexpected files in -t json output, got:\n%s", resJSON.Stdout)
	}
}
