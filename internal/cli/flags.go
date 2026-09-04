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
			if err := parseShortFlags(arg, args, &i, cfg); err != nil {
				return nil, err
			}
			i++
			continue
		}

		// Positional argument
		positional = append(positional, arg)
		i++
	}

	if err := resolvePositionals(cfg, positional, pathsAfterDoubleDash); err != nil {
		return nil, err
	}

	return cfg, nil
}

