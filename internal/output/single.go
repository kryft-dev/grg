package output

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/model"
)

// SingleLineFormatter formats matches as individual lines:
// <commit-short>:<path>:<line>:<text>
type SingleLineFormatter struct {
	cfg   *model.Config
	color *Colorizer
}

// NewSingleLineFormatter constructs a SingleLineFormatter.
func NewSingleLineFormatter(cfg *model.Config) *SingleLineFormatter {
	return &SingleLineFormatter{
		cfg:   cfg,
		color: NewColorizer(cfg.Color),
	}
}

// Format writes single-line matches to w, aborting with ctx.Err() if ctx is
// cancelled mid-render.
func (s *SingleLineFormatter) Format(ctx context.Context, w io.Writer, results *aggregator.AggregatedResults) error {
	if results == nil || len(results.Files) == 0 {
		return nil
	}

	c := s.color
	guard := newCancelGuard(ctx)
	for _, file := range results.Files {
		if err := guard.boundary(); err != nil {
			return err
		}

		for _, commit := range file.Commits {
			shortSHA := commit.ShortSHA
			if shortSHA == "" {
				shortSHA = model.ShortSHA(commit.CommitSHA)
			}

			if commit.IsBinary {
				if err := WriteBinaryNotice(w, file.Path, shortSHA, c); err != nil {
					return err
				}
				continue
			}

			if len(commit.ContextGroups) > 0 {
				for gi, group := range commit.ContextGroups {
					if gi > 0 {
						if _, err := fmt.Fprintln(w, c.Separator("--")); err != nil {
							return err
						}
					}
					for _, line := range group.Lines {
						if err := s.writeEntry(w, shortSHA, file.Path, line.LineNum, line.LineText, line.IsMatch, line.Submatches); err != nil {
							return err
						}
						if err := guard.lines(1); err != nil {
							return err
						}
					}
				}
			} else {
				for _, match := range commit.Matches {
					if err := s.writeEntry(w, shortSHA, file.Path, match.LineNum, match.LineText, true, match.Submatches); err != nil {
						return err
					}
					if err := guard.lines(1); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (s *SingleLineFormatter) writeEntry(w io.Writer, shortSHA, path string, lineNum int, text string, isMatch bool, submatches []model.Submatch) error {
	c := s.color
	sep := ":"
	if !isMatch {
		sep = "-"
	}

	formattedCommit := c.Commit(shortSHA)
	formattedSep := c.Separator(sep)
	formattedPath := c.Path(path)

	formattedText := text
	if isMatch {
		formattedText = c.HighlightLine(text, submatches)
	}

	if s.cfg.LineNumber {
		formattedLine := c.LineNumber(strconv.Itoa(lineNum))
		_, err := fmt.Fprintf(w, "%s%s%s%s%s%s%s\n",
			formattedCommit, formattedSep,
			formattedPath, formattedSep,
			formattedLine, formattedSep,
			formattedText)
		return err
	}

	_, err := fmt.Fprintf(w, "%s%s%s%s%s\n",
		formattedCommit, formattedSep,
		formattedPath, formattedSep,
		formattedText)
	return err
}
