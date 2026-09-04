package integration

import (
	"strings"
	"testing"
)

// TestBinaryBlob_DefaultNotice verifies binary files produce a binary notice instead of dumping raw bytes.
func TestBinaryBlob_DefaultNotice(t *testing.T) {
	repo := NewTestRepo(t)

	// Binary payload containing null bytes and an ASCII target token
	binData := []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01, 0x02}
	binData = append(binData, []byte("BINARY_PAYLOAD_TOKEN_42")...)
	binData = append(binData, 0x00, 0x03, 0x04)

	repo.WriteBinaryFile("assets/firmware.bin", binData)
	repo.Git("add", ".")
	repo.Git("commit", "-m", "Add binary firmware")
	c1 := repo.Git("rev-parse", "HEAD")

	// 1. Searching for the binary token outputs the binary notice
	res := repo.RunSuccess("--color=never", "BINARY_PAYLOAD_TOKEN_42")
	expectedNotice := "Binary file assets/firmware.bin matches in " + c1[:7]
	if !strings.Contains(res.Stdout, expectedNotice) {
		t.Errorf("expected binary match notice %q, got:\n%s", expectedNotice, res.Stdout)
	}

	// 2. Raw binary bytes must not be dumped
	if strings.Contains(res.Stdout, "\x7fELF") {
		t.Errorf("binary file content leaked into stdout:\n%s", res.Stdout)
	}

	// 3. Searching for a non-existent pattern in the binary file must return exit code 1
	repo.RunNoMatch("ABSENT_BINARY_PATTERN_999")
}

// TestBinaryBlob_TextOverride verifies -a/--text treats binary files as text and prints matches.
func TestBinaryBlob_TextOverride(t *testing.T) {
	repo := NewTestRepo(t)

	binData := []byte("first line with null \x00 byte\nBINARY_TEXT_SEARCH_TARGET middle\nfinal \x00 line\n")
	repo.WriteBinaryFile("data.bin", binData)
	repo.Git("add", ".")
	repo.Git("commit", "-m", "Add data with null bytes")

	// 1. Without -a: outputs binary notice
	resDefault := repo.RunSuccess("--color=never", "BINARY_TEXT_SEARCH_TARGET")
	if !strings.Contains(resDefault.Stdout, "Binary file data.bin matches") {
		t.Errorf("expected binary notice by default, got:\n%s", resDefault.Stdout)
	}

	// 2. With -a: searches as text and outputs the matching line
	resText := repo.RunSuccess("--color=never", "-a", "BINARY_TEXT_SEARCH_TARGET")
	if strings.Contains(resText.Stdout, "Binary file") {
		t.Errorf("did not expect binary notice with -a, got:\n%s", resText.Stdout)
	}
	if !strings.Contains(resText.Stdout, "2:BINARY_TEXT_SEARCH_TARGET middle") {
		t.Errorf("expected line 2 text match with -a, got:\n%s", resText.Stdout)
	}

	// 3. With --text long flag
	resLongText := repo.RunSuccess("--color=never", "--text", "BINARY_TEXT_SEARCH_TARGET")
	if !strings.Contains(resLongText.Stdout, "2:BINARY_TEXT_SEARCH_TARGET middle") {
		t.Errorf("expected line 2 text match with --text, got:\n%s", resLongText.Stdout)
	}
}

// TestBinaryBlob_OutputModes verifies -l, -c, and -q handling on binary files.
func TestBinaryBlob_OutputModes(t *testing.T) {
	repo := NewTestRepo(t)

	binData := []byte("header \x00 BINARY_MODE_MARKER \x00 footer")
	repo.WriteBinaryFile("blob.bin", binData)
	repo.Git("add", ".")
	repo.Git("commit", "-m", "Add binary blob")
	c1 := repo.Git("rev-parse", "HEAD")

	// -l: <commit>:<path>
	resL := repo.RunSuccess("--color=never", "-l", "BINARY_MODE_MARKER")
	expectedL := c1[:7] + ":blob.bin"
	if strings.TrimSpace(resL.Stdout) != expectedL {
		t.Errorf("expected %q for -l on binary file, got %q", expectedL, strings.TrimSpace(resL.Stdout))
	}

	// -c: <commit>:<path>:1
	resC := repo.RunSuccess("--color=never", "-c", "BINARY_MODE_MARKER")
	expectedC := c1[:7] + ":blob.bin:1"
	if strings.TrimSpace(resC.Stdout) != expectedC {
		t.Errorf("expected %q for -c on binary file, got %q", expectedC, strings.TrimSpace(resC.Stdout))
	}

	// -q: exit code 0
	resQ := repo.RunSuccess("-q", "BINARY_MODE_MARKER")
	if len(resQ.Stdout) > 0 {
		t.Errorf("expected empty stdout with -q, got: %q", resQ.Stdout)
	}
}
