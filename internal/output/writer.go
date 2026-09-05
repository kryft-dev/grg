package output

import (
	"context"
	"io"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/model"
)

// cancelCheckInterval is the number of emitted lines between context
// cancellation checks performed by a cancelGuard.
const cancelCheckInterval = 1024

// Formatter provides a unified interface for rendering aggregated search results.
//
// Format renders results to w and aborts with ctx.Err() if ctx is cancelled
// mid-render. Cancellation is observed at coarse boundaries, so output written
// before the abort is always a whole-line prefix of the complete rendering.
type Formatter interface {
	Format(ctx context.Context, w io.Writer, results *aggregator.AggregatedResults) error
}

// cancelGuard amortises cancellation checks over emitted output. Consulting
// the context per line would put an atomic load on the innermost write path,
// so the guard only reads it once every cancelCheckInterval lines and at
// structural boundaries; the per-line cost is a decrement and a branch.
type cancelGuard struct {
	ctx  context.Context
	left int
}

func newCancelGuard(ctx context.Context) cancelGuard {
	if ctx == nil {
		ctx = context.Background()
	}
	return cancelGuard{ctx: ctx, left: cancelCheckInterval}
}

// lines records n freshly emitted lines, returning the context error once the
// check interval elapses and the context is done.
func (g *cancelGuard) lines(n int) error {
	g.left -= n
	if g.left > 0 {
		return nil
	}
	g.left = cancelCheckInterval
	return g.ctx.Err()
}

// boundary forces a check at a coarse structural boundary, such as the start
// of a file group, and restarts the line interval.
func (g *cancelGuard) boundary() error {
	g.left = cancelCheckInterval
	return g.ctx.Err()
}

// NewFormatter constructs the appropriate Formatter based on configuration flags.
func NewFormatter(cfg *model.Config) Formatter {
	if cfg == nil {
		cfg = &model.Config{}
	}

	if cfg.FilesWithMatches {
		return NewFilesWithMatchesFormatter(cfg)
	}
	if cfg.Count {
		return NewCountFormatter(cfg)
	}
	if !cfg.Heading {
		return NewSingleLineFormatter(cfg)
	}
	return NewGroupedFormatter(cfg)
}
