package aggregator

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/kryft-dev/grg/internal/model"
	"github.com/kryft-dev/grg/internal/search"
)

// CommitMatches represents matches found in a file for a specific commit.
type CommitMatches struct {
	CommitSHA     string
	ShortSHA      string
	CommitDate    time.Time
	Author        string
	AuthorName    string
	Summary       string
	Matches       []model.SearchMatch
	ContextGroups []search.ContextGroup
	IsBinary      bool
}

// FileMatches groups commit matches for a single repository file path.
type FileMatches struct {
	Path    string
	Commits []CommitMatches
}

// NewestCommitDate returns the most recent commit date among all commits in this file.
func (f FileMatches) NewestCommitDate() time.Time {
	var newest time.Time
	for _, c := range f.Commits {
		if c.CommitDate.After(newest) {
			newest = c.CommitDate
		}
	}
	return newest
}

// AggregatedResults encapsulates search results grouped by file and commit.
type AggregatedResults struct {
	Files        []FileMatches
	TotalMatches int
	TotalFiles   int
}

// HasMatches returns true if any text or binary matches were found.
func (r *AggregatedResults) HasMatches() bool {
	if r == nil {
		return false
	}
	return r.TotalMatches > 0
}

// Aggregator groups, deduplicates, and sorts search results.
type Aggregator struct {
	cfg *model.Config
}

// New creates an Aggregator using the specified configuration.
func New(cfg *model.Config) *Aggregator {
	if cfg == nil {
		cfg = &model.Config{}
	}
	return &Aggregator{cfg: cfg}
}

type fileBuilder struct {
	path        string
	commitOrder []string
	commitMap   map[string]*CommitMatches
}

// Aggregate organizes raw BlobResult items into structured, ordered FileMatches.
func (a *Aggregator) Aggregate(results []*search.BlobResult) *AggregatedResults {
	var fileOrder []string
	fileMap := make(map[string]*fileBuilder)

	for _, res := range results {
		if res == nil {
			continue
		}
		a.processBlobResult(res, fileMap, &fileOrder)
	}

	return a.finalizeResults(fileMap, fileOrder)
}

// AggregateChannel organizes streamed BlobResult items into structured, ordered FileMatches.
//
// It consumes resultsCh and errCh until both are closed and then returns the aggregated
// results. It returns early, with a nil result, when errCh yields a non-nil error, when a
// result carries a fatal error, or when ctx is cancelled.
//
// The producer owns both channels and must close both when it stops. It may publish its
// terminal error before or after closing resultsCh: aggregation finalizes only once errCh
// is closed, so a late error is still surfaced rather than silently dropped.
//
// The caller must pass a context that the producer also observes, and that is cancelled
// when the caller loses interest. On every early return this function drains resultsCh so
// the producer is never left blocked on a send, and that drain only terminates once the
// producer closes resultsCh; a shared cancellable context is what guarantees the producer
// gets there.
func (a *Aggregator) AggregateChannel(ctx context.Context, resultsCh <-chan *search.BlobResult, errCh <-chan error) (out *AggregatedResults, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Every early return below walks away from a producer that is still sending. Producers
	// use a bounded results channel, so once its buffer fills every worker blocks on its
	// send, the task dispatcher blocks behind them, and the object store they pin is never
	// released - the producer wedges permanently and never closes its channels. Draining
	// hands the producer the receives it is waiting for so it can run itself down.
	//
	// Termination: a nil resultsCh means the loop below already observed the close and
	// consumed the channel to completion, so there is nothing left to unblock - and
	// ranging over a nil channel would block forever, so return instead. Otherwise every
	// receive advances the producer, so the drain ends as soon as the producer closes
	// resultsCh. A producer honouring the contract above always gets there, whether it
	// finishes its work or unwinds because the shared context was cancelled.
	defer func() {
		if resultsCh == nil || (err == nil && ctx.Err() == nil) {
			return
		}
		for range resultsCh {
		}
	}()

	var fileOrder []string
	fileMap := make(map[string]*fileBuilder)

	// A receive on a closed channel is ready forever, so an exhausted arm must be disabled
	// by nilling its channel: a nil channel is never ready and its arm is never chosen
	// again. Leaving a closed channel in place would make the select spin on it at full
	// CPU. Both channels closed means the producer is done and the results are complete.
	for resultsCh != nil || errCh != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case perr, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if perr != nil {
				return nil, perr
			}
		case res, ok := <-resultsCh:
			if !ok {
				resultsCh = nil
				continue
			}
			if res == nil {
				continue
			}
			if res.Error != nil {
				// A blob the pipeline could not read is a soft failure: it carries no
				// matches and is skipped so the remaining blobs still aggregate.
				var bre *search.BlobReadError
				if errors.As(res.Error, &bre) {
					continue
				}
				return nil, res.Error
			}
			a.processBlobResult(res, fileMap, &fileOrder)
		}
	}

	return a.finalizeResults(fileMap, fileOrder), nil
}

// AggregateStream is an alias to AggregateChannel for streaming API flexibility. The
// producer and caller contracts documented on AggregateChannel apply unchanged.
func (a *Aggregator) AggregateStream(ctx context.Context, resultsCh <-chan *search.BlobResult, errCh <-chan error) (*AggregatedResults, error) {
	return a.AggregateChannel(ctx, resultsCh, errCh)
}

func (a *Aggregator) processBlobResult(res *search.BlobResult, fileMap map[string]*fileBuilder, fileOrder *[]string) {
	if len(res.Matches) == 0 && !res.IsBinary {
		return
	}

	occurrences := a.selectOccurrences(res.Occurrences)

	for _, occ := range occurrences {
		fb, exists := fileMap[occ.Path]
		if !exists {
			fb = &fileBuilder{
				path:      occ.Path,
				commitMap: make(map[string]*CommitMatches),
			}
			fileMap[occ.Path] = fb
			*fileOrder = append(*fileOrder, occ.Path)
		}

		_, hasCommit := fb.commitMap[occ.CommitSHA]
		if !hasCommit {
			author := occ.CommitAuthor
			if author == "" && occ.Commit != nil {
				if occ.Commit.AuthorName != "" {
					author = occ.Commit.AuthorName
				} else {
					author = occ.Commit.Author
				}
			}
			summary := occ.CommitSummary
			if summary == "" && occ.Commit != nil {
				summary = occ.Commit.Summary
			}

			// Defensively copy Matches to prevent slice aliasing across commits/goroutines
			var copiedMatches []model.SearchMatch
			if len(res.Matches) > 0 {
				copiedMatches = make([]model.SearchMatch, len(res.Matches))
				for i, m := range res.Matches {
					copiedMatches[i] = m
					if len(m.Submatches) > 0 {
						copiedMatches[i].Submatches = append([]model.Submatch(nil), m.Submatches...)
					}
				}
			}

			// Defensively copy ContextGroups
			var copiedContextGroups []search.ContextGroup
			if len(res.ContextGroups) > 0 {
				copiedContextGroups = make([]search.ContextGroup, len(res.ContextGroups))
				for i, cg := range res.ContextGroups {
					copiedContextGroups[i] = cg
					if len(cg.Lines) > 0 {
						copiedContextGroups[i].Lines = make([]search.ContextLine, len(cg.Lines))
						for j, cl := range cg.Lines {
							copiedContextGroups[i].Lines[j] = cl
							if len(cl.Submatches) > 0 {
								copiedContextGroups[i].Lines[j].Submatches = append([]model.Submatch(nil), cl.Submatches...)
							}
						}
					}
				}
			}

			cm := &CommitMatches{
				CommitSHA:     occ.CommitSHA,
				ShortSHA:      model.ShortSHA(occ.CommitSHA),
				CommitDate:    occ.CommitDate,
				Author:        author,
				AuthorName:    author,
				Summary:       summary,
				Matches:       copiedMatches,
				ContextGroups: copiedContextGroups,
				IsBinary:      res.IsBinary,
			}
			fb.commitMap[occ.CommitSHA] = cm
			fb.commitOrder = append(fb.commitOrder, occ.CommitSHA)
		}
	}
}

func (a *Aggregator) finalizeResults(fileMap map[string]*fileBuilder, fileOrder []string) *AggregatedResults {
	var files []FileMatches
	for _, path := range fileOrder {
		fb := fileMap[path]
		var commits []CommitMatches

		for _, sha := range fb.commitOrder {
			if cm, ok := fb.commitMap[sha]; ok {
				commits = append(commits, *cm)
			}
		}

		if !a.cfg.Unordered {
			sort.SliceStable(commits, func(i, j int) bool {
				if !commits[i].CommitDate.Equal(commits[j].CommitDate) {
					return commits[i].CommitDate.After(commits[j].CommitDate)
				}
				return commits[i].CommitSHA > commits[j].CommitSHA
			})
		}

		files = append(files, FileMatches{
			Path:    path,
			Commits: commits,
		})
	}

	if !a.cfg.Unordered {
		sort.SliceStable(files, func(i, j int) bool {
			dateI := files[i].NewestCommitDate()
			dateJ := files[j].NewestCommitDate()
			if !dateI.Equal(dateJ) {
				return dateI.After(dateJ)
			}
			return files[i].Path < files[j].Path
		})
	}

	totalMatches := 0
	for _, f := range files {
		for _, c := range f.Commits {
			if c.IsBinary {
				totalMatches++
			} else {
				totalMatches += len(c.Matches)
			}
		}
	}

	return &AggregatedResults{
		Files:        files,
		TotalMatches: totalMatches,
		TotalFiles:   len(files),
	}
}

// selectOccurrences collapses identical blob occurrences to the introducing commit
// unless cfg.ExpandCommits is enabled.
func (a *Aggregator) selectOccurrences(occurrences []model.BlobOccurrence) []model.BlobOccurrence {
	if a.cfg.ExpandCommits {
		return occurrences
	}

	// Group by path and select the introducing commit (oldest commit date)
	bestByPath := make(map[string]model.BlobOccurrence)
	var pathOrder []string

	for _, occ := range occurrences {
		best, exists := bestByPath[occ.Path]
		if !exists {
			bestByPath[occ.Path] = occ
			pathOrder = append(pathOrder, occ.Path)
			continue
		}

		// Introducing commit has the earliest commit date
		if occ.CommitDate.Before(best.CommitDate) {
			bestByPath[occ.Path] = occ
		} else if occ.CommitDate.Equal(best.CommitDate) && occ.CommitSHA < best.CommitSHA {
			bestByPath[occ.Path] = occ
		}
	}

	selected := make([]model.BlobOccurrence, 0, len(pathOrder))
	for _, p := range pathOrder {
		selected = append(selected, bestByPath[p])
	}
	return selected
}
