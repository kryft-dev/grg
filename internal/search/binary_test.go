package search

import (
	"bytes"
	"testing"
)

func TestIsBinary(t *testing.T) {
	// Plain text
	plain := []byte("This is a regular utf-8 text file.\nWith multiple lines of text.\n")
	if IsBinary(plain) {
		t.Errorf("plain text should not be detected as binary")
	}

	// Binary with null byte near beginning
	binaryEarly := []byte("Header\x00data\x01\x02")
	if !IsBinary(binaryEarly) {
		t.Errorf("expected binary data to be detected")
	}

	// Null byte within first 8000 bytes
	largeData := bytes.Repeat([]byte("a"), 7999)
	largeData = append(largeData, 0)
	largeData = append(largeData, []byte("remainder")...)
	if !IsBinary(largeData) {
		t.Errorf("expected null byte at index 7999 to be detected as binary")
	}

	// Null byte after 8000 bytes
	textThenNull := bytes.Repeat([]byte("a"), 8000)
	textThenNull = append(textThenNull, 0)
	if IsBinary(textThenNull) {
		t.Errorf("null byte beyond 8000 bytes limit should not trigger binary detection")
	}
}
