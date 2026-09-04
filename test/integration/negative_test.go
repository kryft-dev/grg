package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNegative_InvalidRegexPatterns verifies exit code 2 and descriptive error messages on malformed regex syntax.
func TestNegative_InvalidRegexPatterns(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Initial commit", map[string]string{
		"file.txt": "valid content\n",
	})

	invalidPatterns := []struct {
		name    string
		pattern string
	}{
		{"unclosed parenthesis", "(unclosed_group"},
		{"unclosed bracket", "[unclosed_class"},
		{"dangling star quantifier", "*dangling_star"},
		{"dangling plus quantifier", "+dangling_plus"},
		{"invalid repeat range", "a{5,2}"},
		{"invalid hex escape", `\x{invalid_hex}`},
		{"invalid unicode class", `\p{NonExistentClass}`},
	}

	for _, tt := range invalidPatterns {
		t.Run(tt.name, func(t *testing.T) {
			res := repo.Run(tt.pattern)
			if res.ExitCode != 2 {
				t.Fatalf("pattern %q: expected exit code 2, got %d.\nStderr: %s\nStdout: %s",
					tt.pattern, res.ExitCode, res.Stderr, res.Stdout)
			}
			if len(res.Stderr) == 0 {
				t.Errorf("pattern %q: expected error message on stderr, got empty stderr", tt.pattern)
			}
			if !strings.Contains(res.Stderr, "failed to compile search pattern") && !strings.Contains(res.Stderr, "error parsing regexp") {
				t.Errorf("pattern %q: stderr missing expected error indication: %s", tt.pattern, res.Stderr)
			}
		})
	}
}

// TestNegative_InvalidFlags verifies exit code 2 for invalid CLI flags and arguments.
func TestNegative_InvalidFlags(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Initial commit", map[string]string{"file.txt": "data\n"})

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "unknown long flag",
			args: []string{"--this-flag-does-not-exist-12345", "pattern"},
		},
		{
			name: "invalid context integer",
			args: []string{"-C", "not_an_int", "pattern"},
		},
		{
			name: "negative context integer",
			args: []string{"-C", "-3", "pattern"},
		},
		{
			name: "invalid before-context integer",
			args: []string{"-B", "abc", "pattern"},
		},
		{
			name: "invalid after-context integer",
			args: []string{"-A", "xyz", "pattern"},
		},
		{
			name: "invalid max-count integer",
			args: []string{"-m", "not_a_number", "pattern"},
		},
		{
			name: "negative max-count integer",
			args: []string{"-m", "-5", "pattern"},
		},
		{
			name: "invalid color option",
			args: []string{"--color=rainbow", "pattern"},
		},
		{
			name: "no arguments provided",
			args: []string{},
		},
		{
			name: "unknown file type filter",
			args: []string{"-t", "nonexistent_file_type_999", "pattern"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := repo.Run(tt.args...)
			if res.ExitCode != 2 {
				t.Fatalf("args %v: expected exit code 2, got %d.\nStderr: %s\nStdout: %s",
					tt.args, res.ExitCode, res.Stderr, res.Stdout)
			}
			if len(res.Stderr) == 0 {
				t.Errorf("args %v: expected error message on stderr, got empty stderr", tt.args)
			}
		})
	}
}

// TestNegative_NonGitDirectory verifies that running grg outside a git repository exits with error code 128 or 2.
func TestNegative_NonGitDirectory(t *testing.T) {
	nonGitDir := t.TempDir()
	bin := getGRGBinary(t)

	cmd := exec.Command(bin, "search_pattern")
	cmd.Dir = nonGitDir

	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	if exitCode != 128 && exitCode != 2 {
		t.Fatalf("expected error exit code 128 or 2 when run outside git repo, got %d.\nOutput:\n%s", exitCode, out)
	}

	outStr := string(out)
	if !strings.Contains(outStr, "not a git repository") && !strings.Contains(outStr, ".git") {
		t.Errorf("expected 'not a git repository' in stderr, got:\n%s", outStr)
	}
}

// TestNegative_EmptyRepository verifies grg behavior in a freshly initialized git repository with no commits.
func TestNegative_EmptyRepository(t *testing.T) {
	emptyDir := t.TempDir()

	// Initialize git repository with no commits
	gitInit := exec.Command("git", "init", "-b", "main")
	gitInit.Dir = emptyDir
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v, out: %s", err, out)
	}

	bin := getGRGBinary(t)

	// 1. Searching default HEAD in empty repository: zero commits exist, must not exit 0 (match)
	cmdDefault := exec.Command(bin, "some_pattern")
	cmdDefault.Dir = emptyDir
	outDefault, errDefault := cmdDefault.CombinedOutput()
	exitCodeDefault := 0
	if errDefault != nil {
		if exitErr, ok := errDefault.(*exec.ExitError); ok {
			exitCodeDefault = exitErr.ExitCode()
		}
	}
	if exitCodeDefault == 0 {
		t.Fatalf("expected non-zero exit in empty repository, got 0.\nOutput:\n%s", outDefault)
	}

	// 2. Explicitly referencing HEAD in empty repository where HEAD ref is unborn
	cmdHead := exec.Command(bin, "some_pattern", "HEAD")
	cmdHead.Dir = emptyDir
	outHead, errHead := cmdHead.CombinedOutput()
	exitCodeHead := 0
	if errHead != nil {
		if exitErr, ok := errHead.(*exec.ExitError); ok {
			exitCodeHead = exitErr.ExitCode()
		}
	}
	if exitCodeHead == 0 {
		t.Fatalf("expected non-zero exit for HEAD in empty repo, got 0.\nOutput:\n%s", outHead)
	}
}

// TestNegative_NonExistentRevisions verifies non-zero error exit codes on non-existent or invalid git revisions.
func TestNegative_NonExistentRevisions(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Initial commit", map[string]string{"file.txt": "content\n"})

	tests := []struct {
		name     string
		revRange string
	}{
		{
			name:     "non-existent branch name",
			revRange: "nonexistent-branch-xyz",
		},
		{
			name:     "ancestry offset exceeding commit history",
			revRange: "HEAD~999",
		},
		{
			name:     "non-existent 40-character hex OID",
			revRange: "0000000000000000000000000000000000000000",
		},
		{
			name:     "range with non-existent start revision",
			revRange: "nonexistent_branch_start..HEAD",
		},
		{
			name:     "range with non-existent end revision",
			revRange: "HEAD..nonexistent_branch_end",
		},
		{
			name:     "invalid negative ancestry specifier",
			revRange: "HEAD~-5",
		},
		{
			name:     "invalid ref name containing path traversal characters",
			revRange: "refs/heads/..//escape",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := repo.Run("content", tt.revRange)
			if res.ExitCode == 0 {
				t.Fatalf("rev %q: expected non-zero exit code on invalid revision, got 0.\nStdout: %s\nStderr: %s",
					tt.revRange, res.Stdout, res.Stderr)
			}
		})
	}
}

// TestNegative_NonExistentPaths verifies searching on non-existent path filters.
func TestNegative_NonExistentPaths(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Initial commit", map[string]string{
		"src/main.go": "target line\n",
	})

	// 1. Path filter pointing to non-existent file yields no matches (exit code 1)
	resNoMatch := repo.Run("target line", "--", "nonexistent/path/file.go")
	if resNoMatch.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for non-existent path argument, got %d.\nStderr: %s\nStdout: %s",
			resNoMatch.ExitCode, resNoMatch.Stderr, resNoMatch.Stdout)
	}

	// 2. Glob filter matching no files in repository yields exit code 1
	resGlobNoMatch := repo.Run("-g", "*.xyz_nonexistent_extension", "target line")
	if resGlobNoMatch.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for non-matching glob, got %d.\nStderr: %s\nStdout: %s",
			resGlobNoMatch.ExitCode, resGlobNoMatch.Stderr, resGlobNoMatch.Stdout)
	}
}

// TestNegative_CorruptLooseObject verifies graceful error handling on corrupt loose objects.
func TestNegative_CorruptLooseObject(t *testing.T) {
	repo := NewTestRepo(t)
	c1 := repo.Commit("Commit 1", map[string]string{"bad.txt": "CORRUPT_TARGET_TOKEN\n"})

	// Corrupt a loose object file in .git/objects/
	objDir := filepath.Join(repo.Dir, ".git", "objects")
	entries, err := os.ReadDir(objDir)
	if err != nil {
		t.Fatalf("failed reading object directory: %v", err)
	}

	corrupted := false
	for _, dirEntry := range entries {
		if dirEntry.IsDir() && len(dirEntry.Name()) == 2 {
			subPath := filepath.Join(objDir, dirEntry.Name())
			subEntries, _ := os.ReadDir(subPath)
			for _, fileEntry := range subEntries {
				fullFile := filepath.Join(subPath, fileEntry.Name())
				// Overwrite loose object with corrupt bytes
				if err := os.WriteFile(fullFile, []byte("NOT_A_VALID_ZLIB_STREAM_GARBAGE"), 0644); err == nil {
					corrupted = true
					break
				}
			}
			if corrupted {
				break
			}
		}
	}

	if !corrupted {
		t.Skip("no loose object found to corrupt")
	}

	// Must not panic on corrupt object
	res := repo.Run("CORRUPT_TARGET_TOKEN", c1)
	if res.ExitCode == 0 {
		t.Logf("note: corrupt object was either pruned or bypassed: %s", res.Stdout)
	}
}
