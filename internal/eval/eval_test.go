package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

func projectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from internal/eval to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}

type evalEnv struct {
	deps     *tools.Deps
	root     string // project root
	testdata string // absolute path to testdata/
	internal string // absolute path to internal/
}

func setupEvalEnv(t *testing.T) *evalEnv {
	t.Helper()
	root := projectRoot(t)
	tmp := t.TempDir()

	store, err := storage.NewIndexStore(tmp)
	if err != nil {
		t.Fatalf("NewIndexStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	tracker := storage.NewTokenTracker(store.DB())

	env := &evalEnv{
		deps:     &tools.Deps{Store: store, Tracker: tracker, Throttle: ratelimit.NewThrottler()},
		root:     root,
		testdata: filepath.Join(root, "testdata"),
		internal: filepath.Join(root, "internal"),
	}

	indexDir(t, store, "eval", "testdata", env.testdata)
	indexDir(t, store, "eval", "self", env.internal)

	return env
}

func indexDir(t *testing.T, store *storage.IndexStore, owner, name, absDir string) {
	t.Helper()
	fileHashes := make(map[string]string)
	fileLangs := make(map[string]string)
	var allSymbols []parser.Symbol

	err := filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		lang := parser.DetectLanguage(d.Name())
		if lang == "" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(absDir, path)
		rel = filepath.ToSlash(rel)

		symbols, err := parser.ParseFile(content, rel, lang)
		if err != nil {
			return nil
		}

		h := sha256.Sum256(content)
		fileHashes[rel] = hex.EncodeToString(h[:])
		fileLangs[rel] = lang
		allSymbols = append(allSymbols, symbols...)

		if err := store.SaveContentFile(owner, name, rel, content); err != nil {
			t.Logf("warning: SaveContentFile(%s): %v", rel, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s): %v", absDir, err)
	}

	if err := store.ReplaceRepoIndex(owner, name, "local", "", fileHashes, fileLangs, allSymbols); err != nil {
		t.Fatalf("ReplaceRepoIndex(%s/%s): %v", owner, name, err)
	}
	t.Logf("indexed %s/%s: %d files, %d symbols", owner, name, len(fileHashes), len(allSymbols))
}

// resultText extracts the JSON text from a CallToolResult.
func resultText(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// sumFileSizes returns total bytes for files relative to basePath.
func sumFileSizes(basePath string, relPaths []string) int64 {
	var total int64
	for _, rp := range relPaths {
		info, err := os.Stat(filepath.Join(basePath, filepath.FromSlash(rp)))
		if err == nil {
			total += info.Size()
		}
	}
	return total
}

// allTestdataFiles returns relative paths of all testdata sample files.
func allTestdataFiles(t *testing.T, testdataDir string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(testdataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if parser.DetectLanguage(d.Name()) != "" {
			rel, _ := filepath.Rel(testdataDir, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files
}

// ---------------------------------------------------------------------------
// Eval 1: Token Efficiency
// ---------------------------------------------------------------------------

type tokenScenario struct {
	name           string
	repo           string
	baseDir        string
	baselineFiles  []string
	codeScaleFn    func(env *evalEnv) string // returns JSON response
	expectContains []string
}

func TestEval_TokenEfficiency(t *testing.T) {
	env := setupEvalEnv(t)
	ctx := context.Background()

	allTD := allTestdataFiles(t, env.testdata)

	// allSelfFiles collects all Go files under internal/ as relative paths.
	allSelfFiles := allTestdataFiles(t, env.internal) // reuse helper — it checks DetectLanguage

	scenarios := []tokenScenario{
		// --- Testdata scenarios: compare both repository-wide and containing-file baselines ---
		{
			name:          "Find authenticate function (native search + containing file)",
			repo:          "eval/testdata",
			baseDir:       env.testdata,
			baselineFiles: []string{"python/sample.py"},
			codeScaleFn: func(env *evalEnv) string {
				searchH := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := searchH(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/testdata", Query: "authenticate"})
				text := resultText(r)

				var resp map[string]any
				_ = json.Unmarshal([]byte(text), &resp)
				results, _ := resp["results"].([]any)
				if len(results) == 0 {
					return text
				}
				first := results[0].(map[string]any)
				symID := first["id"].(string)

				getH := tools.GetSymbolHandler(env.deps)
				r2, _, _ := getH(ctx, nil, tools.GetSymbolArgs{Repo: "eval/testdata", SymbolID: symID})
				return text + resultText(r2)
			},
			expectContains: []string{"authenticate"},
		},
		{
			name:          "UserService methods (search all)",
			repo:          "eval/testdata",
			baseDir:       env.testdata,
			baselineFiles: allTD, // agent would grep all files to find UserService
			codeScaleFn: func(env *evalEnv) string {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/testdata", Query: "UserService", MaxResults: 20})
				return resultText(r)
			},
			expectContains: []string{"UserService"},
		},
		{
			name:          "List all constants",
			repo:          "eval/testdata",
			baseDir:       env.testdata,
			baselineFiles: allTD,
			codeScaleFn: func(env *evalEnv) string {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/testdata", Query: "MAX", Kind: "constant", MaxResults: 50})
				return resultText(r)
			},
			expectContains: []string{"constant"},
		},
		{
			name:          "Repository overview",
			repo:          "eval/testdata",
			baseDir:       env.testdata,
			baselineFiles: allTD,
			codeScaleFn: func(env *evalEnv) string {
				h := tools.GetRepoOutlineHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.GetRepoOutlineArgs{Repo: "eval/testdata"})
				return resultText(r)
			},
			expectContains: []string{"file_count", "symbol_count", "python"},
		},
		// --- Self-indexed scenarios: real Go code, realistic file sizes ---
		{
			name:          "Find TokenTracker (self, search all)",
			repo:          "eval/self",
			baseDir:       env.internal,
			baselineFiles: allSelfFiles, // agent greps all 29 Go files
			codeScaleFn: func(env *evalEnv) string {
				searchH := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := searchH(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/self", Query: "TokenTracker"})
				text := resultText(r)

				var resp map[string]any
				_ = json.Unmarshal([]byte(text), &resp)
				results, _ := resp["results"].([]any)
				if len(results) == 0 {
					return text
				}
				first := results[0].(map[string]any)
				symID := first["id"].(string)

				getH := tools.GetSymbolHandler(env.deps)
				r2, _, _ := getH(ctx, nil, tools.GetSymbolArgs{Repo: "eval/self", SymbolID: symID})
				return text + resultText(r2)
			},
			expectContains: []string{"TokenTracker"},
		},
		{
			name:          "Find all Handler functions (self)",
			repo:          "eval/self",
			baseDir:       env.internal,
			baselineFiles: allSelfFiles,
			codeScaleFn: func(env *evalEnv) string {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/self", Query: "Handler", Kind: "function", MaxResults: 20})
				return resultText(r)
			},
			expectContains: []string{"Handler"},
		},
		{
			name:          "Repo overview (self)",
			repo:          "eval/self",
			baseDir:       env.internal,
			baselineFiles: allSelfFiles,
			codeScaleFn: func(env *evalEnv) string {
				h := tools.GetRepoOutlineHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.GetRepoOutlineArgs{Repo: "eval/self"})
				return resultText(r)
			},
			expectContains: []string{"file_count", "symbol_count", "go"},
		},
		{
			name:          "Get specific symbol: EstimateSavings (self)",
			repo:          "eval/self",
			baseDir:       env.internal,
			baselineFiles: []string{"storage/tracker.go", "storage/store.go"},
			codeScaleFn: func(env *evalEnv) string {
				h := tools.GetSymbolHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.GetSymbolArgs{Repo: "eval/self", SymbolID: "storage/tracker.go::EstimateSavings#function"})
				return resultText(r)
			},
			expectContains: []string{"EstimateSavings", "rawBytes"},
		},
	}

	type tokenResult struct {
		name           string
		baselineBytes  int64
		codeScaleBytes int64
		savingsPercent float64
		correct        bool
	}

	var results []tokenResult

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			baselineBytes := sumFileSizes(sc.baseDir, sc.baselineFiles)
			responseJSON := sc.codeScaleFn(env)
			codeScaleBytes := int64(len(responseJSON))

			savings := 0.0
			if baselineBytes > 0 {
				savings = (1.0 - float64(codeScaleBytes)/float64(baselineBytes)) * 100
			}

			correct := true
			for _, expected := range sc.expectContains {
				if !strings.Contains(strings.ToLower(responseJSON), strings.ToLower(expected)) {
					correct = false
					t.Errorf("response missing expected string %q", expected)
				}
			}

			if savings <= 0 {
				t.Logf("note: negative savings (%.1f%%) — expected for small files where JSON metadata > raw bytes", savings)
			}

			results = append(results, tokenResult{
				name:           sc.name,
				baselineBytes:  baselineBytes,
				codeScaleBytes: codeScaleBytes,
				savingsPercent: savings,
				correct:        correct,
			})
		})
	}

	// Print report
	t.Run("Report", func(t *testing.T) {
		var totalSavings float64
		allCorrect := true

		report := "\n=== TOKEN EFFICIENCY REPORT ===\n"
		report += fmt.Sprintf("%-40s | %10s | %10s | %8s | %s\n", "Scenario", "Baseline", "CodeScale", "Savings", "Correct")
		report += strings.Repeat("-", 90) + "\n"

		for _, r := range results {
			status := "PASS"
			if !r.correct {
				status = "FAIL"
				allCorrect = false
			}
			report += fmt.Sprintf("%-40s | %8d B | %8d B | %6.1f%% | %s\n",
				r.name, r.baselineBytes, r.codeScaleBytes, r.savingsPercent, status)
			totalSavings += r.savingsPercent
		}

		avgSavings := totalSavings / float64(len(results))
		report += strings.Repeat("-", 90) + "\n"
		report += fmt.Sprintf("AVERAGE SAVINGS: %.1f%%\n", avgSavings)
		report += fmt.Sprintf("ALL CORRECT: %v\n", allCorrect)

		// Token equivalents
		var totalBaseline, totalCodeScale int64
		for _, r := range results {
			totalBaseline += r.baselineBytes
			totalCodeScale += r.codeScaleBytes
		}
		report += fmt.Sprintf("\nToken estimate (baseline):   %d tokens\n", totalBaseline/4)
		report += fmt.Sprintf("Token estimate (code-scale): %d tokens\n", totalCodeScale/4)
		report += fmt.Sprintf("Tokens saved:                %d tokens\n", (totalBaseline-totalCodeScale)/4)

		t.Log(report)

		if !allCorrect {
			t.Error("some scenarios returned incorrect results")
		}
		// This is an observational benchmark: small files and metadata-heavy
		// responses can legitimately have negative savings. Do not turn a
		// measured result into a marketing threshold.
	})
}

func TestEval_PunctuationSearchEfficiency(t *testing.T) {
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	const query = "avenlo:server:createCharacter"
	files := make(map[string]string, 160)
	langs := make(map[string]string, 160)
	var baselineBytes int64
	for i := 0; i < 160; i++ {
		path := fmt.Sprintf("lua/file-%03d.lua", i)
		content := fmt.Sprintf("local value_%03d = 'unrelated'\n", i)
		if i == 87 {
			content += "TriggerServerEvent('" + query + "', data)\n"
		}
		files[path] = content
		langs[path] = "lua"
		baselineBytes += int64(len(content))
		if err := store.SaveContentFile("eval", "events", path, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReplaceRepoIndex("eval", "events", "local", "", files, langs, nil); err != nil {
		t.Fatal(err)
	}

	deps := &tools.Deps{Store: store, Throttle: ratelimit.NewThrottler()}
	result, _, err := tools.SearchTextHandler(deps)(context.Background(), nil, tools.SearchTextArgs{
		Repo: "eval/events", Query: query, MaxResults: 20,
	})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("punctuation search failed: result=%#v err=%v", result, err)
	}
	response := resultText(result)
	if !strings.Contains(response, "lua/file-087.lua") || !strings.Contains(response, query) {
		t.Fatalf("punctuation search missed exact match: %s", response)
	}
	responseBytes := int64(len([]byte(response)))
	t.Logf("punctuation search: generated_files=%d baseline_bytes=%d response_bytes=%d estimated_saved_bytes=%d estimated_saved_tokens=%d", len(files), baselineBytes, responseBytes, maxInt64(0, baselineBytes-responseBytes), maxInt64(0, baselineBytes-responseBytes)/4)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Eval 2: Task Effectiveness
// ---------------------------------------------------------------------------

type effectivenessTask struct {
	name       string
	run        func(env *evalEnv) (string, bool) // returns (JSON, isError)
	assertions func(t *testing.T, json string)
}

func TestEval_TaskEffectiveness(t *testing.T) {
	env := setupEvalEnv(t)
	ctx := context.Background()

	tasks := []effectivenessTask{
		{
			name: "search_symbols: find authenticate",
			run: func(env *evalEnv) (string, bool) {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/testdata", Query: "authenticate"})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "authenticate")
				assertJSONContains(t, js, "function")
			},
		},
		{
			name: "search_symbols: UserService methods",
			run: func(env *evalEnv) (string, bool) {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/testdata", Query: "UserService", MaxResults: 20})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "UserService")
			},
		},
		{
			name: "search_symbols: all constants",
			run: func(env *evalEnv) (string, bool) {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/testdata", Query: "MAX", Kind: "constant", MaxResults: 50})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "constant")
				assertResultCount(t, js, 1)
			},
		},
		{
			name: "get_file_outline: python sample",
			run: func(env *evalEnv) (string, bool) {
				h := tools.GetFileOutlineHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.GetFileOutlineArgs{Repo: "eval/testdata", FilePath: "python/sample.py", Flat: true})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "get_user")
				assertJSONContains(t, js, "delete_user")
				assertJSONContains(t, js, "authenticate")
				assertSymbolCount(t, js, 4)
			},
		},
		{
			name: "get_symbol: authenticate by ID",
			run: func(env *evalEnv) (string, bool) {
				h := tools.GetSymbolHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.GetSymbolArgs{Repo: "eval/testdata", SymbolID: "python/sample.py::authenticate#function"})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "def authenticate")
				assertJSONContains(t, js, "function")
			},
		},
		{
			name: "get_repo_outline: testdata",
			run: func(env *evalEnv) (string, bool) {
				h := tools.GetRepoOutlineHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.GetRepoOutlineArgs{Repo: "eval/testdata"})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "python")
				assertJSONContains(t, js, "go")
				var resp map[string]any
				_ = json.Unmarshal([]byte(js), &resp)
				fc, _ := resp["file_count"].(float64)
				if int(fc) < 10 {
					t.Errorf("expected file_count >= 10, got %d", int(fc))
				}
			},
		},
		{
			name: "search_text: authenticate across files",
			run: func(env *evalEnv) (string, bool) {
				h := tools.SearchTextHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchTextArgs{Repo: "eval/testdata", Query: "authenticate"})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "python/sample.py")
				assertJSONContains(t, js, "go/sample.go")
			},
		},
		{
			name: "search_symbols (self): find IndexStore",
			run: func(env *evalEnv) (string, bool) {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/self", Query: "IndexStore"})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "IndexStore")
			},
		},
		{
			name: "search_symbols (self): find Handler functions",
			run: func(env *evalEnv) (string, bool) {
				h := tools.SearchSymbolsHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.SearchSymbolsArgs{Repo: "eval/self", Query: "Handler", Kind: "function", MaxResults: 20})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertJSONContains(t, js, "Handler")
				assertResultCount(t, js, 2)
			},
		},
		{
			name: "get_file_outline (self): store.go",
			run: func(env *evalEnv) (string, bool) {
				h := tools.GetFileOutlineHandler(env.deps)
				r, _, _ := h(ctx, nil, tools.GetFileOutlineArgs{Repo: "eval/self", FilePath: "storage/store.go", Flat: true})
				return resultText(r), r.IsError
			},
			assertions: func(t *testing.T, js string) {
				assertSymbolCount(t, js, 10)
			},
		},
	}

	var passed, failed int

	for _, task := range tasks {
		t.Run(task.name, func(t *testing.T) {
			js, isErr := task.run(env)
			if isErr {
				t.Fatalf("tool returned error: %s", js)
			}
			if js == "" {
				t.Fatal("empty response")
			}
			task.assertions(t, js)
			if t.Failed() {
				failed++
			} else {
				passed++
			}
		})
	}

	t.Run("Report", func(t *testing.T) {
		total := passed + failed
		report := "\n=== TASK EFFECTIVENESS REPORT ===\n"
		report += fmt.Sprintf("PASSED: %d/%d tasks\n", passed, total)
		if failed > 0 {
			report += fmt.Sprintf("FAILED: %d/%d tasks\n", failed, total)
		}
		t.Log(report)
	})
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

func assertJSONContains(t *testing.T, js, substr string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(js), strings.ToLower(substr)) {
		t.Errorf("response missing %q", substr)
	}
}

func assertResultCount(t *testing.T, js string, minCount int) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(js), &resp); err != nil {
		t.Errorf("failed to parse JSON: %v", err)
		return
	}
	rc, _ := resp["result_count"].(float64)
	if int(rc) < minCount {
		t.Errorf("expected result_count >= %d, got %d", minCount, int(rc))
	}
}

func assertSymbolCount(t *testing.T, js string, minCount int) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(js), &resp); err != nil {
		t.Errorf("failed to parse JSON: %v", err)
		return
	}
	meta, ok := resp["_meta"].(map[string]any)
	if !ok {
		t.Error("missing _meta in response")
		return
	}
	sc, _ := meta["symbol_count"].(float64)
	if int(sc) < minCount {
		t.Errorf("expected symbol_count >= %d, got %d", minCount, int(sc))
	}
}
