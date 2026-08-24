package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/Athernaa/code-scale-mcpv2/internal/github"
	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/security"
	"github.com/Athernaa/code-scale-mcpv2/internal/summarizer"
)

type IndexRepoArgs struct {
	URL            string `json:"url" jsonschema:"GitHub repository URL or owner/repo"`
	UseAISummaries bool   `json:"use_ai_summaries,omitempty" jsonschema:"Use AI for symbol summaries"`
}

func IndexRepoHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, IndexRepoArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args IndexRepoArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		owner, repoName, err := github.ParseRepoURL(args.URL)
		if err != nil {
			r, _ := errorResult("invalid repository (expected GitHub URL or owner/repo format)")
			return r, nil, nil
		}

		if !security.SafeRepoComponent(owner) || !security.SafeRepoComponent(repoName) {
			r, _ := errorResult("invalid repository name: owner and repo must be alphanumeric")
			return r, nil, nil
		}

		client := github.NewClient()

		// Fetch repo tree
		tree, err := client.FetchRepoTree(owner, repoName)
		if err != nil {
			log.Printf("index_repo: fetch tree error for %s/%s: %v", owner, repoName, err)
			r, _ := errorResult("failed to fetch repository tree")
			return r, nil, nil
		}

		// Filter source files
		var sourcePaths []string
		for _, entry := range tree {
			if entry.Type != "blob" {
				continue
			}
			// Skip by name patterns
			if security.ShouldSkipFile(entry.Path) {
				continue
			}
			// Skip binary extensions
			if security.IsBinaryExtension(entry.Path) {
				continue
			}
			// Skip secrets
			if security.IsSecretFile(entry.Path) {
				continue
			}
			// Skip large files
			if entry.Size > security.DefaultMaxFileSize {
				continue
			}
			// Check if supported language
			if parser.DetectLanguage(entry.Path) == "" {
				continue
			}
			// Skip directories in skip patterns
			skip := false
			for _, sp := range security.SkipPatterns {
				if containsDir(entry.Path, sp) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			sourcePaths = append(sourcePaths, entry.Path)
		}

		// Limit files
		if len(sourcePaths) > DefaultMaxFiles {
			sourcePaths = prioritizeFiles(sourcePaths, DefaultMaxFiles)
		}

		// Fetch file contents concurrently
		fileContents := client.FetchFilesConcurrently(owner, repoName, sourcePaths, 20)

		// Parse and index
		fileHashes := make(map[string]string)
		fileLangs := make(map[string]string)
		var allSymbols []parser.Symbol

		for path, content := range fileContents {
			// Skip binary content
			if security.IsBinaryContent(content) {
				continue
			}

			lang := parser.DetectLanguage(path)
			symbols, err := parser.ParseFile(content, path, lang)
			if err != nil {
				continue
			}

			h := sha256.Sum256(content)
			fileHashes[path] = hex.EncodeToString(h[:])
			fileLangs[path] = lang
			allSymbols = append(allSymbols, symbols...)

			// Save content file
			if err := deps.Store.SaveContentFile(owner, repoName, path, content); err != nil {
				log.Printf("index_repo: failed to save content for %s: %v", path, err)
			}
		}

		// Summarize
		summarizer.SummarizeSymbols(allSymbols, args.UseAISummaries)

		// Save index
		err = deps.Store.ReplaceRepoIndex(owner, repoName, "github", "", fileHashes, fileLangs, allSymbols)
		if err != nil {
			r, _ := errorResult("save index: " + err.Error())
			return r, nil, nil
		}

		// Count languages
		langCounts := make(map[string]int)
		for _, l := range fileLangs {
			langCounts[l]++
		}

		result := map[string]any{
			"success":      true,
			"repo":         owner + "/" + repoName,
			"indexed_at":   time.Now().UTC().Format(time.RFC3339),
			"file_count":   len(fileHashes),
			"symbol_count": len(allSymbols),
			"languages":    langCounts,
			"_meta": Meta{
				TimingMs:    t.elapsedMs(),
				Repo:        owner + "/" + repoName,
				FileCount:   len(fileHashes),
				SymbolCount: len(allSymbols),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

// containsDir checks if a path contains a specific directory component.
func containsDir(path, dir string) bool {
	parts := splitPath(path)
	for _, p := range parts {
		if p == dir {
			return true
		}
	}
	return false
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range []byte(path) {
		if p == '/' {
			parts = append(parts, "")
		} else if len(parts) > 0 {
			parts[len(parts)-1] += string(p)
		} else {
			parts = append(parts, string(p))
		}
	}
	return parts
}
