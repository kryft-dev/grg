package model

import (
	"time"
)

// CommitMetadata encapsulates Git commit header and provenance information.
type CommitMetadata struct {
	SHA            string    // 40-char (SHA-1) or 64-char (SHA-256) hex commit identifier
	TreeOID        string    // Root tree OID for the commit
	Parents        []string  // Parent commit SHAs
	Author         string    // Full author string (Name <email>)
	AuthorName     string    // Extracted author name
	AuthorEmail    string    // Extracted author email
	AuthorDate     time.Time // Author timestamp
	Committer      string    // Full committer string (Name <email>)
	CommitterName  string    // Extracted committer name
	CommitterEmail string    // Extracted committer email
	CommitterDate  time.Time // Committer timestamp
	Date           time.Time // Commit date (defaults to AuthorDate)
	Summary        string    // First line of commit message
	Message        string    // Full commit message body
}

// BlobOccurrence represents a specific blob appearing at a file path within a commit.
// Captures the provenance of a blob occurrence in repository history.
type BlobOccurrence struct {
	BlobOID    string    // Cryptographic object identifier of the Git blob
	Path       string    // Repository-relative file path
	CommitSHA  string    // Commit where this blob was introduced or observed
	CommitDate time.Time // Commit timestamp
	Mode          uint32          // Git file mode (e.g. 0100644, 0100755)
	CommitSummary string          // First line of commit message
	CommitAuthor  string          // Commit author name or string
	Commit        *CommitMetadata // Associated commit metadata, if available
}

// ShortSHA returns the canonical 7-character Git short hash.
func ShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Submatch represents the byte offset bounds of a match within a line.
type Submatch struct {
	Start int // 0-based byte start index
	End   int // 0-based byte end index (exclusive)
}

// SearchMatch represents a single matching line found in a blob.
type SearchMatch struct {
	LineNum    int        // 1-based line number
	LineText   string     // Content of the matched line
	Submatches []Submatch // Byte ranges of matched patterns within LineText
}

// CaseMode specifies case-sensitivity handling for pattern matching.
type CaseMode int

const (
	CaseSensitive CaseMode = iota
	IgnoreCase
	SmartCase
)

// ColorChoice specifies whether terminal colors should be used.
type ColorChoice string

const (
	ColorAuto   ColorChoice = "auto"
	ColorAlways ColorChoice = "always"
	ColorNever  ColorChoice = "never"
)

// Config (or SearchOptions) encapsulates all configuration parameters for grg.
type Config struct {
	// Search pattern and flags
	Patterns      []string // Patterns to search for (-e)
	Pattern       string   // Primary pattern
	FixedStrings  bool     // -F, --fixed-strings: literal string matching
	CaseMode      CaseMode // Case sensitivity mode
	IgnoreCase    bool     // -i, --ignore-case
	CaseSensitive bool     // -s, --case-sensitive
	SmartCase     bool     // -S, --smart-case
	WordRegexp    bool     // -w, --word-regexp: match whole words only
	InvertMatch   bool     // -v, --invert-match: invert match sense

	// Output formatting flags
	LineNumber       bool        // -n, -N: show line numbers (default true)
	FilesWithMatches bool        // -l, --files-with-matches: only print paths with matches
	Count            bool        // -c, --count: print match count instead of lines
	BeforeContext    int         // -B, --before-context
	AfterContext     int         // -A, --after-context
	MaxCount         int         // -m, --max-count: max matches per blob/file
	Quiet            bool        // -q, --quiet: suppress normal output
	Text             bool        // -a, --text: search binary files as text
	Color            ColorChoice // --color: auto, always, never
	Heading          bool        // --heading, --no-heading: print commit/path heading

	// File and path filtering
	Globs []string // -g, --glob: glob filters
	Types []string // -t, --type: file type filters
	Paths []string // Positional path filters (e.g. after --)

	// Git revision and history filtering
	All           bool     // --all: search all reachable commits
	FirstParent   bool     // --first-parent: follow only first parent on merges
	Since         string   // --since: commits newer than date
	Until         string   // --until: commits older than date
	Author        string   // --author: commit author regex
	Committer     string   // --committer: commit committer regex
	Branches      []string // --branch: specific branches to search
	Uncommitted   bool     // --uncommitted: include working tree and index
	ExpandCommits bool     // --expand-commits: show all commits containing blob
	Unordered     bool     // --unordered: stream matches without sorting by date
	RevRange      string   // Positional revision range (e.g. HEAD~5..HEAD)

	// Help and info
	Help     bool // -h, --help
	LongHelp bool // --help (comprehensive help)
	Version  bool // -V, --version
}

// SearchOptions is an alias to Config for API flexibility.
type SearchOptions = Config
