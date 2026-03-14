package truncate

import (
	"fmt"
	"strings"
)

// SmartTruncate applies 60% head / 40% tail truncation when text exceeds maxBytes.
// Preserves the end of output where errors and return values typically appear.
// Returns the (possibly truncated) text and whether truncation was applied.
func SmartTruncate(text string, maxBytes int) (string, bool) {
	if len(text) <= maxBytes || maxBytes <= 0 {
		return text, false
	}

	lines := strings.Split(text, "\n")
	if len(lines) <= 3 {
		// Too few lines to truncate meaningfully
		if len(text) > maxBytes {
			return text[:maxBytes], true
		}
		return text, false
	}

	// Calculate line budget
	totalLines := len(lines)
	headLines := int(float64(totalLines) * 0.6)
	tailLines := totalLines - headLines

	// Binary search to fit within maxBytes
	for headLines+tailLines > 2 {
		head := strings.Join(lines[:headLines], "\n")
		tail := strings.Join(lines[totalLines-tailLines:], "\n")
		omitted := totalLines - headLines - tailLines
		marker := fmt.Sprintf("\n\n... [%d lines omitted] ...\n\n", omitted)

		result := head + marker + tail
		if len(result) <= maxBytes {
			return result, true
		}

		// Reduce proportionally
		if headLines > tailLines {
			headLines--
		} else {
			tailLines--
		}
	}

	// Fallback: just raw byte truncation
	return text[:maxBytes], true
}
