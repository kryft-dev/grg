package filter

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// GlobMatcher matches file paths against a set of positive and negative (-g/--glob) patterns.
type GlobMatcher struct {
	rules       []globRule
	hasPositive bool
}

type globRule struct {
	pattern  string
	negated  bool
	hasSlash bool
	re       *regexp.Regexp
}

// NewGlobMatcher compiles a slice of glob patterns into a GlobMatcher.
func NewGlobMatcher(patterns []string) (*GlobMatcher, error) {
	gm := &GlobMatcher{}

	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		negated := false
		if strings.HasPrefix(p, "!") {
			negated = true
			p = strings.TrimPrefix(p, "!")
		} else {
			gm.hasPositive = true
		}

		// Normalize slashes
		p = filepath.ToSlash(p)

		// Check if pattern contains a directory slash
		hasSlash := strings.Contains(p, "/")

		regexStr, err := globToRegex(p, hasSlash)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", p, err)
		}

		re, err := regexp.Compile(regexStr)
		if err != nil {
			return nil, fmt.Errorf("failed to compile glob regex for %q: %w", p, err)
		}

		gm.rules = append(gm.rules, globRule{
			pattern:  p,
			negated:  negated,
			hasSlash: hasSlash,
			re:       re,
		})
	}

	return gm, nil
}

// Match evaluates whether path matches the glob rules.
// Follows ripgrep/gitignore ordering semantics.
func (gm *GlobMatcher) Match(path string) bool {
	if len(gm.rules) == 0 {
		return true
	}

	normPath := filepath.ToSlash(path)
	base := filepath.Base(normPath)

	// If there are positive rules, initial state is false. If only negative rules, initial state is true.
	matched := !gm.hasPositive

	for _, rule := range gm.rules {
		target := normPath
		if !rule.hasSlash {
			target = base
		}

		if rule.re.MatchString(target) {
			if rule.negated {
				matched = false
			} else {
				matched = true
			}
		}
	}

	return matched
}

// globToRegex translates a shell glob into a regular expression.
func globToRegex(glob string, hasSlash bool) (string, error) {
	var b strings.Builder
	b.WriteString("^")

	inCharClass := false
	i := 0
	n := len(glob)

	for i < n {
		c := glob[i]
		switch c {
		case '*':
			if i+1 < n && glob[i+1] == '*' {
				// "**"
				i += 2
				if i < n && glob[i] == '/' {
					i++
					// "**/": matches zero or more directory segments
					b.WriteString("(?:.+/)?")
				} else {
					// "**": matches anything across slashes
					b.WriteString(".*")
				}
			} else {
				// "*": matches within a path segment (no slash)
				b.WriteString("[^/]*")
				i++
			}
		case '?':
			b.WriteString("[^/]")
			i++
		case '[':
			inCharClass = true
			b.WriteByte('[')
			i++
			if i < n && (glob[i] == '!' || glob[i] == '^') {
				b.WriteByte('^')
				i++
			}
		case ']':
			inCharClass = false
			b.WriteByte(']')
			i++
		case '\\':
			if i+1 < n {
				i++
				b.WriteString(regexp.QuoteMeta(string(glob[i])))
				i++
			} else {
				b.WriteString(`\\`)
				i++
			}
		case '.', '+', '(', ')', '{', '}', '^', '$', '|':
			if inCharClass {
				b.WriteByte(c)
			} else {
				b.WriteString(`\` + string(c))
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}

	b.WriteString("$")
	return b.String(), nil
}
