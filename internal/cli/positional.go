package cli

import (
	"fmt"
	"strings"

	"github.com/kryft-dev/grg/internal/model"
)

// resolvePositionals maps positional arguments to pattern, revision range, and paths.
func resolvePositionals(cfg *model.Config, positional []string, pathsAfterDoubleDash []string) error {
	if cfg.Help || cfg.Version {
		return nil
	}

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
			return fmt.Errorf("error: pattern is required\n\nUsage: grg [FLAGS] PATTERN [REV_RANGE] [-- PATHS...]\nTry 'grg --help' for more information.")
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

	if len(pathsAfterDoubleDash) > 0 {
		cfg.Paths = append(cfg.Paths, pathsAfterDoubleDash...)
	}

	return nil
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
