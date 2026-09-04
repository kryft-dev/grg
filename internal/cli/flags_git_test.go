package cli

import (
	"reflect"
	"strings"
	"testing"
)

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
	cfg, err := ParseArgs([]string{"-h"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Help {
		t.Errorf("expected Help=true for -h")
	}

	cfg, err = ParseArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Help || !cfg.LongHelp {
		t.Errorf("expected Help=true and LongHelp=true for --help")
	}

	cfg, err = ParseArgs([]string{"-V"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Version {
		t.Errorf("expected Version=true for -V")
	}

	cfg, err = ParseArgs([]string{"--version"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Version {
		t.Errorf("expected Version=true for --version")
	}

	shortHelpStr := ShortHelp()
	if !strings.Contains(shortHelpStr, "grg [FLAGS]") {
		t.Errorf("invalid short help string: %s", shortHelpStr)
	}

	longHelpStr := LongHelp()
	if !strings.Contains(longHelpStr, "Search Flags:") {
		t.Errorf("invalid long help string: %s", longHelpStr)
	}

	verStr := Version()
	if !strings.Contains(verStr, "grg 0.1.0") {
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
