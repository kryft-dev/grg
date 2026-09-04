package fuzz

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kryft-dev/grg/internal/cli"
	"github.com/kryft-dev/grg/internal/filter"
	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
	"github.com/kryft-dev/grg/internal/search"
)

// FuzzCLIArgParsing fuzzes command-line argument parsing and config cloning.
func FuzzCLIArgParsing(f *testing.F) {
	// Seed corpus
	f.Add("pattern")
	f.Add("-i -F --no-heading test-pattern")
	f.Add("-g *.go -g !*_test.go -t md search_query")
	f.Add("--context 3 -m 10 -A 2 -B 1 --color=never search HEAD~3..HEAD")
	f.Add("-q -s -w identifier -- path/to/file.go path/two.txt")
	f.Add("--author Alice --committer Bot --since 2023-01-01 --until 2023-12-31 token")
	f.Add("--first-parent --expand-commits --all target_query")
	f.Add("--help")
	f.Add("--version")
	f.Add("")
	f.Add("-C invalid_number pattern")
	f.Add("--color=invalid_choice pattern")

	f.Fuzz(func(t *testing.T, argLine string) {
		// Split fuzzy argLine into arguments
		args := strings.Fields(argLine)

		cfg, err := cli.Parse(args)
		if err != nil {
			// Structured error is expected for invalid flag combinations
			return
		}

		if cfg == nil {
			t.Fatal("expected non-nil config when Parse returns without error")
		}

		// Test Clone safety and defensive copy invariants
		clone := cfg.Clone()
		if clone == nil {
			t.Fatal("Clone returned nil for valid config")
		}

		// Verify modifying clone slice fields does not mutate original config
		if len(clone.Paths) > 0 {
			origLen := len(cfg.Paths)
			clone.Paths = append(clone.Paths, "mutated_path_entry")
			if len(cfg.Paths) != origLen {
				t.Errorf("slice aliasing detected in Config.Paths clone")
			}
		}

		if len(clone.Globs) > 0 {
			origLen := len(cfg.Globs)
			clone.Globs = append(clone.Globs, "mutated_glob_entry")
			if len(cfg.Globs) != origLen {
				t.Errorf("slice aliasing detected in Config.Globs clone")
			}
		}

		// If pattern was resolved and not help/version, verify matcher creation doesn't panic
		if !cfg.Help && !cfg.Version && cfg.Pattern != "" {
			_, _ = search.NewMatcher(cfg)
		}
	})
}

// FuzzPatternMatching fuzzes regex and literal blob matching across arbitrary bytes and flags.
func FuzzPatternMatching(f *testing.F) {
	f.Add("target", []byte("prefix target suffix\nsecond line\n"), false, false, false, false)
	f.Add("[0-9]+", []byte("order 12345 completed\n"), false, false, false, false)
	f.Add("special[chars]*(1+2)?", []byte("special[chars]*(1+2)? literal text\n"), true, false, false, false)
	f.Add("word", []byte("word word_sub sub_word (word)\n"), false, false, true, false)
	f.Add("CASE", []byte("case Case CASE\n"), false, true, false, false)
	f.Add("invert", []byte("line 1\nline 2 invert\nline 3\n"), false, false, false, true)
	f.Add("empty", []byte{}, false, false, false, false)
	f.Add("", []byte("some content"), false, false, false, false)

	f.Fuzz(func(t *testing.T, pattern string, content []byte, fixed, ignoreCase, wordRegexp, invert bool) {
		// Avoid unbounded regex input length
		if len(pattern) > 256 || len(content) > 128*1024 {
			return
		}

		cfg := &model.Config{
			Pattern:       pattern,
			FixedStrings:  fixed,
			WordRegexp:    wordRegexp,
			InvertMatch:   invert,
			CaseSensitive: !ignoreCase,
			IgnoreCase:    ignoreCase,
		}
		if ignoreCase {
			cfg.CaseMode = model.IgnoreCase
		} else {
			cfg.CaseMode = model.CaseSensitive
		}

		matcher, err := search.NewMatcher(cfg)
		if err != nil {
			// Malformed pattern properly rejected
			return
		}

		// Verify MatchBytes does not panic
		_ = matcher.MatchBytes(content)

		// Verify MatchBlob does not panic
		matches, err := matcher.MatchBlob(content)
		if err != nil {
			return
		}

		// Verify submatches bounds
		for _, m := range matches {
			for _, sm := range m.Submatches {
				if sm.Start < 0 || sm.End < sm.Start {
					t.Errorf("invalid submatch range [%d, %d] in line %d", sm.Start, sm.End, m.LineNum)
				}
			}
		}

		// Verify MatchBlobWithContext does not panic
		_, _, _ = matcher.MatchBlobWithContext(content)

		// Invariant: If fixed literal matching without invert/word/ignore-case,
		// and content does not contain pattern, MatchBytes must be false.
		if fixed && !ignoreCase && !wordRegexp && !invert && len(pattern) > 0 {
			hasSubstring := bytes.Contains(content, []byte(pattern))
			matched := matcher.MatchBytes(content)
			if !hasSubstring && matched {
				t.Errorf("false positive: pattern %q not in content, but MatchBytes returned true", pattern)
			}
		}
	})
}

// FuzzGlobMatching fuzzes path glob matching against arbitrary path strings.
func FuzzGlobMatching(f *testing.F) {
	f.Add("*.go", "src/main.go")
	f.Add("!*_test.go", "src/main_test.go")
	f.Add("docs/**", "docs/sub/readme.md")
	f.Add("*.{json,yaml}", "config.json")
	f.Add("src/*/*.txt", "src/pkg/data.txt")
	f.Add("**", "any/path/file.ext")
	f.Add("", "some/path")

	f.Fuzz(func(t *testing.T, globPattern, path string) {
		if len(globPattern) > 128 || len(path) > 256 {
			return
		}

		gm, err := filter.NewGlobMatcher([]string{globPattern})
		if err != nil {
			return
		}

		// Must not panic and must return deterministic boolean
		m1 := gm.Match(path)
		m2 := gm.Match(path)
		if m1 != m2 {
			t.Errorf("non-deterministic glob match for pattern %q and path %q", globPattern, path)
		}
	})
}

// FuzzGitDeltaApply fuzzes Git delta decompression with arbitrary byte sequences.
func FuzzGitDeltaApply(f *testing.F) {
	// Seed with valid base and delta header
	base := []byte("hello world baseline text for delta testing\n")
	// Delta header: baseSize=44 (0x2c), targetSize=48 (0x30), insert 4 bytes "1234"
	var validDelta []byte
	validDelta = append(validDelta, 0x2c)                        // baseSize LEB128
	validDelta = append(validDelta, 0x30)                        // targetSize LEB128
	validDelta = append(validDelta, 0x90, 0x00, 0x2c)            // Copy opcode: offset 0, size 44
	validDelta = append(validDelta, 0x04, '1', '2', '3', '4')    // Insert opcode: 4 bytes

	f.Add(base, validDelta)
	f.Add(base, []byte{})
	f.Add([]byte{}, []byte{})
	f.Add(base, []byte{0x80, 0x80, 0x80, 0x80}) // Malformed LEB128

	f.Fuzz(func(t *testing.T, base, delta []byte) {
		if len(base) > 64*1024 || len(delta) > 64*1024 {
			return
		}

		// ReadDeltaHeader must never panic
		_, _, _, _ = gitengine.ReadDeltaHeader(delta)

		// ApplyDelta must never panic or crash on corrupt/malformed deltas
		_, _ = gitengine.ApplyDelta(base, delta)
	})
}
