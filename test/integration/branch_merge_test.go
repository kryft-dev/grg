package integration

import (
	"strings"
	"testing"
)

// TestMergeCommits_FullDAGTraversal verifies that grg traverses side branches by default on merge commits.
func TestMergeCommits_FullDAGTraversal(t *testing.T) {
	repo := NewTestRepo(t)

	repo.Commit("Base commit on main", map[string]string{
		"base.txt": "shared base content\n",
	})

	// Create feature branch and commit feature-exclusive content
	repo.CreateBranch("feature")
	cFeature := repo.Commit("Implement feature branch work", map[string]string{
		"feature.txt": "FEATURE_BRANCH_PAYLOAD_101\n",
	})

	// Return to main and commit main-exclusive content
	repo.Checkout("main")
	cMain := repo.Commit("Independent progress on main", map[string]string{
		"main.txt": "MAIN_BRANCH_PAYLOAD_202\n",
	})

	// Merge feature into main
	repo.Merge("feature", "Merge feature branch into main")

	// Full DAG traversal: feature branch commit must be discovered as introducing commit
	resFeature := repo.RunSuccess("--color=never", "FEATURE_BRANCH_PAYLOAD_101")
	if !strings.Contains(resFeature.Stdout, "feature.txt") {
		t.Errorf("expected feature.txt in output, got:\n%s", resFeature.Stdout)
	}
	if !strings.Contains(resFeature.Stdout, "FEATURE_BRANCH_PAYLOAD_101") {
		t.Errorf("expected feature match line in output, got:\n%s", resFeature.Stdout)
	}
	if !strings.Contains(resFeature.Stdout, cFeature[:7]) {
		t.Errorf("expected feature commit %s in output, got:\n%s", cFeature[:7], resFeature.Stdout)
	}

	// Main branch commit must also be discovered
	resMain := repo.RunSuccess("--color=never", "MAIN_BRANCH_PAYLOAD_202")
	if !strings.Contains(resMain.Stdout, "main.txt") {
		t.Errorf("expected main.txt in output, got:\n%s", resMain.Stdout)
	}
	if !strings.Contains(resMain.Stdout, "MAIN_BRANCH_PAYLOAD_202") {
		t.Errorf("expected main match line in output, got:\n%s", resMain.Stdout)
	}
	if !strings.Contains(resMain.Stdout, cMain[:7]) {
		t.Errorf("expected main commit %s in output, got:\n%s", cMain[:7], resMain.Stdout)
	}
}

// TestMergeCommits_FirstParent verifies that --first-parent limits traversal to the main lineage
// and does not traverse into intermediate commits on side branches.
func TestMergeCommits_FirstParent(t *testing.T) {
	repo := NewTestRepo(t)

	repo.Commit("Root commit", map[string]string{
		"root.txt": "initial line\n",
	})

	repo.CreateBranch("feature-side")
	// Commit 1 on side branch: intermediate secret
	cSide1 := repo.Commit("Side branch intermediate work", map[string]string{
		"side.txt": "INTERMEDIATE_SIDE_SECRET\n",
	})
	// Commit 2 on side branch: replaces intermediate secret
	cSide2 := repo.Commit("Side branch final work", map[string]string{
		"side.txt": "FINAL_MERGED_SECRET\n",
	})

	repo.Checkout("main")
	cMain := repo.Commit("Main trunk work", map[string]string{
		"trunk.txt": "TRUNK_BRANCH_PAYLOAD_ONLY\n",
	})

	cMerge := repo.Merge("feature-side", "Merge branch feature-side")

	// 1. Full DAG traversal: finds intermediate secret on side branch commit cSide1
	resFull := repo.RunSuccess("--color=never", "INTERMEDIATE_SIDE_SECRET")
	if !strings.Contains(resFull.Stdout, "side.txt") || !strings.Contains(resFull.Stdout, cSide1[:7]) {
		t.Errorf("expected intermediate secret in full DAG output, got:\n%s", resFull.Stdout)
	}

	// 2. With --first-parent: intermediate secret on side branch must NOT be found (exit code 1)
	// because cSide1 was never part of the first-parent lineage.
	resFirstParentNoMatch := repo.RunNoMatch("--first-parent", "INTERMEDIATE_SIDE_SECRET")
	if strings.Contains(resFirstParentNoMatch.Stdout, "INTERMEDIATE_SIDE_SECRET") {
		t.Errorf("did not expect intermediate side commit with --first-parent, got:\n%s", resFirstParentNoMatch.Stdout)
	}

	// 3. Trunk commit must be matched with --first-parent
	resTrunk := repo.RunSuccess("--color=never", "--first-parent", "TRUNK_BRANCH_PAYLOAD_ONLY")
	if !strings.Contains(resTrunk.Stdout, "trunk.txt") || !strings.Contains(resTrunk.Stdout, cMain[:7]) {
		t.Errorf("expected trunk commit in --first-parent output, got:\n%s", resTrunk.Stdout)
	}

	// 4. In full DAG, FINAL_MERGED_SECRET is attributed to cSide2
	resFinalFull := repo.RunSuccess("--color=never", "FINAL_MERGED_SECRET")
	if !strings.Contains(resFinalFull.Stdout, cSide2[:7]) {
		t.Errorf("expected cSide2 in full DAG, got:\n%s", resFinalFull.Stdout)
	}

	// 5. In --first-parent, FINAL_MERGED_SECRET is attributed to the merge commit on the trunk
	resFinalFirstParent := repo.RunSuccess("--color=never", "--first-parent", "FINAL_MERGED_SECRET")
	if !strings.Contains(resFinalFirstParent.Stdout, cMerge[:7]) {
		t.Errorf("expected merge commit %s in --first-parent, got:\n%s", cMerge[:7], resFinalFirstParent.Stdout)
	}
}

// TestMultipleMergesAndDiamondDAG verifies complex diamond DAG topologies and deduplication across branches.
func TestMultipleMergesAndDiamondDAG(t *testing.T) {
	repo := NewTestRepo(t)

	repo.Commit("Common ancestor commit", map[string]string{
		"common.txt": "shared common line\n",
	})

	// Branch A
	repo.CreateBranch("branch-a")
	cA := repo.Commit("Branch A commit", map[string]string{
		"a.txt": "ALPHA_DIAMOND_TOKEN\n",
	})

	// Branch B off main
	repo.Checkout("main")
	repo.CreateBranch("branch-b")
	cB := repo.Commit("Branch B commit", map[string]string{
		"b.txt": "BETA_DIAMOND_TOKEN\n",
	})

	// Merge A into main
	repo.Checkout("main")
	repo.Merge("branch-a", "Merge branch-a into main")

	// Merge B into main
	repo.Merge("branch-b", "Merge branch-b into main")

	// Search for both diamond branch tokens
	resA := repo.RunSuccess("--color=never", "ALPHA_DIAMOND_TOKEN")
	if !strings.Contains(resA.Stdout, "a.txt") || !strings.Contains(resA.Stdout, cA[:7]) {
		t.Errorf("failed finding branch A match in diamond DAG:\n%s", resA.Stdout)
	}

	resB := repo.RunSuccess("--color=never", "BETA_DIAMOND_TOKEN")
	if !strings.Contains(resB.Stdout, "b.txt") || !strings.Contains(resB.Stdout, cB[:7]) {
		t.Errorf("failed finding branch B match in diamond DAG:\n%s", resB.Stdout)
	}
}
