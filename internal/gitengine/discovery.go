package gitengine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepoInfo contains discovered Git repository paths and structure.
type RepoInfo struct {
	WorkTree     string // Working tree root directory (empty string for bare repo)
	GitDir       string // Path to the .git directory (or gitdir for worktree/submodule)
	CommonGitDir string // Path to common git directory where objects/ and refs/ reside
	IsBare       bool   // True if this is a bare Git repository
	IsWorktree   bool   // True if this is a linked worktree or submodule
}

// Discover discovers and validates the Git repository from the current working directory
// and environment variables ($GIT_DIR, $GIT_WORK_TREE).
func Discover() (*RepoInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}
	return DiscoverWithEnv(cwd, os.Getenv("GIT_DIR"), os.Getenv("GIT_WORK_TREE"))
}

// DiscoverFrom discovers and validates the Git repository starting at startDir.
func DiscoverFrom(startDir string) (*RepoInfo, error) {
	return DiscoverWithEnv(startDir, "", "")
}

// DiscoverWithEnv discovers and validates the Git repository with explicit environment overrides.
func DiscoverWithEnv(startDir, gitDirEnv, gitWorkTreeEnv string) (*RepoInfo, error) {
	absStart, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for '%s': %w", startDir, err)
	}

	// 1. If $GIT_DIR is specified, honor it directly
	if gitDirEnv != "" {
		gitDir := gitDirEnv
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Clean(filepath.Join(absStart, gitDir))
		}

		fi, err := os.Stat(gitDir)
		if err != nil {
			return nil, fmt.Errorf("fatal: not a git repository: '%s': %w", gitDirEnv, err)
		}

		// If GIT_DIR points to a file, resolve gitdir link
		if !fi.IsDir() {
			target, err := readGitDirFile(gitDir, filepath.Dir(gitDir))
			if err != nil {
				return nil, err
			}
			gitDir = target
			fi, err = os.Stat(gitDir)
			if err != nil || !fi.IsDir() {
				return nil, fmt.Errorf("fatal: gitdir '%s' referenced in '%s' does not exist or is not a directory", target, gitDirEnv)
			}
		}

		commonGitDir := resolveCommonDir(gitDir)

		workTree := ""
		isBare := true
		if gitWorkTreeEnv != "" {
			workTree = gitWorkTreeEnv
			if !filepath.IsAbs(workTree) {
				workTree = filepath.Clean(filepath.Join(absStart, workTree))
			}
			isBare = false
		} else if filepath.Base(gitDir) == ".git" {
			workTree = filepath.Dir(gitDir)
			isBare = false
		}

		repo := &RepoInfo{
			WorkTree:     workTree,
			GitDir:       gitDir,
			CommonGitDir: commonGitDir,
			IsBare:       isBare,
			IsWorktree:   commonGitDir != gitDir,
		}

		if err := ValidateRepo(repo); err != nil {
			return nil, err
		}
		return repo, nil
	}

	// 2. Upward directory traversal from startDir
	curr := absStart
	for {
		dotGit := filepath.Join(curr, ".git")
		fi, err := os.Stat(dotGit)
		if err == nil {
			if fi.IsDir() {
				// Standard Git repository directory
				commonGitDir := resolveCommonDir(dotGit)
				repo := &RepoInfo{
					WorkTree:     curr,
					GitDir:       dotGit,
					CommonGitDir: commonGitDir,
					IsBare:       false,
					IsWorktree:   commonGitDir != dotGit,
				}
				if err := ValidateRepo(repo); err != nil {
					return nil, err
				}
				return repo, nil
			}

			// .git is a file (git worktree or submodule)
			target, err := readGitDirFile(dotGit, curr)
			if err != nil {
				return nil, err
			}
			targetFi, err := os.Stat(target)
			if err != nil || !targetFi.IsDir() {
				return nil, fmt.Errorf("fatal: gitdir '%s' referenced in '%s' does not exist or is not a directory", target, dotGit)
			}
			commonGitDir := resolveCommonDir(target)
			repo := &RepoInfo{
				WorkTree:     curr,
				GitDir:       target,
				CommonGitDir: commonGitDir,
				IsBare:       false,
				IsWorktree:   true,
			}
			if err := ValidateRepo(repo); err != nil {
				return nil, err
			}
			return repo, nil
		}

		// Check if curr is itself a bare repository
		if isBareRepo(curr) {
			repo := &RepoInfo{
				WorkTree:     "",
				GitDir:       curr,
				CommonGitDir: curr,
				IsBare:       true,
				IsWorktree:   false,
			}
			if err := ValidateRepo(repo); err == nil {
				return repo, nil
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			// Reached filesystem root without finding .git
			break
		}
		curr = parent
	}

	return nil, fmt.Errorf("fatal: not a git repository (or any of the parent directories): .git")
}

// ValidateRepo verifies that the repository contains a valid HEAD and objects directory.
func ValidateRepo(repo *RepoInfo) error {
	if repo == nil {
		return fmt.Errorf("fatal: nil repository info")
	}
	if repo.GitDir == "" {
		return fmt.Errorf("fatal: empty git directory")
	}

	// Validate HEAD exists in GitDir
	headPath := filepath.Join(repo.GitDir, "HEAD")
	if fi, err := os.Stat(headPath); err != nil || fi.IsDir() {
		return fmt.Errorf("fatal: not a git repository: missing or invalid HEAD in '%s'", repo.GitDir)
	}

	// Validate objects directory exists in CommonGitDir
	objectsPath := filepath.Join(repo.CommonGitDir, "objects")
	if fi, err := os.Stat(objectsPath); err != nil || !fi.IsDir() {
		return fmt.Errorf("fatal: not a git repository: missing objects directory in '%s'", repo.CommonGitDir)
	}

	return nil
}

// readGitDirFile parses a .git file containing "gitdir: <path>".
func readGitDirFile(filePath, baseDir string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read gitdir file '%s': %w", filePath, err)
	}

	content := strings.TrimSpace(string(data))
	prefix := "gitdir:"
	if !strings.HasPrefix(content, prefix) {
		return "", fmt.Errorf("invalid .git file at '%s': expected 'gitdir: <path>'", filePath)
	}

	target := strings.TrimSpace(content[len(prefix):])
	if target == "" {
		return "", fmt.Errorf("empty gitdir path in '%s'", filePath)
	}

	if !filepath.IsAbs(target) {
		target = filepath.Clean(filepath.Join(baseDir, target))
	}
	return target, nil
}

// resolveCommonDir returns the common git directory if a "commondir" file is present,
// or returns gitDir itself if not.
func resolveCommonDir(gitDir string) string {
	commondirFile := filepath.Join(gitDir, "commondir")
	data, err := os.ReadFile(commondirFile)
	if err != nil {
		return gitDir
	}

	target := strings.TrimSpace(string(data))
	if target == "" {
		return gitDir
	}

	if !filepath.IsAbs(target) {
		return filepath.Clean(filepath.Join(gitDir, target))
	}
	return filepath.Clean(target)
}

// isBareRepo checks if a directory directly contains HEAD and objects/
func isBareRepo(dir string) bool {
	headPath := filepath.Join(dir, "HEAD")
	objPath := filepath.Join(dir, "objects")
	if fi, err := os.Stat(headPath); err != nil || fi.IsDir() {
		return false
	}
	if fi, err := os.Stat(objPath); err != nil || !fi.IsDir() {
		return false
	}
	return true
}
