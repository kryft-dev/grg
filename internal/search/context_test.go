package search

import (
	"testing"

	"github.com/kryft-dev/grg/internal/model"
)

func TestExtractContextGroups(t *testing.T) {
	lines := []string{
		"line 1",
		"line 2",
		"line 3 MATCH",
		"line 4",
		"line 5 MATCH",
		"line 6",
		"line 7",
		"line 8 MATCH",
		"line 9",
	}

	matches := []model.SearchMatch{
		{LineNum: 3, LineText: "line 3 MATCH"},
		{LineNum: 5, LineText: "line 5 MATCH"},
		{LineNum: 8, LineText: "line 8 MATCH"},
	}

	// Context -C 1: Before=1, After=1
	// Match 3 -> lines 2..4
	// Match 5 -> lines 4..6
	// Overlapping -> Group 1 is lines 2..6
	// Match 8 -> lines 7..9 -> Group 2 is lines 7..9
	// Notice line 6 to line 7 is adjacent (6 + 1 = 7), so interval [2..6] and [7..9] merge into one contiguous group [2..9]!
	groups := ExtractContextGroups(lines, matches, 1, 1)
	if len(groups) != 1 {
		t.Fatalf("expected 1 merged group, got %d", len(groups))
	}
	if len(groups[0].Lines) != 8 {
		t.Fatalf("expected 8 lines in merged group, got %d", len(groups[0].Lines))
	}

	// Context with gap: Before=0, After=0
	groupsNoCtx := ExtractContextGroups(lines, matches, 0, 0)
	if len(groupsNoCtx) != 3 {
		t.Fatalf("expected 3 separate groups with zero context, got %d", len(groupsNoCtx))
	}

	// Context with gap: Before=1, After=0 on line 3 and line 8
	spacedMatches := []model.SearchMatch{
		{LineNum: 3, LineText: "line 3 MATCH"},
		{LineNum: 8, LineText: "line 8 MATCH"},
	}
	groupsSpaced := ExtractContextGroups(lines, spacedMatches, 1, 0)
	if len(groupsSpaced) != 2 {
		t.Fatalf("expected 2 separated groups, got %d", len(groupsSpaced))
	}
	if groupsSpaced[0].Lines[0].LineNum != 2 || groupsSpaced[0].Lines[1].LineNum != 3 {
		t.Errorf("wrong lines in group 0: %+v", groupsSpaced[0].Lines)
	}
	if groupsSpaced[1].Lines[0].LineNum != 7 || groupsSpaced[1].Lines[1].LineNum != 8 {
		t.Errorf("wrong lines in group 1: %+v", groupsSpaced[1].Lines)
	}
}
