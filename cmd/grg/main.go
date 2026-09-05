// Package main provides the command-line entry point for grg (Git Ripgrep).
// It coordinates flag parsing, signal handling, repository discovery,
// commit graph walking, parallel blob searching, match aggregation,
// and terminal output rendering with ripgrep-compatible exit codes.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/cli"
	"github.com/kryft-dev/grg/internal/filter"
	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
	"github.com/kryft-dev/grg/internal/output"
	"github.com/kryft-dev/grg/internal/search"
)

func main() {
	os.Exit(realMain())
}

// realMain owns the whole run and returns the process exit code. main does
// nothing but hand that code to os.Exit, which runs no deferred functions:
// exiting from inside this function would skip signal cleanup on every
// non-zero exit, and exit code 1 (no match found) is the common case.
func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Release the signal handler as soon as the first signal cancels ctx. That
	// restores the default signal disposition, so a second Ctrl-C terminates
	// the process immediately instead of being swallowed by a handler that
	// would otherwise stay installed for the whole run. This goroutine cannot
	// leak: defer stop() cancels ctx on the normal path too, so <-ctx.Done()
	// always returns.
	go func() {
		<-ctx.Done()
		stop()
	}()

	if err := runContext(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		ec := exitCodeForError(err)
		if ec != 1 && !isQuietError(err) {
			fmt.Fprintf(os.Stderr, "grg: %v\n", err)
		}
		return ec
	}
	return 0
}

func run(args []string) error {
	return runContext(context.Background(), args, os.Stdout, os.Stderr)
}

func runWithOutput(args []string, stdout io.Writer) error {
	return runContext(context.Background(), args, stdout, os.Stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, err := cli.Parse(args)
	if err != nil {
		return cliError{err: err}
	}

	quiet := cfg != nil && cfg.Quiet

	if err := ctx.Err(); err != nil {
		return cancelError{err: err, quiet: quiet}
	}

	if cfg.Help {
		if cfg.LongHelp {
			fmt.Fprint(stdout, cli.LongHelp())
		} else {
			fmt.Fprint(stdout, cli.ShortHelp())
		}
		return nil
	}

	if cfg.Version {
		fmt.Fprintln(stdout, cli.Version())
		return nil
	}

	if err := cli.Validate(cfg); err != nil {
		return cliError{err: err}
	}

	if err := ctx.Err(); err != nil {
		return cancelError{err: err, quiet: cfg.Quiet}
	}

	repo, err := gitengine.Discover()
	if err != nil {
		return repoError{err: err}
	}

	reader, err := gitengine.NewRepositoryReader(repo)
	if err != nil {
		return repoError{err: err}
	}
	defer reader.Close()

	matcher, err := search.NewMatcher(cfg)
	if err != nil {
		return cliError{err: err}
	}

	pathFilter, err := buildPathFilter(repo, cfg)
	if err != nil {
		return cliError{err: err}
	}

	walker := gitengine.NewHistoryWalker(repo, reader, cfg, pathFilter)

	var occurrences []model.BlobOccurrence
	err = walker.Walk(ctx, func(occ model.BlobOccurrence) error {
		occurrences = append(occurrences, occ)
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return cancelError{err: err, quiet: cfg.Quiet}
		}
		return err
	}

	if err := ctx.Err(); err != nil {
		return cancelError{err: err, quiet: cfg.Quiet}
	}

	if len(occurrences) == 0 {
		return noMatchError{}
	}

	searchPipeline := search.NewPipeline(reader, matcher, cfg)

	var results []*search.BlobResult
	resultsCh, errCh := searchPipeline.ExecuteContext(ctx, occurrences)
	for res := range resultsCh {
		results = append(results, res)
	}
	if err = <-errCh; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return cancelError{err: err, quiet: cfg.Quiet}
		}
		return err
	}

	if err := ctx.Err(); err != nil {
		return cancelError{err: err, quiet: cfg.Quiet}
	}

	// Warn about unreadable blobs before any match output so stdout stays clean.
	skipped := reportSkippedBlobs(stderr, results)

	return withSkippedBlobs(emitResults(ctx, cfg, results, stdout), skipped, cfg.Quiet)
}

// reportSkippedBlobs writes one warning line per blob the pipeline could not read
// and returns how many were skipped.
func reportSkippedBlobs(stderr io.Writer, results []*search.BlobResult) int {
	skipped := 0
	for _, res := range results {
		if res == nil {
			continue
		}
		var bre *search.BlobReadError
		if errors.As(res.Error, &bre) {
			skipped++
			fmt.Fprintf(stderr, "grg: warning: skipping blob %s (%s): %v\n", bre.OID, bre.Path, bre.Err)
		}
	}
	return skipped
}

// emitResults aggregates and renders the search results, returning noMatchError
// when nothing matched. Under --quiet nothing is written to stdout.
func emitResults(ctx context.Context, cfg *model.Config, results []*search.BlobResult, stdout io.Writer) error {
	if !hasMatches(results) {
		return noMatchError{}
	}

	if cfg.Quiet {
		// Suppress stdout, exit code 0 when match is found
		return nil
	}

	agg := aggregator.New(cfg)
	aggregated := agg.Aggregate(results)

	if !aggregated.HasMatches() {
		return noMatchError{}
	}

	// Rendering is cancellable: Format observes ctx at coarse boundaries, so the
	// pre-flight check is folded into the render itself.
	formatter := output.NewFormatter(cfg)
	if err := formatter.Format(ctx, stdout, aggregated); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return cancelError{err: err, quiet: cfg.Quiet}
		}
		return err
	}

	return nil
}

// hasMatches reports whether any result carries a text or binary match.
// Results for skipped blobs carry only an error and do not count.
func hasMatches(results []*search.BlobResult) bool {
	for _, res := range results {
		if res != nil && (len(res.Matches) > 0 || res.IsBinary) {
			return true
		}
	}
	return false
}

func buildPathFilter(repo *gitengine.RepoInfo, cfg *model.Config) (func(path string) bool, error) {
	var gm *filter.GlobMatcher
	if len(cfg.Globs) > 0 {
		var err error
		gm, err = filter.NewGlobMatcher(cfg.Globs)
		if err != nil {
			return nil, err
		}
	}

	var tm *filter.TypeMatcher
	if len(cfg.Types) > 0 {
		var err error
		tm, err = filter.NewTypeMatcher(cfg.Types)
		if err != nil {
			return nil, err
		}
	}

	var paths []string
	cwd, cwdErr := os.Getwd()
	for _, p := range cfg.Paths {
		cleaned := filepath.ToSlash(filepath.Clean(p))
		if cleaned == "." || cleaned == "" {
			continue
		}
		// Anchor relative paths to repository worktree if run from subdirectory (SEC-03)
		if cwdErr == nil && repo != nil && repo.WorkTree != "" && !filepath.IsAbs(p) {
			absPath := filepath.Join(cwd, p)
			if relToWorkTree, err := filepath.Rel(repo.WorkTree, absPath); err == nil && !strings.HasPrefix(relToWorkTree, "..") {
				normRel := filepath.ToSlash(filepath.Clean(relToWorkTree))
				if normRel != "." && normRel != "" {
					paths = append(paths, normRel)
				}
			}
		}
		paths = append(paths, cleaned)
	}

	return func(path string) bool {
		norm := filepath.ToSlash(path)
		if len(paths) > 0 {
			matched := false
			for _, p := range paths {
				if norm == p || strings.HasPrefix(norm, p+"/") {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		if gm != nil && !gm.Match(norm) {
			return false
		}
		if tm != nil && !tm.Match(norm) {
			return false
		}
		return true
	}, nil
}
