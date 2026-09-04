package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kryft-dev/grg/internal/aggregator"
	"github.com/kryft-dev/grg/internal/cli"
	"github.com/kryft-dev/grg/internal/filter"
	"github.com/kryft-dev/grg/internal/gitengine"
	"github.com/kryft-dev/grg/internal/model"
	"github.com/kryft-dev/grg/internal/output"
	"github.com/kryft-dev/grg/internal/search"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		ec := exitCodeForError(err)
		if ec != 1 {
			fmt.Fprintf(os.Stderr, "grg: %v\n", err)
		}
		os.Exit(ec)
	}
}

type exitCoder interface {
	ExitCode() int
}

type repoError struct {
	err error
}

func (r repoError) Error() string {
	return r.err.Error()
}

func (r repoError) ExitCode() int {
	return 128
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

type noMatchError struct{}

func (n noMatchError) Error() string {
	return "no matches found"
}

func (n noMatchError) ExitCode() int {
	return 1
}

func exitCodeForError(err error) int {
	if ec, ok := err.(exitCoder); ok {
		return ec.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}

func run(args []string) error {
	return runWithOutput(args, os.Stdout)
}

func runWithOutput(args []string, stdout io.Writer) error {
	cfg, err := cli.Parse(args)
	if err != nil {
		return cliError{err: err}
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

	pathFilter, err := buildPathFilter(cfg)
	if err != nil {
		return cliError{err: err}
	}

	walker := gitengine.NewHistoryWalker(repo, reader, cfg, pathFilter)

	var occurrences []model.BlobOccurrence
	err = walker.Walk(func(occ model.BlobOccurrence) error {
		occurrences = append(occurrences, occ)
		return nil
	})
	if err != nil {
		return err
	}

	if len(occurrences) == 0 {
		return noMatchError{}
	}

	searchPipeline := search.NewPipeline(reader, matcher, cfg)
	results, err := searchPipeline.Execute(occurrences)
	if err != nil {
		return err
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

	formatter := output.NewFormatter(cfg)
	if err := formatter.Format(stdout, aggregated); err != nil {
		return err
	}

	return nil
}

func buildPathFilter(cfg *model.Config) (func(path string) bool, error) {
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
	for _, p := range cfg.Paths {
		cleaned := filepath.ToSlash(filepath.Clean(p))
		if cleaned == "." || cleaned == "" {
			continue
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
