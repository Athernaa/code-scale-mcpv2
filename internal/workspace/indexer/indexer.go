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
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
)

type Result struct {
	Discovery                                     workspace.Discovery
	FiveMCount, WorkspaceCount, RelationshipCount int
	FilesIndexed                                  int
	ResourcesWithSemantics                        int
	ResourcesWithoutSemantics                     int
	FailedResources                               []string
	FrameworkCount                                int
	FrameworkFailed                               bool
	FailedFrameworkResources                      []string
}

// analyzeResourceFn is a small seam for deterministic analyzer failure tests.
var analyzeResourceFn = analyzeResource

// analyzeFrameworkFn is a deterministic failure seam for analyzer-isolation
// tests. It is package-local so production uses the immutable analyzer path.
var analyzeFrameworkFn = analyzeFramework

// Index builds per-resource FiveM facts and workspace-level cross-resource
// facts without reparsing source. The caller supplies already indexed files
// and parser symbols.
func Index(ctx context.Context, store *storage.IndexStore, repoID int64, repo, root string, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol, discoveries ...workspace.Discovery) (Result, error) {
	var d workspace.Discovery
	var err error
	if len(discoveries) > 0 {
		d = discoveries[0]
	} else {
		d, err = workspace.Discover(root)
	}
	if err != nil {
		return Result{}, err
	}
	if d.Mode != workspace.KindFiveMWorkspace {
		return Result{Discovery: d}, nil
	}
	combined := semantic.Result{}
	frameworkEntities := make([]semantic.Entity, 0)
	manifestByResource := map[string]semantic.Entity{}
	failedResources := make([]string, 0, 3)
	failedFrameworkResources := make([]string, 0, 3)
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
		result, e := analyzeResourceFn(ctx, repo, r, localFiles, localLang, localSymbols)
		if e != nil {
			if len(failedResources) < 3 {
				failedResources = append(failedResources, r.Name)
			}
			continue
		}
		frameworkResult, frameworkErr := analyzeFrameworkFn(ctx, semantic.RepositoryInput{
			Repo: repo, Resource: r.Name, SourceType: "local", Files: localFiles, Languages: localLang, Symbols: localSymbols,
			SemanticEntities: result.Entities,
		})
		if frameworkErr != nil {
			if len(failedFrameworkResources) < 3 {
				failedFrameworkResources = append(failedFrameworkResources, r.Name)
			}
			frameworkEntities = append(frameworkEntities, framework.FailureStatus(repo, r.Name, r.RelativePath))
		} else {
			frameworkEntities = append(frameworkEntities, normalizeFrameworkResult(repo, r, frameworkResult).Entities...)
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
			entity.Metadata["source_resource_path"] = r.RelativePath
			entity.Metadata["source_resource_id"] = semantic.StableID("workspace_resource", repo, r.RelativePath)
			// `resource` is the owning resource in indexed workspace facts. Keep
			// export targets separate so resource-scoped search and trace output
			// cannot attribute a call to its callee's resource.
			if entity.Kind == fivem.KindExportCall {
				if target, ok := entity.Metadata["resource"].(string); ok && target != "" {
					entity.Metadata["target_resource"] = target
				}
			}
			entity.Metadata["resource"] = r.Name
			entity.Metadata["resource_path"] = r.RelativePath
			entity.ID = semantic.StableID("fivem", repo, r.RelativePath, entity.Kind, entity.Name, fmt.Sprint(entity.Line), fmt.Sprint(entity.EndLine), entity.Side)
			idMap[old] = entity.ID
			combined.Entities = append(combined.Entities, entity)
			if entity.Kind == fivem.KindManifestResource {
				manifestByResource[r.RelativePath] = entity
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
	resourcesWithSemantics, resourcesWithoutSemantics := semanticCoverage(d, combined.Entities)
	workspaceEntities, workspaceRelationships := resolveWorkspace(repo, d, combined.Entities, manifestByResource)
	workspaceResult := semantic.Result{Entities: workspaceEntities, Relationships: workspaceRelationships}
	frameworkResult := framework.RebuildFacts(repo, frameworkEntities)
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, combined); err != nil {
		return Result{}, err
	}
	frameworkFailed := len(failedFrameworkResources) > 0
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFramework, frameworkResult); err != nil {
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
	if err := store.ReplaceWorkspaceState(repoID, root, d.Mode, resources, configs, storage.WorkspaceCompleteness{FilesDiscoveredTotal: len(files), FilesIndexed: len(files), Incomplete: resourcesWithoutSemantics > 0 || frameworkFailed, ResourcesWithSemantics: resourcesWithSemantics, ResourcesWithoutSemantics: resourcesWithoutSemantics}); err != nil {
		return Result{}, err
	}
	return Result{Discovery: d, FiveMCount: len(combined.Entities), WorkspaceCount: len(workspaceResult.Entities), RelationshipCount: len(workspaceResult.Relationships), FilesIndexed: len(files), ResourcesWithSemantics: resourcesWithSemantics, ResourcesWithoutSemantics: resourcesWithoutSemantics, FailedResources: failedResources, FrameworkCount: len(frameworkResult.Entities), FrameworkFailed: frameworkFailed, FailedFrameworkResources: failedFrameworkResources}, nil
}

func analyzeResource(ctx context.Context, repo string, r workspace.Resource, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol) (semantic.Result, error) {
	return fivem.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{Repo: repo, Resource: r.Name, SourceType: "local", Files: files, Languages: languages, Symbols: symbols})
}

func analyzeFramework(ctx context.Context, input semantic.RepositoryInput) (semantic.Result, error) {
	return framework.NewAnalyzer().AnalyzeRepository(ctx, input)
}

func normalizeFrameworkResult(repo string, r workspace.Resource, result semantic.Result) semantic.Result {
	normalized := semantic.Result{}
	idMap := map[string]string{}
	resourceID := semantic.StableID("workspace_resource", repo, r.RelativePath)
	for _, entity := range result.Entities {
		oldID := entity.ID
		entity.Analyzer = semantic.AnalyzerFramework
		entity.File = joinPath(r.RelativePath, entity.File)
		if entity.Metadata == nil {
			entity.Metadata = map[string]any{}
		}
		entity.Metadata["source_resource"] = r.Name
		entity.Metadata["source_resource_path"] = r.RelativePath
		entity.Metadata["source_resource_id"] = resourceID
		if entity.Kind == framework.KindAPIProvider || entity.Kind == framework.KindCandidate {
			entity.Metadata["provider_resource"] = r.Name
			entity.Metadata["provider_resource_path"] = r.RelativePath
			entity.Metadata["provider_resource_id"] = resourceID
		}
		entity.ID = semantic.StableID("framework", repo, r.RelativePath, entity.Kind, entity.Name, fmt.Sprint(entity.Line), fmt.Sprint(entity.EndLine), oldID)
		idMap[oldID] = entity.ID
		normalized.Entities = append(normalized.Entities, entity)
	}
	for _, relationship := range result.Relationships {
		relationship.Analyzer = semantic.AnalyzerFramework
		relationship.FromEntityID = idMap[relationship.FromEntityID]
		relationship.ToEntityID = idMap[relationship.ToEntityID]
		relationship.ID = semantic.StableID("framework_relationship", repo, relationship.FromEntityID, relationship.ToEntityID, relationship.Kind)
		relationship.File = joinPath(r.RelativePath, relationship.File)
		normalized.Relationships = append(normalized.Relationships, relationship)
	}
	return normalized
}

func normalizeResourceResult(repo string, r workspace.Resource, result semantic.Result) semantic.Result {
	normalized := semantic.Result{}
	idMap := map[string]string{}
	for _, entity := range result.Entities {
		oldID := entity.ID
		entity.File = joinPath(r.RelativePath, entity.File)
		entity.Analyzer = semantic.AnalyzerFiveM
		if entity.Metadata == nil {
			entity.Metadata = map[string]any{}
		}
		entity.Metadata["source_resource"] = r.Name
		entity.Metadata["source_resource_path"] = r.RelativePath
		entity.Metadata["source_resource_id"] = semantic.StableID("workspace_resource", repo, r.RelativePath)
		if entity.Kind == fivem.KindExportCall {
			if target, ok := entity.Metadata["resource"].(string); ok && target != "" {
				entity.Metadata["target_resource"] = target
			}
		}
		entity.Metadata["resource"] = r.Name
		entity.Metadata["resource_path"] = r.RelativePath
		entity.ID = semantic.StableID("fivem", repo, r.RelativePath, entity.Kind, entity.Name, fmt.Sprint(entity.Line), fmt.Sprint(entity.EndLine), entity.Side)
		idMap[oldID] = entity.ID
		normalized.Entities = append(normalized.Entities, entity)
	}
	for _, relationship := range result.Relationships {
		relationship.Analyzer = semantic.AnalyzerFiveM
		relationship.FromEntityID = idMap[relationship.FromEntityID]
		relationship.ToEntityID = idMap[relationship.ToEntityID]
		relationship.ID = semantic.StableID("relationship", relationship.FromEntityID, relationship.ToEntityID, relationship.Kind)
		relationship.File = joinPath(r.RelativePath, relationship.File)
		normalized.Relationships = append(normalized.Relationships, relationship)
	}
	return normalized
}

// RefreshResource reparses only the changed resource's indexed files. It then
// resolves workspace edges from persisted compact facts without re-running
// the FiveM analyzer for unrelated resources.
func RefreshResource(ctx context.Context, store *storage.IndexStore, repoID int64, repo, root, resourcePath string, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol, discoveries ...workspace.Discovery) (Result, error) {
	var d workspace.Discovery
	var err error
	if len(discoveries) > 0 {
		d = discoveries[0]
	} else {
		d, err = workspace.Discover(root)
	}
	if err != nil {
		return Result{}, err
	}
	var resource workspace.Resource
	found := false
	for _, candidate := range d.Resources {
		if workspace.NormalizePath(candidate.RelativePath) == workspace.NormalizePath(resourcePath) {
			resource, found = candidate, true
			break
		}
	}
	if !found {
		if err := store.ReplaceSemanticResourceForAnalyzer(repoID, semantic.AnalyzerFiveM, resourcePath, semantic.Result{}); err != nil {
			return Result{}, err
		}
		return rebuildWorkspaceFacts(store, repoID, repo, d, false)
	}
	localFiles := map[string][]byte{}
	localLanguages := map[string]string{}
	localSymbols := map[string][]parser.Symbol{}
	for path, data := range files {
		if path == resource.RelativePath || strings.HasPrefix(path, resource.RelativePath+"/") {
			local := strings.TrimPrefix(path, resource.RelativePath+"/")
			localFiles[local] = data
			localLanguages[local] = languages[path]
			localSymbols[local] = symbols[path]
		}
	}
	result, err := analyzeResourceFn(ctx, repo, resource, localFiles, localLanguages, localSymbols)
	if err != nil {
		if clearErr := store.ReplaceSemanticResourceForAnalyzer(repoID, semantic.AnalyzerFiveM, resource.RelativePath, semantic.Result{}); clearErr != nil {
			return Result{}, fmt.Errorf("resource %s analysis failed: %v; clearing facts failed: %w", resource.Name, err, clearErr)
		}
		if _, rebuildErr := rebuildWorkspaceFacts(store, repoID, repo, d, true); rebuildErr != nil {
			return Result{}, fmt.Errorf("resource %s analysis failed: %v; rebuilding workspace facts failed: %w", resource.Name, err, rebuildErr)
		}
		return Result{}, err
	}
	normalized := normalizeResourceResult(repo, resource, result)
	if err := store.ReplaceSemanticResourceForAnalyzer(repoID, semantic.AnalyzerFiveM, resource.RelativePath, normalized); err != nil {
		return Result{}, err
	}
	return rebuildWorkspaceFacts(store, repoID, repo, d, false)
}

// RebuildWorkspaceFacts rebuilds workspace-level facts from persisted FiveM
// entities without invoking any source analyzer.
func RebuildWorkspaceFacts(store *storage.IndexStore, repoID int64, repo string, d workspace.Discovery) (Result, error) {
	return rebuildWorkspaceFacts(store, repoID, repo, d, false)
}

func rebuildWorkspaceFacts(store *storage.IndexStore, repoID int64, repo string, d workspace.Discovery, degraded bool) (Result, error) {
	entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		return Result{}, err
	}
	manifests := map[string]semantic.Entity{}
	for _, entity := range entities {
		if entity.Kind == fivem.KindManifestResource {
			manifests[sourceResourcePath(entity)] = entity
		}
	}
	workspaceEntities, workspaceRelationships := resolveWorkspace(repo, d, entities, manifests)
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{Entities: workspaceEntities, Relationships: workspaceRelationships}); err != nil {
		return Result{}, err
	}
	withSemantics, withoutSemantics := semanticCoverage(d, entities)
	if err := updateWorkspaceSemanticCoverage(store, repoID, withSemantics, withoutSemantics, degraded); err != nil {
		return Result{}, err
	}
	return Result{Discovery: d, FiveMCount: len(entities), WorkspaceCount: len(workspaceEntities), RelationshipCount: len(workspaceRelationships), ResourcesWithSemantics: withSemantics, ResourcesWithoutSemantics: withoutSemantics}, nil
}

// RefreshWorkspaceConfiguration updates config/resource metadata and
// workspace relationships from persisted facts. It deliberately does not
// analyze resource source files.
func RefreshWorkspaceConfiguration(store *storage.IndexStore, repoID int64, repo, root string, d workspace.Discovery) (Result, error) {
	result, err := RebuildWorkspaceFacts(store, repoID, repo, d)
	if err != nil {
		return Result{}, err
	}
	previous, err := store.GetWorkspace(repoID)
	if err != nil {
		return Result{}, err
	}
	resources, configs := workspaceState(d)
	if err := store.ReplaceWorkspaceState(repoID, root, d.Mode, resources, configs, storage.WorkspaceCompleteness{
		FilesDiscoveredTotal:      previous.FilesDiscoveredTotal,
		FilesIndexed:              previous.FilesIndexed,
		IndexTruncated:            previous.IndexTruncated,
		Incomplete:                previous.IndexTruncated || previous.FilesDiscoveredTotal != previous.FilesIndexed || result.ResourcesWithoutSemantics > 0,
		ResourcesWithSemantics:    result.ResourcesWithSemantics,
		ResourcesWithoutSemantics: result.ResourcesWithoutSemantics,
	}); err != nil {
		return Result{}, err
	}
	return result, nil
}

func workspaceState(d workspace.Discovery) ([]storage.WorkspaceResourceInfo, []storage.WorkspaceConfigInfo) {
	resources := make([]storage.WorkspaceResourceInfo, 0, len(d.Resources))
	for _, r := range d.Resources {
		resources = append(resources, storage.WorkspaceResourceInfo{Name: r.Name, RelativePath: r.RelativePath, ManifestPath: r.ManifestPath, ManifestType: r.ManifestType, EnabledState: r.EnabledState, StartOrder: r.StartOrder, GroupPath: r.GroupPath})
	}
	configs := make([]storage.WorkspaceConfigInfo, 0, len(d.ConfigFiles))
	for _, c := range d.ConfigFiles {
		configs = append(configs, storage.WorkspaceConfigInfo{Path: c.Path, ContentHash: workspace.ContentHash(c.Content)})
	}
	return resources, configs
}

func semanticCoverage(d workspace.Discovery, entities []semantic.Entity) (int, int) {
	covered := map[string]bool{}
	for _, entity := range entities {
		if entity.Kind == fivem.KindManifestResource {
			if path := sourceResourcePath(entity); path != "" {
				covered[path] = true
			}
		}
	}
	withSemantics := 0
	for _, resource := range d.Resources {
		if covered[workspace.NormalizePath(resource.RelativePath)] {
			withSemantics++
		}
	}
	return withSemantics, len(d.Resources) - withSemantics
}

func updateWorkspaceSemanticCoverage(store *storage.IndexStore, repoID int64, withSemantics, withoutSemantics int, degraded bool) error {
	previous, err := store.GetWorkspace(repoID)
	if err != nil {
		if storage.IsNotFound(err) {
			return nil
		}
		return err
	}
	return store.UpdateWorkspaceCompleteness(repoID, storage.WorkspaceCompleteness{
		FilesDiscoveredTotal:      previous.FilesDiscoveredTotal,
		FilesIndexed:              previous.FilesIndexed,
		IndexTruncated:            previous.IndexTruncated,
		Incomplete:                previous.IndexTruncated || previous.FilesDiscoveredTotal != previous.FilesIndexed || degraded || withoutSemantics > 0,
		ResourcesWithSemantics:    withSemantics,
		ResourcesWithoutSemantics: withoutSemantics,
	})
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
		resourceID := semantic.StableID("workspace_resource", repo, r.RelativePath)
		e := semantic.Entity{ID: resourceID, Analyzer: semantic.AnalyzerFiveMWorkspace, Repo: repo, File: r.ManifestPath, Kind: "workspace_resource", Name: r.Name, Side: "shared", Line: 1, Metadata: map[string]any{"resource": r.Name, "resource_id": resourceID, "path": r.RelativePath, "enabled": r.EnabledState, "start_order": r.StartOrder}}
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
		r := sourceResourcePath(e)
		targets := resourceByName[e.Name]
		if len(targets) == 1 {
			if from, ok := manifests[r]; ok {
				rels = append(rels, workspaceRel(from, targets[0], "depends_on", e.Name))
			}
		}
	}
	registrations := map[string][]semantic.Entity{}
	handlersByName := map[string][]semantic.Entity{}
	exports := map[string][]semantic.Entity{}
	callbacksByName := map[string][]semantic.Entity{}
	for _, entity := range entities {
		owner := sourceResourcePath(entity)
		switch entity.Kind {
		case fivem.KindEventRegistration:
			registrations[owner+"\x00"+entity.Name] = append(registrations[owner+"\x00"+entity.Name], entity)
		case fivem.KindEventHandler:
			handlersByName[entity.Name] = append(handlersByName[entity.Name], entity)
		case fivem.KindExportDefinition:
			exports[owner+"\x00"+entity.Name] = append(exports[owner+"\x00"+entity.Name], entity)
		case fivem.KindCallbackRegistration:
			callbacksByName[entity.Name] = append(callbacksByName[entity.Name], entity)
		}
	}
	for _, from := range entities {
		if from.Dynamic {
			continue
		}
		r := sourceResourcePath(from)
		switch from.Kind {
		case fivem.KindEventTrigger:
			targetSide := networkTarget(from)
			if targetSide == "" {
				continue
			}
			for _, to := range handlersByName[from.Name] {
				tr := sourceResourcePath(to)
				if tr == r || to.Dynamic || !sideOK(targetSide, to.Side) {
					continue
				}
				valid := false
				for _, registration := range registrations[tr+"\x00"+to.Name] {
					if !registration.Dynamic && sideOK(targetSide, registration.Side) {
						valid = true
						break
					}
				}
				if valid {
					rels = append(rels, workspaceRel(from, to, "cross_resource_event", from.Name))
				}
			}
		case fivem.KindExportCall:
			target, _ := from.Metadata["target_resource"].(string)
			if target == "" {
				continue
			}
			ts := resourceByName[target]
			if len(ts) != 1 {
				continue
			}
			for _, targetResource := range ts {
				for _, to := range exports[sourceResourcePathForWorkspaceEntity(targetResource)+"\x00"+from.Name] {
					rels = append(rels, workspaceRel(from, to, "cross_resource_export", from.Name))
				}
			}
		case fivem.KindCallbackCall:
			for _, to := range callbacksByName[from.Name] {
				if sourceResourcePath(to) != r && callbackOK(from.Side, to.Side) {
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
func sourceResourcePath(e semantic.Entity) string {
	if e.Metadata != nil {
		if path, ok := e.Metadata["source_resource_path"].(string); ok && path != "" {
			return workspace.NormalizePath(path)
		}
		if path, ok := e.Metadata["resource_path"].(string); ok && path != "" {
			return workspace.NormalizePath(path)
		}
	}
	return sourceOf(e)
}
func sourceResourcePathForWorkspaceEntity(e semantic.Entity) string {
	if e.Metadata != nil {
		if path, ok := e.Metadata["path"].(string); ok && path != "" {
			return workspace.NormalizePath(path)
		}
	}
	return sourceResourcePath(e)
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
