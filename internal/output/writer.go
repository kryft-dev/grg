package output

import (
	"io"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/model"
)

// Formatter provides a unified interface for rendering aggregated search results.
type Formatter interface {
	Format(w io.Writer, results *aggregator.AggregatedResults) error
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
