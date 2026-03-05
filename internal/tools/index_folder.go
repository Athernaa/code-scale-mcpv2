package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/syphon1c/code-scale-mcp/internal/parser"
	"github.com/syphon1c/code-scale-mcp/internal/security"
	"github.com/syphon1c/code-scale-mcp/internal/summarizer"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const DefaultMaxFiles = 10000

type IndexFolderArgs struct {
	Path           string   `json:"path" jsonschema:"Absolute or ~ path to local folder"`
	UseAISummaries bool     `json:"use_ai_summaries,omitempty" jsonschema:"Use AI for symbol summaries"`
	ExtraIgnore    []string `json:"extra_ignore_patterns,omitempty" jsonschema:"Additional ignore patterns"`
	FollowSymlinks bool     `json:"follow_symlinks,omitempty" jsonschema:"Follow symlinks (default false)"`
}

func IndexFolderHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, IndexFolderArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args IndexFolderArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		// Expand ~ in path
		folderPath, err := expandHomePath(args.Path)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		absPath, err := filepath.Abs(folderPath)
		if err != nil {
			r, _ := errorResult("invalid path: " + err.Error())
			return r, nil, nil
		}

		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			r, _ := errorResult("not a directory: " + absPath)
			return r, nil, nil
		}

		folderName := filepath.Base(absPath)
		owner := "local"
		repoName := folderName

		// Discover files
		var sourceFiles []string
		_ = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			// Skip directories
			if d.IsDir() {
				if security.ShouldSkipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip symlinks if not following
			if d.Type()&os.ModeSymlink != 0 && !args.FollowSymlinks {
				return nil
			}

			// Skip by file name
			if security.ShouldSkipFile(d.Name()) {
				return nil
			}

			// Security filter
			if reason := security.ShouldExcludeFile(path, absPath, security.DefaultMaxFileSize); reason != "" {
				return nil
			}

			// Check if it's a supported language
			lang := parser.DetectLanguage(d.Name())
			if lang == "" {
				return nil
			}

			rel, _ := filepath.Rel(absPath, path)
			sourceFiles = append(sourceFiles, filepath.ToSlash(rel))
			return nil
		})

		// Limit files
		if len(sourceFiles) > DefaultMaxFiles {
			sourceFiles = prioritizeFiles(sourceFiles, DefaultMaxFiles)
		}

		// Parse files concurrently
		type parseResult struct {
			path     string
			symbols  []parser.Symbol
			content  []byte
			hash     string
			language string
		}

		results := make(chan parseResult, len(sourceFiles))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 20) // Concurrency limit

		for _, relPath := range sourceFiles {
			wg.Add(1)
			go func(rp string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				fullPath := filepath.Join(absPath, filepath.FromSlash(rp))
				content, err := os.ReadFile(fullPath)
				if err != nil {
					return
				}

				// Check for binary content
				if security.IsBinaryContent(content) {
					return
				}

				lang := parser.DetectLanguage(rp)
				symbols, err := parser.ParseFile(content, rp, lang)
				if err != nil {
					return
				}

				h := sha256.Sum256(content)
				results <- parseResult{
					path:     rp,
					symbols:  symbols,
					content:  content,
					hash:     hex.EncodeToString(h[:]),
					language: lang,
				}
			}(relPath)
		}

		go func() {
			wg.Wait()
			close(results)
		}()

		// Collect results
		fileHashes := make(map[string]string)
		fileLangs := make(map[string]string)
		var allSymbols []parser.Symbol

		for pr := range results {
			fileHashes[pr.path] = pr.hash
			fileLangs[pr.path] = pr.language
			allSymbols = append(allSymbols, pr.symbols...)

			// Save content file
			_ = deps.Store.SaveContentFile(owner, repoName, pr.path, pr.content)
		}

		// Summarize
		summarizer.SummarizeSymbols(allSymbols, args.UseAISummaries)

		// Save index
		err = deps.Store.SaveIndex(owner, repoName, "local", "", fileHashes, fileLangs, allSymbols)
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
			"folder_path":  absPath,
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

// prioritizeFiles selects the most important files up to maxCount.
// Prioritizes: src/ > lib/ > pkg/ > cmd/ > internal/ > other
func prioritizeFiles(files []string, maxCount int) []string {
	priority := map[string]int{
		"src":      0,
		"lib":      1,
		"pkg":      2,
		"cmd":      3,
		"internal": 4,
	}

	type scored struct {
		path  string
		score int
	}
	var scored_files []scored
	for _, f := range files {
		parts := strings.Split(f, "/")
		s := 5 // default priority
		if len(parts) > 0 {
			if p, ok := priority[parts[0]]; ok {
				s = p
			}
		}
		scored_files = append(scored_files, scored{f, s})
	}

	// Sort by priority
	sort.Slice(scored_files, func(i, j int) bool {
		return scored_files[i].score < scored_files[j].score
	})

	if len(scored_files) > maxCount {
		scored_files = scored_files[:maxCount]
	}

	result := make([]string, len(scored_files))
	for i, sf := range scored_files {
		result[i] = sf.path
	}
	return result
}
