package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(nonGitDir); err != nil {
		t.Fatal(err)
	}

	runErr := run([]string{"pattern"})
	if runErr == nil {
		t.Fatalf("expected repo error when run outside git repository")
	}
	if exitCodeForError(runErr) != 2 {
		t.Errorf("expected exit code 2 for repo error, got %d", exitCodeForError(runErr))
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
	defer func() { _ = os.Chdir(origWd) }()

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
	defer func() { _ = os.Chdir(origWd) }()

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

func TestRun_SignalCancellation(t *testing.T) {
	repoDir := setupTestGitRepoWithCommits(t)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(repoDir); err != nil {
		t.Fatal(err)
	}

	t.Run("pre-cancelled context returns exit code 2", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		var outBuf, errBuf bytes.Buffer
		err := runContext(ctx, []string{"pattern"}, &outBuf, &errBuf)
		if err == nil {
			t.Fatalf("expected error from cancelled context")
		}
		if ec := exitCodeForError(err); ec != 2 {
			t.Errorf("expected exit code 2 on cancellation, got %d", ec)
		}
		if isQuietError(err) {
			t.Errorf("expected isQuietError to be false without -q")
		}
	})

	t.Run("pre-cancelled context with quiet flag", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var outBuf, errBuf bytes.Buffer
		err := runContext(ctx, []string{"-q", "pattern"}, &outBuf, &errBuf)
		if err == nil {
			t.Fatalf("expected error from cancelled context")
		}
		if ec := exitCodeForError(err); ec != 2 {
			t.Errorf("expected exit code 2 on cancellation with -q, got %d", ec)
		}
		if !isQuietError(err) {
			t.Errorf("expected isQuietError to be true with -q")
		}
	})

	t.Run("cancellation mid-flight aborts cleanly", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after short delay
		time.AfterFunc(1*time.Millisecond, cancel)

		var outBuf, errBuf bytes.Buffer
		err := runContext(ctx, []string{"pattern"}, &outBuf, &errBuf)
		// May succeed before 1ms or cancel
		if err != nil {
			if ec := exitCodeForError(err); ec != 2 {
				t.Errorf("expected exit code 2 on mid-flight cancellation, got %d", ec)
			}
		}
	})
}

func TestRun_ArgumentValidation(t *testing.T) {
	repoDir := setupTestGitRepoWithCommits(t)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(repoDir); err != nil {
		t.Fatal(err)
	}

	validationCases := []struct {
		name string
		args []string
	}{
		{"invalid regex", []string{"(unclosed-group"}},
		{"invalid author regex", []string{"--author=[bad", "pattern"}},
		{"invalid committer regex", []string{"--committer=[bad", "pattern"}},
		{"invalid since date", []string{"--since=not-a-date", "pattern"}},
		{"invalid until date", []string{"--until=2024-99-99", "pattern"}},
		{"path traversal in revision", []string{"pattern", "../../../etc/passwd"}},
		{"invalid ancestry in revision", []string{"pattern", "HEAD~invalid"}},
		{"path with null byte", []string{"pattern", "--", "file\x00.txt"}},
	}

	for _, tc := range validationCases {
		t.Run(tc.name, func(t *testing.T) {
			var outBuf, errBuf bytes.Buffer
			err := runContext(context.Background(), tc.args, &outBuf, &errBuf)
			if err == nil {
				t.Fatalf("expected validation error for %v", tc.args)
			}
			if ec := exitCodeForError(err); ec != 2 {
				t.Errorf("expected exit code 2 for validation failure, got %d (err: %v)", ec, err)
			}
		})
	}
}

func TestRun_POSIXShortFlags(t *testing.T) {
	repoDir := setupTestGitRepoWithCommits(t)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(repoDir); err != nil {
		t.Fatal(err)
	}

	// Bundled -inv: ignore-case, line-number, invert-match
	var buf bytes.Buffer
	err = runWithOutput([]string{"--color=never", "-inv", "PATTERN"}, &buf)
	if err != nil {
		t.Fatalf("expected success with -inv, got %v", err)
	}
	// Inverted match of "PATTERN" (case insensitive) should find line 1 and line 3
	out := buf.String()
	if !strings.Contains(out, "1:line 1 before") || !strings.Contains(out, "3:line 3 after") {
		t.Errorf("expected inverted match lines in output, got: %s", out)
	}
	if strings.Contains(out, "pattern match line") {
		t.Errorf("expected matching line to be inverted out, got: %s", out)
	}

	// Attached '=' in numeric flag: -C=1
	buf.Reset()
	err = runWithOutput([]string{"--color=never", "-C=1", "pattern"}, &buf)
	if err != nil {
		t.Fatalf("expected success with -C=1, got %v", err)
	}
	if !strings.Contains(buf.String(), "1-line 1 before") {
		t.Errorf("expected context line with -C=1, got: %s", buf.String())
	}

	// -H flag enables heading
	buf.Reset()
	err = runWithOutput([]string{"--color=never", "-H", "pattern"}, &buf)
	if err != nil {
		t.Fatalf("expected success with -H, got %v", err)
	}
	if !strings.Contains(buf.String(), "sample.txt") {
		t.Errorf("expected heading with -H, got: %s", buf.String())
	}
}

func TestRun_SEC03_SubdirectoryPathAnchoring(t *testing.T) {
	repoDir := setupTestGitRepoWithCommits(t)

	// Create a subdirectory with a committed file
	subDir := filepath.Join(repoDir, "subpkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "subfile.txt"), []byte("unique_subpkg_match\n"), 0644); err != nil {
		t.Fatal(err)
	}

	gitCmd := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
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
	gitCmd("add", "subpkg/subfile.txt")
	gitCmd("commit", "-m", "Add subfile")

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	// Change cwd into the subdirectory
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}

	// Searching from within subpkg for "unique_subpkg_match" using relative path "subfile.txt"
	var buf bytes.Buffer
	err = runWithOutput([]string{"--color=never", "unique_subpkg_match", "--", "subfile.txt"}, &buf)
	if err != nil {
		t.Fatalf("expected search to succeed with relative path from subdirectory, got %v", err)
	}
	if !strings.Contains(buf.String(), "unique_subpkg_match") {
		t.Errorf("expected match in output when anchored relative to worktree, got: %s", buf.String())
	}
}
