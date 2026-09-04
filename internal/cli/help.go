package cli

import "fmt"

// ShortHelp returns concise help output for -h.
func ShortHelp() string {
	return `grg - fast Git repository grep

Usage:
    grg [FLAGS] PATTERN [REV_RANGE] [-- PATHS...]
    grg [FLAGS] -e PATTERN... [REV_RANGE] [-- PATHS...]

Common Flags:
    -i, --ignore-case          Case-insensitive search
    -s, --case-sensitive       Case-sensitive search
    -S, --smart-case           Smart-case search (case-insensitive unless uppercase present)
    -F, --fixed-strings        Treat pattern as literal string instead of regex
    -w, --word-regexp          Only match whole words
    -v, --invert-match         Invert match (select non-matching lines)
    -n, --line-number          Show 1-based line numbers (default: enabled)
    -N, --no-line-number       Suppress line numbers
    -l, --files-with-matches   Print only commits and paths that contain matches
    -c, --count                Print total number of matching lines
    -C, --context NUM          Show NUM lines before and after matches
    -m, --max-count NUM        Stop after NUM matches per blob
    -q, --quiet                Do not print matches; exit 0 if match found, 1 otherwise
    -a, --text                 Search binary files as text
    -H, --heading              Print commit and path header above grouped matches
    -g, --glob GLOB            Include/exclude files matching glob (multi-value)
    -t, --type TYPE            Only search files of type TYPE (multi-value)

Git Flags:
        --all                  Search across all reachable commits, branches, and tags
        --first-parent         Follow only first parent upon seeing merge commits
        --since DATE           Search commits more recent than date
        --until DATE           Search commits older than date
        --author PATTERN       Filter commits by author regex
        --committer PATTERN    Filter commits by committer regex
        --branch BRANCH        Search specified branch (multi-value)
        --uncommitted          Include dirty working tree and staged index changes
        --expand-commits       Attribute matches to all commits containing blob
        --unordered            Stream matches immediately without sorting

    -h                         Print concise help (use --help for all options)
    -V, --version              Print version information
`
}

// LongHelp returns comprehensive help output for --help.
func LongHelp() string {
	return `grg - standalone ripgrep-class search across Git repository history

Usage:
    grg [FLAGS] PATTERN [REV_RANGE] [-- PATHS...]
    grg [FLAGS] -e PATTERN... [REV_RANGE] [-- PATHS...]

Arguments:
    PATTERN        Regular expression or literal pattern to search for.
    REV_RANGE      Optional Git revision or range (e.g. HEAD, main, v1.0..v2.0, HEAD~5).
                   Defaults to HEAD ancestry if omitted.
    PATHS          Optional path filters (e.g. internal/ cmd/ grg.go). Can follow '--'.

Search Flags:
    -e, --regexp PATTERN       A pattern to search for. Can be specified multiple times.
    -i, --ignore-case          Case-insensitive search. Overrides -s and -S.
    -s, --case-sensitive       Case-sensitive search. Overrides -i and -S.
    -S, --smart-case           Smart-case search: case-insensitive if pattern is all lowercase,
                               case-sensitive if pattern contains any uppercase characters.
    -F, --fixed-strings        Treat pattern as literal string instead of a regular expression.
    -w, --word-regexp          Only match whole words (surrounds pattern with \b boundaries).
    -v, --invert-match         Invert matching: select non-matching lines.
    -a, --text                 Search binary blobs as if they were text.

Output Formatting Flags:
    -n, --line-number          Show 1-based line numbers for matches (default: enabled).
    -N, --no-line-number       Suppress line numbers from output.
    -l, --files-with-matches   Only print the commit and file path of matching files.
    -c, --count                Print only the count of matching lines instead of matches.
    -C, --context NUM          Show NUM lines of context before and after each match.
    -A, --after-context NUM    Show NUM lines of context after each match.
    -B, --before-context NUM   Show NUM lines of context before each match.
    -m, --max-count NUM        Stop scanning a blob after NUM matching lines.
    -q, --quiet                Do not print matches to stdout. Exit 0 if match found, 1 otherwise.
        --color WHEN           Control terminal color: 'auto', 'always', or 'never' (default: auto).
    -H, --heading              Print commit and path header above grouped matches (default on TTY).
        --no-heading           Print matches inline in format <commit>:<path>:<line>:<text>.

Filtering Flags:
    -g, --glob GLOB            Include or exclude files and directories matching GLOB pattern.
                               Can be provided multiple times (e.g. -g '*.go' -g '!*_test.go').
    -t, --type TYPE            Only search files matching TYPE definition (e.g. -t go -t rust).
                               Can be provided multiple times.

Git History & Revision Flags:
        --all                  Search all reachable commits across all branches, tags, and remotes.
        --first-parent         Follow only the first parent commit upon encountering merge commits.
        --since DATE           Search commits more recent than given date (e.g. '2024-01-01', '2 weeks ago').
        --until DATE           Search commits older than given date.
        --author PATTERN       Filter commits whose author matches PATTERN regex.
        --committer PATTERN    Filter commits whose committer matches PATTERN regex.
        --branch BRANCH        Search commits on specified branch (can be provided multiple times).
        --uncommitted          Include dirty working tree and staged index changes in search.
        --expand-commits       Exhaustively attribute matches to all commits containing each blob,
                               instead of only the introducing / modifying commit.
        --unordered            Stream matches immediately without chronological sorting for speed.

Help & Information:
    -h                         Print concise summary of common options.
        --help                 Print this comprehensive help message.
    -V, --version              Print version information.

Examples:
    grg "TODO"                             Search for "TODO" across HEAD ancestry
    grg -i "func main" main                Search case-insensitively on branch 'main'
    grg "panic" HEAD~10..HEAD              Search within a commit range
    grg "config" -- internal/              Filter search to the 'internal/' directory
    grg -l --since "2024-01-01" "secret"   List files introducing "secret" since 2024
    grg --all -e "fix" -e "bug"            Search across all branches for multiple patterns
`
}

// Version returns the version string.
func Version() string {
	if GitCommit != "" && GitCommit != "none" {
		return fmt.Sprintf("grg %s (commit %s)", AppVersion, GitCommit)
	}
	return fmt.Sprintf("grg %s", AppVersion)
}
