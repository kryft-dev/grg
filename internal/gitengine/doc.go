// Package gitengine implements a pure-Go Git object reader and DAG traversal engine.
// It parses Git packfiles, version 2 pack index (.idx) files, loose objects,
// commit headers, and directory trees directly from .git/objects/ without requiring
// external git binaries, libgit2, or working tree checkouts.
package gitengine
