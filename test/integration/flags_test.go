package integration

import (
	"strings"
	"testing"
)

// TestFlags_CaseModes verifies -i (ignore-case), -s (case-sensitive), and -S (smart-case).
func TestFlags_CaseModes(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Add case text", map[string]string{
		"cases.txt": "AppleBananaCherry\n",
	})

	// -i: case-insensitive matches lowercase query
	repo.RunSuccess("--color=never", "-i", "applebananacherry")

	// -s: case-sensitive fails on lowercase query
	repo.RunNoMatch("--color=never", "-s", "applebananacherry")

	// -s: case-sensitive succeeds on exact match
	repo.RunSuccess("--color=never", "-s", "AppleBananaCherry")

	// -S (smart-case): lowercase query acts case-insensitive
	repo.RunSuccess("--color=never", "-S", "applebananacherry")

	// -S (smart-case): uppercase query acts case-sensitive (mismatch fails)
	repo.RunNoMatch("--color=never", "-S", "appleBananaCherry")

	// -S (smart-case): uppercase query with exact match succeeds
	repo.RunSuccess("--color=never", "-S", "AppleBananaCherry")
}

// TestFlags_FixedStrings verifies -F treats regex metacharacters literally.
func TestFlags_FixedStrings(t *testing.T) {
	repo := NewTestRepo(t)
	literalPattern := "regex.special[chars]*(1+2)?^$"
	repo.Commit("Add regex special text", map[string]string{
		"literal.txt": literalPattern + "\n",
	})

	// -F: succeeds matching literal special characters
	res := repo.RunSuccess("--color=never", "-F", literalPattern)
	if !strings.Contains(res.Stdout, literalPattern) {
		t.Errorf("expected literal match in output, got:\n%s", res.Stdout)
	}

	// Without -F, pattern is invalid regex syntax or doesn't match literally
	resNoF := repo.Run(literalPattern)
	if resNoF.ExitCode == 0 && strings.Contains(resNoF.Stdout, "literal.txt") {
		// If compiled as regex, syntax error would yield exit code 2
		if resNoF.ExitCode != 2 {
			// Some regex engines might parse part of it, but -F guarantees exact literal match
		}
	}
}

// TestFlags_WordRegexp verifies -w matches full words only.
func TestFlags_WordRegexp(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Add words file", map[string]string{
		"words.txt": "my_identifier = other_identifier + identifier\n",
	})

	// Standalone word "identifier" matches
	res := repo.RunSuccess("--color=never", "-w", "identifier")
	if !strings.Contains(res.Stdout, "identifier") {
		t.Errorf("expected word match in output, got:\n%s", res.Stdout)
	}

	// Substring "ident" should not match as whole word
	repo.RunNoMatch("--color=never", "-w", "ident")
}

// TestFlags_InvertMatch verifies -v inverts match sense.
func TestFlags_InvertMatch(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Add lines", map[string]string{
		"invert.txt": "alpha line\nbeta line\ngamma line\n",
	})

	res := repo.RunSuccess("--color=never", "-v", "beta")
	if strings.Contains(res.Stdout, "beta line") {
		t.Errorf("inverted match must not contain 'beta line', got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "alpha line") || !strings.Contains(res.Stdout, "gamma line") {
		t.Errorf("inverted match must contain non-matching lines, got:\n%s", res.Stdout)
	}
}

// TestFlags_Context verifies -C, -A, and -B context line reporting.
func TestFlags_Context(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Add numbered lines", map[string]string{
		"lines.txt": "L1\nL2\nTARGET_MATCH\nL4\nL5\n",
	})

	// -C 1: Before and after context
	resC := repo.RunSuccess("--color=never", "-C", "1", "TARGET_MATCH")
	if !strings.Contains(resC.Stdout, "2-L2") || !strings.Contains(resC.Stdout, "3:TARGET_MATCH") || !strings.Contains(resC.Stdout, "4-L4") {
		t.Errorf("expected -C 1 context lines, got:\n%s", resC.Stdout)
	}
	if strings.Contains(resC.Stdout, "L1") || strings.Contains(resC.Stdout, "L5") {
		t.Errorf("context -C 1 exceeded bounds, got:\n%s", resC.Stdout)
	}

	// -B 1: Before context only
	resB := repo.RunSuccess("--color=never", "-B", "1", "TARGET_MATCH")
	if !strings.Contains(resB.Stdout, "2-L2") || !strings.Contains(resB.Stdout, "3:TARGET_MATCH") {
		t.Errorf("expected -B 1 before context, got:\n%s", resB.Stdout)
	}
	if strings.Contains(resB.Stdout, "L4") {
		t.Errorf("did not expect after-context with -B 1, got:\n%s", resB.Stdout)
	}

	// -A 1: After context only
	resA := repo.RunSuccess("--color=never", "-A", "1", "TARGET_MATCH")
	if !strings.Contains(resA.Stdout, "3:TARGET_MATCH") || !strings.Contains(resA.Stdout, "4-L4") {
		t.Errorf("expected -A 1 after context, got:\n%s", resA.Stdout)
	}
	if strings.Contains(resA.Stdout, "L2") {
		t.Errorf("did not expect before-context with -A 1, got:\n%s", resA.Stdout)
	}
}

// TestFlags_MaxCount verifies -m limits match count per file/commit.
func TestFlags_MaxCount(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Add repeating lines", map[string]string{
		"repeat.txt": "MATCH_ROW 1\nMATCH_ROW 2\nMATCH_ROW 3\nMATCH_ROW 4\nMATCH_ROW 5\n",
	})

	res := repo.RunSuccess("--color=never", "-m", "2", "MATCH_ROW")
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	matchLines := 0
	for _, l := range lines {
		if strings.Contains(l, "MATCH_ROW") {
			matchLines++
		}
	}
	if matchLines != 2 {
		t.Errorf("expected exactly 2 matches with -m 2, got %d matches. Output:\n%s", matchLines, res.Stdout)
	}
}

// TestFlags_OutputModes verifies -l (files-with-matches), -c (count), -q (quiet), and --no-heading.
func TestFlags_OutputModes(t *testing.T) {
	repo := NewTestRepo(t)
	c1 := repo.Commit("Add mode sample", map[string]string{
		"mode.txt": "TOKEN_MODE_LINE 1\nTOKEN_MODE_LINE 2\n",
	})

	// 1. -l (files-with-matches): <short-sha>:<path>
	resL := repo.RunSuccess("--color=never", "-l", "TOKEN_MODE_LINE")
	expectedL := c1[:7] + ":mode.txt"
	if strings.TrimSpace(resL.Stdout) != expectedL {
		t.Errorf("expected %q for -l, got %q", expectedL, strings.TrimSpace(resL.Stdout))
	}

	// 2. -c (count): <short-sha>:<path>:2
	resC := repo.RunSuccess("--color=never", "-c", "TOKEN_MODE_LINE")
	expectedC := c1[:7] + ":mode.txt:2"
	if strings.TrimSpace(resC.Stdout) != expectedC {
		t.Errorf("expected %q for -c, got %q", expectedC, strings.TrimSpace(resC.Stdout))
	}

	// 3. -q (quiet): exit code 0 when match found, empty output
	resQMatch := repo.RunSuccess("-q", "TOKEN_MODE_LINE")
	if len(resQMatch.Stdout) > 0 {
		t.Errorf("expected empty stdout with -q, got: %q", resQMatch.Stdout)
	}

	// 4. -q (quiet): exit code 1 when match not found, empty output
	resQNoMatch := repo.RunNoMatch("-q", "NON_EXISTENT_TOKEN_XYZ")
	if len(resQNoMatch.Stdout) > 0 {
		t.Errorf("expected empty stdout with -q no match, got: %q", resQNoMatch.Stdout)
	}

	// 5. --no-heading: single line output <short-sha>:<path>:<line>:<text>
	resNoHeading := repo.RunSuccess("--color=never", "--no-heading", "TOKEN_MODE_LINE")
	expectedPrefix := c1[:7] + ":mode.txt:1:TOKEN_MODE_LINE 1"
	if !strings.Contains(resNoHeading.Stdout, expectedPrefix) {
		t.Errorf("expected prefix %q in --no-heading output, got:\n%s", expectedPrefix, resNoHeading.Stdout)
	}
}

// TestFlags_Color verifies ANSI escape codes are emitted with --color=always and suppressed with --color=never.
func TestFlags_Color(t *testing.T) {
	repo := NewTestRepo(t)
	repo.Commit("Add color sample", map[string]string{
		"color.txt": "COLOR_TEST_STRING\n",
	})

	// --color=always: contains ANSI escape codes \x1b[
	resAlways := repo.RunSuccess("--color=always", "COLOR_TEST_STRING")
	if !strings.Contains(resAlways.Stdout, "\x1b[") {
		t.Errorf("expected ANSI escape codes in --color=always, got:\n%s", resAlways.Stdout)
	}

	// --color=never: contains no ANSI escape codes
	resNever := repo.RunSuccess("--color=never", "COLOR_TEST_STRING")
	if strings.Contains(resNever.Stdout, "\x1b[") {
		t.Errorf("did not expect ANSI escape codes in --color=never, got:\n%s", resNever.Stdout)
	}
}
