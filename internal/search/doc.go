// Package search provides a high-throughput, concurrent blob search pipeline.
// It coordinates worker pools to execute literal and regular expression searches
// across deduplicated Git blob contents, supporting context lines, case sensitivity
// modes, and binary payload detection.
package search
