package aggregator

import (
	"context"
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
// It terminates cleanly when resultsCh is closed, an error is received on errCh, or ctx is cancelled.
func (a *Aggregator) AggregateChannel(ctx context.Context, resultsCh <-chan *search.BlobResult, errCh <-chan error) (*AggregatedResults, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var fileOrder []string
	fileMap := make(map[string]*fileBuilder)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err, ok := <-errCh:
			if ok && err != nil {
				return nil, err
			}
		case res, ok := <-resultsCh:
			if !ok {
				// Channel closed: verify if any pending error remains on errCh
				if errCh != nil {
					select {
					case err, ok := <-errCh:
						if ok && err != nil {
							return nil, err
						}
					default:
					}
				}
				return a.finalizeResults(fileMap, fileOrder), nil
			}
			if res != nil {
				if res.Error != nil {
					return nil, res.Error
				}
				a.processBlobResult(res, fileMap, &fileOrder)
			}
		}
	}
}

// AggregateStream is an alias to AggregateChannel for streaming API flexibility.
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
