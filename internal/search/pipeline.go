package search

import (
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

// Execute processes a list of blob occurrences, deduplicating by BlobOID before searching.
func (p *Pipeline) Execute(occurrences []model.BlobOccurrence) ([]*BlobResult, error) {
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

	if len(jobOrder) == 0 {
		return nil, nil
	}

	tasksCh := make(chan *blobTask, len(jobOrder))
	resultsCh := make(chan *BlobResult, len(jobOrder))

	for _, task := range jobOrder {
		tasksCh <- task
	}
	close(tasksCh)

	// Launch worker pool
	var stop atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasksCh {
				if stop.Load() {
					continue
				}
				res := p.processTask(task)
				if p.cfg.Quiet && (len(res.Matches) > 0 || res.IsBinary) {
					stop.Store(true)
				}
				resultsCh <- res
			}
		}()
	}

	wg.Wait()
	close(resultsCh)

	// Collect results in map to restore original deduplicated order
	resMap := make(map[string]*BlobResult, len(jobOrder))
	for res := range resultsCh {
		resMap[res.BlobOID] = res
	}

	var results []*BlobResult
	for _, task := range jobOrder {
		if res, ok := resMap[task.oid]; ok {
			// Only include results that have matches, are binary, or have errors
			if len(res.Matches) > 0 || res.IsBinary || res.Error != nil {
				results = append(results, res)
			}
		}
	}

	return results, nil
}

// processTask reads the blob from Git object store and applies binary detection and pattern matching.
func (p *Pipeline) processTask(task *blobTask) *BlobResult {
	res := &BlobResult{
		BlobOID:     task.oid,
		Occurrences: task.occurrences,
	}

	obj, err := p.reader.ReadObject(task.oid)
	if err != nil {
		res.Error = fmt.Errorf("failed to read blob %s: %w", task.oid, err)
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
