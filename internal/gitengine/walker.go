package gitengine

import (
	"container/heap"
	"context"
	"fmt"
	"regexp"
	"slices"

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
// Walk aborts with ctx.Err() once ctx is cancelled, including during the initial commit-DAG collection
// that runs before the first occurrence is emitted.
func (w *HistoryWalker) Walk(ctx context.Context, fn func(occ model.BlobOccurrence) error) error {
	commits, err := w.collectOrderedCommits(ctx)
	if err != nil {
		return err
	}

	seenBlobOcc := make(map[string]bool)

	for _, commit := range commits {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.walkCommitBlobs(ctx, commit, seenBlobOcc, fn); err != nil {
			return err
		}
	}

	return nil
}

func (w *HistoryWalker) walkCommitBlobs(ctx context.Context, commit *model.CommitMetadata, seenBlobOcc map[string]bool, fn func(occ model.BlobOccurrence) error) error {
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
				return w.diffTreesAndEmit(ctx, parentCommit.TreeOID, commit.TreeOID, "", commit, seenBlobOcc, fn)
			}
		}
	}

	// Full tree traversal for root commits or when ExpandCommits is enabled
	return TraverseTree(ctx, w.reader, commit.TreeOID, func(path string, entry TreeEntry) error {
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

		author := commit.AuthorName
		if author == "" {
			author = commit.Author
		}
		return fn(model.BlobOccurrence{
			BlobOID:       entry.OID,
			Path:          path,
			CommitSHA:     commit.SHA,
			CommitDate:    commit.Date,
			Mode:          entry.Mode,
			CommitSummary: commit.Summary,
			CommitAuthor:  author,
			Commit:        commit,
		})
	})
}

// compareTreeEntries compares two TreeEntry items using Git's canonical tree sort order:
// compares entry names, with directories sorting as if ending with '/'.
func compareTreeEntries(a, b TreeEntry) int {
	len1 := len(a.Name)
	len2 := len(b.Name)
	minLen := len1
	if len2 < minLen {
		minLen = len2
	}
	for i := 0; i < minLen; i++ {
		c1 := a.Name[i]
		c2 := b.Name[i]
		if c1 != c2 {
			if c1 < c2 {
				return -1
			}
			return 1
		}
	}
	var c1 byte
	if len1 > minLen {
		c1 = a.Name[minLen]
	} else if a.IsTree() {
		c1 = '/'
	}
	var c2 byte
	if len2 > minLen {
		c2 = b.Name[minLen]
	} else if b.IsTree() {
		c2 = '/'
	}
	if c1 < c2 {
		return -1
	}
	if c1 > c2 {
		return 1
	}
	return 0
}

// diffTreesAndEmit performs subtree-pruned tree comparison between oldTreeOID and newTreeOID.
// Leverages canonical sorted tree entry order with a two-pointer merge to eliminate map allocations.
func (w *HistoryWalker) diffTreesAndEmit(ctx context.Context, oldTreeOID, newTreeOID, prefix string, commit *model.CommitMetadata, seenBlobOcc map[string]bool, fn func(occ model.BlobOccurrence) error) error {
	if oldTreeOID == newTreeOID {
		// Subtree OID match: prune subtree descending entirely!
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
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

	if !slices.IsSortedFunc(oldEntries, compareTreeEntries) {
		slices.SortFunc(oldEntries, compareTreeEntries)
	}
	if !slices.IsSortedFunc(newEntries, compareTreeEntries) {
		slices.SortFunc(newEntries, compareTreeEntries)
	}

	i := 0
	for _, newEntry := range newEntries {
		var (
			hasOld   bool
			oldEntry TreeEntry
		)

		for i < len(oldEntries) {
			cmp := compareTreeEntries(oldEntries[i], newEntry)
			if cmp < 0 {
				i++
			} else if cmp == 0 {
				hasOld = true
				oldEntry = oldEntries[i]
				i++
				break
			} else {
				break
			}
		}

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
			if err := w.diffTreesAndEmit(ctx, oldSubOID, newEntry.OID, entryPath, commit, seenBlobOcc, fn); err != nil {
				return err
			}
		} else if newEntry.IsBlob() {
			if err := w.emitBlobOccurrence(ctx, newEntry, entryPath, commit, seenBlobOcc, fn); err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *HistoryWalker) emitBlobOccurrence(ctx context.Context, newEntry TreeEntry, entryPath string, commit *model.CommitMetadata, seenBlobOcc map[string]bool, fn func(occ model.BlobOccurrence) error) error {
	if w.pathFilter != nil && !w.pathFilter(entryPath) {
		return nil
	}

	// In full DAG traversal, a merge commit does not introduce a blob
	// if that blob was already present in another parent branch.
	if len(commit.Parents) > 1 && !w.cfg.FirstParent {
		inOtherParent := false
		for _, pSHA := range commit.Parents[1:] {
			if err := ctx.Err(); err != nil {
				return err
			}
			pMeta := w.readCommit(pSHA)
			if pMeta != nil {
				if pe, ok := FindTreeEntry(ctx, w.reader, pMeta.TreeOID, entryPath); ok && pe.OID == newEntry.OID {
					inOtherParent = true
					break
				}
			}
		}
		if inOtherParent {
			return nil
		}
	}

	key := newEntry.OID + ":" + entryPath
	if !w.cfg.ExpandCommits {
		if seenBlobOcc[key] {
			return nil
		}
		seenBlobOcc[key] = true
	}

	author := commit.AuthorName
	if author == "" {
		author = commit.Author
	}
	return fn(model.BlobOccurrence{
		BlobOID:       newEntry.OID,
		Path:          entryPath,
		CommitSHA:     commit.SHA,
		CommitDate:    commit.Date,
		Mode:          newEntry.Mode,
		CommitSummary: commit.Summary,
		CommitAuthor:  author,
		Commit:        commit,
	})
}

// commitHeap implements a priority queue ordered by commit date descending.
type commitHeap []*model.CommitMetadata

func (h commitHeap) Len() int           { return len(h) }
func (h commitHeap) Less(i, j int) bool { return h[i].Date.After(h[j].Date) }
func (h commitHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *commitHeap) Push(x any) {
	if item, ok := x.(*model.CommitMetadata); ok {
		*h = append(*h, item)
	}
}
func (h *commitHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (w *HistoryWalker) collectOrderedCommits(ctx context.Context) ([]*model.CommitMetadata, error) {
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
		if err := w.traverseExclude(ctx, exclOID, excluded); err != nil {
			return nil, err
		}
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		popped := heap.Pop(pq)
		commit, ok := popped.(*model.CommitMetadata)
		if !ok {
			continue
		}

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

func (w *HistoryWalker) traverseExclude(ctx context.Context, startSHA string, excluded map[string]bool) error {
	stack := []string{startSHA}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		n := len(stack) - 1
		sha := stack[n]
		stack = stack[:n]

		if excluded[sha] {
			continue
		}
		excluded[sha] = true
		meta := w.readCommit(sha)
		if meta == nil {
			continue
		}
		for _, parent := range meta.Parents {
			if !excluded[parent] {
				stack = append(stack, parent)
			}
		}
	}

	return nil
}
