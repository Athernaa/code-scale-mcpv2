package snippet

import "strings"

// Snippet represents a contextual code snippet around a match.
type Snippet struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Text      string `json:"text"`
	MatchLine int    `json:"match_line"`
}

type window struct {
	start     int
	end       int
	matchLine int
}

type mergedWindow struct {
	start     int
	end       int
	matchLine int
}

// ExtractSnippets finds all lines matching query and builds context windows around them.
// contextLines controls how many lines before/after each match to include.
// Overlapping windows are merged into consolidated snippets.
func ExtractSnippets(content string, filePath string, query string, contextLines int, maxSnippets int) []Snippet {
	if contextLines <= 0 {
		contextLines = 3
	}
	if maxSnippets <= 0 {
		maxSnippets = 5
	}

	lines := strings.Split(content, "\n")
	queryLower := strings.ToLower(query)

	// Find matching line indices
	var matchLines []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), queryLower) {
			matchLines = append(matchLines, i)
		}
	}

	if len(matchLines) == 0 {
		return nil
	}

	// Build windows around matches
	var windows []window
	for _, ml := range matchLines {
		start := ml - contextLines
		if start < 0 {
			start = 0
		}
		end := ml + contextLines + 1
		if end > len(lines) {
			end = len(lines)
		}
		windows = append(windows, window{start, end, ml})
	}

	// Merge overlapping windows
	merged := mergeWindows(windows)

	// Build snippets
	var snippets []Snippet
	for _, w := range merged {
		if len(snippets) >= maxSnippets {
			break
		}
		text := strings.Join(lines[w.start:w.end], "\n")
		snippets = append(snippets, Snippet{
			File:      filePath,
			StartLine: w.start + 1, // 1-indexed
			EndLine:   w.end,       // 1-indexed inclusive
			Text:      text,
			MatchLine: w.matchLine + 1, // 1-indexed
		})
	}

	return snippets
}

func mergeWindows(windows []window) []mergedWindow {
	if len(windows) == 0 {
		return nil
	}

	var result []mergedWindow
	current := mergedWindow{
		start:     windows[0].start,
		end:       windows[0].end,
		matchLine: windows[0].matchLine,
	}

	for i := 1; i < len(windows); i++ {
		w := windows[i]
		if w.start <= current.end {
			// Overlapping — extend
			if w.end > current.end {
				current.end = w.end
			}
		} else {
			result = append(result, current)
			current = mergedWindow(w)
		}
	}
	result = append(result, current)

	return result
}
