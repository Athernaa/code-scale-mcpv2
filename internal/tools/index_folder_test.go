package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIndexFolderAppliesGitignoreAndExtraPatterns(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".gitignore":          "ignored/\ndist/\n*.generated.ts\nprivate/\n",
		"keep.go":             "package keep\n\nfunc Keep() {}\n",
		"ignored/bad.go":      "package bad\n\nfunc Ignored() {}\n",
		"dist/bad.go":         "package bad\n\nfunc Dist() {}\n",
		"private/bad.go":      "package bad\n\nfunc Private() {}\n",
		"generated/bad.go":    "package bad\n\nfunc Generated() {}\n",
		"client.generated.ts": "const generated = () => {}\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	deps := &Deps{Store: store}
	result, _, err := IndexFolderHandler(deps)(context.Background(), nil, IndexFolderArgs{
		Path:        root,
		ExtraIgnore: []string{"generated/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("index_folder returned an error: %#v", result)
	}

	id, err := repository.Local(root)
	if err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	filesInIndex, err := store.GetFiles(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(filesInIndex) != 1 || filesInIndex[0].Path != "keep.go" {
		t.Fatalf("ignore rules did not filter repository files: %#v", filesInIndex)
	}

	var response map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &response); err != nil {
		t.Fatal(err)
	}
	if response["files_discovered"] != float64(1) || response["file_count"] != float64(1) {
		t.Fatalf("unexpected indexing diagnostics: %#v", response)
	}
}

func TestIndexFolderWorkspaceCoverageDoesNotSilentlyApplyGenericFileCap(t *testing.T) {
	root := t.TempDir()
	resourceDir := filepath.Join(root, "resources", "[bulk]", "bulk_resource")
	if err := os.MkdirAll(resourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("ensure bulk_resource\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "fxmanifest.lua"), []byte("fx_version 'cerulean'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= DefaultMaxFiles; i++ {
		path := filepath.Join(resourceDir, fmt.Sprintf("file_%05d.lua", i))
		if err := os.WriteFile(path, []byte("local value = 1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, _, err := IndexFolderHandler(&Deps{Store: store})(context.Background(), nil, IndexFolderArgs{Path: root})
	if err != nil || result.IsError {
		t.Fatalf("workspace indexing failed: result=%#v err=%v", result, err)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &response); err != nil {
		t.Fatal(err)
	}
	if response["mode"] != "fivem_workspace" || response["index_truncated"] != false || response["index_complete"] != true {
		t.Fatalf("workspace coverage was not truthful: %#v", response)
	}
	if response["files_discovered_total"] != float64(DefaultMaxFiles+2) || response["files_indexed"] != float64(DefaultMaxFiles+2) || response["resources_discovered"] != float64(1) {
		t.Fatalf("workspace coverage counts were incorrect: %#v", response)
	}
}

func TestIndexFolderPersistsFiveMSemantics(t *testing.T) {
	resourcePath := filepath.Join("..", "..", "testdata", "fivem", "basic_resource")
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	result, _, err := IndexFolderHandler(&Deps{Store: store})(context.Background(), nil, IndexFolderArgs{Path: resourcePath})
	if err != nil || result.IsError {
		t.Fatalf("index_folder failed for FiveM fixture: result=%#v err=%v", result, err)
	}
	id, err := repository.Local(resourcePath)
	if err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	entities, err := store.GetSemanticEntities(repoID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entity := range entities {
		if entity.Kind == "event_handler" && entity.Name == "avenlo:createCharacter" && entity.Side == "server" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("indexed FiveM semantics missing server handler: %#v", entities)
	}
	resourceFound := false
	exportResourceCorrect := false
	for _, entity := range entities {
		if entity.Kind == "manifest_resource" && entity.Name == "basic_resource" {
			resourceFound = true
		}
		if entity.Kind == "export_definition" && entity.Name == "getCharacter" && entity.Metadata["resource"] == "basic_resource" {
			exportResourceCorrect = true
		}
	}
	if !resourceFound || !exportResourceCorrect {
		t.Fatalf("semantic resource identity used the storage hash: %#v", entities)
	}
	response := decodeToolJSON(t, result)
	if response["mode"] != "fivem_resource" || response["resource"] != "basic_resource" {
		t.Fatalf("single resource mode metadata is incorrect: %#v", response)
	}
}

func TestIndexFolderDetectsFiveMWorkspaceMode(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"server.cfg":                             "exec resources.cfg\n",
		"resources.cfg":                          "ensure core_a\nensure app_a\n",
		"resources/[core]/core_a/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/[core]/core_a/server.lua":     "RegisterNetEvent('workspace:test')\n",
		"resources/[app]/app_a/fxmanifest.lua":   "fx_version 'cerulean'\nclient_script 'client.lua'\n",
		"resources/[app]/app_a/client.lua":       "TriggerServerEvent('workspace:test')\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, _, err := IndexFolderHandler(&Deps{Store: store})(context.Background(), nil, IndexFolderArgs{Path: root})
	if err != nil || result.IsError {
		t.Fatalf("workspace index failed: result=%#v err=%v", result, err)
	}
	decoded := decodeToolJSON(t, result)
	if decoded["mode"] != "fivem_workspace" || decoded["resources_discovered"] != float64(2) || decoded["resources_enabled"] != float64(2) {
		t.Fatalf("unexpected workspace response: %#v", decoded)
	}
	id, err := repository.Local(root)
	if err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetWorkspace(repoID); err != nil {
		t.Fatal(err)
	}
}
