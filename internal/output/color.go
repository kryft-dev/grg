package output

import (
	"io"
	"os"
	"sort"
	"strings"

	"github.com/kryft-dev/grg/internal/model"
)

// ANSI escape sequences for terminal styling.
const (
	Reset       = "\x1b[0m"
	Bold        = "\x1b[1m"
	Dim         = "\x1b[2m"
	Green       = "\x1b[32m"
	Yellow      = "\x1b[33m"
	Magenta     = "\x1b[35m"
	Cyan        = "\x1b[36m"
	BoldRed     = "\x1b[1;31m"
	BoldMagenta = "\x1b[1;35m"
	BoldYellow  = "\x1b[1;33m"
	BoldCyan    = "\x1b[1;36m"
)

// Colorizer applies ANSI styles based on the configured color policy.
type Colorizer struct {
	enabled bool
}

// NewColorizer constructs a Colorizer targeting os.Stdout.
func NewColorizer(choice model.ColorChoice) *Colorizer {
	return NewColorizerForWriter(choice, os.Stdout)
}

// NewColorizerForWriter constructs a Colorizer targeting an arbitrary io.Writer.
func NewColorizerForWriter(choice model.ColorChoice, w io.Writer) *Colorizer {
	var enabled bool
	switch choice {
	case model.ColorAlways, "ansi":
		enabled = true
	case model.ColorNever:
		enabled = false
	case model.ColorAuto, "":
		if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
			enabled = false
		} else if f, ok := w.(*os.File); ok {
			enabled = isTerminal(f)
		} else {
			enabled = false
		}
	default:
		enabled = false
	}
	return &Colorizer{enabled: enabled}
}

// isTerminal checks whether a file descriptor is an active terminal/TTY.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Enabled reports whether colorization is currently active.
func (c *Colorizer) Enabled() bool {
	return c.enabled
}

// Path formats a repository file path (bold magenta).
func (c *Colorizer) Path(s string) string {
	if !c.enabled || s == "" {
		return s
	}
	return BoldMagenta + s + Reset
}

// Commit formats a commit identifier or subheader (bold yellow).
func (c *Colorizer) Commit(s string) string {
	if !c.enabled || s == "" {
		return s
	}
	return BoldYellow + s + Reset
}

// LineNumber formats a line number (green).
func (c *Colorizer) LineNumber(s string) string {
	if !c.enabled || s == "" {
		return s
	}
	return Green + s + Reset
}

// MatchText formats a matched text segment (bold red).
func (c *Colorizer) MatchText(s string) string {
	if !c.enabled || s == "" {
		return s
	}
	return BoldRed + s + Reset
}

// Separator formats delimiters like ':', '-', and '--' (cyan).
func (c *Colorizer) Separator(s string) string {
	if !c.enabled || s == "" {
		return s
	}
	return Cyan + s + Reset
}

// HighlightLine highlights all submatch ranges within text using bold red.
func (c *Colorizer) HighlightLine(text string, submatches []model.Submatch) string {
	if !c.enabled || len(submatches) == 0 {
		return text
	}

	// Sort submatches by Start offset
	sorted := make([]model.Submatch, len(submatches))
	copy(sorted, submatches)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})

	var b strings.Builder
	lastEnd := 0
	textLen := len(text)

	for _, sm := range sorted {
		if sm.Start < lastEnd || sm.Start > textLen || sm.End > textLen || sm.Start >= sm.End {
			continue
		}

		b.WriteString(text[lastEnd:sm.Start])
		b.WriteString(BoldRed)
		b.WriteString(text[sm.Start:sm.End])
		b.WriteString(Reset)
		lastEnd = sm.End
	}

	if lastEnd < textLen {
		b.WriteString(text[lastEnd:])
	}

	return b.String()
}
