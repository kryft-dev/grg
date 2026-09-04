package output

import (
	"fmt"
	"io"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/model"
)

// FilesWithMatchesFormatter implements -l / --files-with-matches formatting,
// emitting distinct <commit-short>:<path> entries.
type FilesWithMatchesFormatter struct {
	cfg   *model.Config
	color *Colorizer
}

// NewFilesWithMatchesFormatter constructs a FilesWithMatchesFormatter.
func NewFilesWithMatchesFormatter(cfg *model.Config) *FilesWithMatchesFormatter {
	return &FilesWithMatchesFormatter{
		cfg:   cfg,
		color: NewColorizer(cfg.Color),
	}
}

// Format writes distinct <commit-short>:<path> lines to w.
func (f *FilesWithMatchesFormatter) Format(w io.Writer, results *aggregator.AggregatedResults) error {
	if results == nil || len(results.Files) == 0 {
		return nil
	}

	c := f.color
	seen := make(map[string]bool)

	for _, file := range results.Files {
		for _, commit := range file.Commits {
			if len(commit.Matches) == 0 && !commit.IsBinary {
				continue
			}

			shortSHA := commit.ShortSHA
			if shortSHA == "" {
				shortSHA = model.ShortSHA(commit.CommitSHA)
			}

			key := shortSHA + ":" + file.Path
			if seen[key] {
				continue
			}
			seen[key] = true

			formattedCommit := c.Commit(shortSHA)
			formattedSep := c.Separator(":")
			formattedPath := c.Path(file.Path)

			if _, err := fmt.Fprintf(w, "%s%s%s\n", formattedCommit, formattedSep, formattedPath); err != nil {
				return err
			}
		}
	}

	return nil
}
