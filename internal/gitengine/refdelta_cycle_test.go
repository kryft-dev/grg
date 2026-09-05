package gitengine

import (
	"encoding/hex"
	"errors"
	"testing"
)

func mustHexSHA(t *testing.T, s string) [20]byte {
	t.Helper()

	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("invalid hex SHA %q: %v", s, err)
	}
	var sha [20]byte
	if len(raw) != len(sha) {
		t.Fatalf("expected a 20-byte SHA, got %d bytes", len(raw))
	}
	copy(sha[:], raw)
	return sha
}

// TestRefDeltaCycle_ReturnsCorruptObject covers a packfile containing an
// OBJ_REF_DELTA cycle: A is a delta against B and B is a delta against A.
//
// Resolving such a chain used to re-enter the pack through ReadObject, which
// restarted the delta-depth count at zero on every hop, so the maxDeltaDepth
// ceiling never tripped and the recursion continued until the goroutine stack was
// exhausted. That is a fatal runtime error, not a panic: it cannot be recovered
// and it takes the whole process down. The depth must survive the resolver hop so
// the cycle is reported as a corrupt object instead.
func TestRefDeltaCycle_ReturnsCorruptObject(t *testing.T) {
	shaA := mustHexSHA(t, "aa11111111111111111111111111111111111111")
	shaB := mustHexSHA(t, "bb22222222222222222222222222222222222222")

	// base size 1, target size 1, insert one literal byte
	delta := []byte{0x01, 0x01, 0x01, 'x'}

	gitDir := writeTestPack(t, "cycle", []testPackObj{
		{sha: shaA, objType: TypeRefDelta, raw: delta, baseSHA: shaB},
		{sha: shaB, objType: TypeRefDelta, raw: delta, baseSHA: shaA},
	})

	// NewRepositoryReader wires the combined reader in as each pack's resolver,
	// which is the production path the cycle escapes through.
	reader, err := NewRepositoryReader(&RepoInfo{CommonGitDir: gitDir})
	if err != nil {
		t.Fatalf("NewRepositoryReader failed: %v", err)
	}
	defer func() { _ = reader.Close() }()

	oidA := hex.EncodeToString(shaA[:])
	obj, err := reader.ReadObject(oidA)
	if err == nil {
		t.Fatalf("expected an error for the REF_DELTA cycle, got object %+v", obj)
	}
	if !errors.Is(err, ErrCorruptObject) {
		t.Fatalf("expected an error wrapping ErrCorruptObject, got %v", err)
	}
	if errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("a corrupt delta chain must not be reported as a missing object: %v", err)
	}
}
