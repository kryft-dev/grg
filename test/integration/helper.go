package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	grgBinary   string
	grgBuildErr error
	buildOnce   sync.Once
)

func getGRGBinary(t testing.TB) string {
	buildOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "grg-test-bin-*")
		if err != nil {
			grgBuildErr = fmt.Errorf("failed to create temp dir: %w", err)
			return
		}
		binPath := filepath.Join(tmpDir, "grg")
		cmd := exec.Command("go", "build", "-o", binPath, "github.com/kryft-dev/grg/cmd/grg")
		if out, err := cmd.CombinedOutput(); err != nil {
			grgBuildErr = fmt.Errorf("failed to build grg: %w\nOutput: %s", err, out)
			return
		}
		grgBinary = binPath
	})
	if grgBuildErr != nil {
		t.Fatalf("grg binary build failed: %v", grgBuildErr)
	}
	return grgBinary
}

// ExecResult contains the output and exit status of a grg execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// CommitOptions configures a commit with specific metadata and timestamps.
type CommitOptions struct {
	Message        string
	Files          map[string]string
	AuthorName     string
	AuthorEmail    string
	AuthorDate     time.Time
	CommitterName  string
	CommitterEmail string
	CommitterDate  time.Time
}

// TestRepo wraps a temporary Git repository for integration testing.
type TestRepo struct {
	t   *testing.T
	Dir string
}

// NewTestRepo initializes a clean Git repository in a temporary directory.
func NewTestRepo(t *testing.T) *TestRepo {
	t.Helper()
	dir := t.TempDir()

	repo := &TestRepo{t: t, Dir: dir}
	repo.Git("init", "-b", "main")
	repo.Git("config", "user.name", "Test Author")
	repo.Git("config", "user.email", "author@example.com")
	repo.Git("config", "commit.gpgsign", "false")
	return repo
}

// Git executes a git command in the test repository directory.
func (r *TestRepo) Git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=Test Author",
		"GIT_AUTHOR_EMAIL=author@example.com",
		"GIT_COMMITTER_NAME=Test Committer",
		"GIT_COMMITTER_EMAIL=committer@example.com",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v failed: %v\nOutput: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// WriteFile writes a text file at relPath inside the test repo.
func (r *TestRepo) WriteFile(relPath, content string) {
	r.t.Helper()
	absPath := filepath.Join(r.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		r.t.Fatalf("failed creating directory for %s: %v", relPath, err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		r.t.Fatalf("failed writing file %s: %v", relPath, err)
	}
}

// WriteBinaryFile writes a binary byte slice to relPath inside the test repo.
func (r *TestRepo) WriteBinaryFile(relPath string, content []byte) {
	r.t.Helper()
	absPath := filepath.Join(r.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		r.t.Fatalf("failed creating directory for %s: %v", relPath, err)
	}
	if err := os.WriteFile(absPath, content, 0644); err != nil {
		r.t.Fatalf("failed writing binary file %s: %v", relPath, err)
	}
}

// Commit writes the specified files, stages them, and creates a commit.
func (r *TestRepo) Commit(msg string, files map[string]string) string {
	r.t.Helper()
	for p, c := range files {
		r.WriteFile(p, c)
	}
	r.Git("add", ".")
	r.Git("commit", "-m", msg)
	return r.Git("rev-parse", "HEAD")
}

// CommitWithOptions creates a commit with custom author, committer, and dates.
func (r *TestRepo) CommitWithOptions(opt CommitOptions) string {
	r.t.Helper()
	for p, c := range opt.Files {
		r.WriteFile(p, c)
	}
	r.Git("add", ".")

	authorName := opt.AuthorName
	if authorName == "" {
		authorName = "Test Author"
	}
	authorEmail := opt.AuthorEmail
	if authorEmail == "" {
		authorEmail = "author@example.com"
	}
	committerName := opt.CommitterName
	if committerName == "" {
		committerName = authorName
	}
	committerEmail := opt.CommitterEmail
	if committerEmail == "" {
		committerEmail = authorEmail
	}

	cmd := exec.Command("git", "commit", "-m", opt.Message)
	cmd.Dir = r.Dir
	env := append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		fmt.Sprintf("GIT_AUTHOR_NAME=%s", authorName),
		fmt.Sprintf("GIT_AUTHOR_EMAIL=%s", authorEmail),
		fmt.Sprintf("GIT_COMMITTER_NAME=%s", committerName),
		fmt.Sprintf("GIT_COMMITTER_EMAIL=%s", committerEmail),
	)
	if !opt.AuthorDate.IsZero() {
		dateStr := opt.AuthorDate.Format(time.RFC3339)
		env = append(env, fmt.Sprintf("GIT_AUTHOR_DATE=%s", dateStr))
	}
	if !opt.CommitterDate.IsZero() {
		dateStr := opt.CommitterDate.Format(time.RFC3339)
		env = append(env, fmt.Sprintf("GIT_COMMITTER_DATE=%s", dateStr))
	}
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git commit failed: %v\nOutput: %s", err, out)
	}
	return r.Git("rev-parse", "HEAD")
}

// CreateBranch creates and checks out a new branch.
func (r *TestRepo) CreateBranch(name string) {
	r.t.Helper()
	r.Git("checkout", "-b", name)
}

// Checkout switches to the specified branch or ref.
func (r *TestRepo) Checkout(name string) {
	r.t.Helper()
	r.Git("checkout", name)
}

// Merge merges branch into the current HEAD using a merge commit (--no-ff).
func (r *TestRepo) Merge(branch, msg string) string {
	r.t.Helper()
	r.Git("merge", "--no-ff", "-m", msg, branch)
	return r.Git("rev-parse", "HEAD")
}

// RenameFile moves a file using git mv.
func (r *TestRepo) RenameFile(oldPath, newPath string) {
	r.t.Helper()
	absNew := filepath.Join(r.Dir, newPath)
	_ = os.MkdirAll(filepath.Dir(absNew), 0755)
	r.Git("mv", oldPath, newPath)
}

// RemoveFile removes a file using git rm.
func (r *TestRepo) RemoveFile(relPath string) {
	r.t.Helper()
	r.Git("rm", relPath)
}

// GC packs objects into packfiles using git gc.
func (r *TestRepo) GC(aggressive bool) {
	r.t.Helper()
	if aggressive {
		r.Git("gc", "--aggressive", "--prune=now")
	} else {
		r.Git("gc", "--prune=now")
	}
}

// Run executes grg inside the repository directory.
func (r *TestRepo) Run(args ...string) ExecResult {
	r.t.Helper()
	bin := getGRGBinary(r.t)

	cmd := exec.Command(bin, args...)
	cmd.Dir = r.Dir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return ExecResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
		Err:      err,
	}
}

// RunSuccess executes grg and asserts exit code is 0 (match found).
func (r *TestRepo) RunSuccess(args ...string) ExecResult {
	r.t.Helper()
	res := r.Run(args...)
	if res.ExitCode != 0 {
		r.t.Fatalf("expected grg to exit 0, got %d.\nStderr: %s\nStdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}
	return res
}

// RunNoMatch executes grg and asserts exit code is 1 (no match found).
func (r *TestRepo) RunNoMatch(args ...string) ExecResult {
	r.t.Helper()
	res := r.Run(args...)
	if res.ExitCode != 1 {
		r.t.Fatalf("expected grg to exit 1, got %d.\nStderr: %s\nStdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}
	return res
}
