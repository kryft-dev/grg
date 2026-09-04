package cli

import (
	"fmt"
	"strconv"

	"github.com/kryft-dev/grg/internal/model"
)

// parseShortFlags processes a bundled short flag argument like -nv or -C2.
func parseShortFlags(arg string, args []string, i *int, cfg *model.Config) error {
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
				j = len(rest) // consumed remainder of bundle
			} else {
				if *i+1 >= len(args) {
					return fmt.Errorf("flag '-%c' requires an argument", ch)
				}
				v = args[*i+1]
				*i++
			}
			switch ch {
			case 'C':
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return fmt.Errorf("invalid context value '%s': must be a non-negative integer", v)
				}
				cfg.BeforeContext = n
				cfg.AfterContext = n
			case 'A':
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return fmt.Errorf("invalid after-context value '%s': must be a non-negative integer", v)
				}
				cfg.AfterContext = n
			case 'B':
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return fmt.Errorf("invalid before-context value '%s': must be a non-negative integer", v)
				}
				cfg.BeforeContext = n
			case 'm':
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return fmt.Errorf("invalid max-count value '%s': must be a non-negative integer", v)
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
			return fmt.Errorf("unknown flag: '-%c'", ch)
		}
	}
	return nil
}
