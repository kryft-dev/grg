package main

import (
	"context"
	"errors"
)

// Exit-code error types. Every error returned from runContext is mapped to a process
// exit code by exitCodeForError: 0 on success, 1 when no match was found, and 2 for
// any other failure (CLI, repository, cancellation), mirroring ripgrep.

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
