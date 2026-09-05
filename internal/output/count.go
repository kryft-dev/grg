package output

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/model"
)

// CountFormatter implements -c / --count formatting,
// emitting match counts per commit and file.
type CountFormatter struct {
	cfg   *model.Config
	color *Colorizer
}

// NewCountFormatter constructs a CountFormatter.
func NewCountFormatter(cfg *model.Config) *CountFormatter {
	return &CountFormatter{
		cfg:   cfg,
		color: NewColorizer(cfg.Color),
	}
}

// Format writes <commit-short>:<path>:<count> lines to w, aborting with
// ctx.Err() if ctx is cancelled mid-render.
func (f *CountFormatter) Format(ctx context.Context, w io.Writer, results *aggregator.AggregatedResults) error {
	if results == nil || len(results.Files) == 0 {
		return nil
	}

	c := f.color
	guard := newCancelGuard(ctx)
	for _, file := range results.Files {
		if err := guard.boundary(); err != nil {
			return err
		}

		for _, commit := range file.Commits {
			count := len(commit.Matches)
			if commit.IsBinary {
				count = 1
			}
			if count == 0 {
				continue
			}

			shortSHA := commit.ShortSHA
			if shortSHA == "" {
				shortSHA = model.ShortSHA(commit.CommitSHA)
			}

			formattedCommit := c.Commit(shortSHA)
			formattedSep := c.Separator(":")
			formattedPath := c.Path(file.Path)
			formattedCount := c.LineNumber(strconv.Itoa(count))

			if _, err := fmt.Fprintf(w, "%s%s%s%s%s\n",
				formattedCommit, formattedSep,
				formattedPath, formattedSep,
				formattedCount); err != nil {
				return err
			}
			if err := guard.lines(1); err != nil {
				return err
			}
		}
	}

	return nil
}
