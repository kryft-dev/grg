package gitengine

import (
	"container/heap"
	"fmt"
	"regexp"

	"github.com/kryft-dev/grg/internal/model"
)

// HistoryWalker traverses commit history and yields blob occurrences.
type HistoryWalker struct {
	repo       *RepoInfo
	reader     ObjectReader
	cfg        *model.Config
	pathFilter func(path string) bool
}

// NewHistoryWalker creates a history walker for the repository.
func NewHistoryWalker(repo *RepoInfo, reader ObjectReader, cfg *model.Config, pathFilter func(path string) bool) *HistoryWalker {
	return &HistoryWalker{
		repo:       repo,
		reader:     reader,
		cfg:        cfg,
		pathFilter: pathFilter,
	}
}

// Walk traverses repository history, pruning subtrees by OID and invoking fn on each blob occurrence.
func (w *HistoryWalker) Walk(fn func(occ model.BlobOccurrence) error) error {
	commits, err := w.collectOrderedCommits()
	if err != nil {
		return err
	}

	seenBlobOcc := make(map[string]bool)

	for _, commit := range commits {
		if err := w.walkCommitBlobs(commit, seenBlobOcc, fn); err != nil {
			return err
		}
	}

	return nil
}

func (w *HistoryWalker) walkCommitBlobs(commit *model.CommitMetadata, seenBlobOcc map[string]bool, fn func(occ model.BlobOccurrence) error) error {
	// If ExpandCommits is false and commit has a parent, perform tree diff against first parent
	if !w.cfg.ExpandCommits && len(commit.Parents) > 0 {
		parentObj, err := w.reader.ReadObject(commit.Parents[0])
		if err == nil {
			parentCommit, err := ParseCommit(commit.Parents[0], parentObj.Data)
			if err == nil {
				if commit.TreeOID == parentCommit.TreeOID {
					// Identical root tree: zero files introduced/modified
					return nil
				}
				return w.diffTreesAndEmit(parentCommit.TreeOID, commit.TreeOID, "", commit, seenBlobOcc, fn)
			}
		}
	}

	// Full tree traversal for root commits or when ExpandCommits is enabled
	return TraverseTree(w.reader, commit.TreeOID, func(path string, entry TreeEntry) error {
		if entry.IsTree() {
			return nil
		}
		if !entry.IsBlob() {
			return nil
		}
		if w.pathFilter != nil && !w.pathFilter(path) {
			return nil
		}

		key := entry.OID + ":" + path
		if !w.cfg.ExpandCommits {
			if seenBlobOcc[key] {
				return nil
			}
			seenBlobOcc[key] = true
		}

		return fn(model.BlobOccurrence{
			BlobOID:    entry.OID,
			Path:       path,
			CommitSHA:  commit.SHA,
			CommitDate: commit.Date,
			Mode:       entry.Mode,
		})
	})
}

// diffTreesAndEmit performs subtree-pruned tree comparison between oldTreeOID and newTreeOID.
func (w *HistoryWalker) diffTreesAndEmit(oldTreeOID, newTreeOID, prefix string, commit *model.CommitMetadata, seenBlobOcc map[string]bool, fn func(occ model.BlobOccurrence) error) error {
	if oldTreeOID == newTreeOID {
		// Subtree OID match: prune subtree descending entirely!
		return nil
	}

	var oldEntries []TreeEntry
	if oldTreeOID != "" {
		if oldObj, err := w.reader.ReadObject(oldTreeOID); err == nil && oldObj.Type == TypeTree {
			oldEntries, _ = ParseTree(oldObj.Data)
		}
	}

	newObj, err := w.reader.ReadObject(newTreeOID)
	if err != nil || newObj.Type != TypeTree {
		return fmt.Errorf("failed reading new tree %s: %v", newTreeOID, err)
	}
	newEntries, err := ParseTree(newObj.Data)
	if err != nil {
		return err
	}

	oldMap := make(map[string]TreeEntry, len(oldEntries))
	for _, e := range oldEntries {
		oldMap[e.Name] = e
	}

	for _, newEntry := range newEntries {
		oldEntry, hasOld := oldMap[newEntry.Name]
		if hasOld && oldEntry.OID == newEntry.OID {
			// Identical entry OID: subtree/blob unchanged, prune!
			continue
		}

		var entryPath string
		if prefix == "" {
			entryPath = newEntry.Name
		} else {
			entryPath = prefix + "/" + newEntry.Name
		}

		if newEntry.IsTree() {
			oldSubOID := ""
			if hasOld && oldEntry.IsTree() {
				oldSubOID = oldEntry.OID
			}
			if err := w.diffTreesAndEmit(oldSubOID, newEntry.OID, entryPath, commit, seenBlobOcc, fn); err != nil {
				return err
			}
		} else if newEntry.IsBlob() {
			if w.pathFilter != nil && !w.pathFilter(entryPath) {
				continue
			}

			key := newEntry.OID + ":" + entryPath
			if !w.cfg.ExpandCommits {
				if seenBlobOcc[key] {
					continue
				}
				seenBlobOcc[key] = true
			}

			if err := fn(model.BlobOccurrence{
				BlobOID:    newEntry.OID,
				Path:       entryPath,
				CommitSHA:  commit.SHA,
				CommitDate: commit.Date,
				Mode:       newEntry.Mode,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// commitHeap implements a priority queue ordered by commit date descending.
type commitHeap []*model.CommitMetadata

func (h commitHeap) Len() int           { return len(h) }
func (h commitHeap) Less(i, j int) bool { return h[i].Date.After(h[j].Date) }
func (h commitHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *commitHeap) Push(x any)        { *h = append(*h, x.(*model.CommitMetadata)) }
func (h *commitHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (w *HistoryWalker) collectOrderedCommits() ([]*model.CommitMetadata, error) {
	spec, err := ParseRevSpec(w.repo, w.reader, w.cfg.RevRange)
	if err != nil {
		return nil, err
	}

	// Handle --all or --branch
	if w.cfg.All {
		allRefs, err := ListAllRefs(w.repo)
		if err == nil {
			for _, oid := range allRefs {
				spec.Include = append(spec.Include, oid)
			}
		}
	} else if len(w.cfg.Branches) > 0 {
		for _, b := range w.cfg.Branches {
			oid, err := ResolveRef(w.repo, b)
			if err == nil {
				spec.Include = append(spec.Include, oid)
			}
		}
	}

	// Build exclude set from spec.Exclude
	excluded := make(map[string]bool)
	for _, exclOID := range spec.Exclude {
		w.traverseExclude(exclOID, excluded)
	}

	var authorRe *regexp.Regexp
	if w.cfg.Author != "" {
		authorRe, err = regexp.Compile("(?i)" + w.cfg.Author)
		if err != nil {
			return nil, fmt.Errorf("invalid author pattern: %w", err)
		}
	}

	var committerRe *regexp.Regexp
	if w.cfg.Committer != "" {
		committerRe, err = regexp.Compile("(?i)" + w.cfg.Committer)
		if err != nil {
			return nil, fmt.Errorf("invalid committer pattern: %w", err)
		}
	}

	sinceTime, _ := ParseFilterDate(w.cfg.Since)
	untilTime, _ := ParseFilterDate(w.cfg.Until)

	pq := &commitHeap{}
	heap.Init(pq)
	visited := make(map[string]bool)

	for _, incOID := range spec.Include {
		if visited[incOID] || excluded[incOID] {
			continue
		}
		visited[incOID] = true
		if meta := w.readCommit(incOID); meta != nil {
			heap.Push(pq, meta)
		}
	}

	var matchedCommits []*model.CommitMetadata
	for pq.Len() > 0 {
		commit := heap.Pop(pq).(*model.CommitMetadata)

		if MatchesCommitFilters(commit, w.cfg, authorRe, committerRe, sinceTime, untilTime) {
			matchedCommits = append(matchedCommits, commit)
		}

		for idx, parentSHA := range commit.Parents {
			if w.cfg.FirstParent && idx > 0 {
				break
			}
			if visited[parentSHA] || excluded[parentSHA] {
				continue
			}
			visited[parentSHA] = true
			if parentMeta := w.readCommit(parentSHA); parentMeta != nil {
				heap.Push(pq, parentMeta)
			}
		}
	}

	return matchedCommits, nil
}

func (w *HistoryWalker) readCommit(sha string) *model.CommitMetadata {
	obj, err := w.reader.ReadObject(sha)
	if err != nil {
		return nil
	}
	meta, err := ParseCommit(sha, obj.Data)
	if err != nil {
		return nil
	}
	return meta
}

func (w *HistoryWalker) traverseExclude(sha string, excluded map[string]bool) {
	if excluded[sha] {
		return
	}
	excluded[sha] = true
	meta := w.readCommit(sha)
	if meta == nil {
		return
	}
	for _, parent := range meta.Parents {
		w.traverseExclude(parent, excluded)
	}
}

