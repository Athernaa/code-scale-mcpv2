package contextpack

import "strings"

const omissionMarker = "// ... context omitted by code-scale ..."

func sliceToTokens(source string, limit int, counter TokenCounter) (string, bool) {
	if limit <= 0 {
		return "", source != ""
	}
	if counter.Count(source) <= limit {
		return source, false
	}
	lines := strings.Split(source, "\n")
	best := ""
	low, high := 0, len(lines)
	for low <= high {
		n := (low + high) / 2
		head := (n*3 + 4) / 5
		tail := n - head
		parts := append([]string(nil), lines[:minInt(head, len(lines))]...)
		parts = append(parts, omissionMarker)
		if tail > 0 && tail < len(lines) {
			parts = append(parts, lines[len(lines)-tail:]...)
		}
		candidate := strings.Join(parts, "\n")
		if counter.Count(candidate) <= limit {
			best = candidate
			low = n + 1
		} else {
			high = n - 1
		}
	}
	if best != "" {
		return best, true
	}
	runes := []rune(source)
	low, high = 0, len(runes)
	for low <= high {
		n := (low + high) / 2
		candidate := string(runes[:n])
		if n < len(runes) {
			candidate += "\n" + omissionMarker
		}
		if counter.Count(candidate) <= limit {
			best = candidate
			low = n + 1
		} else {
			high = n - 1
		}
	}
	return best, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
