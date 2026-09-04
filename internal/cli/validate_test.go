package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/kryft-dev/grg/internal/model"
)

func TestValidate_NilConfig(t *testing.T) {
	err := Validate(nil)
	if err == nil {
		t.Fatalf("expected error for nil config")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected error to wrap ErrInvalidArgument, got %v", err)
	}
}

func TestValidate_HelpAndVersionBypass(t *testing.T) {
	cfgHelp := &model.Config{Help: true}
	if err := Validate(cfgHelp); err != nil {
		t.Errorf("expected Validate to succeed for Help=true, got %v", err)
	}

	cfgVersion := &model.Config{Version: true}
	if err := Validate(cfgVersion); err != nil {
		t.Errorf("expected Validate to succeed for Version=true, got %v", err)
	}
}

func TestValidate_PatternRequired(t *testing.T) {
	cfg := &model.Config{}
	err := Validate(cfg)
	if err == nil {
		t.Fatalf("expected error for missing pattern")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
	if !strings.Contains(err.Error(), "pattern is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_RegexSyntax(t *testing.T) {
	tests := []struct {
		name         string
		pattern      string
		patterns     []string
		fixedStrings bool
		wordRegexp   bool
		wantErr      bool
	}{
		{
			name:    "valid regex",
			pattern: `func\s+\w+\(.*\)`,
			wantErr: false,
		},
		{
			name:    "invalid regex unclosed bracket",
			pattern: `[invalid-pattern`,
			wantErr: true,
		},
		{
			name:    "invalid regex unclosed paren",
			pattern: `(unclosed-group`,
			wantErr: true,
		},
		{
			name:         "fixed strings bypasses invalid regex",
			pattern:      `[literal-brackets*(`,
			fixedStrings: true,
			wantErr:      false,
		},
		{
			name:       "valid word regexp",
			pattern:    `identifier`,
			wordRegexp: true,
			wantErr:    false,
		},
		{
			name:     "multiple patterns all valid",
			patterns: []string{`foo`, `bar\d+`, `baz`},
			wantErr:  false,
		},
		{
			name:     "multiple patterns with one invalid",
			patterns: []string{`foo`, `[bad-regex`, `baz`},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &model.Config{
				Pattern:      tt.pattern,
				Patterns:     tt.patterns,
				FixedStrings: tt.fixedStrings,
				WordRegexp:   tt.wordRegexp,
			}
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected error to wrap ErrInvalidArgument, got %v", err)
			}
		})
	}
}

func TestValidate_AuthorAndCommitterRegex(t *testing.T) {
	cfg := &model.Config{
		Pattern: "test",
		Author:  "Alice <.*@example\\.com>",
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid author regex to pass, got %v", err)
	}

	cfg.Author = "[invalid-author-regex"
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for invalid author regex")
	}

	cfg.Author = ""
	cfg.Committer = "Bob <.*@example\\.com>"
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid committer regex to pass, got %v", err)
	}

	cfg.Committer = "(unclosed-committer"
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for invalid committer regex")
	}
}

func TestValidate_RevisionSpecs(t *testing.T) {
	tests := []struct {
		name     string
		revRange string
		wantErr  bool
	}{
		{name: "empty rev range is valid", revRange: "", wantErr: false},
		{name: "HEAD", revRange: "HEAD", wantErr: false},
		{name: "branch name", revRange: "main", wantErr: false},
		{name: "tag name", revRange: "v1.2.3", wantErr: false},
		{name: "full commit SHA", revRange: "4b825dc642cb6eb9a060e54bf8d69288fbee4904", wantErr: false},
		{name: "ancestry HEAD~3", revRange: "HEAD~3", wantErr: false},
		{name: "two-dot range", revRange: "HEAD~5..HEAD", wantErr: false},
		{name: "branch range", revRange: "main..feature", wantErr: false},
		{name: "open-ended start range", revRange: "..HEAD", wantErr: false},
		{name: "open-ended end range", revRange: "main..", wantErr: false},
		{name: "parent syntax HEAD^1", revRange: "HEAD^1", wantErr: false},
		// Invalid cases
		{name: "path traversal slash dot dot", revRange: "../../../etc/passwd", wantErr: true},
		{name: "path traversal backslash dot dot", revRange: "..\\..\\windows", wantErr: true},
		{name: "leading slash", revRange: "/refs/heads/main", wantErr: true},
		{name: "embedded whitespace", revRange: "HEAD ~1", wantErr: true},
		{name: "invalid ancestry non-number", revRange: "HEAD~abc", wantErr: true},
		{name: "invalid ancestry empty number", revRange: "HEAD~", wantErr: true},
		{name: "invalid ancestry negative", revRange: "HEAD~-1", wantErr: true},
		{name: "multiple ranges", revRange: "A..B..C", wantErr: true},
		{name: "control character", revRange: "HEAD\x00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &model.Config{
				Pattern:  "test",
				RevRange: tt.revRange,
			}
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() for %q error = %v, wantErr = %v", tt.revRange, err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument for %q, got %v", tt.revRange, err)
			}
		})
	}
}

func TestValidate_BranchNames(t *testing.T) {
	cfg := &model.Config{
		Pattern:  "test",
		Branches: []string{"main", "feature-123"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid branches to pass, got %v", err)
	}

	cfg.Branches = []string{"../escaped"}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for branch with directory traversal")
	}

	cfg.Branches = []string{""}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for empty branch name")
	}
}

func TestValidate_Paths(t *testing.T) {
	cfg := &model.Config{
		Pattern: "test",
		Paths:   []string{"cmd/grg", "internal/cli/flags.go"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid paths to pass, got %v", err)
	}

	cfg.Paths = []string{"valid/path", "invalid\x00path"}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for path with null byte")
	}

	cfg.Paths = []string{"   "}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for whitespace-only path")
	}
}

func TestValidate_DateFilters(t *testing.T) {
	validDates := []string{
		"2024-01-01",
		"2024-01-01T15:04:05Z",
		"2024-01-01 15:04:05",
		"1704067200", // unix timestamp
		"2 weeks ago",
		"yesterday",
		"now",
		"3 days ago",
	}

	for _, d := range validDates {
		cfg := &model.Config{
			Pattern: "test",
			Since:   d,
			Until:   d,
		}
		if err := Validate(cfg); err != nil {
			t.Errorf("expected date %q to be valid, got %v", d, err)
		}
	}

	invalidDates := []string{
		"not-a-date",
		"yesteryear",
		"2024-99-99",
	}

	for _, d := range invalidDates {
		cfg := &model.Config{
			Pattern: "test",
			Since:   d,
		}
		if err := Validate(cfg); err == nil {
			t.Errorf("expected date %q to fail validation", d)
		}
	}
}

func TestValidate_NumericBoundsAndColor(t *testing.T) {
	// Negative context
	cfg := &model.Config{
		Pattern:       "test",
		BeforeContext: -1,
	}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for negative BeforeContext")
	}

	cfg = &model.Config{
		Pattern:      "test",
		AfterContext: -1,
	}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for negative AfterContext")
	}

	// Negative MaxCount
	cfg = &model.Config{
		Pattern:  "test",
		MaxCount: -1,
	}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for negative MaxCount")
	}

	// Color validation
	cfg = &model.Config{
		Pattern: "test",
		Color:   "neon-green",
	}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for invalid Color choice")
	}

	// Glob with null byte
	cfg = &model.Config{
		Pattern: "test",
		Globs:   []string{"*.go\x00"},
	}
	if err := Validate(cfg); err == nil {
		t.Errorf("expected error for glob with null byte")
	}
}
