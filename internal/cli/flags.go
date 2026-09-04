package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kryft-dev/grg/internal/model"
)

// AppVersion represents the current release version of grg.
const AppVersion = "0.1.0"

// Parse parses command line arguments (excluding the program name, e.g. os.Args[1:])
// into a model.Config struct.
func Parse(args []string) (*model.Config, error) {
	return ParseArgs(args)
}

// ParseArgs parses command line arguments into a model.Config struct.
func ParseArgs(args []string) (*model.Config, error) {
	cfg := &model.Config{
		LineNumber:    true,
		Heading:       true,
		Color:         model.ColorAuto,
		CaseMode:      model.CaseSensitive,
		CaseSensitive: true,
	}

	var positional []string
	var pathsAfterDoubleDash []string
	doubleDashSeen := false

	i := 0
	for i < len(args) {
		arg := args[i]

		if doubleDashSeen {
			pathsAfterDoubleDash = append(pathsAfterDoubleDash, arg)
			i++
			continue
		}

		if arg == "--" {
			doubleDashSeen = true
			i++
			continue
		}

		if strings.HasPrefix(arg, "--") {
			// Long flag
			flagExpr := arg[2:]
			var name, val string
			hasVal := false

			if eqIdx := strings.IndexByte(flagExpr, '='); eqIdx != -1 {
				name = flagExpr[:eqIdx]
				val = flagExpr[eqIdx+1:]
				hasVal = true
			} else {
				name = flagExpr
			}

			// Helper to retrieve value for flags that require an argument
			requireVal := func() (string, error) {
				if hasVal {
					return val, nil
				}
				if i+1 < len(args) {
					v := args[i+1]
					i++
					return v, nil
				}
				return "", fmt.Errorf("flag '--%s' requires an argument", name)
			}

			switch name {
			case "help":
				cfg.Help = true
				cfg.LongHelp = true
			case "version":
				cfg.Version = true
			case "ignore-case":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				if b {
					cfg.CaseMode = model.IgnoreCase
					cfg.IgnoreCase = true
					cfg.CaseSensitive = false
					cfg.SmartCase = false
				} else {
					cfg.CaseMode = model.CaseSensitive
					cfg.CaseSensitive = true
					cfg.IgnoreCase = false
				}
			case "case-sensitive":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				if b {
					cfg.CaseMode = model.CaseSensitive
					cfg.CaseSensitive = true
					cfg.IgnoreCase = false
					cfg.SmartCase = false
				}
			case "smart-case":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				if b {
					cfg.CaseMode = model.SmartCase
					cfg.SmartCase = true
					cfg.CaseSensitive = false
					cfg.IgnoreCase = false
				}
			case "fixed-strings":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.FixedStrings = b
			case "word-regexp":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.WordRegexp = b
			case "invert-match":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.InvertMatch = b
			case "line-number":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.LineNumber = b
			case "no-line-number":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.LineNumber = !b
			case "files-with-matches":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.FilesWithMatches = b
			case "count":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.Count = b
			case "quiet":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.Quiet = b
			case "text":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.Text = b
			case "heading":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.Heading = b
			case "no-heading":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.Heading = !b
			case "all":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.All = b
			case "first-parent":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.FirstParent = b
			case "uncommitted":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.Uncommitted = b
			case "expand-commits", "all-commits":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.ExpandCommits = b
			case "unordered":
				b, err := parseBool(val, hasVal)
				if err != nil {
					return nil, err
				}
				cfg.Unordered = b
			case "context":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid context value '%s': must be a non-negative integer", v)
				}
				cfg.BeforeContext = n
				cfg.AfterContext = n
			case "after-context":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid after-context value '%s': must be a non-negative integer", v)
				}
				cfg.AfterContext = n
			case "before-context":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid before-context value '%s': must be a non-negative integer", v)
				}
				cfg.BeforeContext = n
			case "max-count":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid max-count value '%s': must be a non-negative integer", v)
				}
				cfg.MaxCount = n
			case "glob":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Globs = append(cfg.Globs, v)
			case "type":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Types = append(cfg.Types, v)
			case "regexp":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Patterns = append(cfg.Patterns, v)
			case "since":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Since = v
			case "until":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Until = v
			case "author":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Author = v
			case "committer":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Committer = v
			case "branch":
				v, err := requireVal()
				if err != nil {
					return nil, err
				}
				cfg.Branches = append(cfg.Branches, v)
			case "color":
				var cVal string
				if hasVal {
					cVal = val
				} else if i+1 < len(args) && isColorChoice(args[i+1]) {
					cVal = args[i+1]
					i++
				} else {
					cVal = "always"
				}
				switch strings.ToLower(cVal) {
				case "auto", "always", "never", "ansi":
					cfg.Color = model.ColorChoice(strings.ToLower(cVal))
				default:
					return nil, fmt.Errorf("invalid argument '%s' for '--color': must be one of 'auto', 'always', 'never'", cVal)
				}
			default:
				return nil, fmt.Errorf("unknown flag: '--%s'", name)
			}
			i++
			continue
		}

		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			// Short flags bundle
			rest := arg[1:]
			for j := 0; j < len(rest); j++ {
				ch := rest[j]
				switch ch {
				case 'h':
					cfg.Help = true
				case 'V':
					cfg.Version = true
				case 'i':
					cfg.CaseMode = model.IgnoreCase
					cfg.IgnoreCase = true
					cfg.CaseSensitive = false
					cfg.SmartCase = false
				case 's':
					cfg.CaseMode = model.CaseSensitive
					cfg.CaseSensitive = true
					cfg.IgnoreCase = false
					cfg.SmartCase = false
				case 'S':
					cfg.CaseMode = model.SmartCase
					cfg.SmartCase = true
					cfg.CaseSensitive = false
					cfg.IgnoreCase = false
				case 'F':
					cfg.FixedStrings = true
				case 'w':
					cfg.WordRegexp = true
				case 'v':
					cfg.InvertMatch = true
				case 'n':
					cfg.LineNumber = true
				case 'N':
					cfg.LineNumber = false
				case 'l':
					cfg.FilesWithMatches = true
				case 'c':
					cfg.Count = true
				case 'q':
					cfg.Quiet = true
				case 'a':
					cfg.Text = true
				case 'C', 'A', 'B', 'm', 'g', 't', 'e':
					var v string
					if j+1 < len(rest) {
						v = rest[j+1:]
						j = len(rest) // consumed the rest
					} else {
						if i+1 >= len(args) {
							return nil, fmt.Errorf("flag '-%c' requires an argument", ch)
						}
						v = args[i+1]
						i++
					}
					switch ch {
					case 'C':
						n, err := strconv.Atoi(v)
						if err != nil || n < 0 {
							return nil, fmt.Errorf("invalid context value '%s': must be a non-negative integer", v)
						}
						cfg.BeforeContext = n
						cfg.AfterContext = n
					case 'A':
						n, err := strconv.Atoi(v)
						if err != nil || n < 0 {
							return nil, fmt.Errorf("invalid after-context value '%s': must be a non-negative integer", v)
						}
						cfg.AfterContext = n
					case 'B':
						n, err := strconv.Atoi(v)
						if err != nil || n < 0 {
							return nil, fmt.Errorf("invalid before-context value '%s': must be a non-negative integer", v)
						}
						cfg.BeforeContext = n
					case 'm':
						n, err := strconv.Atoi(v)
						if err != nil || n < 0 {
							return nil, fmt.Errorf("invalid max-count value '%s': must be a non-negative integer", v)
						}
						cfg.MaxCount = n
					case 'g':
						cfg.Globs = append(cfg.Globs, v)
					case 't':
						cfg.Types = append(cfg.Types, v)
					case 'e':
						cfg.Patterns = append(cfg.Patterns, v)
					}
				default:
					return nil, fmt.Errorf("unknown flag: '-%c'", ch)
				}
			}
			i++
			continue
		}

		// Positional argument
		positional = append(positional, arg)
		i++
	}

	// If help or version requested, return early without validating pattern
	if cfg.Help || cfg.Version {
		return cfg, nil
	}

	// Positional arguments resolution:
	// Pattern [REV_RANGE] [-- PATHS...]
	if len(cfg.Patterns) > 0 {
		cfg.Pattern = cfg.Patterns[0]
		if len(positional) == 1 {
			cfg.RevRange = positional[0]
		} else if len(positional) >= 2 {
			cfg.RevRange = positional[0]
			cfg.Paths = append(cfg.Paths, positional[1:]...)
		}
	} else {
		if len(positional) == 0 {
			return nil, fmt.Errorf("error: pattern is required\n\nUsage: grg [FLAGS] PATTERN [REV_RANGE] [-- PATHS...]\nTry 'grg --help' for more information.")
		}
		cfg.Pattern = positional[0]
		cfg.Patterns = []string{positional[0]}
		if len(positional) == 2 {
			cfg.RevRange = positional[1]
		} else if len(positional) >= 3 {
			cfg.RevRange = positional[1]
			cfg.Paths = append(cfg.Paths, positional[2:]...)
		}
	}

	// Append any paths specified after "--"
	if len(pathsAfterDoubleDash) > 0 {
		cfg.Paths = append(cfg.Paths, pathsAfterDoubleDash...)
	}

	return cfg, nil
}

func parseBool(val string, hasVal bool) (bool, error) {
	if !hasVal {
		return true, nil
	}
	switch strings.ToLower(val) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value '%s'", val)
	}
}

func isColorChoice(s string) bool {
	switch strings.ToLower(s) {
	case "auto", "always", "never", "ansi":
		return true
	default:
		return false
	}
}

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
        --heading              Print commit and path header above grouped matches (default on TTY).
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
	return fmt.Sprintf("grg %s", AppVersion)
}
