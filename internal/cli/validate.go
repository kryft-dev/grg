package cli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kryft-dev/grg/internal/model"
)

// Validate checks a parsed model.Config for semantic correctness and safety.
// It verifies regular expression syntax, revision formats, path safety,
// date formats, context bounds, and flag combinations.
func Validate(cfg *model.Config) error {
	if cfg == nil {
		return fmt.Errorf("%w: configuration cannot be nil", ErrInvalidArgument)
	}

	// If help or version was requested, further validation is bypassed
	if cfg.Help || cfg.Version {
		return nil
	}

	// 1. Pattern validation
	if err := validatePatterns(cfg); err != nil {
		return err
	}

	// 2. Author and committer regex validation
	if cfg.Author != "" {
		if _, err := regexp.Compile(cfg.Author); err != nil {
			return fmt.Errorf("%w: invalid author regex '%s': %v", ErrInvalidArgument, cfg.Author, err)
		}
	}
	if cfg.Committer != "" {
		if _, err := regexp.Compile(cfg.Committer); err != nil {
			return fmt.Errorf("%w: invalid committer regex '%s': %v", ErrInvalidArgument, cfg.Committer, err)
		}
	}

	// 3. Revision range and branch validation
	if cfg.RevRange != "" {
		if err := validateRevSpec(cfg.RevRange); err != nil {
			return fmt.Errorf("%w: invalid revision range '%s': %v", ErrInvalidArgument, cfg.RevRange, err)
		}
	}
	for _, b := range cfg.Branches {
		if err := validateBranchName(b); err != nil {
			return fmt.Errorf("%w: invalid branch name '%s': %v", ErrInvalidArgument, b, err)
		}
	}

	// 4. Path argument validation
	for _, p := range cfg.Paths {
		if err := validatePath(p); err != nil {
			return fmt.Errorf("%w: invalid path filter '%s': %v", ErrInvalidArgument, p, err)
		}
	}

	// 5. Date filters validation
	if cfg.Since != "" {
		if err := validateDate(cfg.Since); err != nil {
			return fmt.Errorf("%w: invalid --since date: %v", ErrInvalidArgument, err)
		}
	}
	if cfg.Until != "" {
		if err := validateDate(cfg.Until); err != nil {
			return fmt.Errorf("%w: invalid --until date: %v", ErrInvalidArgument, err)
		}
	}

	// 6. Context lines bounds check
	if cfg.BeforeContext < 0 || cfg.AfterContext < 0 {
		return fmt.Errorf("%w: context lines must be non-negative", ErrInvalidArgument)
	}

	// 7. Max count bounds check
	if cfg.MaxCount < 0 {
		return fmt.Errorf("%w: max-count must be non-negative", ErrInvalidArgument)
	}

	// 8. Color choice validation
	if cfg.Color != "" {
		switch cfg.Color {
		case model.ColorAuto, model.ColorAlways, model.ColorNever, "ansi":
			// Valid
		default:
			return fmt.Errorf("%w: invalid color choice '%s'", ErrInvalidArgument, cfg.Color)
		}
	}

	// 9. Glob and type validation
	for _, g := range cfg.Globs {
		if strings.ContainsRune(g, 0) {
			return fmt.Errorf("%w: glob contains null byte", ErrInvalidArgument)
		}
	}
	for _, t := range cfg.Types {
		if strings.ContainsRune(t, 0) || strings.TrimSpace(t) == "" {
			return fmt.Errorf("%w: invalid type specifier '%s'", ErrInvalidArgument, t)
		}
	}

	return nil
}

func validatePatterns(cfg *model.Config) error {
	patterns := cfg.Patterns
	if len(patterns) == 0 && cfg.Pattern != "" {
		patterns = []string{cfg.Pattern}
	}
	if len(patterns) == 0 {
		return fmt.Errorf("%w: pattern is required", ErrInvalidArgument)
	}

	if !cfg.FixedStrings {
		for _, p := range patterns {
			patternStr := p
			if cfg.WordRegexp {
				patternStr = `\b(?:` + patternStr + `)\b`
			}
			if _, err := regexp.Compile(patternStr); err != nil {
				return fmt.Errorf("%w: invalid regular expression '%s': %v", ErrInvalidArgument, p, err)
			}
		}
	}

	return nil
}

func validateRevSpec(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}

	// Reject directory traversal attempts
	if strings.Contains(spec, "../") || strings.Contains(spec, "..\\") ||
		strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "\\") {
		return fmt.Errorf("revision contains path traversal sequences")
	}

	// Check control characters
	for i := 0; i < len(spec); i++ {
		if spec[i] < 32 || spec[i] == 127 {
			return fmt.Errorf("revision contains non-printable control characters")
		}
	}

	// Range handling (e.g. A..B or A...B)
	if strings.Contains(spec, "..") {
		parts := strings.Split(spec, "..")
		if len(parts) > 2 {
			return fmt.Errorf("multiple '..' range specifiers in revision")
		}
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				if err := validateSingleRev(trimmed); err != nil {
					return err
				}
			}
		}
		return nil
	}

	return validateSingleRev(spec)
}

func validateSingleRev(rev string) error {
	if rev == "" {
		return fmt.Errorf("empty revision specifier")
	}

	// Check for whitespace within single ref name
	if strings.ContainsAny(rev, " \t\n\r") {
		return fmt.Errorf("revision cannot contain whitespace")
	}

	// Ancestry specifier ~N
	if idx := strings.IndexByte(rev, '~'); idx != -1 {
		baseRef := rev[:idx]
		nStr := rev[idx+1:]
		if baseRef == "" {
			return fmt.Errorf("missing base revision before '~'")
		}
		if nStr == "" {
			return fmt.Errorf("missing numeric ancestry after '~'")
		}
		n, err := strconv.Atoi(nStr)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid ancestry specifier '~%s': must be a non-negative integer", nStr)
		}
		return validateSingleRev(baseRef)
	}

	// Parent specifier ^N
	if idx := strings.IndexByte(rev, '^'); idx != -1 {
		baseRef := rev[:idx]
		nStr := rev[idx+1:]
		if baseRef == "" {
			return fmt.Errorf("missing base revision before '^'")
		}
		if nStr != "" {
			n, err := strconv.Atoi(nStr)
			if err != nil || n < 0 {
				return fmt.Errorf("invalid parent specifier '^%s': must be a non-negative integer", nStr)
			}
		}
		return validateSingleRev(baseRef)
	}

	// Reject directory traversal or invalid ref characters
	if strings.HasPrefix(rev, "/") || strings.HasPrefix(rev, ".") {
		return fmt.Errorf("revision cannot start with '/' or '.'")
	}
	if strings.Contains(rev, "@{") && !strings.HasSuffix(rev, "}") {
		return fmt.Errorf("malformed reflog syntax '@{'")
	}

	return nil
}

func validateBranchName(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if strings.Contains(branch, "..") || strings.Contains(branch, "/") || strings.Contains(branch, "\\") {
		return fmt.Errorf("invalid characters in branch name '%s'", branch)
	}
	for i := 0; i < len(branch); i++ {
		if branch[i] < 32 || branch[i] == 127 {
			return fmt.Errorf("branch contains control characters")
		}
	}
	return nil
}

func validatePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("path filter cannot be empty")
	}
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("path contains null byte")
	}
	return nil
}

var relativeTimeRegex = regexp.MustCompile(`^\d+\s+(second|minute|hour|day|week|month|year)s?(\s+ago)?$`)

func validateDate(dateStr string) error {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return nil
	}

	// 1. Unix timestamp
	if _, err := strconv.ParseInt(dateStr, 10, 64); err == nil {
		return nil
	}

	// 2. Standard timestamp formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		time.RFC1123,
		time.RFC822,
		"2006/01/02",
		"2006/01/02 15:04:05",
		"01/02/2006",
		"02-Jan-2006",
	}
	for _, f := range formats {
		if _, err := time.Parse(f, dateStr); err == nil {
			return nil
		}
	}

	// 3. Relative keywords & phrases
	lower := strings.ToLower(dateStr)
	if lower == "now" || lower == "yesterday" || lower == "today" {
		return nil
	}
	if relativeTimeRegex.MatchString(lower) {
		return nil
	}

	return fmt.Errorf("unrecognized date format '%s'", dateStr)
}
