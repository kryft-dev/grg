package filter

import (
	"testing"
)

func TestTypeMatcher(t *testing.T) {
	tm, err := NewTypeMatcher([]string{"go", "rust"})
	if err != nil {
		t.Fatalf("NewTypeMatcher failed: %v", err)
	}

	if !tm.Match("main.go") {
		t.Errorf("expected main.go to match")
	}
	if !tm.Match("src/lib.rs") {
		t.Errorf("expected src/lib.rs to match")
	}
	if tm.Match("index.js") {
		t.Errorf("index.js should not match go/rust")
	}

	// Test dockerfile exact name
	dockerTm, err := NewTypeMatcher([]string{"docker"})
	if err != nil {
		t.Fatalf("NewTypeMatcher docker failed: %v", err)
	}
	if !dockerTm.Match("Dockerfile") {
		t.Errorf("Dockerfile should match docker type")
	}
	if !dockerTm.Match("my.dockerfile") {
		t.Errorf("my.dockerfile should match docker type")
	}

	// Unknown type error
	_, err = NewTypeMatcher([]string{"nonexistenttype"})
	if err == nil {
		t.Errorf("expected error for unknown type")
	}

	// Nil / empty matcher matches all
	var emptyTm *TypeMatcher
	if !emptyTm.Match("anything.xyz") {
		t.Errorf("empty TypeMatcher should match everything")
	}

	// ListTypes
	types := ListTypes()
	if len(types) < 20 {
		t.Errorf("expected at least 20 supported types, got %d", len(types))
	}
}
