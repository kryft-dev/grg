package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestGitRepoWithCommits(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	gitCmd := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice",
			"GIT_AUTHOR_EMAIL=alice@example.com",
			"GIT_COMMITTER_NAME=Alice",
			"GIT_COMMITTER_EMAIL=alice@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\nOutput: %s", args, err, out)
		}
	}

	gitCmd("init", "-b", "main")
	gitCmd("config", "user.name", "Alice")
	gitCmd("config", "user.email", "alice@example.com")

	content := "line 1 before\npattern match line\nline 3 after\n"
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd("add", "sample.txt")
	gitCmd("commit", "-m", "Add sample text")

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

	// Invalid flag
	err = run([]string{"--nonexistent-flag-xyz"})
	if err == nil || exitCodeForError(err) != 2 {
		t.Errorf("expected exit code 2 for invalid flag, got %v", err)
	}

	// Invalid regex
	err = run([]string{"[invalid-regex"})
	if err == nil || exitCodeForError(err) != 2 {
		t.Errorf("expected exit code 2 for invalid regex pattern, got %v", err)
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

func TestRun_ExitCodes(t *testing.T) {
	repoDir := setupTestGitRepoWithCommits(t)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	if err := os.Chdir(repoDir); err != nil {
		t.Fatal(err)
	}

	// Match found -> exit code 0
	err = run([]string{"pattern"})
	if err != nil {
		t.Errorf("expected exit code 0 for match found, got error: %v", err)
	}

	// No match found -> exit code 1
	err = run([]string{"nonexistent_string_12345"})
	if err == nil || exitCodeForError(err) != 1 {
		t.Errorf("expected exit code 1 for no match, got %v", err)
	}

	// Quiet flag with match found -> exit code 0
	var quietBuf bytes.Buffer
	err = runWithOutput([]string{"-q", "pattern"}, &quietBuf)
	if err != nil {
		t.Errorf("expected -q with match to return 0, got %v", err)
	}
	if quietBuf.Len() > 0 {
		t.Errorf("expected -q to produce no stdout, got %q", quietBuf.String())
	}

	// Quiet flag with no match -> exit code 1
	err = runWithOutput([]string{"-q", "nonexistent_string_12345"}, &quietBuf)
	if err == nil || exitCodeForError(err) != 1 {
		t.Errorf("expected -q with no match to return 1, got %v", err)
	}
}

func TestRun_OutputFormats(t *testing.T) {
	repoDir := setupTestGitRepoWithCommits(t)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	if err := os.Chdir(repoDir); err != nil {
		t.Fatal(err)
	}

	t.Run("grouped output", func(t *testing.T) {
		var buf bytes.Buffer
		err := runWithOutput([]string{"--color=never", "pattern"}, &buf)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "sample.txt") {
			t.Errorf("expected path in output, got: %s", out)
		}
		if !strings.Contains(out, "2:pattern match line") {
			t.Errorf("expected line number and match in output, got: %s", out)
		}
		if !strings.Contains(out, "Alice") {
			t.Errorf("expected author in commit subheader, got: %s", out)
		}
	})

	t.Run("single-line output", func(t *testing.T) {
		var buf bytes.Buffer
		err := runWithOutput([]string{"--color=never", "--no-heading", "pattern"}, &buf)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, ":sample.txt:2:pattern match line") {
			t.Errorf("expected <commit>:sample.txt:2:pattern match line, got: %s", out)
		}
	})

	t.Run("files with matches -l", func(t *testing.T) {
		var buf bytes.Buffer
		err := runWithOutput([]string{"--color=never", "-l", "pattern"}, &buf)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		out := strings.TrimSpace(buf.String())
		if !strings.HasSuffix(out, ":sample.txt") {
			t.Errorf("expected <commit>:sample.txt, got: %s", out)
		}
	})

	t.Run("count -c", func(t *testing.T) {
		var buf bytes.Buffer
		err := runWithOutput([]string{"--color=never", "-c", "pattern"}, &buf)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		out := strings.TrimSpace(buf.String())
		if !strings.HasSuffix(out, ":sample.txt:1") {
			t.Errorf("expected <commit>:sample.txt:1, got: %s", out)
		}
	})

	t.Run("context lines -C 1", func(t *testing.T) {
		var buf bytes.Buffer
		err := runWithOutput([]string{"--color=never", "-C", "1", "pattern"}, &buf)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "1-line 1 before") {
			t.Errorf("expected context line before, got: %s", out)
		}
		if !strings.Contains(out, "2:pattern match line") {
			t.Errorf("expected match line, got: %s", out)
		}
		if !strings.Contains(out, "3-line 3 after") {
			t.Errorf("expected context line after, got: %s", out)
		}
	})

	t.Run("glob filter excludes", func(t *testing.T) {
		var buf bytes.Buffer
		err := runWithOutput([]string{"-g", "!*.txt", "pattern"}, &buf)
		if err == nil || exitCodeForError(err) != 1 {
			t.Errorf("expected exit code 1 when file is glob excluded, got %v", err)
		}
	})
}
