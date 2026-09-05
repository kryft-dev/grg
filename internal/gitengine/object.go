package gitengine

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/kryft-dev/grg/internal/model"
)

// MaxObjectSize defines the upper ceiling for any decompressed Git object (512 MB).
const MaxObjectSize = model.MaxObjectSize

// ErrObjectNotFound indicates that the requested Git object does not exist.
var ErrObjectNotFound = errors.New("git object not found")

// ErrCorruptObject indicates an unreadable or corrupt Git object.
var ErrCorruptObject = errors.New("corrupt git object")

// ErrReaderClosed indicates that the repository reader was closed, either before
// the read started or while it was in flight. It is deliberately distinct from
// ErrObjectNotFound so that a shutdown race is never reported as a missing object.
var ErrReaderClosed = errors.New("repository reader closed")

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
	OID     string
	Type    ObjectType
	Size    int64
	Data    []byte
	poolBuf *[]byte
}

// ObjectReader provides read access to Git objects across loose files and packfiles.
type ObjectReader interface {
	ReadObject(oid string) (*Object, error)
	HasObject(oid string) bool
	Close() error
}

// RepositoryReader combines loose object storage and packfiles into a unified ObjectReader.
//
// Concurrency: ReadObject and HasObject are safe for concurrent use, and may be
// re-entered from inside a read when a packfile resolves an OBJ_REF_DELTA base
// through this reader. mu therefore guards only the loose/packs fields and is
// never held across I/O; the lifetime of the underlying file descriptors is
// tracked separately by inflight, so Close cannot pull a packfile out from under
// a read that is still running. Once Close has been called, every read fails with
// ErrReaderClosed rather than being misreported as ErrObjectNotFound.
type RepositoryReader struct {
	loose    *LooseReader
	packs    []*PackReader
	mu       sync.RWMutex
	inflight sync.WaitGroup
	closed   atomic.Bool
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

	// Supply the combined reader as the resolver for OBJ_REF_DELTA bases that live
	// outside the pack. The field is written here, before the reader is published,
	// and is immutable afterwards.
	for _, p := range reader.packs {
		p.resolver = reader
	}

	return reader, nil
}

// acquire registers an in-flight read and returns a snapshot of the object
// sources. The caller must call r.inflight.Done() once it is finished with them.
// Registering under RLock and releasing the lock before any I/O keeps the
// OBJ_REF_DELTA re-entry deadlock-free: neither Add nor Done ever blocks, and an
// outer read's registration keeps the counter above zero, so a nested Add can
// never race Close's Wait.
func (r *RepositoryReader) acquire() (*LooseReader, []*PackReader, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed.Load() {
		return nil, nil, ErrReaderClosed
	}
	r.inflight.Add(1)
	return r.loose, r.packs, nil
}

// ReadObject looks up an object by OID across loose objects and packfiles.
func (r *RepositoryReader) ReadObject(oid string) (*Object, error) {
	return r.readObjectDepth(oid, 0)
}

// readObjectDepth resolves oid, carrying depth as the number of delta hops already
// traversed. A packfile resolving an OBJ_REF_DELTA base re-enters here rather than
// through ReadObject, so the maxDeltaDepth ceiling keeps counting across the hop
// and a REF_DELTA cycle terminates with ErrCorruptObject instead of recursing
// until the goroutine stack is exhausted.
func (r *RepositoryReader) readObjectDepth(oid string, depth int) (*Object, error) {
	loose, packs, err := r.acquire()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, oid)
	}
	defer r.inflight.Done()

	// A source that claims the object but fails to produce it is a real error, not
	// an absent object: remember it and keep looking, but never downgrade it to
	// ErrObjectNotFound.
	var firstErr error

	if loose != nil && loose.HasObject(oid) {
		obj, err := loose.ReadObject(oid)
		if err == nil {
			return obj, nil
		}
		firstErr = err
	}

	for _, pack := range packs {
		if !pack.HasObject(oid) {
			continue
		}
		obj, err := pack.readObjectDepth(oid, depth)
		if err == nil {
			return obj, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, oid)
}

// HasObject checks if an object exists in loose storage or packfiles.
// A closed reader reports every object as absent.
func (r *RepositoryReader) HasObject(oid string) bool {
	loose, packs, err := r.acquire()
	if err != nil {
		return false
	}
	defer r.inflight.Done()

	if loose != nil && loose.HasObject(oid) {
		return true
	}
	for _, pack := range packs {
		if pack.HasObject(oid) {
			return true
		}
	}
	return false
}

// Close closes loose and packfile resources. It waits for in-flight reads to
// finish before releasing any file descriptor, so a concurrent read either
// completes against a live packfile or fails with ErrReaderClosed. Close is
// idempotent and safe to call concurrently with reads and with itself.
func (r *RepositoryReader) Close() error {
	r.mu.Lock()
	if r.closed.Load() {
		r.mu.Unlock()
		return nil
	}
	r.closed.Store(true)
	loose, packs := r.loose, r.packs
	r.loose, r.packs = nil, nil
	r.mu.Unlock()

	// No new read can register past this point, so this drains the readers that
	// are still holding the descriptors below.
	r.inflight.Wait()

	var firstErr error
	if loose != nil {
		if err := loose.Close(); err != nil {
			firstErr = err
		}
	}
	for _, pack := range packs {
		if err := pack.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
