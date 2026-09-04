package gitengine

import (
	"testing"
	"time"
)

func TestParseCommit(t *testing.T) {
	raw := `tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904
parent aaaaa11111222223333344444555556666677777
parent bbbbb11111222223333344444555556666677777
author Alice Smith <alice@example.com> 1609459200 +0200
committer Bob Jones <bob@example.com> 1609459260 -0500
gpgsig -----BEGIN PGP SIGNATURE-----
 iQIzBAABCAAdFiEE...
 -----END PGP SIGNATURE-----

feat: add history walker

This is the extended body message explaining the change.
Another paragraph here.
`

	sha := "cccc11111222223333344444555556666677777"
	meta, err := ParseCommit(sha, []byte(raw))
	if err != nil {
		t.Fatalf("ParseCommit failed: %v", err)
	}

	if meta.SHA != sha {
		t.Errorf("expected SHA %s, got %s", sha, meta.SHA)
	}
	if meta.TreeOID != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" {
		t.Errorf("wrong TreeOID: %s", meta.TreeOID)
	}
	if len(meta.Parents) != 2 {
		t.Fatalf("expected 2 parents, got %d", len(meta.Parents))
	}
	if meta.Parents[0] != "aaaaa11111222223333344444555556666677777" ||
		meta.Parents[1] != "bbbbb11111222223333344444555556666677777" {
		t.Errorf("wrong parents: %v", meta.Parents)
	}
	if meta.AuthorName != "Alice Smith" {
		t.Errorf("wrong AuthorName: %s", meta.AuthorName)
	}
	if meta.AuthorEmail != "alice@example.com" {
		t.Errorf("wrong AuthorEmail: %s", meta.AuthorEmail)
	}
	if meta.CommitterName != "Bob Jones" {
		t.Errorf("wrong CommitterName: %s", meta.CommitterName)
	}
	if meta.CommitterEmail != "bob@example.com" {
		t.Errorf("wrong CommitterEmail: %s", meta.CommitterEmail)
	}

	// Verify dates
	expectedAuthorDate := time.Unix(1609459200, 0).In(time.FixedZone("+0200", 2*3600))
	if !meta.AuthorDate.Equal(expectedAuthorDate) {
		t.Errorf("expected author date %v, got %v", expectedAuthorDate, meta.AuthorDate)
	}

	if meta.Summary != "feat: add history walker" {
		t.Errorf("wrong summary: %q", meta.Summary)
	}
}

func TestParseCommitMissingTree(t *testing.T) {
	raw := `author Alice <a@b.com> 1000 +0000

Commit message
`
	_, err := ParseCommit("1234", []byte(raw))
	if err == nil {
		t.Fatal("expected error for missing tree OID, got nil")
	}
}
