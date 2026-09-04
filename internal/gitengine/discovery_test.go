package gitengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper to setup a mock standard git repo
func setupStandardRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0755); err != nil {
		t.Fatalf("failed to create objects dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("failed to create HEAD file: %v", err)
	}
	return tmpDir
}

func TestDiscover_StandardRepo(t *testing.T) {
	root := setupStandardRepo(t)

	// Discover from root
	repo, err := DiscoverFrom(root)
	if err != nil {
		t.Fatalf("expected successful discovery, got err: %v", err)
	}

	if repo.WorkTree != root {
		t.Errorf("expected WorkTree %q, got %q", root, repo.WorkTree)
	}
	expectedGitDir := filepath.Join(root, ".git")
	if repo.GitDir != expectedGitDir {
		t.Errorf("expected GitDir %q, got %q", expectedGitDir, repo.GitDir)
	}
	if repo.CommonGitDir != expectedGitDir {
		t.Errorf("expected CommonGitDir %q, got %q", expectedGitDir, repo.CommonGitDir)
	}
	if repo.IsBare {
		t.Errorf("expected IsBare to be false")
	}
	if repo.IsWorktree {
		t.Errorf("expected IsWorktree to be false")
	}
}

func TestDiscover_FromNestedSubdirectory(t *testing.T) {
	root := setupStandardRepo(t)
	subDir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create nested dirs: %v", err)
	}

	repo, err := DiscoverFrom(subDir)
	if err != nil {
		t.Fatalf("expected successful discovery from nested dir, got err: %v", err)
	}

	if repo.WorkTree != root {
		t.Errorf("expected WorkTree %q, got %q", root, repo.WorkTree)
	}
	if repo.GitDir != filepath.Join(root, ".git") {
		t.Errorf("expected GitDir %q, got %q", filepath.Join(root, ".git"), repo.GitDir)
	}
}

func TestDiscover_Worktree(t *testing.T) {
	// Setup main repo
	mainRepo := setupStandardRepo(t)
	mainGitDir := filepath.Join(mainRepo, ".git")

	// Setup worktree gitdir inside main repo's .git/worktrees/wt1
	wtGitDir := filepath.Join(mainGitDir, "worktrees", "wt1")
	if err := os.MkdirAll(wtGitDir, 0755); err != nil {
		t.Fatalf("failed to create worktree gitdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "HEAD"), []byte("ref: refs/heads/feature\n"), 0644); err != nil {
		t.Fatalf("failed to create wt HEAD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "commondir"), []byte("../..\n"), 0644); err != nil {
		t.Fatalf("failed to write commondir: %v", err)
	}

	// Setup separate worktree directory with .git file
	wtDir := t.TempDir()
	dotGitFile := filepath.Join(wtDir, ".git")
	gitdirContent := "gitdir: " + wtGitDir + "\n"
	if err := os.WriteFile(dotGitFile, []byte(gitdirContent), 0644); err != nil {
		t.Fatalf("failed to write .git file: %v", err)
	}

	repo, err := DiscoverFrom(wtDir)
	if err != nil {
		t.Fatalf("expected successful discovery for worktree, got err: %v", err)
	}

	if repo.WorkTree != wtDir {
		t.Errorf("expected WorkTree %q, got %q", wtDir, repo.WorkTree)
	}
	if repo.GitDir != wtGitDir {
		t.Errorf("expected GitDir %q, got %q", wtGitDir, repo.GitDir)
	}
	// CommonGitDir should resolve to mainGitDir
	if repo.CommonGitDir != mainGitDir {
		t.Errorf("expected CommonGitDir %q, got %q", mainGitDir, repo.CommonGitDir)
	}
	if !repo.IsWorktree {
		t.Errorf("expected IsWorktree to be true")
	}

	// Also discover from worktree subdirectory
	wtSub := filepath.Join(wtDir, "src", "pkg")
	if err := os.MkdirAll(wtSub, 0755); err != nil {
		t.Fatalf("failed to create worktree sub dir: %v", err)
	}
	repoSub, err := DiscoverFrom(wtSub)
	if err != nil {
		t.Fatalf("expected discovery from worktree sub dir to succeed, got %v", err)
	}
	if repoSub.WorkTree != wtDir {
		t.Errorf("expected WorkTree %q, got %q", wtDir, repoSub.WorkTree)
	}
}

func TestDiscover_Submodule(t *testing.T) {
	// Superproject
	mainRepo := setupStandardRepo(t)
	submoduleGitDir := filepath.Join(mainRepo, ".git", "modules", "mysub")
	if err := os.MkdirAll(filepath.Join(submoduleGitDir, "objects"), 0755); err != nil {
		t.Fatalf("failed to create submodule objects: %v", err)
	}
	if err := os.WriteFile(filepath.Join(submoduleGitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("failed to write submodule HEAD: %v", err)
	}

	// Submodule checkout inside superproject
	submoduleDir := filepath.Join(mainRepo, "submodule")
	if err := os.MkdirAll(submoduleDir, 0755); err != nil {
		t.Fatalf("failed to create submodule dir: %v", err)
	}
	// Relative gitdir link
	relGitDir := filepath.Join("..", ".git", "modules", "mysub")
	if err := os.WriteFile(filepath.Join(submoduleDir, ".git"), []byte("gitdir: "+relGitDir+"\n"), 0644); err != nil {
		t.Fatalf("failed to write submodule .git file: %v", err)
	}

	repo, err := DiscoverFrom(submoduleDir)
	if err != nil {
		t.Fatalf("expected successful discovery for submodule, got err: %v", err)
	}

	if repo.WorkTree != submoduleDir {
		t.Errorf("expected WorkTree %q, got %q", submoduleDir, repo.WorkTree)
	}
	if repo.GitDir != submoduleGitDir {
		t.Errorf("expected GitDir %q, got %q", submoduleGitDir, repo.GitDir)
	}
	if repo.CommonGitDir != submoduleGitDir {
		t.Errorf("expected CommonGitDir %q, got %q", submoduleGitDir, repo.CommonGitDir)
	}
}

func TestDiscover_GitDirEnv(t *testing.T) {
	root := setupStandardRepo(t)
	customGitDir := filepath.Join(root, ".git")

	// Outside dir
	outsideDir := t.TempDir()

	repo, err := DiscoverWithEnv(outsideDir, customGitDir, "")
	if err != nil {
		t.Fatalf("expected successful discovery via GIT_DIR, got: %v", err)
	}
	if repo.GitDir != customGitDir {
		t.Errorf("expected GitDir %q, got %q", customGitDir, repo.GitDir)
	}
	if repo.WorkTree != root {
		t.Errorf("expected WorkTree %q, got %q", root, repo.WorkTree)
	}

	// Test non-existent GIT_DIR
	_, err = DiscoverWithEnv(outsideDir, "/non/existent/git/dir", "")
	if err == nil {
		t.Fatalf("expected error for non-existent GIT_DIR")
	}
	if !strings.Contains(err.Error(), "fatal: not a git repository") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDiscover_NotAGitRepository(t *testing.T) {
	emptyDir := t.TempDir()

	_, err := DiscoverFrom(emptyDir)
	if err == nil {
		t.Fatalf("expected error when not inside a git repository, got nil")
	}

	if !strings.Contains(err.Error(), "fatal: not a git repository") {
		t.Errorf("expected 'fatal: not a git repository' error, got %v", err)
	}
}

func TestDiscover_CorruptedRepos(t *testing.T) {
	t.Run("missing HEAD", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0755); err != nil {
			t.Fatal(err)
		}
		// Notice no HEAD created

		_, err := DiscoverFrom(tmpDir)
		if err == nil {
			t.Fatalf("expected error for missing HEAD, got nil")
		}
		if !strings.Contains(err.Error(), "missing or invalid HEAD") {
			t.Errorf("expected missing HEAD error, got %v", err)
		}
	})

	t.Run("missing objects", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
			t.Fatal(err)
		}
		// Notice no objects dir created

		_, err := DiscoverFrom(tmpDir)
		if err == nil {
			t.Fatalf("expected error for missing objects, got nil")
		}
		if !strings.Contains(err.Error(), "missing objects directory") {
			t.Errorf("expected missing objects error, got %v", err)
		}
	})

	t.Run("invalid .git file format", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, ".git"), []byte("something invalid\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := DiscoverFrom(tmpDir)
		if err == nil {
			t.Fatalf("expected error for invalid .git file, got nil")
		}
		if !strings.Contains(err.Error(), "expected 'gitdir: <path>'") {
			t.Errorf("expected invalid format error, got %v", err)
		}
	})

	t.Run(".git file pointing to non-existent target", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, ".git"), []byte("gitdir: /no/such/gitdir\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := DiscoverFrom(tmpDir)
		if err == nil {
			t.Fatalf("expected error for dangling gitdir, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected does not exist error, got %v", err)
		}
	})
}

func TestDiscover_BareRepo(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "objects"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo, err := DiscoverFrom(tmpDir)
	if err != nil {
		t.Fatalf("expected bare repo discovery to succeed, got: %v", err)
	}
	if !repo.IsBare {
		t.Errorf("expected IsBare to be true")
	}
	if repo.WorkTree != "" {
		t.Errorf("expected empty WorkTree for bare repo, got %q", repo.WorkTree)
	}
	if repo.GitDir != tmpDir {
		t.Errorf("expected GitDir %q, got %q", tmpDir, repo.GitDir)
	}
}
