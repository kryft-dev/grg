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
