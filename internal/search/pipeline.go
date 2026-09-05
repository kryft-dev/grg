package search

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
)

// BlobResult contains search matches and all provenance occurrences for a deduplicated blob OID.
//
// Error is set when the blob could not be searched. A *BlobReadError is a soft
// failure: the pipeline still delivers the result (with no matches) on the results
// channel so callers can warn about it, but does not report it on the error channel
// and the search continues. Any other error is fatal and also surfaces on the error
// channel.
type BlobResult struct {
	BlobOID       string
	Occurrences   []model.BlobOccurrence
	Matches       []model.SearchMatch
	ContextGroups []ContextGroup
	Lines         []string
	IsBinary      bool
	Error         error
}

// BlobReadError reports that a blob could not be read from the object store
// (missing, truncated, or corrupt object) and was skipped by the search.
// Path is the first occurrence path of the blob, for diagnostics.
type BlobReadError struct {
	OID  string
	Path string
	Err  error
}

func (e *BlobReadError) Error() string {
	return fmt.Sprintf("failed to read blob %s: %v", e.OID, e.Err)
}

func (e *BlobReadError) Unwrap() error {
	return e.Err
}

// isBlobReadError reports whether err is (or wraps) a soft per-blob read failure.
func isBlobReadError(err error) bool {
	var bre *BlobReadError
	return errors.As(err, &bre)
}

// Pipeline coordinates concurrent blob decompression, search matching, and provenance association.
//
// A Pipeline is immutable after construction: its reader, matcher, config, and
// worker count are never written after NewPipeline returns, so a single
// Pipeline is safe for concurrent use by multiple goroutines.
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
// bounded worker pools, and channel backpressure. It streams results and errors
// over bounded channels until completion or cancellation.
//
// The pipeline leaks no goroutines provided the caller either drains resultsCh to
// completion or cancels ctx: workers block on resultsCh sends and unblock only on
// a receive or on cancellation. resultsCh is closed once every worker has exited,
// and errCh is closed immediately after, so a caller may read errCh once the
// range over resultsCh ends.
//
// Work runs under a cancellable child of ctx, so the pipeline stops early on the
// first fatal error and, in quiet mode, on the first match. Neither is a
// cancellation error: only cancellation of ctx itself is reported on errCh.
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
	// errCh has capacity 1 and every send on it is non-blocking, which implements
	// first-fatal-error-wins: the first fatal error is kept and later ones are
	// dropped. That capacity is exactly what guarantees a worker never blocks
	// publishing an error, because the consumer usually does not read errCh until
	// resultsCh has been drained. Raising it "so no error is lost" would let a
	// worker's error send outlive the consumer's interest and reintroduce that
	// deadlock.
	errCh := make(chan error, 1)

	// All work runs under a cancellable child of the caller's context so that the
	// first fatal error and a quiet-mode match can abandon the remaining blobs at
	// once. callerCtx is kept for the closer: an internal early stop must never be
	// reported as a cancellation.
	callerCtx := ctx
	ctx, cancelWork := context.WithCancel(callerCtx)

	go p.dispatch(ctx, jobOrder, tasksCh)

	// searched counts tasks whose result was fully delivered. It tells the closer
	// whether the run actually finished, so a cancellation arriving after the last
	// result was handed over cannot turn a complete result set into a failure.
	var searched atomic.Int64
	var wg sync.WaitGroup
	for range p.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.runWorker(ctx, cancelWork, &searched, tasksCh, resultsCh, errCh)
		}()
	}

	// The closer is the sole owner of resultsCh and errCh: it waits for every
	// worker to exit, publishes a caller-side cancellation, and closes both. The
	// publication is gated on callerCtx, never on the derived ctx, because a quiet
	// early stop or a fatal error cancels the derived one and must not surface as
	// context.Canceled. It is gated on searched as well, because a caller who
	// received every result was not cut short, whenever the cancel arrived.
	go func() {
		defer cancelWork()
		wg.Wait()
		if searched.Load() < int64(len(jobOrder)) {
			if err := callerCtx.Err(); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}
		close(resultsCh)
		close(errCh)
	}()

	return resultsCh, errCh
}

// dispatch feeds every deduplicated task into tasksCh and is its only sender and
// closer. It abandons the remaining tasks as soon as ctx is cancelled, which is
// how a quiet-mode match, a fatal error, or a caller cancellation stops the run
// without draining jobOrder.
func (p *Pipeline) dispatch(ctx context.Context, jobOrder []*blobTask, tasksCh chan<- *blobTask) {
	defer close(tasksCh)
	for _, task := range jobOrder {
		select {
		case <-ctx.Done():
			return
		case tasksCh <- task:
		}
	}
}

// runWorker searches tasks until tasksCh is drained and closed or ctx is
// cancelled. It never closes any channel it is given; cancelWork cancels ctx to
// stop the whole run after the first fatal error and, in quiet mode, after the
// first match. Every task whose result reaches resultsCh, or that has no result
// to report, is counted in searched; a task abandoned to cancellation is not.
func (p *Pipeline) runWorker(
	ctx context.Context,
	cancelWork context.CancelFunc,
	searched *atomic.Int64,
	tasksCh <-chan *blobTask,
	resultsCh chan<- *BlobResult,
	errCh chan<- error,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-tasksCh:
			if !ok {
				return
			}
			res := p.safeProcessTask(ctx, task)
			if ctx.Err() != nil {
				// The run is already being torn down, by the caller or by another
				// worker. res is redundant, and any error it carries is just that
				// cancellation, which the closer reports if the caller caused it.
				return
			}
			// Soft per-blob read failures are delivered on resultsCh only; every
			// other error is fatal: it is reported on errCh and stops the run.
			fatal := res.Error != nil && !isBlobReadError(res.Error)
			if fatal {
				select {
				case errCh <- res.Error:
				default:
				}
			}
			// Only send results that have matches, are binary, or have errors
			if len(res.Matches) > 0 || res.IsBinary || res.Error != nil {
				select {
				case <-ctx.Done():
					return
				case resultsCh <- res:
				}
			}
			searched.Add(1)
			// The result is published, so the remaining blobs are now pointless:
			// -q wants nothing beyond the first hit, and a fatal error abandons the
			// run. Cancelling unblocks the dispatcher and the other workers.
			if fatal || (p.cfg.Quiet && (len(res.Matches) > 0 || res.IsBinary)) {
				cancelWork()
				return
			}
		}
	}
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

// safeProcessTask turns a panic in processTask into an ordinary fatal error.
// processTask drives packfile index lookups, zlib inflate, and delta
// reconstruction over bytes from an arbitrary .git directory; a panic on a worker
// goroutine cannot be recovered by the caller and would kill the process, taking
// the reader's cleanup and the exit-code contract with it. The recover is scoped
// to a single task so one poisoned blob does not stop the worker from reporting
// it through the normal fatal-error path.
func (p *Pipeline) safeProcessTask(ctx context.Context, task *blobTask) (res *BlobResult) {
	defer func() {
		if r := recover(); r != nil {
			res = &BlobResult{
				BlobOID:     task.oid,
				Occurrences: task.occurrences,
				Error:       fmt.Errorf("panic searching blob %s: %v", task.oid, r),
			}
		}
	}()
	return p.processTask(ctx, task)
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
		var path string
		if len(task.occurrences) > 0 {
			path = task.occurrences[0].Path
		}
		res.Error = &BlobReadError{OID: task.oid, Path: path, Err: err}
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
