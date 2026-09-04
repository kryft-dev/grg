package gitengine

import (
	"errors"
	"fmt"
	"sync"
)

// ErrObjectNotFound indicates that the requested Git object does not exist.
var ErrObjectNotFound = errors.New("git object not found")

// ErrCorruptObject indicates an unreadable or corrupt Git object.
var ErrCorruptObject = errors.New("corrupt git object")

// ObjectType represents the type of a Git object.
type ObjectType uint8

const (
	TypeInvalid  ObjectType = 0
	TypeCommit   ObjectType = 1 // OBJ_COMMIT
	TypeTree     ObjectType = 2 // OBJ_TREE
	TypeBlob     ObjectType = 3 // OBJ_BLOB
	TypeTag      ObjectType = 4 // OBJ_TAG
	TypeOfsDelta ObjectType = 6 // OBJ_OFS_DELTA
	TypeRefDelta ObjectType = 7 // OBJ_REF_DELTA
)

func (t ObjectType) String() string {
	switch t {
	case TypeCommit:
		return "commit"
	case TypeTree:
		return "tree"
	case TypeBlob:
		return "blob"
	case TypeTag:
		return "tag"
	case TypeOfsDelta:
		return "ofs-delta"
	case TypeRefDelta:
		return "ref-delta"
	default:
		return "unknown"
	}
}

// ParseObjectType parses a type string into ObjectType.
func ParseObjectType(s string) ObjectType {
	switch s {
	case "commit":
		return TypeCommit
	case "tree":
		return TypeTree
	case "blob":
		return TypeBlob
	case "tag":
		return TypeTag
	default:
		return TypeInvalid
	}
}

// Object represents an uncompressed Git object.
type Object struct {
	OID  string
	Type ObjectType
	Size int64
	Data []byte
}

// ObjectReader provides read access to Git objects across loose files and packfiles.
type ObjectReader interface {
	ReadObject(oid string) (*Object, error)
	HasObject(oid string) bool
	Close() error
}

// RepositoryReader combines loose object storage and packfiles into a unified ObjectReader.
type RepositoryReader struct {
	loose *LooseReader
	packs []*PackReader
	mu    sync.RWMutex
}

// NewRepositoryReader creates an ObjectReader spanning loose objects and packfiles.
func NewRepositoryReader(repo *RepoInfo) (*RepositoryReader, error) {
	loose := NewLooseReader(repo.CommonGitDir)

	packFiles, err := OpenPackfiles(repo.CommonGitDir)
	if err != nil {
		_ = loose.Close()
		return nil, fmt.Errorf("failed to open packfiles: %w", err)
	}

	reader := &RepositoryReader{
		loose: loose,
		packs: packFiles,
	}

	// Supply the combined reader resolver to packfiles for resolving OBJ_REF_DELTA bases
	for _, p := range reader.packs {
		p.SetResolver(reader)
	}

	return reader, nil
}

// ReadObject looks up an object by OID across loose objects and packfiles.
func (r *RepositoryReader) ReadObject(oid string) (*Object, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check loose objects first
	if r.loose != nil && r.loose.HasObject(oid) {
		obj, err := r.loose.ReadObject(oid)
		if err == nil {
			return obj, nil
		}
	}

	// Check packfiles
	for _, pack := range r.packs {
		if pack.HasObject(oid) {
			obj, err := pack.ReadObject(oid)
			if err == nil {
				return obj, nil
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, oid)
}

// HasObject checks if an object exists in loose storage or packfiles.
func (r *RepositoryReader) HasObject(oid string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.loose != nil && r.loose.HasObject(oid) {
		return true
	}
	for _, pack := range r.packs {
		if pack.HasObject(oid) {
			return true
		}
	}
	return false
}

// Close closes loose and packfile resources.
func (r *RepositoryReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	if r.loose != nil {
		if err := r.loose.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, pack := range r.packs {
		if err := pack.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.packs = nil
	return firstErr
}
