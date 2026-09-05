package main

import (
	"context"
	"errors"
	"fmt"
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

// skippedBlobsError records that one or more blobs could not be read and were
// skipped. It follows ripgrep's soft-error semantics: matches (if any) were still
// printed, but the exit code is 2. Each skipped blob was already reported on
// stderr as a warning, so main prints no additional message for this error.
type skippedBlobsError struct {
	count int
}

func (s skippedBlobsError) Error() string {
	return fmt.Sprintf("skipped %d unreadable blob(s)", s.count)
}

func (s skippedBlobsError) ExitCode() int {
	return 2
}

func (s skippedBlobsError) IsQuiet() bool {
	return true
}

// withSkippedBlobs folds the number of skipped blobs into the search outcome.
// Fatal errors take precedence. Otherwise any skipped blob forces exit code 2,
// except that --quiet with a match found still exits 0 (ripgrep semantics).
func withSkippedBlobs(err error, skipped int, quiet bool) error {
	if skipped == 0 {
		return err
	}
	if err == nil {
		if quiet {
			return nil
		}
		return skippedBlobsError{count: skipped}
	}
	if errors.Is(err, noMatchError{}) {
		return skippedBlobsError{count: skipped}
	}
	return err
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
