package summarizer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// SummarizeSymbols applies 3-tier summarization to a list of symbols.
// Tier 1: Docstring extraction (free)
// Tier 2: AI batch summarization (Anthropic Haiku / Gemini Flash)
// Tier 3: Signature fallback
func SummarizeSymbols(symbols []parser.Symbol, useAI bool) {
	// Tier 1: Extract from docstrings
	for i := range symbols {
		if symbols[i].Docstring != "" {
			symbols[i].Summary = extractSummaryFromDocstring(symbols[i].Docstring)
		}
	}

	// Tier 2: AI summarization for symbols without summaries
	if useAI {
		var needSummary []*parser.Symbol
		for i := range symbols {
			if symbols[i].Summary == "" {
				needSummary = append(needSummary, &symbols[i])
			}
		}
		if len(needSummary) > 0 {
			batchSummarize(needSummary)
		}
	}

	// Tier 3: Signature fallback for remaining
	for i := range symbols {
		if symbols[i].Summary == "" {
			symbols[i].Summary = signatureFallback(symbols[i])
		}
	}
}

// extractSummaryFromDocstring extracts first sentence from docstring.
func extractSummaryFromDocstring(docstring string) string {
	line := strings.TrimSpace(strings.SplitN(docstring, "\n", 2)[0])
	// Truncate at first period
	if idx := strings.Index(line, "."); idx > 0 && idx < len(line)-1 {
		line = line[:idx+1]
	}
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

// signatureFallback generates a summary from the symbol signature.
func signatureFallback(sym parser.Symbol) string {
	switch sym.Kind {
	case parser.KindClass:
		return "Class " + sym.Name
	case parser.KindConstant:
		return "Constant " + sym.Name
	case parser.KindType:
		return "Type " + sym.Name
	default:
		if sym.Signature != "" {
			sig := sym.Signature
			if len(sig) > 80 {
				sig = sig[:80] + "..."
			}
			return sig
		}
		return sym.Kind + " " + sym.Name
	}
}

// batchSummarize calls AI APIs to summarize symbols in batches.
func batchSummarize(symbols []*parser.Symbol) {
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	geminiKey := os.Getenv("GOOGLE_API_KEY")

	batchSize := 10
	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]

		var summaries []string
		var err error

		if anthropicKey != "" {
			summaries, err = summarizeWithAnthropic(batch, anthropicKey)
		} else if geminiKey != "" {
			summaries, err = summarizeWithGemini(batch, geminiKey)
		}

		if err != nil || len(summaries) != len(batch) {
			continue
		}

		for j, summary := range summaries {
			batch[j].Summary = summary
		}
	}
}

// buildPrompt creates the summarization prompt for a batch.
func buildPrompt(symbols []*parser.Symbol) string {
	var b strings.Builder
	b.WriteString("Summarize each code symbol in ONE short sentence (max 15 words).\n")
	b.WriteString("Focus on what it does, not how.\n\n")

	for i, sym := range symbols {
		fmt.Fprintf(&b, "%d. %s: %s\n", i+1, sym.Kind, sym.Signature)
	}

	b.WriteString("\nOutput format: NUMBER. SUMMARY")
	return b.String()
}

// summarizeWithAnthropic calls Claude Haiku for summaries.
func summarizeWithAnthropic(symbols []*parser.Symbol, apiKey string) ([]string, error) {
	prompt := buildPrompt(symbols)

	body := map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 500,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic API error: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return parseSummaryResponse(respBody, len(symbols))
}

// summarizeWithGemini calls Gemini Flash for summaries.
func summarizeWithGemini(symbols []*parser.Symbol, apiKey string) ([]string, error) {
	prompt := buildPrompt(symbols)

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gemini API error: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return parseGeminiResponse(respBody, len(symbols))
}

// parseSummaryResponse parses Anthropic API response.
func parseSummaryResponse(body []byte, count int) ([]string, error) {
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return parseNumberedLines(result.Content[0].Text, count), nil
}

// parseGeminiResponse parses Gemini API response.
func parseGeminiResponse(body []byte, count int) ([]string, error) {
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return parseNumberedLines(result.Candidates[0].Content.Parts[0].Text, count), nil
}

// parseNumberedLines parses "1. summary\n2. summary" format.
func parseNumberedLines(text string, count int) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	summaries := make([]string, count)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) < 3 {
			continue
		}
		// Find number prefix
		dotIdx := strings.Index(line, ".")
		if dotIdx <= 0 || dotIdx > 3 {
			continue
		}
		numStr := strings.TrimSpace(line[:dotIdx])
		var num int
		for _, c := range numStr {
			if c >= '0' && c <= '9' {
				num = num*10 + int(c-'0')
			}
		}
		if num >= 1 && num <= count {
			summary := strings.TrimSpace(line[dotIdx+1:])
			if len(summary) > 120 {
				summary = summary[:120]
			}
			summaries[num-1] = summary
		}
	}
	return summaries
}
