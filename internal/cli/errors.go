package cli

import "errors"

// Sentinel errors for the CLI package according to the interface contract.
var (
	// ErrHelpRequested indicates that help output was requested (-h, --help).
	ErrHelpRequested = errors.New("help requested")

	// ErrVersionRequested indicates that version output was requested (-V, --version).
	ErrVersionRequested = errors.New("version requested")

	// ErrInvalidArgument indicates invalid CLI arguments, options, flags, or configuration.
	ErrInvalidArgument = errors.New("invalid argument")
)
