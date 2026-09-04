package search

import (
	"testing"

	"github.com/kryft-dev/grg/internal/model"
)

func TestMatcherLiteralAndRegex(t *testing.T) {
	text := []byte("func main() {\n\tprintln(\"hello world\")\n}\n")

	// 1. Literal matching
	cfgFixed := &model.Config{
		Pattern:      "println(\"hello",
		FixedStrings: true,
	}
	mFixed, err := NewMatcher(cfgFixed)
	if err != nil {
		t.Fatalf("NewMatcher fixed failed: %v", err)
	}
	matches, err := mFixed.MatchBlob(text)
	if err != nil {
		t.Fatalf("MatchBlob error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].LineNum != 2 {
		t.Errorf("expected line 2, got %d", matches[0].LineNum)
	}

	// 2. Regex matching
	cfgRegex := &model.Config{
		Pattern: `func\s+main\(\)`,
	}
	mRegex, err := NewMatcher(cfgRegex)
	if err != nil {
		t.Fatalf("NewMatcher regex failed: %v", err)
	}
	matchesRegex, err := mRegex.MatchBlob(text)
	if err != nil {
		t.Fatalf("MatchBlob error: %v", err)
	}
	if len(matchesRegex) != 1 || matchesRegex[0].LineNum != 1 {
		t.Fatalf("expected 1 match at line 1, got %+v", matchesRegex)
	}
}

func TestMatcherCaseModes(t *testing.T) {
	text := []byte("Apple\napple\nBANANA\n")

	// SmartCase with lowercase: case-insensitive
	cfgSmartLower := &model.Config{
		Pattern:   "apple",
		SmartCase: true,
	}
	mSmartLower, _ := NewMatcher(cfgSmartLower)
	m1, _ := mSmartLower.MatchBlob(text)
	if len(m1) != 2 {
		t.Errorf("smartcase lowercase should match Apple and apple, got %d matches", len(m1))
	}

	// SmartCase with uppercase: case-sensitive
	cfgSmartUpper := &model.Config{
		Pattern:   "Apple",
		SmartCase: true,
	}
	mSmartUpper, _ := NewMatcher(cfgSmartUpper)
	m2, _ := mSmartUpper.MatchBlob(text)
	if len(m2) != 1 || m2[0].LineNum != 1 {
		t.Errorf("smartcase uppercase should only match Apple, got %d matches", len(m2))
	}

	// IgnoreCase explicitly
	cfgIgnore := &model.Config{
		Pattern:    "banana",
		IgnoreCase: true,
	}
	mIgnore, _ := NewMatcher(cfgIgnore)
	m3, _ := mIgnore.MatchBlob(text)
	if len(m3) != 1 {
		t.Errorf("ignore-case should match BANANA, got %d matches", len(m3))
	}
}

func TestMatcherWordBoundary(t *testing.T) {
	text := []byte("int target = 42;\nint my_target_val = 0;\nint target_two = 1;\n")

	cfgWord := &model.Config{
		Pattern:    "target",
		WordRegexp: true,
	}
	mWord, err := NewMatcher(cfgWord)
	if err != nil {
		t.Fatalf("NewMatcher word failed: %v", err)
	}
	matches, _ := mWord.MatchBlob(text)
	if len(matches) != 1 || matches[0].LineNum != 1 {
		t.Errorf("word boundary should only match line 1, got %d matches", len(matches))
	}
}

func TestMatcherInvertAndMaxCount(t *testing.T) {
	text := []byte("match 1\nskip\nmatch 2\nskip\nmatch 3\n")

	// Invert match (-v)
	cfgInvert := &model.Config{
		Pattern:     "skip",
		InvertMatch: true,
	}
	mInvert, _ := NewMatcher(cfgInvert)
	matchesInv, _ := mInvert.MatchBlob(text)
	if len(matchesInv) != 3 {
		t.Errorf("expected 3 inverted matches, got %d", len(matchesInv))
	}

	// MaxCount (-m 2)
	cfgMax := &model.Config{
		Pattern:  "match",
		MaxCount: 2,
	}
	mMax, _ := NewMatcher(cfgMax)
	matchesMax, _ := mMax.MatchBlob(text)
	if len(matchesMax) != 2 {
		t.Errorf("expected max 2 matches, got %d", len(matchesMax))
	}
}

func TestMatcher_MatchBlob_ShortCircuit(t *testing.T) {
	text := []byte("alpha\nbeta\ngamma\ndelta\n")
	cfg := &model.Config{
		Pattern: "nonexistent_pattern",
	}
	m, err := NewMatcher(cfg)
	if err != nil {
		t.Fatalf("NewMatcher failed: %v", err)
	}

	matches, err := m.MatchBlob(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil matches for non-matching blob, got %v", matches)
	}

	// Verify zero allocations on non-matching blobs (tolerating race detector overhead)
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = m.MatchBlob(text)
	})
	if allocs > 1 {
		t.Errorf("expected at most 1 allocation (0 in non-race mode) on short-circuit path, got %v", allocs)
	}
}
