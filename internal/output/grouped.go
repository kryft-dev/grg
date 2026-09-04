package output

import (
	"fmt"
	"io"
	"strconv"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/model"
)

// GroupedFormatter formats search results in a grouped hierarchy:
// path ➔ commit subheader ➔ matches
type GroupedFormatter struct {
	cfg   *model.Config
	color *Colorizer
}

// NewGroupedFormatter constructs a GroupedFormatter.
func NewGroupedFormatter(cfg *model.Config) *GroupedFormatter {
	return &GroupedFormatter{
		cfg:   cfg,
		color: NewColorizer(cfg.Color),
	}
}

// Format writes the grouped results to w.
func (g *GroupedFormatter) Format(w io.Writer, results *aggregator.AggregatedResults) error {
	if results == nil || len(results.Files) == 0 {
		return nil
	}

	c := g.color
	firstFile := true

	for _, file := range results.Files {
		if !firstFile {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		firstFile = false

		// 1. Path header
		if _, err := fmt.Fprintln(w, c.Path(file.Path)); err != nil {
			return err
		}

		// 2. Commits under this path
		for _, commit := range file.Commits {
			subheader := formatCommitSubheader(commit)
			if _, err := fmt.Fprintln(w, c.Commit(subheader)); err != nil {
				return err
			}

			if commit.IsBinary {
				if err := WriteBinaryNotice(w, file.Path, commit.ShortSHA, c); err != nil {
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
						if err := g.writeLine(w, line.LineNum, line.LineText, line.IsMatch, line.Submatches); err != nil {
							return err
						}
					}
				}
			} else {
				for _, match := range commit.Matches {
					if err := g.writeLine(w, match.LineNum, match.LineText, true, match.Submatches); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (g *GroupedFormatter) writeLine(w io.Writer, lineNum int, text string, isMatch bool, submatches []model.Submatch) error {
	c := g.color
	if g.cfg.LineNumber {
		numStr := strconv.Itoa(lineNum)
		sep := ":"
		if !isMatch {
			sep = "-"
		}
		formattedNum := c.LineNumber(numStr)
		formattedSep := c.Separator(sep)
		formattedText := text
		if isMatch {
			formattedText = c.HighlightLine(text, submatches)
		}
		_, err := fmt.Fprintf(w, "%s%s%s\n", formattedNum, formattedSep, formattedText)
		return err
	}

	formattedText := text
	if isMatch {
		formattedText = c.HighlightLine(text, submatches)
	}
	_, err := fmt.Fprintln(w, formattedText)
	return err
}

// formatCommitSubheader builds the subheader "[<commit-short> <date> <author>]".
func formatCommitSubheader(cm aggregator.CommitMatches) string {
	short := cm.ShortSHA
	if short == "" {
		short = model.ShortSHA(cm.CommitSHA)
	}
	var dateStr string
	if !cm.CommitDate.IsZero() {
		dateStr = cm.CommitDate.Format("2006-01-02")
	}
	author := cm.AuthorName
	if author == "" {
		author = cm.Author
	}

	switch {
	case dateStr != "" && author != "":
		return fmt.Sprintf("[%s %s %s]", short, dateStr, author)
	case dateStr != "":
		return fmt.Sprintf("[%s %s]", short, dateStr)
	case author != "":
		return fmt.Sprintf("[%s %s]", short, author)
	default:
		return fmt.Sprintf("[%s]", short)
	}
}
