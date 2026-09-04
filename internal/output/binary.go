package output

import (
	"fmt"
	"io"
)

// FormatBinaryNotice formats a binary match notice line.
// Standard format: "Binary file <path> matches in <commit-short>"
func FormatBinaryNotice(path, shortSHA string, c *Colorizer) string {
	if c == nil || !c.Enabled() {
		return fmt.Sprintf("Binary file %s matches in %s", path, shortSHA)
	}
	return fmt.Sprintf("Binary file %s matches in %s", c.Path(path), c.Commit(shortSHA))
}

// WriteBinaryNotice writes a formatted binary match notice to w.
func WriteBinaryNotice(w io.Writer, path, shortSHA string, c *Colorizer) error {
	_, err := fmt.Fprintln(w, FormatBinaryNotice(path, shortSHA, c))
	return err
}
