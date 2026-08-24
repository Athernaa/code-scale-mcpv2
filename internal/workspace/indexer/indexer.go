package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
)

type Result struct {
	Discovery                                     workspace.Discovery
	FiveMCount, WorkspaceCount, RelationshipCount int
}

// Index builds per-resource FiveM facts and workspace-level cross-resource
// facts without reparsing source. The caller supplies already indexed files
// and parser symbols.
func Index(ctx context.Context, store *storage.IndexStore, repoID int64, repo, root string, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol) (Result, error) {
	d, err := workspace.Discover(root)
	if err != nil {
		return Result{}, err
	}
	if d.Mode != workspace.KindFiveMWorkspace {
		return Result{Discovery: d}, nil
	}
	combined := semantic.Result{}
	manifestByResource := map[string]semantic.Entity{}
	for _, r := range d.Resources {
		localFiles := map[string][]byte{}
		localLang := map[string]string{}
		localSymbols := map[string][]parser.Symbol{}
		for path, data := range files {
			if path == r.RelativePath || strings.HasPrefix(path, r.RelativePath+"/") {
				local := strings.TrimPrefix(path, r.RelativePath+"/")
				localFiles[local] = data
				localLang[local] = languages[path]
				localSymbols[local] = symbols[path]
			}
		}
		input := semantic.RepositoryInput{Repo: repo, Resource: r.Name, SourceType: "local", Files: localFiles, Languages: localLang, Symbols: localSymbols}
		result, e := fivem.NewAnalyzer().AnalyzeRepository(ctx, input)
		if e != nil {
			_ = store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{})
			_ = store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{})
			return Result{}, fmt.Errorf("resource %s: %w", r.Name, e)
		}
		idMap := map[string]string{}
		for _, entity := range result.Entities {
			old := entity.ID
			entity.File = joinPath(r.RelativePath, entity.File)
			entity.Analyzer = semantic.AnalyzerFiveM
			if entity.Metadata == nil {
				entity.Metadata = map[string]any{}
			}
			entity.Metadata["source_resource"] = r.Name
			if entity.Kind != fivem.KindExportCall || entity.Metadata["resource"] == nil {
				entity.Metadata["resource"] = r.Name
			}
			entity.Metadata["resource_path"] = r.RelativePath
			entity.ID = semantic.StableID("fivem", repo, r.RelativePath, entity.Kind, entity.Name, fmt.Sprint(entity.Line), fmt.Sprint(entity.EndLine), entity.Side)
			idMap[old] = entity.ID
			combined.Entities = append(combined.Entities, entity)
			if entity.Kind == fivem.KindManifestResource {
				manifestByResource[r.Name] = entity
			}
		}
		for _, rel := range result.Relationships {
			rel.Analyzer = semantic.AnalyzerFiveM
			rel.FromEntityID = idMap[rel.FromEntityID]
			rel.ToEntityID = idMap[rel.ToEntityID]
			rel.ID = semantic.StableID("relationship", rel.FromEntityID, rel.ToEntityID, rel.Kind)
			rel.File = joinPath(r.RelativePath, rel.File)
			combined.Relationships = append(combined.Relationships, rel)
		}
	}
	workspaceEntities, workspaceRelationships := resolveWorkspace(repo, d, combined.Entities, manifestByResource)
	workspaceResult := semantic.Result{Entities: workspaceEntities, Relationships: workspaceRelationships}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, combined); err != nil {
		return Result{}, err
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, workspaceResult); err != nil {
		_ = store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{})
		_ = store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{})
		return Result{}, err
	}
	resources := make([]storage.WorkspaceResourceInfo, 0, len(d.Resources))
	for _, r := range d.Resources {
		resources = append(resources, storage.WorkspaceResourceInfo{Name: r.Name, RelativePath: r.RelativePath, ManifestPath: r.ManifestPath, ManifestType: r.ManifestType, EnabledState: r.EnabledState, StartOrder: r.StartOrder, GroupPath: r.GroupPath})
	}
	configs := make([]storage.WorkspaceConfigInfo, 0, len(d.ConfigFiles))
	for _, c := range d.ConfigFiles {
		configs = append(configs, storage.WorkspaceConfigInfo{Path: c.Path, ContentHash: workspace.ContentHash(c.Content)})
	}
	if err := store.ReplaceWorkspaceState(repoID, root, d.Mode, resources, configs); err != nil {
		return Result{}, err
	}
	return Result{Discovery: d, FiveMCount: len(combined.Entities), WorkspaceCount: len(workspaceResult.Entities), RelationshipCount: len(workspaceResult.Relationships)}, nil
}

func joinPath(a, b string) string {
	if b == "" {
		return workspace.NormalizePath(a)
	}
	return workspace.NormalizePath(filepath.Join(a, b))
}

func resolveWorkspace(repo string, d workspace.Discovery, entities []semantic.Entity, manifests map[string]semantic.Entity) ([]semantic.Entity, []semantic.Relationship) {
	result := []semantic.Entity{}
	rels := []semantic.Relationship{}
	resourceByName := map[string][]semantic.Entity{}
	for _, r := range d.Resources {
		e := semantic.Entity{Analyzer: semantic.AnalyzerFiveMWorkspace, Repo: repo, File: r.ManifestPath, Kind: "workspace_resource", Name: r.Name, Side: "shared", Line: 1, Metadata: map[string]any{"resource": r.Name, "path": r.RelativePath, "enabled": r.EnabledState, "start_order": r.StartOrder}}
		e.ID = semantic.StableID("workspace_resource", repo, r.RelativePath)
		result = append(result, e)
		resourceByName[r.Name] = append(resourceByName[r.Name], e)
	}
	for _, c := range d.ConfigFiles {
		e := semantic.Entity{Analyzer: semantic.AnalyzerFiveMWorkspace, Repo: repo, File: c.Path, Kind: "workspace_config", Name: c.Path, Side: "shared", Line: 1}
		e.ID = semantic.StableID("workspace_config", repo, c.Path)
		result = append(result, e)
	}
	for _, c := range d.Commands {
		if c.Order == 0 {
			continue
		}
		targets := resourceByName[c.Resource]
		e := semantic.Entity{Analyzer: semantic.AnalyzerFiveMWorkspace, Repo: repo, File: c.Path, Kind: "resource_start", Name: c.Resource, Side: "shared", Line: c.Line, Metadata: map[string]any{"command": c.Command, "order": c.Order, "resolved": len(targets) == 1}}
		e.ID = semantic.StableID("resource_start", repo, c.Path, fmt.Sprint(c.Line), c.Resource)
		result = append(result, e)
		if len(targets) == 1 {
			rels = append(rels, workspaceRel(e, targets[0], "starts", c.Resource))
		}
	}
	// Dependency facts are attached to the manifest entity and target a unique
	// workspace resource. Missing/duplicate resources remain unresolved.
	for _, e := range entities {
		if e.Kind != fivem.KindManifestDependency {
			continue
		}
		r := sourceOf(e)
		targets := resourceByName[e.Name]
		if len(targets) == 1 {
			if from, ok := manifests[r]; ok {
				rels = append(rels, workspaceRel(from, targets[0], "depends_on", e.Name))
			}
		}
	}
	for _, from := range entities {
		if from.Dynamic {
			continue
		}
		r := sourceOf(from)
		switch from.Kind {
		case fivem.KindEventTrigger:
			targetSide := networkTarget(from)
			if targetSide == "" {
				continue
			}
			for _, to := range entities {
				if to.Kind != fivem.KindEventHandler || to.Name != from.Name || to.Dynamic {
					continue
				}
				tr := sourceOf(to)
				if tr == r || !sideOK(targetSide, to.Side) {
					continue
				}
				if hasRegistration(entities, to.Name, tr, targetSide) {
					rels = append(rels, workspaceRel(from, to, "cross_resource_event", from.Name))
				}
			}
		case fivem.KindExportCall:
			target, _ := from.Metadata["resource"].(string)
			if target == "" {
				continue
			}
			ts := resourceByName[target]
			if len(ts) != 1 {
				continue
			}
			for _, to := range entities {
				if to.Kind == fivem.KindExportDefinition && to.Name == from.Name && resourceOf(to) == target {
					rels = append(rels, workspaceRel(from, to, "cross_resource_export", from.Name))
				}
			}
		case fivem.KindCallbackCall:
			for _, to := range entities {
				tr := resourceOf(to)
				if to.Kind == fivem.KindCallbackRegistration && to.Name == from.Name && tr != r && callbackOK(from.Side, to.Side) {
					rels = append(rels, workspaceRel(from, to, "cross_resource_callback", from.Name))
				}
			}
		}
	}
	return result, uniqueRels(rels)
}
func resourceOf(e semantic.Entity) string { r, _ := e.Metadata["resource"].(string); return r }
func sourceOf(e semantic.Entity) string {
	if e.Metadata != nil {
		if r, ok := e.Metadata["source_resource"].(string); ok && r != "" {
			return r
		}
	}
	return resourceOf(e)
}
func networkTarget(e semantic.Entity) string {
	op, _ := e.Metadata["operation"].(string)
	switch op {
	case "TriggerServerEvent", "TriggerLatentServerEvent":
		return "server"
	case "TriggerClientEvent", "TriggerLatentClientEvent":
		return "client"
	}
	return ""
}
func sideOK(want, got string) bool { return got == want || got == "shared" }
func hasRegistration(es []semantic.Entity, name, res, side string) bool {
	for _, e := range es {
		if e.Kind == fivem.KindEventRegistration && e.Name == name && resourceOf(e) == res && (e.Side == side || e.Side == "shared") {
			return true
		}
	}
	return false
}
func callbackOK(call, reg string) bool {
	return (call == "client" && (reg == "server" || reg == "shared")) || (call == "server" && (reg == "client" || reg == "shared"))
}
func workspaceRel(from, to semantic.Entity, kind, name string) semantic.Relationship {
	return semantic.Relationship{Analyzer: semantic.AnalyzerFiveMWorkspace, ID: semantic.StableID("relationship", from.ID, to.ID, kind), Repo: from.Repo, FromEntityID: from.ID, ToEntityID: to.ID, Kind: kind, Name: name, Confidence: 1, File: from.File, Line: from.Line}
}
func uniqueRels(rs []semantic.Relationship) []semantic.Relationship {
	sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
	out := rs[:0]
	seen := map[string]bool{}
	for _, r := range rs {
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	return out
}
