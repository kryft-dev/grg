package integration

import (
	"strings"
	"testing"
)

// TestHistoricalCommitSearch verifies searching across commits where content existed historically.
func TestHistoricalCommitSearch(t *testing.T) {
	repo := NewTestRepo(t)

	c1 := repo.Commit("Initial commit with secret", map[string]string{
		"config.env": "APP_ENV=production\nOLD_API_SECRET_KEY_99=supersecret\nPORT=8080\n",
	})

	c2 := repo.Commit("Rotate secret key", map[string]string{
		"config.env": "APP_ENV=production\nNEW_API_SECRET_KEY_88=rotatedsecret\nPORT=8080\n",
	})

	repo.Commit("Unrelated update", map[string]string{
		"README.md": "# My Service\nDocumentation here.\n",
	})

	// Search for the historical secret replaced in c2
	res1 := repo.RunSuccess("--color=never", "OLD_API_SECRET_KEY_99")
	if !strings.Contains(res1.Stdout, "config.env") {
		t.Errorf("expected config.env in output, got:\n%s", res1.Stdout)
	}
	if !strings.Contains(res1.Stdout, "OLD_API_SECRET_KEY_99=supersecret") {
		t.Errorf("expected matched line in output, got:\n%s", res1.Stdout)
	}
	if !strings.Contains(res1.Stdout, c1[:7]) {
		t.Errorf("expected commit %s in output subheader, got:\n%s", c1[:7], res1.Stdout)
	}

	// Search for the rotated secret introduced in c2
	res2 := repo.RunSuccess("--color=never", "NEW_API_SECRET_KEY_88")
	if !strings.Contains(res2.Stdout, "config.env") {
		t.Errorf("expected config.env in output, got:\n%s", res2.Stdout)
	}
	if !strings.Contains(res2.Stdout, "NEW_API_SECRET_KEY_88=rotatedsecret") {
		t.Errorf("expected matched line in output, got:\n%s", res2.Stdout)
	}
	if !strings.Contains(res2.Stdout, c2[:7]) {
		t.Errorf("expected commit %s in output subheader, got:\n%s", c2[:7], res2.Stdout)
	}
}

// TestDeletedFileDiscovery verifies that files deleted in later commits are discovered in history.
func TestDeletedFileDiscovery(t *testing.T) {
	repo := NewTestRepo(t)

	c1 := repo.Commit("Add legacy database config", map[string]string{
		"legacy/db.conf": "[database]\nhost = db.internal\npassword = LEAKED_PASSWORD_HASH_XYZ\n",
	})

	repo.Commit("Add modern app", map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	})

	// Delete legacy configuration
	repo.RemoveFile("legacy/db.conf")
	repo.Commit("Remove legacy database config", nil)

	// grg should search historical commits and find the deleted file occurrence
	res := repo.RunSuccess("--color=never", "LEAKED_PASSWORD_HASH_XYZ")
	if !strings.Contains(res.Stdout, "legacy/db.conf") {
		t.Errorf("expected legacy/db.conf in output, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "LEAKED_PASSWORD_HASH_XYZ") {
		t.Errorf("expected secret match in output, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, c1[:7]) {
		t.Errorf("expected introducing commit %s in output, got:\n%s", c1[:7], res.Stdout)
	}
}

// TestRenamedFileTracking verifies discovering content across file renames.
func TestRenamedFileTracking(t *testing.T) {
	repo := NewTestRepo(t)

	c1 := repo.Commit("Introduce handler in v1", map[string]string{
		"v1/handler.go": "package v1\n\nfunc HandleWebhookEvent(payload []byte) error {\n\treturn nil\n}\n",
	})

	repo.Commit("Add readme", map[string]string{
		"README.md": "Webhook receiver\n",
	})

	// Rename v1/handler.go to v2/webhook_handler.go
	repo.RenameFile("v1/handler.go", "v2/webhook_handler.go")
	c3 := repo.Commit("Refactor handler into v2 package", nil)

	// Default mode: searches introducing commit
	resDefault := repo.RunSuccess("--color=never", "HandleWebhookEvent")
	if !strings.Contains(resDefault.Stdout, "HandleWebhookEvent") {
		t.Errorf("expected match in output, got:\n%s", resDefault.Stdout)
	}

	// Expand-commits mode: reports all commits and paths where the blob exists
	resExpand := repo.RunSuccess("--color=never", "--expand-commits", "HandleWebhookEvent")
	if !strings.Contains(resExpand.Stdout, "v1/handler.go") {
		t.Errorf("expected v1/handler.go in --expand-commits output, got:\n%s", resExpand.Stdout)
	}
	if !strings.Contains(resExpand.Stdout, "v2/webhook_handler.go") {
		t.Errorf("expected v2/webhook_handler.go in --expand-commits output, got:\n%s", resExpand.Stdout)
	}
	if !strings.Contains(resExpand.Stdout, c1[:7]) || !strings.Contains(resExpand.Stdout, c3[:7]) {
		t.Errorf("expected both commits %s and %s in --expand-commits output, got:\n%s", c1[:7], c3[:7], resExpand.Stdout)
	}
}

// TestRevisionRanges verifies specifying revision ranges (e.g. main~3..main).
func TestRevisionRanges(t *testing.T) {
	repo := NewTestRepo(t)

	c1 := repo.Commit("Commit 1", map[string]string{"file1.txt": "MARKER_TAG_C1\n"})
	c2 := repo.Commit("Commit 2", map[string]string{"file2.txt": "MARKER_TAG_C2\n"})
	c3 := repo.Commit("Commit 3", map[string]string{"file3.txt": "MARKER_TAG_C3\n"})
	c4 := repo.Commit("Commit 4", map[string]string{"file4.txt": "MARKER_TAG_C4\n"})
	c5 := repo.Commit("Commit 5", map[string]string{"file5.txt": "MARKER_TAG_C5\n"})
	c6 := repo.Commit("Commit 6", map[string]string{"file6.txt": "MARKER_TAG_C6\n"})

	_ = c1
	_ = c2
	_ = c3
	_ = c4
	_ = c5
	_ = c6

	// Search range main~3..main (should include C4, C5, C6; exclude C1, C2, C3)
	resRange := repo.RunSuccess("--color=never", "MARKER_TAG", "main~3..main")
	if !strings.Contains(resRange.Stdout, "MARKER_TAG_C4") {
		t.Errorf("expected C4 in range output, got:\n%s", resRange.Stdout)
	}
	if !strings.Contains(resRange.Stdout, "MARKER_TAG_C5") {
		t.Errorf("expected C5 in range output, got:\n%s", resRange.Stdout)
	}
	if !strings.Contains(resRange.Stdout, "MARKER_TAG_C6") {
		t.Errorf("expected C6 in range output, got:\n%s", resRange.Stdout)
	}
	if strings.Contains(resRange.Stdout, "MARKER_TAG_C1") {
		t.Errorf("did not expect C1 in range output, got:\n%s", resRange.Stdout)
	}
	if strings.Contains(resRange.Stdout, "MARKER_TAG_C2") {
		t.Errorf("did not expect C2 in range output, got:\n%s", resRange.Stdout)
	}
	if strings.Contains(resRange.Stdout, "MARKER_TAG_C3") {
		t.Errorf("did not expect C3 in range output, got:\n%s", resRange.Stdout)
	}

	// Search single revision main~4 (traverses main~4 and its parents: C1, C2)
	resSingle := repo.RunSuccess("--color=never", "MARKER_TAG", "main~4")
	if !strings.Contains(resSingle.Stdout, "MARKER_TAG_C1") {
		t.Errorf("expected C1 in main~4 output, got:\n%s", resSingle.Stdout)
	}
	if !strings.Contains(resSingle.Stdout, "MARKER_TAG_C2") {
		t.Errorf("expected C2 in main~4 output, got:\n%s", resSingle.Stdout)
	}
	if strings.Contains(resSingle.Stdout, "MARKER_TAG_C3") {
		t.Errorf("did not expect C3 in main~4 output, got:\n%s", resSingle.Stdout)
	}
	if strings.Contains(resSingle.Stdout, "MARKER_TAG_C6") {
		t.Errorf("did not expect C6 in main~4 output, got:\n%s", resSingle.Stdout)
	}
}
