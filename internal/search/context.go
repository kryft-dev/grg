package search

import (
	"github.com/kryft-dev/grg/internal/model"
)

// ContextLine represents either a matched line or a surrounding context line.
type ContextLine struct {
	LineNum    int
	LineText   string
	IsMatch    bool
	Submatches []model.Submatch
}

// ContextGroup represents a contiguous group of matches and surrounding context lines.
type ContextGroup struct {
	Lines []ContextLine
}

// ExtractContextGroups slices lines into contiguous groups including before and after context.
func ExtractContextGroups(lines []string, matches []model.SearchMatch, before, after int) []ContextGroup {
	if len(matches) == 0 {
		return nil
	}
	if before <= 0 && after <= 0 {
		// No context requested, each match is its own group
		var groups []ContextGroup
		for _, m := range matches {
			groups = append(groups, ContextGroup{
				Lines: []ContextLine{{
					LineNum:    m.LineNum,
					LineText:   m.LineText,
					IsMatch:    true,
					Submatches: m.Submatches,
				}},
			})
		}
		return groups
	}

	totalLines := len(lines)
	matchMap := make(map[int]model.SearchMatch, len(matches))
	for _, m := range matches {
		matchMap[m.LineNum] = m
	}

	// Compute intervals of lines to include [start, end] (1-based)
	type interval struct {
		start int
		end   int
	}
	var intervals []interval
	for _, m := range matches {
		start := m.LineNum - before
		if start < 1 {
			start = 1
		}
		end := m.LineNum + after
		if end > totalLines {
			end = totalLines
		}
		intervals = append(intervals, interval{start: start, end: end})
	}

	// Merge overlapping or adjacent intervals
	var merged []interval
	curr := intervals[0]
	for i := 1; i < len(intervals); i++ {
		next := intervals[i]
		if next.start <= curr.end+1 {
			// Overlapping or adjacent: merge
			if next.end > curr.end {
				curr.end = next.end
			}
		} else {
			merged = append(merged, curr)
			curr = next
		}
	}
	merged = append(merged, curr)

	// Build context groups from merged intervals
	groups := make([]ContextGroup, 0, len(merged))
	for _, iv := range merged {
		var groupLines []ContextLine
		for lineNum := iv.start; lineNum <= iv.end; lineNum++ {
			lineIdx := lineNum - 1
			lineText := ""
			if lineIdx < totalLines {
				lineText = lines[lineIdx]
			}

			if m, isMatch := matchMap[lineNum]; isMatch {
				groupLines = append(groupLines, ContextLine{
					LineNum:    lineNum,
					LineText:   m.LineText,
					IsMatch:    true,
					Submatches: m.Submatches,
				})
			} else {
				groupLines = append(groupLines, ContextLine{
					LineNum:  lineNum,
					LineText: lineText,
					IsMatch:  false,
				})
			}
		}
		groups = append(groups, ContextGroup{Lines: groupLines})
	}

	return groups
}
