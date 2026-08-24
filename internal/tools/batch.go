package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// BatchOp represents a single operation in a batch request.
type BatchOp struct {
	Tool string         `json:"tool" jsonschema:"Tool name (get_symbol, get_symbols, search_symbols, search_text, get_file_outline, get_file_tree, get_repo_outline)"`
	Args map[string]any `json:"args" jsonschema:"Arguments for the tool"`
}

// BatchArgs is the input for the batch_execute tool.
type BatchArgs struct {
	Operations []BatchOp `json:"operations" jsonschema:"List of operations to execute (max 10)"`
}

// batchResult holds the result of a single batch operation.
type batchResult struct {
	Tool   string `json:"tool"`
	Index  int    `json:"index"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// BatchExecuteHandler creates a handler that dispatches multiple operations in parallel.
func BatchExecuteHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, BatchArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args BatchArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		if len(args.Operations) == 0 {
			r, _ := errorResult("no operations provided")
			return r, nil, nil
		}
		if len(args.Operations) > 10 {
			r, _ := errorResult("too many operations (max 10)")
			return r, nil, nil
		}

		results := make([]batchResult, len(args.Operations))
		var wg sync.WaitGroup

		for i, op := range args.Operations {
			wg.Add(1)
			go func(idx int, op BatchOp) {
				defer wg.Done()
				result, err := executeBatchOp(deps, op)
				if err != nil {
					results[idx] = batchResult{Tool: op.Tool, Index: idx, Error: err.Error()}
				} else {
					results[idx] = batchResult{Tool: op.Tool, Index: idx, Result: result}
				}
			}(i, op)
		}

		wg.Wait()

		var errorCount int
		for _, r := range results {
			if r.Error != "" {
				errorCount++
			}
		}

		result := map[string]any{
			"operation_count": len(args.Operations),
			"error_count":     errorCount,
			"results":         results,
			"_meta": Meta{
				TimingMs: t.elapsedMs(),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

// executeBatchOp dispatches a single operation to the appropriate handler logic.
func executeBatchOp(deps *Deps, op BatchOp) (any, error) {
	// Marshal/unmarshal args to typed structs
	argsJSON, err := json.Marshal(op.Args)
	if err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	switch op.Tool {
	case "get_symbol":
		var a GetSymbolArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid get_symbol args: %w", err)
		}
		return execGetSymbol(deps, a)

	case "get_symbols":
		var a GetSymbolsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid get_symbols args: %w", err)
		}
		return execGetSymbols(deps, a)

	case "search_symbols":
		var a SearchSymbolsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid search_symbols args: %w", err)
		}
		return execSearchSymbols(deps, a)

	case "search_text":
		var a SearchTextArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid search_text args: %w", err)
		}
		return execSearchText(deps, a)

	case "get_file_outline":
		var a GetFileOutlineArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid get_file_outline args: %w", err)
		}
		return execGetFileOutline(deps, a)

	case "get_file_tree":
		var a GetFileTreeArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid get_file_tree args: %w", err)
		}
		return execGetFileTree(deps, a)

	case "get_repo_outline":
		var a GetRepoOutlineArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid get_repo_outline args: %w", err)
		}
		return execGetRepoOutline(deps, a)

	default:
		return nil, fmt.Errorf("unsupported tool %q (allowed: get_symbol, get_symbols, search_symbols, search_text, get_file_outline, get_file_tree, get_repo_outline)", op.Tool)
	}
}

func execGetSymbol(deps *Deps, args GetSymbolArgs) (any, error) {
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	sym, err := deps.Store.GetSymbolByID(repoID, args.SymbolID)
	if err != nil {
		return nil, err
	}
	source, err := deps.Store.GetSymbolContent(repoID, args.SymbolID)
	if err != nil {
		return nil, err
	}
	completeSource := source
	var contextBefore, contextAfter string
	if args.ContextLines > 0 {
		if fileContent, contextErr := deps.Store.GetFileContent(repoID, sym.File); contextErr == nil {
			lines := strings.Split(string(fileContent), "\n")
			startLine, endLine := sym.Line-1, sym.EndLine
			beforeStart := startLine - args.ContextLines
			if beforeStart < 0 {
				beforeStart = 0
			}
			if beforeStart < startLine {
				contextBefore = strings.Join(lines[beforeStart:startLine], "\n")
			}
			afterEnd := endLine + args.ContextLines
			if afterEnd > len(lines) {
				afterEnd = len(lines)
			}
			if endLine < afterEnd {
				contextAfter = strings.Join(lines[endLine:afterEnd], "\n")
			}
		}
	}
	truncated := false
	if args.MaxLength > 0 {
		source, truncated = smartTruncateSource(source, args.MaxLength)
	}
	result := map[string]any{
		"id": sym.ID, "kind": sym.Kind, "name": sym.Name,
		"file": sym.File, "line": sym.Line, "end_line": sym.EndLine, "signature": sym.Signature,
		"decorators": sym.Decorators, "docstring": sym.Docstring,
		"source": source,
	}
	if contextBefore != "" {
		result["context_before"] = contextBefore
	}
	if contextAfter != "" {
		result["context_after"] = contextAfter
	}
	if args.Verify {
		result["content_verified"] = parser.ComputeContentHash([]byte(completeSource)) == sym.ContentHash
	}
	if truncated {
		result["truncated"] = true
	}
	return result, nil
}

func execGetSymbols(deps *Deps, args GetSymbolsArgs) (any, error) {
	if len(args.SymbolIDs) > 100 {
		return nil, fmt.Errorf("too many symbol IDs (max 100)")
	}
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	limit := args.MaxTotalBytes
	if limit <= 0 {
		limit = 1024 * 1024
	}
	if limit > 8*1024*1024 {
		limit = 8 * 1024 * 1024
	}
	var symbols []map[string]any
	var errors []string
	var totalBytes int
	truncated := false
	for _, symID := range args.SymbolIDs {
		sym, err := deps.Store.GetSymbolByID(repoID, symID)
		if err != nil {
			continue
		}
		source, err := deps.Store.GetSymbolContent(repoID, symID)
		if err != nil {
			continue
		}
		if totalBytes+len(source) > limit {
			truncated = true
			continue
		}
		totalBytes += len(source)
		symbols = append(symbols, map[string]any{
			"id": sym.ID, "kind": sym.Kind, "name": sym.Name,
			"file": sym.File, "line": sym.Line, "signature": sym.Signature,
			"source": source,
		})
	}
	return map[string]any{"symbols": symbols, "errors": errors, "truncated": truncated}, nil
}

func execSearchSymbols(deps *Deps, args SearchSymbolsArgs) (any, error) {
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	maxResults := clampResults(args.MaxResults, 10, 200)
	scored, err := deps.Store.SearchSymbolsWithTier(repoID, args.Query, args.Kind, args.Language, args.FilePattern, maxResults)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, s := range scored {
		results = append(results, map[string]any{
			"id": s.Symbol.ID, "kind": s.Symbol.Kind, "name": s.Symbol.Name,
			"file": s.Symbol.File, "line": s.Symbol.Line, "signature": s.Symbol.Signature,
			"score": s.Score, "match_tier": string(s.Tier),
		})
	}
	return map[string]any{"repo": args.Repo, "query": args.Query, "results": results, "result_count": len(results)}, nil
}

func execSearchText(deps *Deps, args SearchTextArgs) (any, error) {
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	maxResults := clampResults(args.MaxResults, 20, 200)
	contextLines := args.ContextLines
	if contextLines < 0 {
		contextLines = 0
	}
	if contextLines > 10 {
		contextLines = 10
	}
	results, err := deps.Store.SearchText(repoID, args.Query, args.FilePattern, maxResults, contextLines)
	if err != nil {
		return nil, err
	}
	return map[string]any{"repo": args.Repo, "query": args.Query, "results": results, "result_count": len(results)}, nil
}

func execGetFileOutline(deps *Deps, args GetFileOutlineArgs) (any, error) {
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	symbols, err := deps.Store.GetSymbolsByFile(repoID, args.FilePath)
	if err != nil {
		return nil, err
	}
	maxSymbols := args.MaxSymbols
	if maxSymbols <= 0 {
		maxSymbols = 200
	}
	if maxSymbols > 2000 {
		maxSymbols = 2000
	}
	truncated := len(symbols) > maxSymbols
	if truncated {
		symbols = symbols[:maxSymbols]
	}
	language := ""
	if len(symbols) > 0 {
		language = symbols[0].Language
	}
	var results []OutlineSymbol
	if args.Flat {
		for _, node := range parser.FlattenSymbols(symbols) {
			results = append(results, compactOutline(node.Symbol, node.Depth))
		}
	} else {
		for _, node := range parser.BuildSymbolTree(symbols) {
			results = append(results, compactOutlineTree(node, 0))
		}
	}
	return map[string]any{"repo": args.Repo, "file": args.FilePath, "language": language, "symbols": results, "truncated": truncated}, nil
}

func execGetFileTree(deps *Deps, args GetFileTreeArgs) (any, error) {
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	files, err := deps.Store.GetFiles(repoID)
	if err != nil {
		return nil, err
	}
	symCounts, err := deps.Store.GetSymbolCountsByFile(repoID)
	if err != nil {
		return nil, err
	}
	maxDepth := args.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 20
	}
	if maxDepth > 100 {
		maxDepth = 100
	}
	maxEntries := args.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if maxEntries > 10000 {
		maxEntries = 10000
	}
	root := &TreeNode{Type: "dir", Path: "/", Name: "/"}
	truncated := false
	entries := 0
	for _, f := range files {
		if args.PathPrefix != "" && !strings.HasPrefix(f.Path, args.PathPrefix) {
			continue
		}
		if len(strings.Split(filepath.ToSlash(f.Path), "/")) > maxDepth || entries >= maxEntries {
			truncated = true
			continue
		}
		addToTree(root, f.Path, f.Language, symCounts[f.Path])
		entries++
	}
	return map[string]any{"repo": args.Repo, "tree": root.Children, "truncated": truncated}, nil
}

func execGetRepoOutline(deps *Deps, args GetRepoOutlineArgs) (any, error) {
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	info, dirs, kinds, err := deps.Store.GetRepoOutline(repoID)
	if err != nil {
		return nil, err
	}
	maxDepth := args.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 20
	}
	if maxDepth > 100 {
		maxDepth = 100
	}
	maxDirectories := args.MaxDirectories
	if maxDirectories <= 0 {
		maxDirectories = 500
	}
	if maxDirectories > 10000 {
		maxDirectories = 10000
	}
	filtered := make(map[string]int)
	truncated := false
	for path, count := range dirs {
		depth := len(strings.Split(filepath.ToSlash(strings.Trim(path, "/")), "/"))
		if strings.Trim(path, "/") == "" {
			depth = 0
		}
		if depth > maxDepth || len(filtered) >= maxDirectories {
			truncated = true
			continue
		}
		filtered[path] = count
	}
	return map[string]any{
		"repo": info.Repo, "indexed_at": info.IndexedAt, "file_count": info.FileCount, "symbol_count": info.SymbolCount,
		"languages": info.Languages, "directories": filtered, "symbol_kinds": kinds, "truncated": truncated,
	}, nil
}
