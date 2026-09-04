package gitengine

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrRefNotFound indicates the specified ref does not exist.
	ErrRefNotFound = errors.New("ref not found")
	// ErrInvalidRef indicates a malformed ref.
	ErrInvalidRef = errors.New("invalid ref")
)

const maxRefFollowDepth = 10

// ResolveRef resolves a Git ref (e.g. "HEAD", "main", "refs/heads/main", "v1.0.0") to a commit/object OID.
func ResolveRef(repo *RepoInfo, ref string) (string, error) {
	return resolveRefRecursive(repo, ref, 0)
}

func resolveRefRecursive(repo *RepoInfo, ref string, depth int) (string, error) {
	if depth > maxRefFollowDepth {
		return "", fmt.Errorf("%w: symbolic ref recursion limit exceeded for %s", ErrInvalidRef, ref)
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("%w: empty ref", ErrInvalidRef)
	}

	// 1. If ref is already a valid full hex SHA (40 or 64 hex chars), return it
	if isHexOID(ref) {
		return ref, nil
	}

	// 2. Candidate paths in precedence order
	var candidates []string
	if strings.HasPrefix(ref, "refs/") || ref == "HEAD" {
		candidates = []string{ref}
	} else {
		candidates = []string{
			ref,
			"refs/" + ref,
			"refs/heads/" + ref,
			"refs/tags/" + ref,
			"refs/remotes/" + ref,
			"refs/remotes/origin/" + ref,
		}
	}

	// Try resolving as a loose ref first
	for _, cand := range candidates {
		// Check GitDir first (handles HEAD, worktree-specific refs)
		oid, found, err := readLooseRef(repo.GitDir, cand)
		if err != nil {
			return "", err
		}
		if found {
			if strings.HasPrefix(oid, "ref: ") {
				return resolveRefRecursive(repo, strings.TrimPrefix(oid, "ref: "), depth+1)
			}
			return oid, nil
		}

		// If CommonGitDir differs, check CommonGitDir
		if repo.CommonGitDir != "" && repo.CommonGitDir != repo.GitDir {
			oid, found, err = readLooseRef(repo.CommonGitDir, cand)
			if err != nil {
				return "", err
			}
			if found {
				if strings.HasPrefix(oid, "ref: ") {
					return resolveRefRecursive(repo, strings.TrimPrefix(oid, "ref: "), depth+1)
				}
				return oid, nil
			}
		}
	}

	// 3. If not found loose, check packed-refs
	packedRefs, err := ReadPackedRefs(repo.CommonGitDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to read packed-refs: %w", err)
	}

	for _, cand := range candidates {
		if oid, ok := packedRefs[cand]; ok {
			return oid, nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrRefNotFound, ref)
}

func readLooseRef(baseDir, relPath string) (string, bool, error) {
	fullPath := filepath.Join(baseDir, filepath.FromSlash(relPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed reading ref file %s: %w", fullPath, err)
	}

	content := strings.TrimSpace(string(data))
	return content, true, nil
}

// ReadPackedRefs reads and parses .git/packed-refs, mapping ref name to OID.
func ReadPackedRefs(commonGitDir string) (map[string]string, error) {
	packedPath := filepath.Join(commonGitDir, "packed-refs")
	f, err := os.Open(packedPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	refs := make(map[string]string)
	scanner := bufio.NewScanner(f)
	var lastRef string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Peeling tag line: ^<peeled-oid> applies to previous tag
		if strings.HasPrefix(line, "^") {
			peeledOID := strings.TrimPrefix(line, "^")
			if lastRef != "" {
				// We map both the annotated tag ref and can track peeled OID if needed
				refs[lastRef+"^{}"] = peeledOID
			}
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		oid := parts[0]
		refName := parts[1]
		refs[refName] = oid
		lastRef = refName
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error in packed-refs: %w", err)
	}

	return refs, nil
}

// ListAllRefs returns all refs (branches, tags, remotes) merging loose and packed-refs.
func ListAllRefs(repo *RepoInfo) (map[string]string, error) {
	allRefs := make(map[string]string)

	// 1. Packed refs (baseline)
	packed, err := ReadPackedRefs(repo.CommonGitDir)
	if err == nil {
		for k, v := range packed {
			if !strings.HasSuffix(k, "^{}") {
				allRefs[k] = v
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// 2. Loose refs (override packed)
	scanLooseRefs(filepath.Join(repo.CommonGitDir, "refs"), "refs", allRefs)
	if repo.GitDir != repo.CommonGitDir {
		scanLooseRefs(filepath.Join(repo.GitDir, "refs"), "refs", allRefs)
	}

	// 3. Include HEAD if resolvable
	if headOID, err := ResolveRef(repo, "HEAD"); err == nil {
		allRefs["HEAD"] = headOID
	}

	return allRefs, nil
}

func scanLooseRefs(dir, prefix string, target map[string]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		subPath := prefix + "/" + name
		if entry.IsDir() {
			scanLooseRefs(filepath.Join(dir, name), subPath, target)
		} else {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err == nil {
				oid := strings.TrimSpace(string(data))
				if !strings.HasPrefix(oid, "ref: ") && isHexOID(oid) {
					target[subPath] = oid
				}
			}
		}
	}
}

func isHexOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
