package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// truncateLooseObject zeroes the loose object file for the blob at relPath in
// commit rev, reproducing the "missing blob" corruption git fsck reports.
// Loose objects are written read-only, so the file is made writable first.
func truncateLooseObject(t *testing.T, repo *TestRepo, rev, relPath string) string {
	t.Helper()
	oid := repo.Git("rev-parse", rev+":"+relPath)
	objPath := filepath.Join(repo.Dir, ".git", "objects", oid[:2], oid[2:])
	if err := os.Chmod(objPath, 0600); err != nil {
		t.Fatalf("chmod loose object %s: %v", objPath, err)
	}
	if err := os.Truncate(objPath, 0); err != nil {
		t.Fatalf("truncate loose object %s: %v", objPath, err)
	}
	return oid
}

// newCorruptRepo builds a repo where gone.txt was committed and later deleted,
// so its blob is only reachable through history (as in issue #10), and then
// corrupts that blob's loose object. It returns the corrupted blob OID.
func newCorruptRepo(t *testing.T) (*TestRepo, string) {
	t.Helper()
	repo := NewTestRepo(t)
	c1 := repo.Commit("Add both files", map[string]string{
		"keep.txt": "NEEDLE in a healthy blob\n",
		"gone.txt": "NEEDLE in a soon-corrupt blob\nONLY_IN_GONE\n",
	})
	repo.RemoveFile("gone.txt")
	repo.Git("commit", "-m", "Remove gone.txt")

	oid := truncateLooseObject(t, repo, c1, "gone.txt")
	return repo, oid
}

// TestCorruptBlob_SkippedWithWarning verifies that a single unreadable blob no
// longer aborts the search (regression #10): matches from other blobs are still
// printed, the failure is reported once on stderr, and the exit code is 2.
func TestCorruptBlob_SkippedWithWarning(t *testing.T) {
	repo, oid := newCorruptRepo(t)

	res := repo.Run("--color=never", "NEEDLE")
	if res.ExitCode != 2 {
		t.Fatalf("expected exit code 2 after a skipped blob, got %d.\nStderr: %s\nStdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}
	if !strings.Contains(res.Stdout, "keep.txt") || !strings.Contains(res.Stdout, "NEEDLE in a healthy blob") {
		t.Errorf("expected matches from the healthy blob on stdout, got:\n%s", res.Stdout)
	}
	if strings.Contains(res.Stdout, "gone.txt") {
		t.Errorf("skipped blob must not produce matches, stdout:\n%s", res.Stdout)
	}

	warning := "grg: warning: skipping blob " + oid + " (gone.txt):"
	if !strings.Contains(res.Stderr, warning) {
		t.Errorf("expected stderr to contain %q, got:\n%s", warning, res.Stderr)
	}
	if strings.Count(res.Stderr, "grg: warning:") != 1 {
		t.Errorf("expected exactly one warning line, got:\n%s", res.Stderr)
	}
	if strings.Contains(res.Stderr, "grg: failed to read blob") {
		t.Errorf("blob read failure must not be reported as a fatal error, stderr:\n%s", res.Stderr)
	}
	if strings.Contains(res.Stdout, "grg: warning:") {
		t.Errorf("warnings must go to stderr only, stdout:\n%s", res.Stdout)
	}
}

// TestCorruptBlob_NoOtherMatches verifies exit code 2 (not 1) when the only
// potential match lived in the unreadable blob.
func TestCorruptBlob_NoOtherMatches(t *testing.T) {
	repo, oid := newCorruptRepo(t)

	res := repo.Run("--color=never", "ONLY_IN_GONE")
	if res.ExitCode != 2 {
		t.Fatalf("expected exit code 2, got %d.\nStderr: %s\nStdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}
	if strings.TrimSpace(res.Stdout) != "" {
		t.Errorf("expected no stdout, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stderr, oid) {
		t.Errorf("expected stderr warning naming %s, got:\n%s", oid, res.Stderr)
	}
}

// TestCorruptBlob_Quiet verifies ripgrep's --quiet semantics: a match found
// still exits 0 even though a blob was skipped; no match exits 2.
func TestCorruptBlob_Quiet(t *testing.T) {
	repo, _ := newCorruptRepo(t)

	res := repo.Run("-q", "NEEDLE")
	if res.ExitCode != 0 {
		t.Errorf("-q with a match: expected exit 0, got %d.\nStderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stdout != "" {
		t.Errorf("-q must not print matches, got:\n%s", res.Stdout)
	}

	res = repo.Run("-q", "ONLY_IN_GONE")
	if res.ExitCode != 2 {
		t.Errorf("-q with no match and a skipped blob: expected exit 2, got %d.\nStderr: %s", res.ExitCode, res.Stderr)
	}
}

// TestCorruptBlob_HealthyRepoUnaffected guards the baseline: without corruption
// the exit codes and stderr stay exactly as before.
func TestCorruptBlob_HealthyRepoUnaffected(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Add files", map[string]string{
		"keep.txt":  "NEEDLE in a healthy blob\n",
		"other.txt": "nothing to see\n",
	})

	res := repo.RunSuccess("--color=never", "NEEDLE")
	if res.Stderr != "" {
		t.Errorf("expected empty stderr for a healthy repo, got:\n%s", res.Stderr)
	}

	res = repo.RunNoMatch("--color=never", "ABSENT_PATTERN_XYZ")
	if res.Stderr != "" {
		t.Errorf("expected empty stderr for a healthy repo, got:\n%s", res.Stderr)
	}
}
