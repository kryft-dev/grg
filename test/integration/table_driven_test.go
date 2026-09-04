package integration

import (
	"strings"
	"testing"
)

func setupRipgrepTestRepo(t *testing.T) (*TestRepo, map[string]string) {
	t.Helper()
	repo := NewTestRepo(t)

	// Commit 1: base files
	c1 := repo.Commit("feat: initial commit", map[string]string{
		"src/main.go": `package main

import "fmt"

func main() {
	fmt.Println("Starting server...")
	ProcessWorker(42)
}
`,
		"src/worker.go": `package main

// ProcessWorker handles jobs.
func ProcessWorker(id int) {
	_ = id
}

func HelperCalculate() string {
	return "result_alpha"
}
`,
		"docs/guide.md": `# User Manual

Email: support@example.com
Version: 1.0.0
Instructions on using the service.
`,
		"config/app.json": `{
	"app_id": "service-prod-01",
	"debug": false,
	"max_connections": 100,
	"pattern": "[A-Z]+_TAG"
}
`,
		"data/symbols.txt": `Literal symbols: [special]*{chars}?(1+2)^$
Standalone word: standalone target word
Subword prefix: not_target_prefix
Subword suffix: target_suffix_word
Enclosed word: (target)
Case line upper: ONLY_COMMON_CASE_TOKEN
Case line lower: only_common_case_token
Case line mixed: Only_Common_Case_Token
Context block:
START_BLOCK
BLOCK_LINE_1
BLOCK_LINE_2
MATCH_TARGET_CENTER
BLOCK_LINE_3
BLOCK_LINE_4
END_BLOCK
`,
	})

	// Commit 2: update on main
	c2 := repo.Commit("feat: add additional handler", map[string]string{
		"src/handler.go": `package main

func HandleRequest() string {
	return "RESPONSE_OK_200"
}
`,
		"docs/guide.md": `# User Manual

Email: dev@example.com
Version: 1.1.0
Instructions on using the service.
`,
	})

	// Commit 3: feature branch
	repo.CreateBranch("feature-analytics")
	cFeature := repo.Commit("feat(analytics): add metrics", map[string]string{
		"src/metrics.go": `package main

const MetricsNamespace = "ANALYTICS_METRICS_V1"
`,
	})

	// Return to main
	repo.Checkout("main")

	// Commit 4: release commit on main
	c4 := repo.Commit("chore: release v1.2.0", map[string]string{
		"CHANGELOG.md": `## Changelog
- Release v1.2.0 with performance improvements.
`,
	})

	commits := map[string]string{
		"c1":       c1,
		"c2":       c2,
		"cFeature": cFeature,
		"c4":       c4,
	}

	return repo, commits
}

// TestTableDriven_RipgrepCompatibility tests the full spectrum of ripgrep modes table-driven.
func TestTableDriven_RipgrepCompatibility(t *testing.T) {
	repo, commits := setupRipgrepTestRepo(t)

	tests := []struct {
		name           string
		args           []string
		wantExitCode   int
		mustContain    []string
		mustNotContain []string
	}{
		// --- 1. REGEX SEARCHES ---
		{
			name:         "regex basic wildcard",
			args:         []string{"--color=never", `Process.*`},
			wantExitCode: 0,
			mustContain:  []string{"ProcessWorker(42)", "ProcessWorker handles jobs"},
		},
		{
			name:         "regex character classes and quantifier",
			args:         []string{"--color=never", `v[0-9]+\.[0-9]+\.[0-9]+`},
			wantExitCode: 0,
			mustContain:  []string{"v1.2.0"},
		},
		{
			name:         "regex alternation",
			args:         []string{"--color=never", `(START_BLOCK|END_BLOCK)`},
			wantExitCode: 0,
			mustContain:  []string{"START_BLOCK", "END_BLOCK"},
		},
		{
			name:         "regex line anchors start and end",
			args:         []string{"--color=never", `(?m)^## Changelog$`},
			wantExitCode: 0,
			mustContain:  []string{"## Changelog"},
		},
		{
			name:         "regex non-matching pattern",
			args:         []string{"--color=never", `NON_EXISTENT_PATTERN_[0-9]+`},
			wantExitCode: 1,
		},

		// --- 2. LITERAL SEARCHES (-F / --fixed-strings) ---
		{
			name:         "literal search with regex metacharacters",
			args:         []string{"--color=never", "-F", `[special]*{chars}?(1+2)^$`},
			wantExitCode: 0,
			mustContain:  []string{`[special]*{chars}?(1+2)^$`},
		},
		{
			name:         "literal search with brackets and quotes",
			args:         []string{"--color=never", "-F", `"[A-Z]+_TAG"`},
			wantExitCode: 0,
			mustContain:  []string{`"[A-Z]+_TAG"`},
		},
		{
			name:         "literal search long flag --fixed-strings",
			args:         []string{"--color=never", "--fixed-strings", `support@example.com`},
			wantExitCode: 0,
			mustContain:  []string{"support@example.com"},
		},

		// --- 3. CASE SENSITIVITY MODES (-i, -s, -S) ---
		{
			name:         "case-insensitive lowercase query matches all cases",
			args:         []string{"--color=never", "-i", "only_common_case_token"},
			wantExitCode: 0,
			mustContain:  []string{"ONLY_COMMON_CASE_TOKEN", "only_common_case_token", "Only_Common_Case_Token"},
		},
		{
			name:           "case-sensitive -s lowercase query rejects uppercase lines",
			args:           []string{"--color=never", "-s", "only_common_case_token"},
			wantExitCode:   0,
			mustContain:    []string{"only_common_case_token"},
			mustNotContain: []string{"ONLY_COMMON_CASE_TOKEN", "Only_Common_Case_Token"},
		},
		{
			name:         "smart-case lowercase acts case-insensitive",
			args:         []string{"--color=never", "-S", "helpercalculate"},
			wantExitCode: 0,
			mustContain:  []string{"HelperCalculate"},
		},
		{
			name:           "smart-case uppercase acts case-sensitive",
			args:           []string{"--color=never", "-S", "HelperCalculate"},
			wantExitCode:   0,
			mustContain:    []string{"HelperCalculate"},
			mustNotContain: []string{"helpercalculate"},
		},
		{
			name:         "combined case-insensitive and fixed-strings",
			args:         []string{"--color=never", "-i", "-F", "START_block"},
			wantExitCode: 0,
			mustContain:  []string{"START_BLOCK"},
		},

		// --- 4. WORD MATCHING (-w / --word-regexp) ---
		{
			name:           "word match matches isolated words and punctuation bounds",
			args:           []string{"--color=never", "-w", "target"},
			wantExitCode:   0,
			mustContain:    []string{"standalone target word", "(target)"},
			mustNotContain: []string{"not_target_prefix", "target_suffix_word"},
		},
		{
			name:         "word match fails on subword boundary",
			args:         []string{"--color=never", "-w", "targe"},
			wantExitCode: 1,
		},

		// --- 5. OUTPUT FORMATS (-l, -c, -q, -v, --no-heading, context) ---
		{
			name:         "files with matches -l emits short SHA and path",
			args:         []string{"--color=never", "-l", "Starting server"},
			wantExitCode: 0,
			mustContain:  []string{commits["c1"][:7] + ":src/main.go"},
		},
		{
			name:         "count -c emits short SHA, path and match count",
			args:         []string{"--color=never", "-c", "BLOCK_LINE_"},
			wantExitCode: 0,
			mustContain:  []string{commits["c1"][:7] + ":data/symbols.txt:4"},
		},
		{
			name:         "quiet -q succeeds silently with empty output on match",
			args:         []string{"-q", "Starting server"},
			wantExitCode: 0,
		},
		{
			name:         "quiet -q exits 1 silently on no match",
			args:         []string{"-q", "NON_EXISTENT_TOKEN_12345"},
			wantExitCode: 1,
		},
		{
			name:         "single-line --no-heading emits sha:path:line:content",
			args:         []string{"--color=never", "--no-heading", "RESPONSE_OK_200"},
			wantExitCode: 0,
			mustContain:  []string{commits["c2"][:7] + ":src/handler.go:4:\treturn \"RESPONSE_OK_200\""},
		},
		{
			name:           "invert match -v outputs non-matching lines",
			args:           []string{"--color=never", "-v", "BLOCK", "-g", "data/symbols.txt"},
			wantExitCode:   0,
			mustContain:    []string{"Literal symbols:"},
			mustNotContain: []string{"BLOCK_LINE_1", "BLOCK_LINE_2"},
		},
		{
			name:         "context -C 1 before and after match",
			args:         []string{"--color=never", "-C", "1", "MATCH_TARGET_CENTER"},
			wantExitCode: 0,
			mustContain:  []string{"BLOCK_LINE_2", "MATCH_TARGET_CENTER", "BLOCK_LINE_3"},
		},
		{
			name:           "context -B 1 before match only",
			args:           []string{"--color=never", "-B", "1", "MATCH_TARGET_CENTER"},
			wantExitCode:   0,
			mustContain:    []string{"BLOCK_LINE_2", "MATCH_TARGET_CENTER"},
			mustNotContain: []string{"BLOCK_LINE_3"},
		},
		{
			name:           "context -A 1 after match only",
			args:           []string{"--color=never", "-A", "1", "MATCH_TARGET_CENTER"},
			wantExitCode:   0,
			mustContain:    []string{"MATCH_TARGET_CENTER", "BLOCK_LINE_3"},
			mustNotContain: []string{"BLOCK_LINE_2"},
		},
		{
			name:         "max-count -m 1 limits matches per file",
			args:         []string{"--color=never", "-m", "1", "BLOCK_LINE_"},
			wantExitCode: 0,
			mustContain:  []string{"BLOCK_LINE_1"},
		},

		// --- 6. PATH AND FILE TYPE FILTERS (-g, -t, path arguments) ---
		{
			name:           "glob inclusion -g *.go limits to go files",
			args:           []string{"--color=never", "-g", "*.go", "package main"},
			wantExitCode:   0,
			mustContain:    []string{"src/main.go", "src/worker.go"},
			mustNotContain: []string{"docs/guide.md", "config/app.json"},
		},
		{
			name:           "glob exclusion -g !*.md excludes markdown",
			args:           []string{"--color=never", "-g", "!*.md", "service"},
			wantExitCode:   0,
			mustContain:    []string{"config/app.json"},
			mustNotContain: []string{"docs/guide.md"},
		},
		{
			name:           "file type -t go limits search to Go sources",
			args:           []string{"--color=never", "-t", "go", "return"},
			wantExitCode:   0,
			mustContain:    []string{"src/worker.go"},
			mustNotContain: []string{"docs/guide.md", "config/app.json"},
		},
		{
			name:           "file type -t json limits search to JSON files",
			args:           []string{"--color=never", "-t", "json", "app_id"},
			wantExitCode:   0,
			mustContain:    []string{"config/app.json"},
			mustNotContain: []string{"src/main.go"},
		},
		{
			name:           "positional path argument after double-dash",
			args:           []string{"--color=never", "return", "--", "src/worker.go"},
			wantExitCode:   0,
			mustContain:    []string{"src/worker.go"},
			mustNotContain: []string{"src/handler.go"},
		},

		// --- 7. COMMIT REVISIONS AND BRANCHES ---
		{
			name:         "search single revision by full hash",
			args:         []string{"--color=never", "support@example.com", commits["c1"]},
			wantExitCode: 0,
			mustContain:  []string{"support@example.com", commits["c1"][:7]},
		},
		{
			name:         "search revision range HEAD~1..HEAD",
			args:         []string{"--color=never", "Changelog", "HEAD~1..HEAD"},
			wantExitCode: 0,
			mustContain:  []string{"Changelog"},
		},
		{
			name:         "search feature branch explicitly",
			args:         []string{"--color=never", "ANALYTICS_METRICS_V1", "feature-analytics"},
			wantExitCode: 0,
			mustContain:  []string{"ANALYTICS_METRICS_V1", "src/metrics.go", commits["cFeature"][:7]},
		},
		{
			name:         "search revision range between main and feature",
			args:         []string{"--color=never", "ANALYTICS_METRICS_V1", "main..feature-analytics"},
			wantExitCode: 0,
			mustContain:  []string{"ANALYTICS_METRICS_V1"},
		},
		{
			name:         "search revision range excluding feature",
			args:         []string{"--color=never", "ANALYTICS_METRICS_V1", "feature-analytics..main"},
			wantExitCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := repo.Run(tt.args...)
			if res.ExitCode != tt.wantExitCode {
				t.Fatalf("args %v: expected exit code %d, got %d.\nStderr: %s\nStdout: %s",
					tt.args, tt.wantExitCode, res.ExitCode, res.Stderr, res.Stdout)
			}

			for _, must := range tt.mustContain {
				if !strings.Contains(res.Stdout, must) {
					t.Errorf("args %v: output missing required substring %q.\nStdout:\n%s",
						tt.args, must, res.Stdout)
				}
			}

			for _, mustNot := range tt.mustNotContain {
				if strings.Contains(res.Stdout, mustNot) {
					t.Errorf("args %v: output contains forbidden substring %q.\nStdout:\n%s",
						tt.args, mustNot, res.Stdout)
				}
			}
		})
	}
}
