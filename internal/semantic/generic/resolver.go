package generic

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

type graphIndexes struct {
	files             map[string]semantic.Entity
	symbols           map[string]semantic.Entity
	symbolsByFileName map[string][]semantic.Entity
	symbolsByFile     map[string][]semantic.Entity
	exports           map[string]map[string][]semantic.Entity
	imports           map[string][]semantic.Entity
	packages          map[string]string
	languages         map[string]string
	modulePath        string
}

// ResolveRelationships resolves only exact local/module references. It uses
// compact indexed facts emitted by AnalyzeFile rather than rescanning source.
func ResolveRelationships(entities []semantic.Entity) []semantic.Relationship {
	idx := buildIndexes(entities)
	var result []semantic.Relationship
	for _, entity := range entities {
		if entity.Analyzer != "" && entity.Analyzer != AnalyzerID {
			continue
		}
		switch entity.Kind {
		case KindImportSite:
			targets := resolveImportFiles(entity, idx)
			// A Go import denotes one package, which may contain several
			// indexed files. Without a package entity, emitting one edge per
			// file would violate import-site uniqueness, so keep only the
			// deterministic single-file case. Call resolution still searches
			// all files in the uniquely identified local package.
			if idx.languages[normalizePath(entity.File)] == "go" && len(targets) != 1 {
				continue
			}
			for _, target := range targets {
				from, ok := idx.files[entity.File]
				if !ok {
					continue
				}
				result = append(result, makeRelationship(from, target, RelationshipImports, entity.Name, entity))
			}
		case KindCallSite:
			target, ok := resolveSiteTarget(entity, idx)
			if !ok || !callableTarget(target, entity) {
				continue
			}
			from, ok := sourceEntity(entity, idx)
			if ok {
				result = append(result, makeRelationship(from, target, RelationshipCalls, entity.Name, entity))
			}
		case KindReferenceSite:
			target, ok := resolveSiteTarget(entity, idx)
			if !ok || !referenceTarget(target, entity) {
				continue
			}
			from, ok := sourceEntity(entity, idx)
			if ok && from.ID != target.ID {
				result = append(result, makeRelationship(from, target, RelationshipReferences, entity.Name, entity))
			}
		}
	}
	return deduplicate(result)
}

func buildIndexes(entities []semantic.Entity) graphIndexes {
	idx := graphIndexes{
		files: make(map[string]semantic.Entity), symbols: make(map[string]semantic.Entity),
		symbolsByFileName: make(map[string][]semantic.Entity), symbolsByFile: make(map[string][]semantic.Entity),
		exports: make(map[string]map[string][]semantic.Entity), imports: make(map[string][]semantic.Entity),
		packages: make(map[string]string), languages: make(map[string]string),
	}
	for _, entity := range entities {
		if entity.Analyzer != "" && entity.Analyzer != AnalyzerID {
			continue
		}
		switch entity.Kind {
		case KindCodeFile:
			idx.files[normalizePath(entity.File)] = entity
			language, _ := entity.Metadata["language"].(string)
			idx.languages[normalizePath(entity.File)] = language
			if packageName, ok := entity.Metadata["package"].(string); ok {
				idx.packages[normalizePath(entity.File)] = packageName
			}
			if modulePath, ok := entity.Metadata["module_path"].(string); ok && idx.modulePath == "" {
				idx.modulePath = modulePath
			}
		case KindCodeSymbol:
			idx.symbols[entity.ID] = entity
			path := normalizePath(entity.File)
			idx.symbolsByFileName[path+"\x00"+entity.Name] = append(idx.symbolsByFileName[path+"\x00"+entity.Name], entity)
			idx.symbolsByFile[path] = append(idx.symbolsByFile[path], entity)
			for _, exported := range stringValues(entity.Metadata["exports"]) {
				if idx.exports[path] == nil {
					idx.exports[path] = make(map[string][]semantic.Entity)
				}
				idx.exports[path][exported] = append(idx.exports[path][exported], entity)
			}
		case KindImportSite:
			idx.imports[normalizePath(entity.File)] = append(idx.imports[normalizePath(entity.File)], entity)
		}
	}
	return idx
}

func resolveImportFiles(site semantic.Entity, idx graphIndexes) []semantic.Entity {
	module, _ := site.Metadata["module"].(string)
	if module == "" {
		return nil
	}
	language := idx.languages[normalizePath(site.File)]
	if language == "go" {
		return resolveGoPackageFiles(module, idx)
	}
	if language == "lua" {
		module = strings.ReplaceAll(module, ".", "/")
		files := resolveExactFiles(module, []string{".lua", "/init.lua"}, idx.files)
		if len(files) != 1 {
			return nil
		}
		return files
	}
	if !strings.HasPrefix(module, ".") {
		return nil
	}
	files := resolveJSModule(site.File, module, idx.files)
	if len(files) != 1 {
		return nil
	}
	return files
}

func resolveSiteTarget(site semantic.Entity, idx graphIndexes) (semantic.Entity, bool) {
	meta := site.Metadata
	name, _ := meta["callee"].(string)
	if name == "" {
		name = site.Name
	}
	receiver, _ := meta["receiver"].(string)
	if shadowed, _ := meta["shadowed"].(bool); shadowed {
		return semantic.Entity{}, false
	}
	if localShadowed, _ := meta["local_binding_shadowed"].(bool); localShadowed {
		return semantic.Entity{}, false
	}
	if receiverShadowed, _ := meta["receiver_shadowed"].(bool); receiverShadowed {
		return semantic.Entity{}, false
	}
	path := normalizePath(site.File)
	imports := idx.imports[path]
	if importShadowed, _ := meta["import_shadowed"].(bool); importShadowed {
		imports = nil
	}
	if receiver != "" && receiver != "this" {
		for _, importSite := range imports {
			local, _ := importSite.Metadata["local"].(string)
			imported, _ := importSite.Metadata["imported"].(string)
			if idx.languages[path] == "go" && local == "" {
				module, _ := importSite.Metadata["module"].(string)
				local = filepath.Base(strings.TrimSuffix(module, "/"))
			}
			if local != receiver {
				continue
			}
			files := resolveImportFiles(importSite, idx)
			if idx.languages[path] == "go" {
				var candidates []semantic.Entity
				for _, file := range files {
					for _, candidate := range idx.symbolsByFileName[normalizePath(file.File)+"\x00"+name] {
						if candidate.Metadata["symbol_kind"] == "function" {
							candidates = append(candidates, candidate)
						}
					}
				}
				return exactlyOne(candidates)
			}
			if imported != "*" {
				continue
			}
			return uniqueExportTarget(files, name, idx)
		}
		// Unknown object/receiver types are intentionally unresolved.
		return semantic.Entity{}, false
	}
	if receiver == "this" {
		className, _ := meta["class_name"].(string)
		if className == "" {
			return semantic.Entity{}, false
		}
		candidates := make([]semantic.Entity, 0)
		for _, symbol := range idx.symbolsByFileName[path+"\x00"+name] {
			if symbol.Metadata["class_name"] == className && symbol.Metadata["symbol_kind"] == "method" {
				candidates = append(candidates, symbol)
			}
		}
		return exactlyOne(candidates)
	}
	for _, importSite := range imports {
		local, _ := importSite.Metadata["local"].(string)
		imported, _ := importSite.Metadata["imported"].(string)
		if local != name || imported == "*" {
			continue
		}
		files := resolveImportFiles(importSite, idx)
		return uniqueExportTarget(files, imported, idx)
	}
	language := idx.languages[path]
	if language == "go" {
		packageName := idx.packages[path]
		var candidates []semantic.Entity
		for file, packageValue := range idx.packages {
			if packageValue == packageName {
				for _, candidate := range idx.symbolsByFileName[file+"\x00"+name] {
					if candidate.Metadata["symbol_kind"] == "function" {
						candidates = append(candidates, candidate)
					}
				}
			}
		}
		return exactlyOne(candidates)
	}
	return exactlyOne(filterLexicalCandidates(idx.symbolsByFileName[path+"\x00"+name], meta))
}

func filterLexicalCandidates(candidates []semantic.Entity, metadata map[string]any) []semantic.Entity {
	bindingKind, _ := metadata["binding_kind"].(string)
	if bindingKind == "" || bindingKind == "import" {
		return candidates
	}
	filtered := make([]semantic.Entity, 0, len(candidates))
	for _, candidate := range candidates {
		kind, _ := candidate.Metadata["symbol_kind"].(string)
		switch bindingKind {
		case "function":
			if kind == "function" {
				filtered = append(filtered, candidate)
			}
		case "class":
			if kind == "class" {
				filtered = append(filtered, candidate)
			}
		case "variable":
			if kind != "method" {
				filtered = append(filtered, candidate)
			}
		}
	}
	return filtered
}

func callableTarget(target semantic.Entity, site semantic.Entity) bool {
	kind, _ := target.Metadata["symbol_kind"].(string)
	receiver, _ := site.Metadata["receiver"].(string)
	if receiver == "this" {
		return kind == "method"
	}
	if receiver != "" {
		// Namespace/package member calls are resolved only to callable
		// declarations. Methods require a statically known receiver and are
		// therefore not accepted here.
		return kind == "function"
	}
	return kind == "function"
}

func sourceEntity(site semantic.Entity, idx graphIndexes) (semantic.Entity, bool) {
	symbolID := site.SymbolID
	if symbolID == "" {
		symbolID, _ = site.Metadata["source_symbol_id"].(string)
	}
	if symbolID != "" {
		for _, symbol := range idx.symbolsByFile[normalizePath(site.File)] {
			if symbol.SymbolID == symbolID {
				return symbol, true
			}
		}
	}
	file, ok := idx.files[normalizePath(site.File)]
	return file, ok
}

func uniqueExportTarget(files []semantic.Entity, name string, idx graphIndexes) (semantic.Entity, bool) {
	var candidates []semantic.Entity
	for _, file := range files {
		path := normalizePath(file.File)
		fileCandidates := idx.exports[path][name]
		if len(fileCandidates) == 0 && idx.languages[path] == "lua" {
			fileCandidates = idx.symbolsByFileName[path+"\x00"+name]
		}
		candidates = append(candidates, fileCandidates...)
	}
	return exactlyOne(candidates)
}

func resolveGoPackageFiles(module string, idx graphIndexes) []semantic.Entity {
	module = strings.Trim(module, "\"")
	if idx.modulePath == "" {
		return nil
	}
	if module != idx.modulePath && !strings.HasPrefix(module, idx.modulePath+"/") {
		return nil
	}
	suffix := module
	suffix = strings.TrimPrefix(module, idx.modulePath)
	suffix = strings.TrimPrefix(suffix, "/")
	if suffix == "" {
		suffix = "."
	}
	var result []semantic.Entity
	for path, file := range idx.files {
		if idx.languages[path] != "go" {
			continue
		}
		if filepath.ToSlash(filepath.Dir(path)) == suffix {
			result = append(result, file)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].File < result[j].File })
	return result
}

func referenceTarget(target semantic.Entity, site semantic.Entity) bool {
	kind, _ := target.Metadata["symbol_kind"].(string)
	if kind == "method" {
		return false
	}
	bindingFound, hasBinding := site.Metadata["binding_found"].(bool)
	if !hasBinding || !bindingFound {
		// Generated facts normally carry binding_found. The fallback keeps
		// manually constructed legacy facts conservative by rejecting methods
		// while allowing ordinary indexed declarations.
		return kind == "function" || kind == "class" || kind == "constant" || kind == "type"
	}
	bindingKind, _ := site.Metadata["binding_kind"].(string)
	switch bindingKind {
	case "function":
		return kind == "function"
	case "class":
		return kind == "class"
	case "import":
		return kind == "function" || kind == "class" || kind == "constant" || kind == "type"
	case "variable":
		return kind == "function" || kind == "class" || kind == "constant" || kind == "type"
	default:
		return false
	}
}

func resolveJSModule(importer, module string, files map[string]semantic.Entity) []semantic.Entity {
	base := normalizePath(filepath.Join(filepath.Dir(importer), filepath.FromSlash(module)))
	return resolveExactFiles(base, []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", "/index.ts", "/index.tsx", "/index.js", "/index.jsx"}, files)
}

func resolveExactFiles(base string, suffixes []string, files map[string]semantic.Entity) []semantic.Entity {
	var result []semantic.Entity
	seen := make(map[string]struct{})
	for _, suffix := range suffixes {
		candidate := base
		if strings.HasPrefix(suffix, "/") {
			candidate += suffix
		} else if filepath.Ext(base) == "" {
			candidate += suffix
		} else {
			candidate = base
		}
		candidate = normalizePath(candidate)
		if file, ok := files[candidate]; ok {
			if _, exists := seen[candidate]; !exists {
				seen[candidate] = struct{}{}
				result = append(result, file)
			}
		}
	}
	return result
}

func exactlyOne(candidates []semantic.Entity) (semantic.Entity, bool) {
	if len(candidates) != 1 {
		return semantic.Entity{}, false
	}
	return candidates[0], true
}

func stringValues(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func makeRelationship(from, to semantic.Entity, kind, name string, site semantic.Entity) semantic.Relationship {
	return semantic.Relationship{
		ID: semantic.StableID("generic_relationship", from.ID, to.ID, kind, site.ID), Analyzer: AnalyzerID,
		Repo: from.Repo, FromEntityID: from.ID, ToEntityID: to.ID, Kind: kind, Name: name,
		Confidence: 1, File: site.File, Line: site.Line,
	}
}

func deduplicate(input []semantic.Relationship) []semantic.Relationship {
	seen := make(map[string]struct{}, len(input))
	result := make([]semantic.Relationship, 0, len(input))
	for _, relationship := range input {
		if _, ok := seen[relationship.ID]; ok {
			continue
		}
		seen[relationship.ID] = struct{}{}
		result = append(result, relationship)
	}
	return result
}
