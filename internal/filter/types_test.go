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

func TestLookupTypeReturnsCopy(t *testing.T) {
	def, ok := LookupType("docker")
	if !ok {
		t.Fatal("expected docker to be a known type")
	}
	if len(def.Extensions) == 0 || len(def.Filenames) == 0 {
		t.Fatalf("docker definition should carry extensions and filenames, got %+v", def)
	}

	wantExt := def.Extensions[0]
	wantName := def.Filenames[0]

	// A caller mutating the returned slices must not corrupt the builtin table.
	def.Extensions[0] = ".corrupted"
	def.Filenames[0] = "Corrupted"

	again, ok := LookupType("docker")
	if !ok {
		t.Fatal("docker disappeared from the builtin table")
	}
	if again.Extensions[0] != wantExt {
		t.Errorf("Extensions mutation leaked: got %q, want %q", again.Extensions[0], wantExt)
	}
	if again.Filenames[0] != wantName {
		t.Errorf("Filenames mutation leaked: got %q, want %q", again.Filenames[0], wantName)
	}

	// Matching must still work after the mutation attempt.
	tm, err := NewTypeMatcher([]string{"docker"})
	if err != nil {
		t.Fatalf("NewTypeMatcher failed: %v", err)
	}
	if !tm.Match("Dockerfile") {
		t.Error("Dockerfile stopped matching after a caller mutated a LookupType result")
	}
	if !tm.Match("my.dockerfile") {
		t.Error("my.dockerfile stopped matching after a caller mutated a LookupType result")
	}

	if _, ok := LookupType("nonexistenttype"); ok {
		t.Error("expected LookupType to report an unknown type as missing")
	}
}
