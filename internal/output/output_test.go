package output

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/model"
	"github.com/kryft-dev/grg/internal/search"
)

func makeTestAggregatedResults() *aggregator.AggregatedResults {
	date := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	return &aggregator.AggregatedResults{
		Files: []aggregator.FileMatches{
			{
				Path: "pkg/hello.go",
				Commits: []aggregator.CommitMatches{
					{
						CommitSHA:  "8752dea51234567890abcdef",
						ShortSHA:   "8752dea",
						CommitDate: date,
						Author:     "Alice",
						AuthorName: "Alice",
						Summary:    "Initial commit",
						Matches: []model.SearchMatch{
							{
								LineNum:  10,
								LineText: "fmt.Println(\"Hello world\")",
								Submatches: []model.Submatch{
									{Start: 13, End: 18}, // "Hello"
								},
							},
							{
								LineNum:  15,
								LineText: "return \"Hello again\"",
								Submatches: []model.Submatch{
									{Start: 8, End: 13}, // "Hello"
								},
							},
						},
					},
				},
			},
		},
		TotalMatches: 2,
		TotalFiles:   1,
	}
}

func TestGroupedFormatter(t *testing.T) {
	cfg := &model.Config{
		Heading:    true,
		LineNumber: true,
		Color:      model.ColorNever,
	}
	fmtter := NewFormatter(cfg)

	var buf bytes.Buffer
	results := makeTestAggregatedResults()
	if err := fmtter.Format(context.Background(), &buf, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "pkg/hello.go\n" +
		"[8752dea 2026-09-04 Alice]\n" +
		"10:fmt.Println(\"Hello world\")\n" +
		"15:return \"Hello again\"\n"

	if buf.String() != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestGroupedFormatter_NoLineNumber(t *testing.T) {
	cfg := &model.Config{
		Heading:    true,
		LineNumber: false,
		Color:      model.ColorNever,
	}
	fmtter := NewFormatter(cfg)

	var buf bytes.Buffer
	results := makeTestAggregatedResults()
	if err := fmtter.Format(context.Background(), &buf, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "pkg/hello.go\n" +
		"[8752dea 2026-09-04 Alice]\n" +
		"fmt.Println(\"Hello world\")\n" +
		"return \"Hello again\"\n"

	if buf.String() != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestGroupedFormatter_ContextLines(t *testing.T) {
	date := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	results := &aggregator.AggregatedResults{
		Files: []aggregator.FileMatches{
			{
				Path: "pkg/hello.go",
				Commits: []aggregator.CommitMatches{
					{
						CommitSHA:  "8752dea",
						ShortSHA:   "8752dea",
						CommitDate: date,
						Author:     "Alice",
						ContextGroups: []search.ContextGroup{
							{
								Lines: []search.ContextLine{
									{LineNum: 1, LineText: "// package comment", IsMatch: false},
									{LineNum: 2, LineText: "func Hello() {", IsMatch: true},
									{LineNum: 3, LineText: "}", IsMatch: false},
								},
							},
							{
								Lines: []search.ContextLine{
									{LineNum: 10, LineText: "func HelloAgain() {", IsMatch: true},
								},
							},
						},
					},
				},
			},
		},
		TotalMatches: 2,
		TotalFiles:   1,
	}

	cfg := &model.Config{
		Heading:    true,
		LineNumber: true,
		Color:      model.ColorNever,
	}
	fmtter := NewFormatter(cfg)

	var buf bytes.Buffer
	if err := fmtter.Format(context.Background(), &buf, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "pkg/hello.go\n" +
		"[8752dea 2026-09-04 Alice]\n" +
		"1-// package comment\n" +
		"2:func Hello() {\n" +
		"3-}\n" +
		"--\n" +
		"10:func HelloAgain() {\n"

	if buf.String() != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestSingleLineFormatter(t *testing.T) {
	cfg := &model.Config{
		Heading:    false,
		LineNumber: true,
		Color:      model.ColorNever,
	}
	fmtter := NewFormatter(cfg)

	var buf bytes.Buffer
	results := makeTestAggregatedResults()
	if err := fmtter.Format(context.Background(), &buf, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "8752dea:pkg/hello.go:10:fmt.Println(\"Hello world\")\n" +
		"8752dea:pkg/hello.go:15:return \"Hello again\"\n"

	if buf.String() != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestSingleLineFormatter_NoLineNumber(t *testing.T) {
	cfg := &model.Config{
		Heading:    false,
		LineNumber: false,
		Color:      model.ColorNever,
	}
	fmtter := NewFormatter(cfg)

	var buf bytes.Buffer
	results := makeTestAggregatedResults()
	if err := fmtter.Format(context.Background(), &buf, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "8752dea:pkg/hello.go:fmt.Println(\"Hello world\")\n" +
		"8752dea:pkg/hello.go:return \"Hello again\"\n"

	if buf.String() != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestFilesWithMatchesFormatter(t *testing.T) {
	cfg := &model.Config{
		FilesWithMatches: true,
		Color:            model.ColorNever,
	}
	fmtter := NewFormatter(cfg)

	var buf bytes.Buffer
	results := makeTestAggregatedResults()
	if err := fmtter.Format(context.Background(), &buf, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "8752dea:pkg/hello.go\n"
	if buf.String() != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestCountFormatter(t *testing.T) {
	cfg := &model.Config{
		Count: true,
		Color: model.ColorNever,
	}
	fmtter := NewFormatter(cfg)

	var buf bytes.Buffer
	results := makeTestAggregatedResults()
	if err := fmtter.Format(context.Background(), &buf, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "8752dea:pkg/hello.go:2\n"
	if buf.String() != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestBinaryFormatter(t *testing.T) {
	c := NewColorizer(model.ColorNever)
	notice := FormatBinaryNotice("bin/app", "8752dea", c)
	expected := "Binary file bin/app matches in 8752dea"
	if notice != expected {
		t.Errorf("expected %q, got %q", expected, notice)
	}

	// Grouped formatter with binary commit
	results := &aggregator.AggregatedResults{
		Files: []aggregator.FileMatches{
			{
				Path: "data.bin",
				Commits: []aggregator.CommitMatches{
					{
						CommitSHA: "abc1234",
						ShortSHA:  "abc1234",
						IsBinary:  true,
					},
				},
			},
		},
		TotalMatches: 1,
		TotalFiles:   1,
	}

	cfg := &model.Config{
		Heading: true,
		Color:   model.ColorNever,
	}
	var buf bytes.Buffer
	if err := NewFormatter(cfg).Format(context.Background(), &buf, results); err != nil {
		t.Fatal(err)
	}

	expectedOutput := "data.bin\n[abc1234]\nBinary file data.bin matches in abc1234\n"
	if buf.String() != expectedOutput {
		t.Errorf("expected %q, got %q", expectedOutput, buf.String())
	}
}

func TestColorHighlighting(t *testing.T) {
	c := NewColorizer(model.ColorAlways)
	if !c.Enabled() {
		t.Fatalf("expected colorizer to be enabled with ColorAlways")
	}

	// Path
	if !strings.Contains(c.Path("foo.go"), BoldMagenta) {
		t.Errorf("expected Path to contain BoldMagenta ANSI escape")
	}

	// Commit
	if !strings.Contains(c.Commit("[8752dea]"), BoldYellow) {
		t.Errorf("expected Commit to contain BoldYellow ANSI escape")
	}

	// LineNumber
	if !strings.Contains(c.LineNumber("42"), Green) {
		t.Errorf("expected LineNumber to contain Green ANSI escape")
	}

	// MatchText
	if !strings.Contains(c.MatchText("match"), BoldRed) {
		t.Errorf("expected MatchText to contain BoldRed ANSI escape")
	}

	// Separator
	if !strings.Contains(c.Separator(":"), Cyan) {
		t.Errorf("expected Separator to contain Cyan ANSI escape")
	}

	// HighlightLine
	text := "hello world"
	sub := []model.Submatch{{Start: 0, End: 5}}
	highlighted := c.HighlightLine(text, sub)
	expected := BoldRed + "hello" + Reset + " world"
	if highlighted != expected {
		t.Errorf("expected %q, got %q", expected, highlighted)
	}
}

// makeLargeAggregatedResults builds a result set whose rendering spans several
// cancelCheckInterval windows, so that a mid-render cancellation is observable.
func makeLargeAggregatedResults(files, matchesPerFile int) *aggregator.AggregatedResults {
	date := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	res := &aggregator.AggregatedResults{TotalFiles: files}
	for f := range files {
		matches := make([]model.SearchMatch, matchesPerFile)
		for m := range matches {
			matches[m] = model.SearchMatch{
				LineNum:  m + 1,
				LineText: fmt.Sprintf("needle in file %d line %d", f, m+1),
			}
		}
		res.Files = append(res.Files, aggregator.FileMatches{
			Path: fmt.Sprintf("pkg/file%04d.go", f),
			Commits: []aggregator.CommitMatches{{
				CommitSHA:  fmt.Sprintf("%040x", f),
				ShortSHA:   fmt.Sprintf("%07x", f),
				CommitDate: date,
				Author:     "Alice",
				AuthorName: "Alice",
				Summary:    "bulk commit",
				Matches:    matches,
			}},
		})
		res.TotalMatches += matchesPerFile
	}
	return res
}

// cancelAfterWriter cancels the render's context once after lines have been
// written, and keeps recording whatever the formatter emits afterwards so the
// test can prove the remainder was never produced.
type cancelAfterWriter struct {
	buf    bytes.Buffer
	cancel context.CancelFunc
	after  int
	lines  int
}

func (w *cancelAfterWriter) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	w.lines += bytes.Count(p, []byte("\n"))
	if w.cancel != nil && w.lines >= w.after {
		w.cancel()
		w.cancel = nil
	}
	return n, err
}

func TestFormatterCancelledMidRender(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *model.Config
		results *aggregator.AggregatedResults
	}{
		{
			name:    "grouped",
			cfg:     &model.Config{Heading: true, LineNumber: true, Color: model.ColorNever},
			results: makeLargeAggregatedResults(8, 512),
		},
		{
			name:    "single",
			cfg:     &model.Config{Heading: false, LineNumber: true, Color: model.ColorNever},
			results: makeLargeAggregatedResults(8, 512),
		},
		{
			name:    "count",
			cfg:     &model.Config{Count: true, Color: model.ColorNever},
			results: makeLargeAggregatedResults(3000, 1),
		},
		{
			name:    "files-with-matches",
			cfg:     &model.Config{FilesWithMatches: true, Color: model.ColorNever},
			results: makeLargeAggregatedResults(3000, 1),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var full bytes.Buffer
			if err := NewFormatter(tc.cfg).Format(context.Background(), &full, tc.results); err != nil {
				t.Fatalf("uncancelled render failed: %v", err)
			}
			want := full.String()
			if lines := strings.Count(want, "\n"); lines <= 2*cancelCheckInterval {
				t.Fatalf("fixture renders %d lines, too few to span several check intervals", lines)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := &cancelAfterWriter{cancel: cancel, after: 10}

			err := NewFormatter(tc.cfg).Format(ctx, w, tc.results)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled, got %v", err)
			}

			got := w.buf.String()
			if got == "" {
				t.Fatal("expected the output written before cancellation to be retained")
			}
			if len(got) >= len(want) {
				t.Fatalf("cancellation emitted %d of %d bytes: the remainder was not skipped", len(got), len(want))
			}
			if !strings.HasPrefix(want, got) {
				t.Fatalf("output after cancellation is not a prefix of the full render (%d bytes written)", len(got))
			}
			if !strings.HasSuffix(got, "\n") {
				t.Fatalf("cancellation truncated mid-line: %q", got[max(0, len(got)-64):])
			}
		})
	}
}

func TestFormatterAlreadyCancelledEmitsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &model.Config{Heading: true, LineNumber: true, Color: model.ColorNever}
	var buf bytes.Buffer
	err := NewFormatter(cfg).Format(ctx, &buf, makeTestAggregatedResults())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for an already-cancelled context, got %q", buf.String())
	}
}
