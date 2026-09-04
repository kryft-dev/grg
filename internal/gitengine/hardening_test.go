package gitengine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kryft-dev/grg/internal/model"
)

// SEC-01: Path traversal in ResolveRef and readLooseRef
func TestSecurity_PathTraversal_Refs(t *testing.T) {
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	_ = os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644)
	_ = os.WriteFile(filepath.Join(gitDir, "refs", "heads", "main"), []byte("0123456789abcdef0123456789abcdef01234567\n"), 0644)

	repo := &RepoInfo{
		WorkTree:     tmpDir,
		GitDir:       gitDir,
		CommonGitDir: gitDir,
	}

	traversalPayloads := []string{
		"../../../../etc/passwd",
		"../HEAD",
		"refs/../../HEAD",
		"/etc/passwd",
		"refs/heads/../../HEAD",
		"..",
		".",
		"refs/heads/main/../../../outside",
	}

	for _, payload := range traversalPayloads {
		_, err := ResolveRef(repo, payload)
		if err == nil {
			t.Errorf("ResolveRef(%q) expected error, got nil", payload)
		}
		if !errors.Is(err, ErrInvalidRef) && !errors.Is(err, ErrRefNotFound) {
			t.Errorf("ResolveRef(%q) expected ErrInvalidRef or ErrRefNotFound, got %v", payload, err)
		}
	}
}

// SEC-02: Path traversal in loose object reader
func TestSecurity_PathTraversal_LooseObjects(t *testing.T) {
	tmpDir := t.TempDir()
	loose := NewLooseReader(tmpDir)

	invalidOIDs := []string{
		"../../../../etc/passwd",
		"../0123456789abcdef0123456789abcdef012345",
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
		"12",
		"",
		"0123456789abcdef", // too short (not 40 or 64 hex chars)
	}

	for _, oid := range invalidOIDs {
		if loose.HasObject(oid) {
			t.Errorf("HasObject(%q) expected false, got true", oid)
		}
		_, err := loose.ReadObject(oid)
		if err == nil {
			t.Errorf("ReadObject(%q) expected error, got nil", oid)
		}
	}
}

// SEC-14: uint32 addition wraparound in delta instruction bounds check
func TestSafety_Delta_Uint32Overflow(t *testing.T) {
	base := make([]byte, 100)

	// Delta header: baseSize=100, targetSize=100
	deltaHdr := []byte{100, 100}

	// Copy instruction with offset = 0xFFFFFFFF and size = 1
	// In 32-bit math, offset + size = 0, which would wrap to 0 and bypass bounds check if unhardened.
	copyInstr := []byte{
		0x80 | 0x01 | 0x02 | 0x04 | 0x08 | 0x10, // copy with 4 offset bytes and 1 size byte
		0xff, 0xff, 0xff, 0xff,                  // offset: 0xFFFFFFFF
		0x01,                                    // size: 1
	}

	payload := append(deltaHdr, copyInstr...)
	_, err := ApplyDelta(base, payload)
	if err == nil {
		t.Fatalf("expected error on overflowing copy instruction, got nil")
	}
	if !errors.Is(err, ErrDeltaCorrupt) {
		t.Fatalf("expected ErrDeltaCorrupt, got %v", err)
	}
}

// SEC-07: Bounded LEB128 shift in delta header
func TestSafety_Delta_LEB128ShiftOverflow(t *testing.T) {
	// Delta payload with > 10 continuation bytes (shift >= 64)
	malformed := bytes.Repeat([]byte{0x80}, 15)
	_, _, _, err := ReadDeltaHeader(malformed)
	if err == nil {
		t.Fatalf("expected error on excessive LEB128 continuation bytes, got nil")
	}
}

// SEC-06: Pack index fanout allocation bounds and truncation checks
func TestSafety_PackIndex_TruncatedOrFanoutOverflow(t *testing.T) {
	data := make([]byte, 8+1024+40)
	copy(data[:4], idxV2Magic)
	binary.BigEndian.PutUint32(data[4:8], 2)

	// Set fanout[255] = 0x7FFFFFFF (huge count that would cause huge allocation)
	binary.BigEndian.PutUint32(data[8+255*4:8+256*4], 0x7FFFFFFF)

	_, err := ParsePackIndex(data)
	if err == nil {
		t.Fatalf("expected error on pack index with huge fanout count exceeding file size, got nil")
	}
	if !errors.Is(err, ErrIdxInvalid) {
		t.Fatalf("expected ErrIdxInvalid, got %v", err)
	}
}

// SEC-11: Circular tree reference and tree depth limit
func TestSafety_Tree_CycleAndMaxDepth(t *testing.T) {
	reader := newMockReader()

	// Create circular tree: treeA contains treeB, treeB contains treeA
	treeBOID := "0123456789abcdef0123456789abcdef01234562"
	treeAOID := "0123456789abcdef0123456789abcdef01234561"

	treeAData := buildTreePayload([]TreeEntry{
		{Mode: 0040000, Name: "sub", OID: treeBOID},
	})
	treeBData := buildTreePayload([]TreeEntry{
		{Mode: 0040000, Name: "loop", OID: treeAOID},
	})

	reader.objects[treeAOID] = &Object{OID: treeAOID, Type: TypeTree, Data: treeAData}
	reader.objects[treeBOID] = &Object{OID: treeBOID, Type: TypeTree, Data: treeBData}

	err := TraverseTree(reader, treeAOID, func(path string, entry TreeEntry) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error on circular tree reference, got nil")
	}
	if !errors.Is(err, ErrCorruptObject) {
		t.Fatalf("expected ErrCorruptObject, got %v", err)
	}
}

// SEC-12: Iterative traverseExclude handling deep history without stack overflow
func TestSafety_Walker_DeepLinearHistory(t *testing.T) {
	reader := newMockReader()

	// Chain 5000 commits linearly: commit_N -> commit_{N-1} -> ...
	numCommits := 5000
	var prevSHA string

	for i := 0; i < numCommits; i++ {
		var commitRaw string
		if prevSHA == "" {
			commitRaw = "tree 0123456789abcdef0123456789abcdef01234567\nauthor Test <t@t.com> 1000 +0000\ncommitter Test <t@t.com> 1000 +0000\n\nroot\n"
		} else {
			commitRaw = fmt.Sprintf("tree 0123456789abcdef0123456789abcdef01234567\nparent %s\nauthor Test <t@t.com> 1000 +0000\ncommitter Test <t@t.com> 1000 +0000\n\ncommit %d\n", prevSHA, i)
		}
		sha := fmt.Sprintf("%040x", i+1)
		reader.objects[sha] = &Object{
			OID:  sha,
			Type: TypeCommit,
			Data: []byte(commitRaw),
		}
		prevSHA = sha
	}

	walker := NewHistoryWalker(nil, reader, &model.Config{}, nil)
	excluded := make(map[string]bool)

	// Calling traverseExclude on the tip commit traverses all 5000 parents
	// In the recursive implementation, this risked stack overflow.
	// With the iterative stack, it completes instantly and safely.
	walker.traverseExclude(prevSHA, excluded)

	if len(excluded) != numCommits {
		t.Fatalf("expected %d excluded commits, got %d", numCommits, len(excluded))
	}
}

// SEC-15: Concurrency safety in RepositoryReader (concurrent ReadObject and Close)
func TestConcurrency_RepositoryReader_Deadlock(t *testing.T) {
	reader := &RepositoryReader{}

	var wg sync.WaitGroup
	// Run concurrent ReadObject and Close calls
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = reader.ReadObject("0123456789abcdef0123456789abcdef01234567")
				_ = reader.HasObject("0123456789abcdef0123456789abcdef01234567")
			}
		}()
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reader.Close()
		}()
	}

	wg.Wait()
}

// PERF-04: CompareTreeEntries canonical ordering verification
func TestPerformance_CompareTreeEntries(t *testing.T) {
	// In Git canonical ordering, directory "a" (treated as "a/") sorts AFTER file "a.txt" ('.' is 46, '/' is 47),
	// but BEFORE file "a0" ('0' is 48).
	dirA := TreeEntry{Name: "a", Mode: 0040000}
	fileDot := TreeEntry{Name: "a.txt", Mode: 0100644}
	fileZero := TreeEntry{Name: "a0", Mode: 0100644}

	// fileDot vs dirA: "a.txt" ('.' = 46) vs "a/" ('/' = 47) -> fileDot < dirA
	if cmp := compareTreeEntries(fileDot, dirA); cmp >= 0 {
		t.Errorf("expected file 'a.txt' < dir 'a', got cmp=%d", cmp)
	}

	// dirA vs fileZero: "a/" ('/' = 47) vs "a0" ('0' = 48) -> dirA < fileZero
	if cmp := compareTreeEntries(dirA, fileZero); cmp >= 0 {
		t.Errorf("expected dir 'a' < file 'a0', got cmp=%d", cmp)
	}

	// Equality
	if cmp := compareTreeEntries(dirA, dirA); cmp != 0 {
		t.Errorf("expected cmp == 0 for identical entries, got %d", cmp)
	}
}
