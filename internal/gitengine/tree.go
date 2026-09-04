package gitengine

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrInvalidTree indicates a malformed or truncated tree object payload.
	ErrInvalidTree = errors.New("invalid tree object payload")
	// ErrSkipDir is used as a sentinel error in traversal to skip descending into a directory.
	ErrSkipDir = errors.New("skip directory")
)

// TreeEntry represents a directory record in a Git tree associating a path name and mode with an OID.
type TreeEntry struct {
	Mode uint32
	Name string
	OID  string
}

// IsTree returns true if this entry is a subtree (directory).
func (e TreeEntry) IsTree() bool {
	return (e.Mode & 0170000) == 0040000 || e.Mode == 0040000
}

// IsBlob returns true if this entry is a regular file or executable.
func (e TreeEntry) IsBlob() bool {
	return (e.Mode & 0170000) == 0100000
}

// IsSubmodule returns true if this entry is a git submodule / gitlink.
func (e TreeEntry) IsSubmodule() bool {
	return (e.Mode & 0170000) == 0160000
}

// ParseTree parses the raw Git tree payload into a list of TreeEntry records.
func ParseTree(payload []byte) ([]TreeEntry, error) {
	var entries []TreeEntry
	pos := 0
	total := len(payload)

	for pos < total {
		// 1. Find space after octal file mode
		spaceIdx := bytes.IndexByte(payload[pos:], ' ')
		if spaceIdx == -1 {
			return nil, fmt.Errorf("%w: missing space after mode at offset %d", ErrInvalidTree, pos)
		}
		modeStr := string(payload[pos : pos+spaceIdx])
		mode, err := strconv.ParseUint(modeStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid octal mode %q: %v", ErrInvalidTree, modeStr, err)
		}
		pos += spaceIdx + 1

		// 2. Find null byte after entry name
		nullIdx := bytes.IndexByte(payload[pos:], 0)
		if nullIdx == -1 {
			return nil, fmt.Errorf("%w: missing null terminator after name at offset %d", ErrInvalidTree, pos)
		}
		name := string(payload[pos : pos+nullIdx])
		pos += nullIdx + 1

		// 3. Read 20-byte binary SHA-1 (standard Git tree)
		if pos+20 > total {
			return nil, fmt.Errorf("%w: truncated SHA for entry %q", ErrInvalidTree, name)
		}
		shaBytes := payload[pos : pos+20]
		oid := hex.EncodeToString(shaBytes)
		pos += 20

		entries = append(entries, TreeEntry{
			Mode: uint32(mode),
			Name: name,
			OID:  oid,
		})
	}

	return entries, nil
}

// TraverseTree recursively traverses a Git tree and calls callback for each entry.
// If callback returns ErrSkipDir for a tree entry, the subtree will not be descended into.
func TraverseTree(reader ObjectReader, rootOID string, callback func(path string, entry TreeEntry) error) error {
	return traverseTreeHelper(reader, rootOID, "", callback)
}

func traverseTreeHelper(reader ObjectReader, treeOID, prefix string, callback func(path string, entry TreeEntry) error) error {
	obj, err := reader.ReadObject(treeOID)
	if err != nil {
		return fmt.Errorf("failed to read tree %s: %w", treeOID, err)
	}
	if obj.Type != TypeTree {
		return fmt.Errorf("object %s is type %s, expected tree", treeOID, obj.Type)
	}

	entries, err := ParseTree(obj.Data)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		var entryPath string
		if prefix == "" {
			entryPath = entry.Name
		} else {
			entryPath = prefix + "/" + entry.Name
		}

		cbErr := callback(entryPath, entry)
		if cbErr != nil {
			if errors.Is(cbErr, ErrSkipDir) {
				continue
			}
			return cbErr
		}

		if entry.IsTree() {
			if err := traverseTreeHelper(reader, entry.OID, entryPath, callback); err != nil {
				return err
			}
		}
	}

	return nil
}

// FindTreeEntry searches rootOID for the tree entry at path (slash-separated).
// Returns the entry and true if found, or a zero TreeEntry and false if not found.
func FindTreeEntry(reader ObjectReader, rootOID, path string) (TreeEntry, bool) {
	if rootOID == "" || path == "" {
		return TreeEntry{}, false
	}

	parts := strings.Split(path, "/")
	currOID := rootOID

	for i, part := range parts {
		obj, err := reader.ReadObject(currOID)
		if err != nil || obj.Type != TypeTree {
			return TreeEntry{}, false
		}

		entries, err := ParseTree(obj.Data)
		if err != nil {
			return TreeEntry{}, false
		}

		found := false
		var matchedEntry TreeEntry
		for _, e := range entries {
			if e.Name == part {
				matchedEntry = e
				found = true
				break
			}
		}

		if !found {
			return TreeEntry{}, false
		}

		if i == len(parts)-1 {
			return matchedEntry, true
		}

		if !matchedEntry.IsTree() {
			return TreeEntry{}, false
		}
		currOID = matchedEntry.OID
	}

	return TreeEntry{}, false
}
