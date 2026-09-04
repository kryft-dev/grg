package filter

import (
	"testing"
)

func TestGlobMatcher(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		expected bool
	}{
		{
			name:     "simple extension basename",
			patterns: []string{"*.go"},
			path:     "main.go",
			expected: true,
		},
		{
			name:     "simple extension in subdirectory",
			patterns: []string{"*.go"},
			path:     "internal/gitengine/loose.go",
			expected: true,
		},
		{
			name:     "simple extension negative match",
			patterns: []string{"*.go"},
			path:     "main.py",
			expected: false,
		},
		{
			name:     "directory prefix wildcard",
			patterns: []string{"internal/**"},
			path:     "internal/gitengine/loose.go",
			expected: true,
		},
		{
			name:     "directory prefix negative match",
			patterns: []string{"internal/**"},
			path:     "cmd/grg/main.go",
			expected: false,
		},
		{
			name:     "positive and negative glob",
			patterns: []string{"*.go", "!*_test.go"},
			path:     "main.go",
			expected: true,
		},
		{
			name:     "positive and negative glob excluded",
			patterns: []string{"*.go", "!*_test.go"},
			path:     "main_test.go",
			expected: false,
		},
		{
			name:     "negative glob only excludes matches",
			patterns: []string{"!vendor/**"},
			path:     "vendor/pkg/foo.go",
			expected: false,
		},
		{
			name:     "negative glob only keeps non-matches",
			patterns: []string{"!vendor/**"},
			path:     "internal/foo.go",
			expected: true,
		},
		{
			name:     "negation override",
			patterns: []string{"!vendor/**", "vendor/keep/**"},
			path:     "vendor/keep/safe.go",
			expected: true,
		},
		{
			name:     "character class",
			patterns: []string{"doc[0-9].md"},
			path:     "doc1.md",
			expected: true,
		},
		{
			name:     "character class mismatch",
			patterns: []string{"doc[0-9].md"},
			path:     "docA.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gm, err := NewGlobMatcher(tt.patterns)
			if err != nil {
				t.Fatalf("NewGlobMatcher failed: %v", err)
			}
			res := gm.Match(tt.path)
			if res != tt.expected {
				t.Errorf("Match(%q) = %v, expected %v", tt.path, res, tt.expected)
			}
		})
	}
}
