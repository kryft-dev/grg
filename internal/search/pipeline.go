package search

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
)

// BlobResult contains search matches and all provenance occurrences for a deduplicated blob OID.
type BlobResult struct {
	BlobOID       string
	Occurrences   []model.BlobOccurrence
	Matches       []model.SearchMatch
	ContextGroups []ContextGroup
	Lines         []string
	IsBinary      bool
	Error         error
}

// Pipeline coordinates concurrent blob decompression, search matching, and provenance association.
type Pipeline struct {
	reader  gitengine.ObjectReader
	matcher *Matcher
	cfg     *model.Config
	workers int
}

// NewPipeline initializes a search pipeline with runtime.NumCPU() workers.
func NewPipeline(reader gitengine.ObjectReader, matcher *Matcher, cfg *model.Config) *Pipeline {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	return &Pipeline{
		reader:  reader,
		matcher: matcher,
		cfg:     cfg,
		workers: workers,
	}
}

type blobTask struct {
	oid         string
	occurrences []model.BlobOccurrence
}

// ExecuteContext executes the search pipeline with context cancellation support,
// bounded worker pools, channel backpressure, and zero goroutine leaks.
// It streams results and errors over bounded channels until completion or cancellation.
func (p *Pipeline) ExecuteContext(ctx context.Context, occurrences []model.BlobOccurrence) (<-chan *BlobResult, <-chan error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Deduplicate by BlobOID while preserving provenance occurrences in first-seen order
	jobMap := make(map[string]*blobTask)
	var jobOrder []*blobTask

	for _, occ := range occurrences {
		if task, exists := jobMap[occ.BlobOID]; exists {
			task.occurrences = append(task.occurrences, occ)
		} else {
			task := &blobTask{
				oid:         occ.BlobOID,
				occurrences: []model.BlobOccurrence{occ},
			}
			jobMap[occ.BlobOID] = task
			jobOrder = append(jobOrder, task)
		}
	}

	// If context is already cancelled, return immediately with closed channels
	if err := ctx.Err(); err != nil {
		resultsCh := make(chan *BlobResult)
		errCh := make(chan error, 1)
		errCh <- err
		close(resultsCh)
		close(errCh)
		return resultsCh, errCh
	}

	if len(jobOrder) == 0 {
		resultsCh := make(chan *BlobResult)
		errCh := make(chan error)
		close(resultsCh)
		close(errCh)
		return resultsCh, errCh
	}

	// Enforce bounded worker pool and channel backpressure (runtime.NumCPU() * 4 or min 32)
	bufSize := p.workers * 4
	if bufSize < 32 {
		bufSize = 32
	}

	tasksCh := make(chan *blobTask, bufSize)
	resultsCh := make(chan *BlobResult, bufSize)
	errCh := make(chan error, 1)

	// Dispatcher feeding tasks into bounded tasksCh with ctx cancellation check
	go func() {
		defer close(tasksCh)
		for _, task := range jobOrder {
			select {
			case <-ctx.Done():
				return
			case tasksCh <- task:
			}
		}
	}()

	// Launch worker pool
	var stop atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-tasksCh:
					if !ok {
						return
					}
					if stop.Load() {
						continue
					}
					res := p.processTask(ctx, task)
					if res.Error != nil {
						select {
						case errCh <- res.Error:
						default:
						}
					}
					if p.cfg.Quiet && (len(res.Matches) > 0 || res.IsBinary) {
						stop.Store(true)
					}
					// Only send results that have matches, are binary, or have errors
					if len(res.Matches) > 0 || res.IsBinary || res.Error != nil {
						select {
						case <-ctx.Done():
							return
						case resultsCh <- res:
						}
					}
				}
			}
		}()
	}

	// Closer goroutine waits for all workers to exit, captures cancellation error, and closes channels
	go func() {
		wg.Wait()
		if err := ctx.Err(); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
		close(resultsCh)
		close(errCh)
	}()

	return resultsCh, errCh
}

// Execute provides backwards-compatible synchronous execution by executing with context.Background()
// and collecting all results into a slice in the original deduplicated order.
func (p *Pipeline) Execute(occurrences []model.BlobOccurrence) ([]*BlobResult, error) {
	resultsCh, errCh := p.ExecuteContext(context.Background(), occurrences)
	resMap := make(map[string]*BlobResult)
	for res := range resultsCh {
		resMap[res.BlobOID] = res
	}
	if err := <-errCh; err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var results []*BlobResult
	for _, occ := range occurrences {
		if seen[occ.BlobOID] {
			continue
		}
		seen[occ.BlobOID] = true
		if res, ok := resMap[occ.BlobOID]; ok {
			results = append(results, res)
		}
	}
	return results, nil
}

// ExecuteStream streams search results using context.Background().
func (p *Pipeline) ExecuteStream(occurrences []model.BlobOccurrence) (<-chan *BlobResult, <-chan error) {
	return p.ExecuteContext(context.Background(), occurrences)
}

// processTask reads the blob from Git object store and applies binary detection and pattern matching.
func (p *Pipeline) processTask(ctx context.Context, task *blobTask) *BlobResult {
	res := &BlobResult{
		BlobOID:     task.oid,
		Occurrences: task.occurrences,
	}

	if err := ctx.Err(); err != nil {
		res.Error = err
		return res
	}

	obj, err := p.reader.ReadObject(task.oid)
	if err != nil {
		res.Error = fmt.Errorf("failed to read blob %s: %w", task.oid, err)
		return res
	}

	if err := ctx.Err(); err != nil {
		res.Error = err
		return res
	}

	// Binary detection
	if !p.cfg.Text && IsBinary(obj.Data) {
		if p.matcher.MatchBytes(obj.Data) {
			res.IsBinary = true
		}
		return res
	}

	// Context extraction if -A or -B specified
	if p.cfg.BeforeContext > 0 || p.cfg.AfterContext > 0 {
		groups, lines, err := p.matcher.MatchBlobWithContext(obj.Data)
		if err != nil {
			res.Error = err
			return res
		}
		res.ContextGroups = groups
		res.Lines = lines
		for _, g := range groups {
			for _, l := range g.Lines {
				if l.IsMatch {
					res.Matches = append(res.Matches, model.SearchMatch{
						LineNum:    l.LineNum,
						LineText:   l.LineText,
						Submatches: l.Submatches,
					})
				}
			}
		}
		return res
	}

	// Standard matching
	matches, err := p.matcher.MatchBlob(obj.Data)
	if err != nil {
		res.Error = err
		return res
	}
	res.Matches = matches
	return res
}
