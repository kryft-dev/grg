package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRun_HelpAndVersion(t *testing.T) {
	if err := run([]string{"-h"}); err != nil {
		t.Errorf("expected -h to succeed, got %v", err)
	}
	if err := run([]string{"--help"}); err != nil {
		t.Errorf("expected --help to succeed, got %v", err)
	}
	if err := run([]string{"-V"}); err != nil {
		t.Errorf("expected -V to succeed, got %v", err)
	}
	if err := run([]string{"--version"}); err != nil {
		t.Errorf("expected --version to succeed, got %v", err)
	}
}

func TestRun_CLIError(t *testing.T) {
	err := run([]string{})
	if err == nil {
		t.Fatalf("expected CLI error for missing pattern")
	}
	if exitCodeForError(err) != 2 {
		t.Errorf("expected exit code 2 for CLI error, got %d", exitCodeForError(err))
	}
}

func TestRun_RepoError(t *testing.T) {
	nonGitDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	if err := os.Chdir(nonGitDir); err != nil {
		t.Fatal(err)
	}

	runErr := run([]string{"pattern"})
	if runErr == nil {
		t.Fatalf("expected repo error when run outside git repository")
	}
	if exitCodeForError(runErr) != 128 {
		t.Errorf("expected exit code 128 for repo error, got %d", exitCodeForError(runErr))
	}
	if !strings.Contains(runErr.Error(), "fatal: not a git repository") {
		t.Errorf("unexpected error message: %v", runErr)
	}
}

func TestRun_Success(t *testing.T) {
	repoDir := setupTestGitRepo(t)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	if err := os.Chdir(repoDir); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"pattern"}); err != nil {
		t.Errorf("expected run to succeed inside git repo, got %v", err)
	}
	if err := run([]string{"-q", "pattern"}); err != nil {
		t.Errorf("expected run with -q to succeed, got %v", err)
	}
}
