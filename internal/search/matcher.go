package search

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/kryft-dev/grg/internal/model"
)

// Matcher performs pattern matching on blob content according to search configurations.
type Matcher struct {
	re            *regexp.Regexp
	cfg           *model.Config
	caseSensitive bool
}

// NewMatcher compiles a Matcher based on the search configuration.
func NewMatcher(cfg *model.Config) (*Matcher, error) {
	patterns := cfg.Patterns
	if len(patterns) == 0 && cfg.Pattern != "" {
		patterns = []string{cfg.Pattern}
	}
	if len(patterns) == 0 {
		return nil, fmt.Errorf("no search pattern specified")
	}

	// 1. Determine case mode
	caseSensitive := true
	if cfg.IgnoreCase {
		caseSensitive = false
	} else if cfg.CaseSensitive {
		caseSensitive = true
	} else if cfg.SmartCase || cfg.CaseMode == model.SmartCase {
		// Smart case: if any pattern contains uppercase, case-sensitive; else ignore case
		hasUpper := false
		for _, p := range patterns {
			for _, r := range p {
				if unicode.IsUpper(r) {
					hasUpper = true
					break
				}
			}
			if hasUpper {
				break
			}
		}
		caseSensitive = hasUpper
	} else if cfg.CaseMode == model.IgnoreCase {
		caseSensitive = false
	}

	// 2. Build combined regex
	var parts []string
	for _, p := range patterns {
		patternStr := p
		if cfg.FixedStrings {
			patternStr = regexp.QuoteMeta(patternStr)
		}
		if cfg.WordRegexp {
			patternStr = `\b(?:` + patternStr + `)\b`
		}
		parts = append(parts, "(?:"+patternStr+")")
	}

	combinedPattern := strings.Join(parts, "|")
	if !caseSensitive {
		combinedPattern = "(?i)" + combinedPattern
	}

	re, err := regexp.Compile(combinedPattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile search pattern: %w", err)
	}

	return &Matcher{
		re:            re,
		cfg:           cfg,
		caseSensitive: caseSensitive,
	}, nil
}

// MatchBlob searches data line-by-line and returns matching lines.
func (m *Matcher) MatchBlob(data []byte) ([]model.SearchMatch, error) {
	lines := splitLines(data)
	return m.MatchLines(lines), nil
}

// MatchLines searches a pre-split slice of lines and returns matching lines.
func (m *Matcher) MatchLines(lines []string) []model.SearchMatch {
	var matches []model.SearchMatch
	maxCount := m.cfg.MaxCount

	for i, lineText := range lines {
		lineNum := i + 1

		if m.cfg.InvertMatch {
			if !m.re.MatchString(lineText) {
				matches = append(matches, model.SearchMatch{
					LineNum:  lineNum,
					LineText: lineText,
				})
				if maxCount > 0 && len(matches) >= maxCount {
					break
				}
			}
		} else {
			locs := m.re.FindAllStringIndex(lineText, -1)
			if len(locs) > 0 {
				submatches := make([]model.Submatch, len(locs))
				for j, loc := range locs {
					submatches[j] = model.Submatch{
						Start: loc[0],
						End:   loc[1],
					}
				}

				matches = append(matches, model.SearchMatch{
					LineNum:    lineNum,
					LineText:   lineText,
					Submatches: submatches,
				})

				if maxCount > 0 && len(matches) >= maxCount {
					break
				}
			}
		}
	}

	return matches
}

// MatchBlobWithContext searches data and extracts context groups according to -A, -B, -C.
func (m *Matcher) MatchBlobWithContext(data []byte) ([]ContextGroup, []string, error) {
	lines := splitLines(data)
	matches := m.MatchLines(lines)
	if len(matches) == 0 {
		return nil, lines, nil
	}

	before := m.cfg.BeforeContext
	after := m.cfg.AfterContext

	groups := ExtractContextGroups(lines, matches, before, after)
	return groups, lines, nil
}

// splitLines splits byte data into lines, stripping trailing \r and \n.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}

	var lines []string
	start := 0
	total := len(data)

	for start < total {
		idx := bytes.IndexByte(data[start:], '\n')
		var lineBytes []byte
		if idx == -1 {
			lineBytes = data[start:]
			start = total
		} else {
			lineBytes = data[start : start+idx]
			start += idx + 1
		}

		if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
			lineBytes = lineBytes[:len(lineBytes)-1]
		}
		lines = append(lines, string(lineBytes))
	}

	return lines
}
