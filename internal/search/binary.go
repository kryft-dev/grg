package search

import (
	"bytes"
)

const binaryInspectionLimit = 8000

// IsBinary inspects the first 8000 bytes of data for null bytes to determine if the content is binary.
func IsBinary(data []byte) bool {
	limit := len(data)
	if limit > binaryInspectionLimit {
		limit = binaryInspectionLimit
	}
	return bytes.IndexByte(data[:limit], 0) != -1
}
