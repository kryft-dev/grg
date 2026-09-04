package benchmark

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	benchGRGBin     string
	benchGRGBuildErr error
	benchGRGOnce     sync.Once
)

// GetBenchmarkGRGBinary ensures the grg executable is compiled once and returns its path.
func GetBenchmarkGRGBinary(b testing.TB) string {
	benchGRGOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "grg-bench-bin-*")
		if err != nil {
			benchGRGBuildErr = fmt.Errorf("failed creating temp dir for bench binary: %w", err)
			return
		}
		binPath := filepath.Join(tmpDir, "grg")
		cmd := exec.Command("go", "build", "-o", binPath, "github.com/kryft-dev/grg/cmd/grg")
		if out, err := cmd.CombinedOutput(); err != nil {
			benchGRGBuildErr = fmt.Errorf("failed compiling grg: %w\nOutput: %s", err, out)
			return
		}
		benchGRGBin = binPath
	})
	if benchGRGBuildErr != nil {
		b.Fatalf("failed to obtain grg binary: %v", benchGRGBuildErr)
	}
	return benchGRGBin
}

// BenchmarkRepoOptions specifies parameters for generating synthetic repositories.
type BenchmarkRepoOptions struct {
	CommitCount    int     // Total number of commits in linear history
	FileCount      int     // Total distinct file paths in the tree
	DuplicateRatio float64 // Ratio (0.0 to 1.0) of identical duplicate blob content
	PackRepo       bool    // Run git gc to pack objects into packfile
}

// CreateBenchmarkRepo synthesizes a Git repository with the requested commit count,
// file tree, and duplicate blob ratio using git fast-import for maximum speed.
func CreateBenchmarkRepo(b testing.TB, opts BenchmarkRepoOptions) string {
	b.Helper()
	dir := b.TempDir()

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=Benchmarker",
			"GIT_AUTHOR_EMAIL=bench@example.com",
			"GIT_COMMITTER_NAME=Benchmarker",
			"GIT_COMMITTER_EMAIL=bench@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v failed: %v\nOutput: %s", args, err, out)
		}
	}

	runGit("init", "-b", "main")
	runGit("config", "user.name", "Benchmarker")
	runGit("config", "user.email", "bench@example.com")
	runGit("config", "commit.gpgsign", "false")

	fileCount := opts.FileCount
	if fileCount < 1 {
		fileCount = 10
	}
	commitCount := opts.CommitCount
	if commitCount < 1 {
		commitCount = 10
	}

	var fastImportBuf bytes.Buffer
	sharedDuplicateContent := "const SharedSecretConfigToken = \"BENCHMARK_DUPLICATE_PAYLOAD_TOKEN_XYZ\";\n// Standard shared boilerplate line\n"

	for c := 1; c <= commitCount; c++ {
		msg := fmt.Sprintf("Commit revision %05d\n", c)
		fastImportBuf.WriteString(fmt.Sprintf("commit refs/heads/main\nmark :%d\n", c))
		fastImportBuf.WriteString(fmt.Sprintf("committer Benchmarker <bench@example.com> %d +0000\n", 1670000000+c*60))
		fastImportBuf.WriteString(fmt.Sprintf("data %d\n%s", len(msg), msg))
		if c > 1 {
			fastImportBuf.WriteString(fmt.Sprintf("from :%d\n", c-1))
		}

		// Update a subset of files in each commit
		filesToTouch := fileCount / 5
		if filesToTouch < 1 {
			filesToTouch = 1
		}

		for f := 0; f < filesToTouch; f++ {
			fileIdx := (c*filesToTouch + f) % fileCount
			filePath := fmt.Sprintf("src/pkg%02d/module_%03d.go", fileIdx/10, fileIdx)

			var content string
			// Check if this file revision should use duplicate content
			isDup := false
			if opts.DuplicateRatio > 0 {
				threshold := int(opts.DuplicateRatio * 100)
				if (c+f*7)%100 < threshold {
					isDup = true
				}
			}

			if isDup {
				content = sharedDuplicateContent
			} else {
				content = fmt.Sprintf("package pkg\n// Unique commit %d file %d\nvar Token_%d = \"BENCHMARK_UNIQUE_TOKEN_%d\";\nfunc Handle%d() int { return %d }\n",
					c, fileIdx, c, c, fileIdx, c*10)
			}

			fastImportBuf.WriteString(fmt.Sprintf("M 100644 inline %s\n", filePath))
			fastImportBuf.WriteString(fmt.Sprintf("data %d\n%s\n", len(content), content))
		}
	}

	// Stream to git fast-import
	cmd := exec.Command("git", "fast-import", "--quiet")
	cmd.Dir = dir
	cmd.Stdin = &fastImportBuf
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("git fast-import failed: %v\nOutput: %s", err, out)
	}

	// Reset working tree to HEAD
	runGit("reset", "--hard", "main")

	if opts.PackRepo {
		runGit("gc", "--prune=now")
	}

	return dir
}
