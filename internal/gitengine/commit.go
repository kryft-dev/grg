package gitengine

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kryft-dev/grg/internal/model"
)

var (
	// ErrInvalidCommit indicates an unparseable Git commit object.
	ErrInvalidCommit = errors.New("invalid commit object")
)

// ParseCommit parses a raw Git commit object payload into model.CommitMetadata.
func ParseCommit(sha string, payload []byte) (*model.CommitMetadata, error) {
	meta := &model.CommitMetadata{
		SHA: sha,
	}

	// Split header from message body at first double-newline
	var headerBytes, bodyBytes []byte
	if idx := bytes.Index(payload, []byte("\n\n")); idx != -1 {
		headerBytes = payload[:idx]
		bodyBytes = payload[idx+2:]
	} else if idx := bytes.Index(payload, []byte("\r\n\r\n")); idx != -1 {
		headerBytes = payload[:idx]
		bodyBytes = payload[idx+4:]
	} else {
		// Commit with no body
		headerBytes = payload
	}

	// Process header lines (handling multiline continuation headers starting with space)
	lines := strings.Split(string(headerBytes), "\n")
	var unfoldedLines []string
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if len(unfoldedLines) > 0 {
				unfoldedLines[len(unfoldedLines)-1] += "\n" + strings.TrimPrefix(line, " ")
			}
		} else if line != "" {
			unfoldedLines = append(unfoldedLines, line)
		}
	}

	for _, line := range unfoldedLines {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		key, val := parts[0], parts[1]

		switch key {
		case "tree":
			meta.TreeOID = strings.TrimSpace(val)
		case "parent":
			parentSHA := strings.TrimSpace(val)
			if parentSHA != "" {
				meta.Parents = append(meta.Parents, parentSHA)
			}
		case "author":
			author, name, email, dt, err := parseIdentity(val)
			if err == nil {
				meta.Author = author
				meta.AuthorName = name
				meta.AuthorEmail = email
				meta.AuthorDate = dt
			}
		case "committer":
			committer, name, email, dt, err := parseIdentity(val)
			if err == nil {
				meta.Committer = committer
				meta.CommitterName = name
				meta.CommitterEmail = email
				meta.CommitterDate = dt
			}
		}
	}

	if meta.TreeOID == "" {
		return nil, fmt.Errorf("%w: missing tree OID in commit %s", ErrInvalidCommit, sha)
	}

	// Commit Date defaults to AuthorDate, fallback to CommitterDate
	if !meta.AuthorDate.IsZero() {
		meta.Date = meta.AuthorDate
	} else {
		meta.Date = meta.CommitterDate
	}

	meta.Message = string(bodyBytes)

	// Extract Summary (first non-empty line of body)
	bodyLines := strings.Split(meta.Message, "\n")
	for _, bl := range bodyLines {
		trimmed := strings.TrimSpace(bl)
		if trimmed != "" {
			meta.Summary = trimmed
			break
		}
	}

	return meta, nil
}

// parseIdentity parses "Name <email> timestamp tz"
func parseIdentity(raw string) (fullIdent, name, email string, t time.Time, err error) {
	raw = strings.TrimSpace(raw)

	emailStart := strings.IndexByte(raw, '<')
	emailEnd := strings.IndexByte(raw, '>')

	if emailStart == -1 || emailEnd == -1 || emailEnd <= emailStart {
		// Fallback for malformed identity
		return raw, raw, "", time.Time{}, nil
	}

	name = strings.TrimSpace(raw[:emailStart])
	email = raw[emailStart+1 : emailEnd]
	fullIdent = fmt.Sprintf("%s <%s>", name, email)

	rest := strings.TrimSpace(raw[emailEnd+1:])
	tokens := strings.Fields(rest)
	if len(tokens) >= 1 {
		sec, parseSecErr := strconv.ParseInt(tokens[0], 10, 64)
		if parseSecErr == nil {
			var loc *time.Location = time.UTC
			if len(tokens) >= 2 {
				loc = parseGitTimezone(tokens[1])
			}
			t = time.Unix(sec, 0).In(loc)
		}
	}

	return fullIdent, name, email, t, nil
}

// parseGitTimezone parses timezone format like "+0200", "-0700", or "+0000"
func parseGitTimezone(tz string) *time.Location {
	if len(tz) != 5 || (tz[0] != '+' && tz[0] != '-') {
		return time.UTC
	}

	hours, err1 := strconv.Atoi(tz[1:3])
	mins, err2 := strconv.Atoi(tz[3:5])
	if err1 != nil || err2 != nil {
		return time.UTC
	}

	offsetSec := (hours*3600 + mins*60)
	if tz[0] == '-' {
		offsetSec = -offsetSec
	}

	return time.FixedZone(tz, offsetSec)
}
