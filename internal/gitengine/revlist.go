package gitengine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kryft-dev/grg/internal/model"
)

// RevRangeSpec specifies the include and exclude commits for history walking.
type RevRangeSpec struct {
	Include []string
	Exclude []string
}

// ParseRevSpec resolves revision strings like "HEAD~2..HEAD", "main..feature", or "HEAD".
func ParseRevSpec(repo *RepoInfo, reader ObjectReader, spec string) (*RevRangeSpec, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		headOID, err := ResolveRef(repo, "HEAD")
		if err != nil {
			return nil, err
		}
		return &RevRangeSpec{Include: []string{headOID}}, nil
	}

	if strings.Contains(spec, "..") {
		parts := strings.Split(spec, "..")
		if len(parts) == 2 {
			leftStr := strings.TrimSpace(parts[0])
			rightStr := strings.TrimSpace(parts[1])

			if rightStr == "" {
				rightStr = "HEAD"
			}
			rightOID, err := resolveRevWithAncestry(repo, reader, rightStr)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve range end %q: %w", rightStr, err)
			}

			if leftStr == "" {
				leftStr = "HEAD"
			}
			leftOID, err := resolveRevWithAncestry(repo, reader, leftStr)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve range start %q: %w", leftStr, err)
			}

			return &RevRangeSpec{
				Include: []string{rightOID},
				Exclude: []string{leftOID},
			}, nil
		}
	}

	oid, err := resolveRevWithAncestry(repo, reader, spec)
	if err != nil {
		return nil, err
	}
	return &RevRangeSpec{Include: []string{oid}}, nil
}

// resolveRevWithAncestry resolves ref names and handles ~N suffix (e.g. HEAD~3).
func resolveRevWithAncestry(repo *RepoInfo, reader ObjectReader, rev string) (string, error) {
	rev = strings.TrimSpace(rev)
	if idx := strings.IndexByte(rev, '~'); idx != -1 {
		baseRef := rev[:idx]
		nStr := rev[idx+1:]
		n, err := strconv.Atoi(nStr)
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid ancestry specifier %q", rev)
		}

		baseOID, err := ResolveRef(repo, baseRef)
		if err != nil {
			return "", err
		}

		curr := baseOID
		for i := 0; i < n; i++ {
			obj, err := reader.ReadObject(curr)
			if err != nil {
				return "", fmt.Errorf("failed reading commit %s for ancestry traversal: %w", curr, err)
			}
			meta, err := ParseCommit(curr, obj.Data)
			if err != nil {
				return "", err
			}
			if len(meta.Parents) == 0 {
				return "", fmt.Errorf("cannot traverse ~%d: commit %s has no parents", n, curr)
			}
			curr = meta.Parents[0]
		}
		return curr, nil
	}

	return ResolveRef(repo, rev)
}

// ParseFilterDate parses flexible date strings into time.Time.
func ParseFilterDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return time.Time{}, nil
	}

	// Try unix timestamp
	if sec, err := strconv.ParseInt(dateStr, 10, 64); err == nil {
		return time.Unix(sec, 0).UTC(), nil
	}

	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		time.RFC1123,
		time.RFC822,
	}

	for _, f := range formats {
		if t, err := time.Parse(f, dateStr); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized date format: %q", dateStr)
}

// MatchesCommitFilters evaluates whether a commit matches author, committer, and date filters.
func MatchesCommitFilters(meta *model.CommitMetadata, cfg *model.Config, authorRe, committerRe *regexp.Regexp, sinceTime, untilTime time.Time) bool {
	if !sinceTime.IsZero() && meta.Date.Before(sinceTime) {
		return false
	}
	if !untilTime.IsZero() && meta.Date.After(untilTime) {
		return false
	}

	if authorRe != nil {
		if !authorRe.MatchString(meta.Author) && !authorRe.MatchString(meta.AuthorName) && !authorRe.MatchString(meta.AuthorEmail) {
			return false
		}
	}

	if committerRe != nil {
		if !committerRe.MatchString(meta.Committer) && !committerRe.MatchString(meta.CommitterName) && !committerRe.MatchString(meta.CommitterEmail) {
			return false
		}
	}

	return true
}
