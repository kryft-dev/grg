package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kryft-dev/grg/internal/model"
)

func TestParseArgs_BasicPattern(t *testing.T) {
	cfg, err := ParseArgs([]string{"my-pattern"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Pattern != "my-pattern" {
		t.Errorf("expected Pattern 'my-pattern', got %q", cfg.Pattern)
	}
	if len(cfg.Patterns) != 1 || cfg.Patterns[0] != "my-pattern" {
		t.Errorf("expected Patterns ['my-pattern'], got %v", cfg.Patterns)
	}
	if cfg.RevRange != "" {
		t.Errorf("expected empty RevRange, got %q", cfg.RevRange)
	}
	if len(cfg.Paths) != 0 {
		t.Errorf("expected empty Paths, got %v", cfg.Paths)
	}
	// Verify defaults
	if !cfg.LineNumber {
		t.Errorf("expected default LineNumber to be true")
	}
	if !cfg.Heading {
		t.Errorf("expected default Heading to be true")
	}
	if cfg.Color != model.ColorAuto {
		t.Errorf("expected default Color to be ColorAuto, got %q", cfg.Color)
	}
}

func TestParseArgs_MissingPattern(t *testing.T) {
	_, err := ParseArgs([]string{})
	if err == nil {
		t.Fatalf("expected error for missing pattern")
	}
	if !strings.Contains(err.Error(), "pattern is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseArgs_PositionalSyntax(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantPattern  string
		wantRevRange string
		wantPaths    []string
	}{
		{
			name:         "pattern and rev range",
			args:         []string{"foo", "HEAD~5..HEAD"},
			wantPattern:  "foo",
			wantRevRange: "HEAD~5..HEAD",
			wantPaths:    nil,
		},
		{
			name:         "pattern, rev range, and paths after double-dash",
			args:         []string{"foo", "main", "--", "internal/", "cmd/"},
			wantPattern:  "foo",
			wantRevRange: "main",
			wantPaths:    []string{"internal/", "cmd/"},
		},
		{
			name:         "pattern and paths after double-dash without rev range",
			args:         []string{"foo", "--", "file.go"},
			wantPattern:  "foo",
			wantRevRange: "",
			wantPaths:    []string{"file.go"},
		},
		{
			name:         "pattern, rev range, and paths without double-dash",
			args:         []string{"foo", "HEAD", "dir1", "dir2"},
			wantPattern:  "foo",
			wantRevRange: "HEAD",
			wantPaths:    []string{"dir1", "dir2"},
		},
		{
			name:         "-e flag with rev range and double-dash paths",
			args:         []string{"-e", "my_pattern", "feature-branch", "--", "pkg/"},
			wantPattern:  "my_pattern",
			wantRevRange: "feature-branch",
			wantPaths:    []string{"pkg/"},
		},
		{
			name:         "-e flag with no rev range and double-dash paths",
			args:         []string{"-e", "my_pattern", "--", "pkg/"},
			wantPattern:  "my_pattern",
			wantRevRange: "",
			wantPaths:    []string{"pkg/"},
		},
		{
			name:         "multiple -e patterns",
			args:         []string{"-e", "pat1", "-e", "pat2", "HEAD"},
			wantPattern:  "pat1",
			wantRevRange: "HEAD",
			wantPaths:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseArgs(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Pattern != tt.wantPattern {
				t.Errorf("Pattern got %q, want %q", cfg.Pattern, tt.wantPattern)
			}
			if cfg.RevRange != tt.wantRevRange {
				t.Errorf("RevRange got %q, want %q", cfg.RevRange, tt.wantRevRange)
			}
			if !reflect.DeepEqual(cfg.Paths, tt.wantPaths) && !(len(cfg.Paths) == 0 && len(tt.wantPaths) == 0) {
				t.Errorf("Paths got %v, want %v", cfg.Paths, tt.wantPaths)
			}
		})
	}
}

func TestParseArgs_RipgrepFlags(t *testing.T) {
	t.Run("case sensitivity overrides", func(t *testing.T) {
		// -i then -s
		cfg, err := ParseArgs([]string{"-i", "-s", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.CaseSensitive || cfg.IgnoreCase || cfg.SmartCase || cfg.CaseMode != model.CaseSensitive {
			t.Errorf("expected CaseSensitive to win: %+v", cfg)
		}

		// -s then -S
		cfg, err = ParseArgs([]string{"-s", "-S", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.SmartCase || cfg.CaseSensitive || cfg.IgnoreCase || cfg.CaseMode != model.SmartCase {
			t.Errorf("expected SmartCase to win: %+v", cfg)
		}

		// -S then -i
		cfg, err = ParseArgs([]string{"--smart-case", "--ignore-case", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.IgnoreCase || cfg.SmartCase || cfg.CaseSensitive || cfg.CaseMode != model.IgnoreCase {
			t.Errorf("expected IgnoreCase to win: %+v", cfg)
		}
	})

	t.Run("line numbers and headings", func(t *testing.T) {
		// -N suppresses line numbers
		cfg, err := ParseArgs([]string{"-N", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.LineNumber {
			t.Errorf("expected LineNumber=false after -N")
		}

		// -N then -n re-enables line numbers
		cfg, err = ParseArgs([]string{"-N", "-n", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.LineNumber {
			t.Errorf("expected LineNumber=true after -N then -n")
		}

		// --no-heading suppresses heading
		cfg, err = ParseArgs([]string{"--no-heading", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Heading {
			t.Errorf("expected Heading=false after --no-heading")
		}

		// --no-heading then --heading re-enables heading
		cfg, err = ParseArgs([]string{"--no-heading", "--heading", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Heading {
			t.Errorf("expected Heading=true after --heading")
		}
	})

	t.Run("matching and formatting flags", func(t *testing.T) {
		args := []string{
			"-F", "-w", "-v", "-l", "-c", "-q", "-a", "pat",
		}
		cfg, err := ParseArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.FixedStrings || !cfg.WordRegexp || !cfg.InvertMatch || !cfg.FilesWithMatches || !cfg.Count || !cfg.Quiet || !cfg.Text {
			t.Errorf("flags mismatch: %+v", cfg)
		}
	})

	t.Run("context flags separated and attached", func(t *testing.T) {
		// Separated: -C 3
		cfg, err := ParseArgs([]string{"-C", "3", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.BeforeContext != 3 || cfg.AfterContext != 3 {
			t.Errorf("expected context 3/3, got %d/%d", cfg.BeforeContext, cfg.AfterContext)
		}

		// Attached: -C4
		cfg, err = ParseArgs([]string{"-C4", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.BeforeContext != 4 || cfg.AfterContext != 4 {
			t.Errorf("expected context 4/4, got %d/%d", cfg.BeforeContext, cfg.AfterContext)
		}

		// -B 2 -A 5
		cfg, err = ParseArgs([]string{"-B2", "-A", "5", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.BeforeContext != 2 || cfg.AfterContext != 5 {
			t.Errorf("expected 2/5, got %d/%d", cfg.BeforeContext, cfg.AfterContext)
		}

		// Long forms: --context=3, --before-context 1, --after-context=2
		cfg, err = ParseArgs([]string{"--context=3", "--before-context", "1", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.BeforeContext != 1 || cfg.AfterContext != 3 {
			t.Errorf("expected 1/3, got %d/%d", cfg.BeforeContext, cfg.AfterContext)
		}
	})

	t.Run("max count", func(t *testing.T) {
		cfg, err := ParseArgs([]string{"-m", "10", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MaxCount != 10 {
			t.Errorf("expected MaxCount=10, got %d", cfg.MaxCount)
		}

		cfg, err = ParseArgs([]string{"-m5", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MaxCount != 5 {
			t.Errorf("expected MaxCount=5, got %d", cfg.MaxCount)
		}
	})

	t.Run("globs and types multi-value", func(t *testing.T) {
		args := []string{
			"-g", "*.go", "--glob", "!*_test.go",
			"-t", "go", "--type", "rust",
			"pat",
		}
		cfg, err := ParseArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		expectedGlobs := []string{"*.go", "!*_test.go"}
		expectedTypes := []string{"go", "rust"}
		if !reflect.DeepEqual(cfg.Globs, expectedGlobs) {
			t.Errorf("Globs got %v, want %v", cfg.Globs, expectedGlobs)
		}
		if !reflect.DeepEqual(cfg.Types, expectedTypes) {
			t.Errorf("Types got %v, want %v", cfg.Types, expectedTypes)
		}
	})

	t.Run("color flag variations", func(t *testing.T) {
		for _, color := range []string{"auto", "always", "never", "ansi"} {
			cfg, err := ParseArgs([]string{"--color=" + color, "pat"})
			if err != nil {
				t.Fatalf("color %s failed: %v", color, err)
			}
			if string(cfg.Color) != color {
				t.Errorf("expected color %s, got %s", color, cfg.Color)
			}

			cfg, err = ParseArgs([]string{"--color", color, "pat"})
			if err != nil {
				t.Fatalf("color %s failed: %v", color, err)
			}
			if string(cfg.Color) != color {
				t.Errorf("expected color %s, got %s", color, cfg.Color)
			}
		}

		// Bare --color defaults to always
		cfg, err := ParseArgs([]string{"--color", "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Color != model.ColorAlways {
			t.Errorf("expected bare --color to default to always, got %q", cfg.Color)
		}
	})
}

func TestParseArgs_GitFlags(t *testing.T) {
	args := []string{
		"--all",
		"--first-parent",
		"--since", "2024-01-01",
		"--until=2024-12-31",
		"--author", "Alice",
		"--committer=Bob",
		"--branch", "main",
		"--branch", "feat",
		"--uncommitted",
		"--expand-commits",
		"--unordered",
		"search-term",
	}

	cfg, err := ParseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.All {
		t.Errorf("expected All=true")
	}
	if !cfg.FirstParent {
		t.Errorf("expected FirstParent=true")
	}
	if cfg.Since != "2024-01-01" {
		t.Errorf("expected Since '2024-01-01', got %q", cfg.Since)
	}
	if cfg.Until != "2024-12-31" {
		t.Errorf("expected Until '2024-12-31', got %q", cfg.Until)
	}
	if cfg.Author != "Alice" {
		t.Errorf("expected Author 'Alice', got %q", cfg.Author)
	}
	if cfg.Committer != "Bob" {
		t.Errorf("expected Committer 'Bob', got %q", cfg.Committer)
	}
	if !reflect.DeepEqual(cfg.Branches, []string{"main", "feat"}) {
		t.Errorf("expected Branches ['main', 'feat'], got %v", cfg.Branches)
	}
	if !cfg.Uncommitted {
		t.Errorf("expected Uncommitted=true")
	}
	if !cfg.ExpandCommits {
		t.Errorf("expected ExpandCommits=true")
	}
	if !cfg.Unordered {
		t.Errorf("expected Unordered=true")
	}
	if cfg.Pattern != "search-term" {
		t.Errorf("expected Pattern 'search-term', got %q", cfg.Pattern)
	}
}

func TestParseArgs_ShortFlagBundling(t *testing.T) {
	// Bundle boolean flags: -inv
	cfg, err := ParseArgs([]string{"-inv", "pat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IgnoreCase || !cfg.LineNumber || !cfg.InvertMatch {
		t.Errorf("expected -inv to set IgnoreCase, LineNumber, InvertMatch: %+v", cfg)
	}

	// Bundle boolean with trailing value: -inC2
	cfg, err = ParseArgs([]string{"-inC2", "pat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IgnoreCase || !cfg.LineNumber || cfg.BeforeContext != 2 || cfg.AfterContext != 2 {
		t.Errorf("expected -inC2 to set IgnoreCase, LineNumber, Context=2: %+v", cfg)
	}

	// Attached -e: -epattern
	cfg, err = ParseArgs([]string{"-epattern"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Pattern != "pattern" {
		t.Errorf("expected Pattern 'pattern', got %q", cfg.Pattern)
	}
}

func TestParseArgs_HelpAndVersion(t *testing.T) {
	// -h
	cfg, err := ParseArgs([]string{"-h"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Help || cfg.LongHelp {
		t.Errorf("expected Help=true and LongHelp=false for -h")
	}

	// --help
	cfg, err = ParseArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Help || !cfg.LongHelp {
		t.Errorf("expected Help=true and LongHelp=true for --help")
	}

	// -V
	cfg, err = ParseArgs([]string{"-V"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Version {
		t.Errorf("expected Version=true for -V")
	}

	// --version
	cfg, err = ParseArgs([]string{"--version"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Version {
		t.Errorf("expected Version=true for --version")
	}

	// Check help and version strings
	shortHelp := ShortHelp()
	if !strings.Contains(shortHelp, "Usage:") || !strings.Contains(shortHelp, "grg [FLAGS]") {
		t.Errorf("invalid short help string: %s", shortHelp)
	}

	longHelp := LongHelp()
	if !strings.Contains(longHelp, "Usage:") || !strings.Contains(longHelp, "Examples:") {
		t.Errorf("invalid long help string: %s", longHelp)
	}

	verStr := Version()
	if !strings.HasPrefix(verStr, "grg ") {
		t.Errorf("invalid version string: %s", verStr)
	}
}

func TestParseArgs_Errors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown long flag",
			args:    []string{"--nonexistent", "pat"},
			wantErr: "unknown flag: '--nonexistent'",
		},
		{
			name:    "unknown short flag",
			args:    []string{"-z", "pat"},
			wantErr: "unknown flag: '-z'",
		},
		{
			name:    "missing value for long flag",
			args:    []string{"--context"},
			wantErr: "flag '--context' requires an argument",
		},
		{
			name:    "missing value for short flag",
			args:    []string{"-C"},
			wantErr: "flag '-C' requires an argument",
		},
		{
			name:    "invalid integer value",
			args:    []string{"-C", "abc", "pat"},
			wantErr: "invalid context value 'abc'",
		},
		{
			name:    "negative integer value",
			args:    []string{"-C", "-2", "pat"},
			wantErr: "invalid context value '-2'",
		},
		{
			name:    "invalid color choice",
			args:    []string{"--color=magenta", "pat"},
			wantErr: "invalid argument 'magenta' for '--color'",
		},
		{
			name:    "invalid boolean value",
			args:    []string{"--heading=maybe", "pat"},
			wantErr: "invalid boolean value 'maybe'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseArgs(tt.args)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
