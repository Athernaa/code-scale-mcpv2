package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/pathfilter"
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/security"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/summarizer"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
	workspaceindex "github.com/Athernaa/code-scale-mcpv2/internal/workspace/indexer"
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
			r, _ := errorResult("invalid path")
			return r, nil, nil
		}

		localID, err := repository.Local(folderPath)
		if err != nil {
			r, _ := errorResult("invalid path")
			return r, nil, nil
		}
		absPath := localID.CanonicalPath

		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			r, _ := errorResult("path is not a valid directory")
			return r, nil, nil
		}

		if !security.IsAllowedRootPath(absPath) {
			r, _ := errorResult("directory is not allowed for indexing (system or restricted path)")
			return r, nil, nil
		}
		if args.FollowSymlinks {
			r, _ := errorResult("follow_symlinks=true is not supported safely yet; index with follow_symlinks=false")
			return r, nil, nil
		}
		ignoreMatcher, err := pathfilter.New(absPath, args.ExtraIgnore)
		if err != nil {
			r, _ := errorResult("load ignore rules: " + err.Error())
			return r, nil, nil
		}

		owner := localID.Owner
		repoName := localID.Name
		resourceName, err := repository.LocalResourceName(absPath)
		if err != nil {
			r, _ := errorResult("derive resource name: " + err.Error())
			return r, nil, nil
		}
		detectedMode, detectErr := workspace.DetectMode(absPath)
		if detectErr != nil {
			detectedMode = workspace.KindGeneric
		}
		workspaceDiscovery, workspaceDiscoveryErr := workspace.DiscoverWithIgnore(absPath, ignoreMatcher.Ignored)
		if workspaceDiscoveryErr == nil {
			// Ignore-aware discovery is authoritative. An ignored manifest or
			// config must not leave the earlier ignore-unaware mode decision in
			// place.
			detectedMode = workspaceDiscovery.Mode
		}

		// Discover files
		var sourceFiles []string
		_ = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				log.Printf("index_folder: skipping %s: %v", path, err)
				return nil
			}

			// Skip directories
			if d.IsDir() {
				if security.ShouldSkipDir(d.Name()) {
					return filepath.SkipDir
				}
				if ignoreMatcher.Ignored(path, true) {
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
			if ignoreMatcher.Ignored(path, false) {
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

		// A workspace must not silently lose later resources to the generic
		// repository cap. Keep the cap for ordinary repositories, while
		// workspace indexing remains explicit and complete unless a file-level
		// security/read/parse failure is reported.
		discoveredTotal := len(sourceFiles)
		indexTruncated := false
		if detectedMode != workspace.KindFiveMWorkspace && len(sourceFiles) > DefaultMaxFiles {
			sourceFiles = prioritizeFiles(sourceFiles, DefaultMaxFiles)
			indexTruncated = true
		}

		// Parse files concurrently
		type parseResult struct {
			path         string
			symbols      []parser.Symbol
			content      []byte
			hash         string
			language     string
			failure      string
			parseFailure bool
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
					results <- parseResult{path: rp, failure: err.Error()}
					return
				}

				// Check for binary content
				if security.IsBinaryContent(content) {
					results <- parseResult{path: rp, failure: "binary content"}
					return
				}

				lang := parser.DetectLanguage(rp)
				symbols, err := parser.ParseFile(content, rp, lang)
				if err != nil {
					results <- parseResult{path: rp, failure: err.Error(), parseFailure: true}
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
		fileContents := make(map[string][]byte)
		symbolsByFile := make(map[string][]parser.Symbol)
		var allSymbols []parser.Symbol
		skippedFiles := 0
		parseFailures := 0
		var diagnostics []string

		for pr := range results {
			if pr.failure != "" {
				skippedFiles++
				if pr.parseFailure {
					parseFailures++
				}
				if len(diagnostics) < 3 {
					diagnostics = append(diagnostics, pr.path+": "+pr.failure)
				}
				continue
			}
			fileHashes[pr.path] = pr.hash
			fileLangs[pr.path] = pr.language
			fileContents[pr.path] = pr.content
			symbolsByFile[pr.path] = pr.symbols
			allSymbols = append(allSymbols, pr.symbols...)

			// Save content file
			if err := deps.Store.SaveContentFile(owner, repoName, pr.path, pr.content); err != nil {
				log.Printf("index_folder: failed to save content for %s: %v", pr.path, err)
			}
		}

		// Summarize
		summarizer.SummarizeSymbols(allSymbols, args.UseAISummaries)

		// Save index
		err = deps.Store.ReplaceRepoIndex(owner, repoName, "local", "", fileHashes, fileLangs, allSymbols, absPath)
		if err != nil {
			r, _ := errorResult("save index: " + err.Error())
			return r, nil, nil
		}
		mode, modeErr := detectedMode, detectErr
		if modeErr != nil {
			mode = workspace.KindGeneric
			diagnostics = append(diagnostics, "workspace: "+modeErr.Error())
		}
		semanticCount := 0
		workspaceCount := 0
		workspaceRelationships := 0
		semanticIncomplete := false
		if mode == workspace.KindFiveMWorkspace {
			repoID, repoIDErr := deps.Store.GetRepoID(owner + "/" + repoName)
			var wr workspaceindex.Result
			var semanticErr error
			if repoIDErr == nil {
				if workspaceDiscoveryErr == nil {
					wr, semanticErr = workspaceindex.Index(ctx, deps.Store, repoID, owner+"/"+repoName, absPath, fileContents, fileLangs, symbolsByFile, workspaceDiscovery)
				} else {
					semanticErr = workspaceDiscoveryErr
				}
			} else {
				semanticErr = repoIDErr
			}
			if semanticErr != nil {
				semanticIncomplete = true
				log.Printf("index_folder: workspace analysis failed: %v", semanticErr)
				_ = deps.Store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{})
				_ = deps.Store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{})
				_ = deps.Store.ClearWorkspaceState(repoID)
				diagnostics = append(diagnostics, "workspace: "+semanticErr.Error())
			} else {
				semanticCount = wr.FiveMCount
				workspaceCount = wr.WorkspaceCount
				workspaceRelationships = wr.RelationshipCount
				if len(wr.FailedResources) > 0 {
					semanticIncomplete = true
					diagnostics = append(diagnostics, "workspace resource analysis failed: "+strings.Join(wr.FailedResources, ", "))
				}
				if err := deps.Store.UpdateWorkspaceCompleteness(repoID, storage.WorkspaceCompleteness{FilesDiscoveredTotal: discoveredTotal, FilesIndexed: len(fileHashes), IndexTruncated: indexTruncated, Incomplete: semanticIncomplete || indexTruncated || skippedFiles > 0 || parseFailures > 0 || wr.ResourcesWithoutSemantics > 0, ResourcesWithSemantics: wr.ResourcesWithSemantics, ResourcesWithoutSemantics: wr.ResourcesWithoutSemantics}); err != nil {
					diagnostics = append(diagnostics, "workspace completeness: "+err.Error())
				}
			}
		} else {
			semanticCount, err = indexSemanticRepository(ctx, deps.Store, owner+"/"+repoName, resourceName, "local", fileContents, fileLangs, symbolsByFile)
			if err != nil {
				semanticIncomplete = true
				log.Printf("index_folder: semantic analysis failed: %v", err)
				diagnostics = append(diagnostics, "semantic: "+err.Error())
			}
			if repoID, e := deps.Store.GetRepoID(owner + "/" + repoName); e == nil {
				_ = deps.Store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{})
				_ = deps.Store.ClearWorkspaceState(repoID)
			}
		}
		modulePath := ""
		if moduleContent, readErr := os.ReadFile(filepath.Join(absPath, "go.mod")); readErr == nil {
			modulePath = goModulePath(moduleContent)
		}
		genericCount, genericErr := indexGenericRepository(ctx, deps.Store, owner+"/"+repoName, fileContents, fileLangs, symbolsByFile, modulePath)
		if genericErr != nil {
			semanticIncomplete = true
			log.Printf("index_folder: generic graph analysis failed: %v", genericErr)
			if len(diagnostics) < 3 {
				diagnostics = append(diagnostics, "generic graph: "+genericErr.Error())
			}
		}

		// Count languages
		langCounts := make(map[string]int)
		for _, l := range fileLangs {
			langCounts[l]++
		}

		result := map[string]any{
			"success":                 true,
			"mode":                    mode,
			"repo":                    owner + "/" + repoName,
			"folder_path":             absPath,
			"indexed_at":              time.Now().UTC().Format(time.RFC3339),
			"files_discovered":        discoveredTotal,
			"files_discovered_total":  discoveredTotal,
			"file_count":              len(fileHashes),
			"files_indexed":           len(fileHashes),
			"index_truncated":         indexTruncated,
			"index_complete":          !indexTruncated && skippedFiles == 0 && parseFailures == 0 && !semanticIncomplete,
			"incomplete":              indexTruncated || skippedFiles > 0 || parseFailures > 0 || semanticIncomplete,
			"symbol_count":            len(allSymbols),
			"languages":               langCounts,
			"skipped_files":           skippedFiles,
			"parse_failures":          parseFailures,
			"semantic_entities":       semanticCount,
			"generic_entities":        genericCount,
			"workspace_entities":      workspaceCount,
			"workspace_relationships": workspaceRelationships,
			"_meta": Meta{
				TimingMs:    t.elapsedMs(),
				Repo:        owner + "/" + repoName,
				FileCount:   len(fileHashes),
				SymbolCount: len(allSymbols),
			},
		}
		if mode == workspace.KindFiveMResource {
			result["resource"] = resourceName
		}
		if mode == workspace.KindFiveMWorkspace {
			if d, e := workspaceDiscovery, workspaceDiscoveryErr; e == nil {
				enabled, disabled, unknown := 0, 0, 0
				for _, r := range d.Resources {
					switch r.EnabledState {
					case "enabled":
						enabled++
					case "disabled":
						disabled++
					default:
						unknown++
					}
				}
				result["resources_discovered"] = len(d.Resources)
				result["resources_enabled"] = enabled
				result["resources_disabled"] = disabled
				result["resources_unknown"] = unknown
				result["duplicate_names"] = d.DuplicateNames
			}
		}
		if len(diagnostics) > 0 {
			result["diagnostic_samples"] = diagnostics
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
