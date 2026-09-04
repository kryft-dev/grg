package gitengine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRef(t *testing.T) {
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")

	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0755); err != nil {
		t.Fatalf("failed to create refs dir: %v", err)
	}

	mainOID := "1111111111111111111111111111111111111111"
	featureOID := "2222222222222222222222222222222222222222"
	tagOID := "3333333333333333333333333333333333333333"

	// 1. Write HEAD -> refs/heads/main
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("failed to write HEAD: %v", err)
	}

	// 2. Write loose branch main
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "heads", "main"), []byte(mainOID+"\n"), 0644); err != nil {
		t.Fatalf("failed to write branch: %v", err)
	}

	// 3. Write packed-refs with feature and tag
	packedContent := "# pack-refs with: peeled\n" +
		featureOID + " refs/heads/feature\n" +
		tagOID + " refs/tags/v1.0.0\n" +
		"^" + mainOID + "\n"
	if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packedContent), 0644); err != nil {
		t.Fatalf("failed to write packed-refs: %v", err)
	}

	repo := &RepoInfo{
		GitDir:       gitDir,
		CommonGitDir: gitDir,
	}

	// Test HEAD
	resolvedHEAD, err := ResolveRef(repo, "HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) error: %v", err)
	}
	if resolvedHEAD != mainOID {
		t.Errorf("expected %s, got %s", mainOID, resolvedHEAD)
	}

	// Test branch name "main"
	resolvedMain, err := ResolveRef(repo, "main")
	if err != nil {
		t.Fatalf("ResolveRef(main) error: %v", err)
	}
	if resolvedMain != mainOID {
		t.Errorf("expected %s, got %s", mainOID, resolvedMain)
	}

	// Test packed branch "feature"
	resolvedFeature, err := ResolveRef(repo, "feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature) error: %v", err)
	}
	if resolvedFeature != featureOID {
		t.Errorf("expected %s, got %s", featureOID, resolvedFeature)
	}

	// Test tag "v1.0.0"
	resolvedTag, err := ResolveRef(repo, "v1.0.0")
	if err != nil {
		t.Fatalf("ResolveRef(v1.0.0) error: %v", err)
	}
	if resolvedTag != tagOID {
		t.Errorf("expected %s, got %s", tagOID, resolvedTag)
	}

	// Test direct OID lookup
	directOID := "4444444444444444444444444444444444444444"
	resolvedDirect, err := ResolveRef(repo, directOID)
	if err != nil {
		t.Fatalf("ResolveRef(directOID) error: %v", err)
	}
	if resolvedDirect != directOID {
		t.Errorf("expected %s, got %s", directOID, resolvedDirect)
	}

	// Test ListAllRefs
	allRefs, err := ListAllRefs(repo)
	if err != nil {
		t.Fatalf("ListAllRefs error: %v", err)
	}
	if allRefs["refs/heads/main"] != mainOID {
		t.Errorf("missing or wrong refs/heads/main in ListAllRefs")
	}
	if allRefs["refs/heads/feature"] != featureOID {
		t.Errorf("missing or wrong refs/heads/feature in ListAllRefs")
	}
	if allRefs["refs/tags/v1.0.0"] != tagOID {
		t.Errorf("missing or wrong refs/tags/v1.0.0 in ListAllRefs")
	}
	if allRefs["HEAD"] != mainOID {
		t.Errorf("missing or wrong HEAD in ListAllRefs")
	}
}
