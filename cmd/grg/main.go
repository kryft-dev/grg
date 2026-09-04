package main

import (
	"fmt"
	"os"

	"github.com/kryft-dev/grg/internal/cli"
	"github.com/kryft-dev/grg/internal/gitengine"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "grg: %v\n", err)
		os.Exit(exitCodeForError(err))
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

func exitCodeForError(err error) int {
	if ec, ok := err.(exitCoder); ok {
		return ec.ExitCode()
	}
	return 1
}

func run(args []string) error {
	cfg, err := cli.Parse(args)
	if err != nil {
		return cliError{err: err}
	}

	if cfg.Help {
		if cfg.LongHelp {
			fmt.Print(cli.LongHelp())
		} else {
			fmt.Print(cli.ShortHelp())
		}
		return nil
	}

	if cfg.Version {
		fmt.Println(cli.Version())
		return nil
	}

	repo, err := gitengine.Discover()
	if err != nil {
		return repoError{err: err}
	}

	if !cfg.Quiet {
		location := repo.WorkTree
		if repo.IsBare {
			location = repo.GitDir + " (bare)"
		}
		fmt.Printf("Validated Git repository at %s (gitdir: %s) for pattern %q\n", location, repo.GitDir, cfg.Pattern)
	}

	return nil
}
