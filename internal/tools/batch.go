package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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
	if args.MaxLength > 0 {
		source, _ = smartTruncateSource(source, args.MaxLength)
	}
	return map[string]any{
		"id": sym.ID, "kind": sym.Kind, "name": sym.Name,
		"file": sym.File, "line": sym.Line, "signature": sym.Signature,
		"source": source,
	}, nil
}

func execGetSymbols(deps *Deps, args GetSymbolsArgs) (any, error) {
	if len(args.SymbolIDs) > 100 {
		return nil, fmt.Errorf("too many symbol IDs (max 100)")
	}
	repoID, err := deps.Store.GetRepoID(args.Repo)
	if err != nil {
		return nil, err
	}
	var symbols []map[string]any
	for _, symID := range args.SymbolIDs {
		sym, err := deps.Store.GetSymbolByID(repoID, symID)
		if err != nil {
			continue
		}
		source, err := deps.Store.GetSymbolContent(repoID, symID)
		if err != nil {
			continue
		}
		symbols = append(symbols, map[string]any{
			"id": sym.ID, "kind": sym.Kind, "name": sym.Name,
			"file": sym.File, "line": sym.Line, "signature": sym.Signature,
			"source": source,
		})
	}
	return map[string]any{"symbols": symbols}, nil
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
	return map[string]any{"results": results, "result_count": len(results)}, nil
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
	return map[string]any{"results": results, "result_count": len(results)}, nil
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
	var results []map[string]any
	for _, sym := range symbols {
		results = append(results, map[string]any{
			"id": sym.ID, "kind": sym.Kind, "name": sym.Name,
			"line": sym.Line, "signature": sym.Signature,
		})
	}
	return map[string]any{"file": args.FilePath, "symbols": results}, nil
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
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return map[string]any{"repo": args.Repo, "files": paths}, nil
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
	return map[string]any{
		"repo": info.Repo, "file_count": info.FileCount, "symbol_count": info.SymbolCount,
		"languages": info.Languages, "directories": dirs, "symbol_kinds": kinds,
	}, nil
}
