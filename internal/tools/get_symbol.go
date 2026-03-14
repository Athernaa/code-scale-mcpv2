package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/syphon1c/code-scale-mcp/internal/parser"
	"github.com/syphon1c/code-scale-mcp/internal/storage"
)

type GetSymbolArgs struct {
	Repo         string `json:"repo" jsonschema:"Repository name"`
	SymbolID     string `json:"symbol_id" jsonschema:"Symbol ID (e.g. src/main.py::authenticate#function)"`
	Verify       bool   `json:"verify,omitempty" jsonschema:"Verify content hash for drift detection"`
	ContextLines int    `json:"context_lines,omitempty" jsonschema:"Lines of context before/after symbol"`
	MaxLength    int    `json:"max_length,omitempty" jsonschema:"Max bytes for source (0=unlimited). Applies 60/40 head/tail truncation."`
}

func GetSymbolHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetSymbolArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		sym, err := deps.Store.GetSymbolByID(repoID, args.SymbolID)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		source, err := deps.Store.GetSymbolContent(repoID, args.SymbolID)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		// Context lines
		var contextBefore, contextAfter string
		if args.ContextLines > 0 {
			fileContent, err := deps.Store.GetFileContent(repoID, sym.File)
			if err == nil {
				lines := strings.Split(string(fileContent), "\n")
				startLine := sym.Line - 1 // 0-indexed
				endLine := sym.EndLine    // exclusive

				// Before context
				beforeStart := startLine - args.ContextLines
				if beforeStart < 0 {
					beforeStart = 0
				}
				if beforeStart < startLine {
					contextBefore = strings.Join(lines[beforeStart:startLine], "\n")
				}

				// After context
				afterEnd := endLine + args.ContextLines
				if afterEnd > len(lines) {
					afterEnd = len(lines)
				}
				if endLine < afterEnd {
					contextAfter = strings.Join(lines[endLine:afterEnd], "\n")
				}
			}
		}

		// Smart truncation
		var truncated bool
		if args.MaxLength > 0 {
			source, truncated = smartTruncateSource(source, args.MaxLength)
		}

		// Verify content hash
		var verified *bool
		if args.Verify {
			currentHash := parser.ComputeContentHash([]byte(source))
			v := currentHash == sym.ContentHash
			verified = &v
		}

		saved, total := deps.addSavings(int64(len(source)*10), int64(len(source)))

		result := map[string]any{
			"id":         sym.ID,
			"kind":       sym.Kind,
			"name":       sym.Name,
			"file":       sym.File,
			"line":       sym.Line,
			"end_line":   sym.EndLine,
			"signature":  sym.Signature,
			"decorators": sym.Decorators,
			"docstring":  sym.Docstring,
			"source":     source,
		}
		if contextBefore != "" {
			result["context_before"] = contextBefore
		}
		if contextAfter != "" {
			result["context_after"] = contextAfter
		}
		if verified != nil {
			result["content_verified"] = *verified
		}

		result["_meta"] = Meta{
			TimingMs:    t.elapsedMs(),
			Repo:        args.Repo,
			Truncated:   truncated,
			TokensSaved: saved,
			TotalSaved:  total,
			CostAvoided: storage.CostAvoided(saved),
			TotalCost:   storage.CostAvoided(total),
		}

		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

// GetSymbolsArgs is for batch symbol retrieval.
type GetSymbolsArgs struct {
	Repo      string   `json:"repo" jsonschema:"Repository name"`
	SymbolIDs []string `json:"symbol_ids" jsonschema:"List of symbol IDs to retrieve"`
}

func GetSymbolsHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetSymbolsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolsArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		if len(args.SymbolIDs) > 100 {
			r, _ := errorResult("too many symbol IDs (max 100)")
			return r, nil, nil
		}

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		var symbols []map[string]any
		var errors []string
		var totalBytes int64

		for _, symID := range args.SymbolIDs {
			sym, err := deps.Store.GetSymbolByID(repoID, symID)
			if err != nil {
				errors = append(errors, symID)
				continue
			}

			source, err := deps.Store.GetSymbolContent(repoID, symID)
			if err != nil {
				errors = append(errors, symID)
				continue
			}
			totalBytes += int64(len(source))

			symbols = append(symbols, map[string]any{
				"id":        sym.ID,
				"kind":      sym.Kind,
				"name":      sym.Name,
				"file":      sym.File,
				"line":      sym.Line,
				"signature": sym.Signature,
				"source":    source,
			})
		}

		saved, total := deps.addSavings(totalBytes*10, totalBytes)

		result := map[string]any{
			"symbols": symbols,
			"errors":  errors,
			"_meta": Meta{
				TimingMs:    t.elapsedMs(),
				Repo:        args.Repo,
				SymbolCount: len(symbols),
				TokensSaved: saved,
				TotalSaved:  total,
				CostAvoided: storage.CostAvoided(saved),
				TotalCost:   storage.CostAvoided(total),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
