package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLooseObjectsRepo verifies that grg accurately searches repos containing exclusively loose objects.
func TestLooseObjectsRepo(t *testing.T) {
	repo := NewTestRepo(t)

	c1 := repo.Commit("Commit 1 loose", map[string]string{
		"file1.txt": "LOOSE_OBJECT_STRING_1\n",
	})
	c2 := repo.Commit("Commit 2 loose", map[string]string{
		"file2.txt": "LOOSE_OBJECT_STRING_2\n",
	})

	// Verify that objects are loose (no packfiles exist yet)
	packDir := filepath.Join(repo.Dir, ".git", "objects", "pack")
	entries, _ := os.ReadDir(packDir)
	packCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pack") {
			packCount++
		}
	}
	if packCount > 0 {
		t.Logf("note: repo already has %d packfiles", packCount)
	}

	// Search loose objects
	res1 := repo.RunSuccess("--color=never", "LOOSE_OBJECT_STRING_1")
	if !strings.Contains(res1.Stdout, "file1.txt") || !strings.Contains(res1.Stdout, c1[:7]) {
		t.Errorf("failed to match loose object in commit 1:\n%s", res1.Stdout)
	}

	res2 := repo.RunSuccess("--color=never", "LOOSE_OBJECT_STRING_2")
	if !strings.Contains(res2.Stdout, "file2.txt") || !strings.Contains(res2.Stdout, c2[:7]) {
		t.Errorf("failed to match loose object in commit 2:\n%s", res2.Stdout)
	}
}

// TestFullyPackedRepo_Aggressive verifies searching after git gc --aggressive has packed all objects into .pack/.idx.
func TestFullyPackedRepo_Aggressive(t *testing.T) {
	repo := NewTestRepo(t)

	c1 := repo.Commit("Commit A", map[string]string{
		"doc_a.txt": "PACKED_ALPHA_TARGET\n",
	})
	c2 := repo.Commit("Commit B", map[string]string{
		"doc_b.txt": "PACKED_BETA_TARGET\n",
	})

	// Aggressive pack and prune loose objects
	repo.GC(true)

	// Verify packfile exists
	packDir := filepath.Join(repo.Dir, ".git", "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		t.Fatalf("failed reading pack dir: %v", err)
	}
	hasPack := false
	hasIdx := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pack") {
			hasPack = true
		}
		if strings.HasSuffix(e.Name(), ".idx") {
			hasIdx = true
		}
	}
	if !hasPack || !hasIdx {
		t.Fatalf("expected .pack and .idx files after aggressive gc")
	}

	// Search packed objects
	resA := repo.RunSuccess("--color=never", "PACKED_ALPHA_TARGET")
	if !strings.Contains(resA.Stdout, "doc_a.txt") || !strings.Contains(resA.Stdout, c1[:7]) {
		t.Errorf("failed matching packed alpha object:\n%s", resA.Stdout)
	}

	resB := repo.RunSuccess("--color=never", "PACKED_BETA_TARGET")
	if !strings.Contains(resB.Stdout, "doc_b.txt") || !strings.Contains(resB.Stdout, c2[:7]) {
		t.Errorf("failed matching packed beta object:\n%s", resB.Stdout)
	}
}

// TestMixedPackedAndLooseRepo verifies seamless searching across both packed historical objects
// and newer loose objects created after packing.
func TestMixedPackedAndLooseRepo(t *testing.T) {
	repo := NewTestRepo(t)

	// 1. Create base commits and pack them
	cPacked := repo.Commit("Packed base commit", map[string]string{
		"historical.txt": "HYBRID_PACKED_PORTION\n",
	})
	repo.GC(false)

	// 2. Create new commits as loose objects
	cLoose := repo.Commit("New loose commit", map[string]string{
		"recent.txt": "HYBRID_LOOSE_PORTION\n",
	})

	// Search packed historical commit
	resPacked := repo.RunSuccess("--color=never", "HYBRID_PACKED_PORTION")
	if !strings.Contains(resPacked.Stdout, "historical.txt") || !strings.Contains(resPacked.Stdout, cPacked[:7]) {
		t.Errorf("failed matching packed portion:\n%s", resPacked.Stdout)
	}

	// Search loose recent commit
	resLoose := repo.RunSuccess("--color=never", "HYBRID_LOOSE_PORTION")
	if !strings.Contains(resLoose.Stdout, "recent.txt") || !strings.Contains(resLoose.Stdout, cLoose[:7]) {
		t.Errorf("failed matching loose portion:\n%s", resLoose.Stdout)
	}
}

// TestDeltifiedPackfile verifies delta-compressed objects (OFS_DELTA / REF_DELTA) in packfiles.
func TestDeltifiedPackfile(t *testing.T) {
	repo := NewTestRepo(t)

	// Generate incremental revisions of a file to produce delta chains during pack compression
	baseText := strings.Repeat("Shared common baseline boilerplate text.\n", 50)
	var commits []string

	for i := 1; i <= 6; i++ {
		revText := baseText + "\nREVISION_DELTA_TOKEN_" + string(rune('0'+i)) + "\n"
		c := repo.Commit("Revision "+string(rune('0'+i)), map[string]string{
			"versioned.txt": revText,
		})
		commits = append(commits, c)
	}

	// Aggressive GC forces delta compression among revisions
	repo.GC(true)

	// Verify all revisions are searchable from delta-compressed packfile
	for i := 1; i <= 6; i++ {
		query := "REVISION_DELTA_TOKEN_" + string(rune('0'+i))
		res := repo.RunSuccess("--color=never", query)
		if !strings.Contains(res.Stdout, "versioned.txt") {
			t.Errorf("expected versioned.txt in output for %s, got:\n%s", query, res.Stdout)
		}
		if !strings.Contains(res.Stdout, query) {
			t.Errorf("expected %s in output, got:\n%s", query, res.Stdout)
		}
		commitSHA := commits[i-1]
		if !strings.Contains(res.Stdout, commitSHA[:7]) {
			t.Errorf("expected commit %s in output for %s, got:\n%s", commitSHA[:7], query, res.Stdout)
		}
	}
}
