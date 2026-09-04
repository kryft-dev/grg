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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := runContext(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		ec := exitCodeForError(err)
		if ec != 1 {
			if !isQuietError(err) {
				fmt.Fprintf(os.Stderr, "grg: %v\n", err)
			}
		}
		os.Exit(ec)
	}
}

type exitCoder interface {
	ExitCode() int
}

type quietChecker interface {
	IsQuiet() bool
}

type repoError struct {
	err error
}

func (r repoError) Error() string {
	return r.err.Error()
}

func (r repoError) ExitCode() int {
	return 2
}

func (r repoError) Unwrap() error {
	return r.err
}

type cliError struct {
	err error
}

func (c cliError) Error() string {
	return c.err.Error()
}

func (c cliError) ExitCode() int {
	return 2
}

func (c cliError) Unwrap() error {
	return c.err
}

type cancelError struct {
	err   error
	quiet bool
}

func (c cancelError) Error() string {
	if c.err != nil {
		return c.err.Error()
	}
	return "operation canceled"
}

func (c cancelError) ExitCode() int {
	return 2
}

func (c cancelError) IsQuiet() bool {
	return c.quiet
}

func (c cancelError) Unwrap() error {
	return c.err
}

type noMatchError struct{}

func (n noMatchError) Error() string {
	return "no matches found"
}

func (n noMatchError) ExitCode() int {
	return 1
}

func isQuietError(err error) bool {
	var qc quietChecker
	if errors.As(err, &qc) {
		return qc.IsQuiet()
	}
	return false
}

func exitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	var ec exitCoder
	if errors.As(err, &ec) {
		return ec.ExitCode()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 2
	}
	return 2
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
	err = walker.Walk(func(occ model.BlobOccurrence) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
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

	type chanExecutor interface {
		ExecuteContext(ctx context.Context, occurrences []model.BlobOccurrence) (<-chan *search.BlobResult, <-chan error)
	}
	type sliceExecutor interface {
		ExecuteContext(ctx context.Context, occurrences []model.BlobOccurrence) ([]*search.BlobResult, error)
	}

	var results []*search.BlobResult
	if che, ok := any(searchPipeline).(chanExecutor); ok {
		resultsCh, errCh := che.ExecuteContext(ctx, occurrences)
		for res := range resultsCh {
			results = append(results, res)
		}
		if err = <-errCh; err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return cancelError{err: err, quiet: cfg.Quiet}
			}
			return err
		}
	} else if se, ok := any(searchPipeline).(sliceExecutor); ok {
		results, err = se.ExecuteContext(ctx, occurrences)
	} else {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return cancelError{err: ctxErr, quiet: cfg.Quiet}
		}
		results, err = searchPipeline.Execute(occurrences)
	}

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return cancelError{err: err, quiet: cfg.Quiet}
		}
		return err
	}

	if err := ctx.Err(); err != nil {
		return cancelError{err: err, quiet: cfg.Quiet}
	}

	if len(results) == 0 {
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

	if err := ctx.Err(); err != nil {
		return cancelError{err: err, quiet: cfg.Quiet}
	}

	formatter := output.NewFormatter(cfg)
	if err := formatter.Format(stdout, aggregated); err != nil {
		return err
	}

	return nil
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
