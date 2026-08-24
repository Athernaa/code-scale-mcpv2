package framework

import (
	"context"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/lua"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

type luaAssignment struct {
	name, rhs       string
	start, rhsStart uint32
}
type luaScope struct {
	start, end  uint32
	parent      *luaScope
	repo, file  string
	params      map[string]bool
	assignments []luaAssignment
}

func (a *Analyzer) analyzeLuaFile(ctx context.Context, input semantic.FileInput, state *analysisState, owner resourceOwner, _ []semantic.Entity) (semantic.Result, error) {
	root, err := sitter.ParseCtx(ctx, input.Content, lua.GetLanguage())
	if err != nil {
		return semantic.Result{}, err
	}
	if root == nil {
		return semantic.Result{}, nil
	}
	if state == nil {
		state = &analysisState{input: semantic.RepositoryInput{Repo: input.Repo, Resource: input.Resource}, registry: a.registry, ownerByFile: map[string]resourceOwner{input.File: owner}, providers: map[string]semantic.Entity{}, providerAPIs: map[string]map[string]bool{}, manifestEvidence: map[string]Evidence{}}
	}
	if owner.name == "" {
		owner.name = input.Resource
	}
	if owner.name == "" {
		owner.name = input.Repo
	}
	scopes := buildLuaScopes(root, input.Content, input.Repo, input.File)
	var result semantic.Result
	walkLua(root, func(node *sitter.Node) {
		if node.Type() != "function_call" {
			return
		}
		prefix := luaCallPrefix(node, input.Content)
		if prefix == "" {
			return
		}
		args := luaLiteralArguments(node.ChildByFieldName("args"), input.Content)
		line := int(node.StartPoint().Row) + 1
		calleeResource, api, exportOK := parseExportCallee(prefix)
		if exportOK {
			framework := state.frameworkForTarget(calleeResource)
			call := state.newCall(input, owner, node, api, calleeResource, "export", framework, args)
			state.addCall(&result, call, args)
			return
		}
		receiver, member := luaCallee(prefix)
		if receiver == "" || member == "" {
			return
		}
		scope := scopeFor(scopes, node.StartByte())
		if (receiver == "lib" || receiver == "lib.callback") && state.oxLibEvidence(owner) && !state.shadowed(scope, "lib", node.StartByte()) {
			apiName := member
			if receiver == "lib.callback" {
				apiName = "callback." + member
			}
			call := state.newCall(input, owner, node, apiName, "ox_lib", "library", "ox_lib", args)
			state.addCall(&result, call, args)
			return
		}
		base := receiver
		if dot := strings.IndexAny(base, ".:"); dot >= 0 {
			base = base[:dot]
		}
		origin := resolveBinding(scope, base, node.StartByte(), map[string]bool{})
		if !origin.Valid || origin.Ambiguous {
			return
		}
		framework := origin.Framework
		if framework == "" {
			framework = state.frameworkForTarget(origin.Resource)
		}
		call := state.newCall(input, owner, node, member, origin.Resource, "object_method", framework, args)
		if call.Metadata == nil {
			call.Metadata = map[string]any{}
		}
		call.Metadata["object_origin"] = true
		call.Metadata["origin_factory_api"] = origin.FactoryAPI
		call.Metadata["origin_factory_id"] = origin.FactoryID
		state.addCall(&result, call, args)
		_ = line
	})
	return result, nil
}

func (s *analysisState) newCall(input semantic.FileInput, owner resourceOwner, node *sitter.Node, api, target, mechanism, framework string, args []literalValue) semantic.Entity {
	if owner.name == "" {
		owner.name = input.Resource
	}
	if owner.path == "" {
		owner.path = owner.name
	}
	if owner.id == "" {
		owner.id = semantic.StableID("workspace_resource", input.Repo, owner.path)
	}
	metadata := map[string]any{
		"source_resource": owner.name, "source_resource_path": owner.path, "source_resource_id": owner.id,
		"target_resource": target, "api": api, "mechanism": mechanism, "raw_operation": api,
		"source_offset":     int(node.StartByte()),
		"provider_verified": false,
	}
	if framework == "" {
		framework = FrameworkUnknown
	}
	call := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: input.Repo, File: input.File, SymbolID: containingSymbol(input.Symbols, int(node.StartPoint().Row)+1), Kind: KindAPICall, Name: api, Framework: framework, Side: semantic.NormalizeSide(input.Side), Line: int(node.StartPoint().Row) + 1, EndLine: int(node.EndPoint().Row) + 1, Metadata: metadata}
	call.ID = semantic.StableID("framework_call", input.Repo, input.File, mechanism, api, strconv.Itoa(int(node.StartByte())))
	return call
}

func (s *analysisState) addCall(result *semantic.Result, call semantic.Entity, args []literalValue) {
	if operation, metadata, ok := s.registry.operation(call.Framework, call.Name, args); ok {
		call.Metadata["operation"] = operation
		for key, value := range metadata {
			call.Metadata[key] = value
		}
	}
	for _, existing := range result.Entities {
		if existing.ID == call.ID {
			return
		}
	}
	result.Entities = append(result.Entities, call)
}

func (s *analysisState) frameworkForTarget(target string) string {
	if target == "" {
		return FrameworkUnknown
	}
	var found string
	paths := map[string]bool{}
	for _, provider := range s.providers {
		resource, _ := provider.Metadata["provider_resource"].(string)
		if resource != target {
			continue
		}
		path, _ := provider.Metadata["provider_resource_path"].(string)
		paths[path] = true
		if found == "" {
			found = provider.Framework
		} else if found != provider.Framework {
			return FrameworkCustom
		}
	}
	if len(paths) > 0 {
		if len(paths) > 1 {
			return FrameworkUnknown
		}
		return found
	}
	switch strings.ToLower(target) {
	case "qbx_core":
		return "qbx"
	case "qb-core":
		return "qbcore"
	case "es_extended":
		return "esx"
	case "ox_lib":
		return "ox_lib"
	case "ox_inventory":
		return "ox_inventory"
	case "ox_target":
		return "ox_target"
	default:
		return FrameworkCustom
	}
}

func (s *analysisState) oxLibEvidence(owner resourceOwner) bool {
	evidence := s.manifestEvidence[owner.path]
	if len(evidence.Dependencies) == 0 && len(evidence.ExternalRefs) == 0 && len(s.manifestEvidence) == 1 {
		for _, value := range s.manifestEvidence {
			evidence = value
		}
	}
	for _, value := range append(evidence.Dependencies, evidence.ExternalRefs...) {
		if value == "ox_lib" || strings.HasPrefix(value, "@ox_lib/") {
			return true
		}
	}
	return false
}

func (s *analysisState) shadowed(scope *luaScope, name string, position uint32) bool {
	for current := scope; current != nil; current = current.parent {
		if current.params[name] {
			return true
		}
		for _, assignment := range current.assignments {
			if assignment.name == name && assignment.start <= position {
				return true
			}
		}
	}
	return false
}

func buildLuaScopes(root *sitter.Node, source []byte, repo, file string) []*luaScope {
	rootScope := &luaScope{start: 0, end: uint32(len(source)), repo: repo, file: file, params: map[string]bool{}}
	scopes := []*luaScope{rootScope}
	seenFunctions := map[string]bool{}
	walkLua(root, func(node *sitter.Node) {
		if node.Type() != "function_statement" && node.Type() != "function_definition" && node.Type() != "function" {
			return
		}
		key := strconv.FormatUint(uint64(node.StartByte()), 10) + ":" + strconv.FormatUint(uint64(node.EndByte()), 10)
		if seenFunctions[key] {
			return
		}
		seenFunctions[key] = true
		parent := scopeFor(scopes, node.StartByte())
		scope := &luaScope{start: node.StartByte(), end: node.EndByte(), parent: parent, repo: repo, file: file, params: map[string]bool{}}
		if params := node.ChildByFieldName("parameters"); params != nil {
			walkLua(params, func(child *sitter.Node) {
				if child.Type() == "identifier" {
					scope.params[nodeText(source, child)] = true
				}
			})
		} else {
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() != "parameter_list" {
					continue
				}
				walkLua(child, func(parameter *sitter.Node) {
					if parameter.Type() == "identifier" {
						scope.params[nodeText(source, parameter)] = true
					}
				})
				break
			}
		}
		scopes = append(scopes, scope)
	})
	walkLua(root, func(node *sitter.Node) {
		if node.Type() != "variable_declaration" && node.Type() != "assignment_statement" {
			return
		}
		text := nodeText(source, node)
		eq := strings.IndexByte(text, '=')
		if eq < 0 {
			return
		}
		left := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text[:eq]), "local"))
		name := firstIdentifier(left)
		if name == "" {
			return
		}
		parent := scopeFor(scopes, node.StartByte())
		rhs := strings.TrimSpace(text[eq+1:])
		rhsStart := node.StartByte() + uint32(eq+1)
		for rhsStart < node.EndByte() && (source[rhsStart] == ' ' || source[rhsStart] == '\t' || source[rhsStart] == '\r' || source[rhsStart] == '\n') {
			rhsStart++
		}
		parent.assignments = append(parent.assignments, luaAssignment{name: name, rhs: rhs, start: node.StartByte(), rhsStart: rhsStart})
	})
	return scopes
}

func resolveBinding(scope *luaScope, name string, position uint32, visiting map[string]bool) origin {
	if name == "" || visiting[name] {
		return origin{Ambiguous: true}
	}
	visiting[name] = true
	defer delete(visiting, name)
	for current := scope; current != nil; current = current.parent {
		if current.params[name] {
			return origin{Ambiguous: true}
		}
		var found origin
		seen := false
		for i := range current.assignments {
			assignment := current.assignments[i]
			if assignment.name != name || assignment.start > position {
				continue
			}
			next := parseOrigin(assignment.rhs, current, assignment.start, assignment.rhsStart, visiting)
			if !seen {
				found, seen = next, true
				continue
			}
			if !sameOrigin(found, next) {
				return origin{Ambiguous: true}
			}
		}
		if seen {
			return found
		}
	}
	return origin{Ambiguous: true}
}

func sameOrigin(left, right origin) bool {
	return left.Valid && right.Valid && !left.Ambiguous && !right.Ambiguous && left.Resource == right.Resource && left.Framework == right.Framework && left.FactoryAPI == right.FactoryAPI && left.FactoryID == right.FactoryID
}

func parseOrigin(value string, scope *luaScope, position, rhsStart uint32, visiting map[string]bool) origin {
	value = strings.TrimSpace(value)
	if resource, api, ok := parseExportCallee(value); ok {
		factoryID := ""
		if scope != nil && scope.repo != "" && scope.file != "" {
			factoryID = semantic.StableID("framework_call", scope.repo, scope.file, "export", api, strconv.Itoa(int(rhsStart)))
		}
		return origin{Resource: resource, FactoryAPI: api, FactoryID: factoryID, Valid: true}
	}
	if isIdentifier(value) {
		return resolveBinding(scope, value, position, visiting)
	}
	base, member := luaCallee(strings.TrimSuffix(strings.Split(value, "(")[0], ")"))
	if base != "" && member != "" {
		baseName := base
		if dot := strings.IndexAny(baseName, ".:"); dot >= 0 {
			baseName = baseName[:dot]
		}
		parent := resolveBinding(scope, baseName, position, visiting)
		if parent.Valid && !parent.Ambiguous {
			parent.FactoryAPI = member
			if scope != nil && scope.repo != "" && scope.file != "" {
				parent.FactoryID = semantic.StableID("framework_call", scope.repo, scope.file, "object_method", member, strconv.Itoa(int(rhsStart)))
			}
			return parent
		}
	}
	return origin{Ambiguous: true}
}

func scopeFor(scopes []*luaScope, position uint32) *luaScope {
	var best *luaScope
	for _, scope := range scopes {
		contains := position >= scope.start && position < scope.end
		if scope.parent == nil {
			contains = position >= scope.start && position <= scope.end
		}
		if contains && (best == nil || scope.end-scope.start < best.end-best.start) {
			best = scope
		}
	}
	return best
}

func luaCallPrefix(node *sitter.Node, source []byte) string {
	text := nodeText(source, node)
	if i := strings.IndexByte(text, '('); i >= 0 {
		return strings.TrimSpace(text[:i])
	}
	if prefix := node.ChildByFieldName("prefix"); prefix != nil {
		return strings.TrimSpace(nodeText(source, prefix))
	}
	return strings.TrimSpace(text)
}

func luaLiteralArguments(args *sitter.Node, source []byte) []literalValue {
	if args == nil {
		return nil
	}
	var values []literalValue
	walkLua(args, func(node *sitter.Node) {
		if node == args || node.Parent() != args {
			return
		}
		switch node.Type() {
		case "string":
			if value, ok := luaString(nodeText(source, node)); ok {
				values = append(values, literalValue{Kind: "string", Value: value})
			}
		case "number":
			values = append(values, literalValue{Kind: "number", Value: nodeText(source, node)})
		}
	})
	return values
}

func parseExportCallee(value string) (resource, api string, ok bool) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "[") && !strings.Contains(value, "'") && !strings.Contains(value, "\"") {
		return "", "", false
	}
	if !strings.HasPrefix(value, "exports") {
		return "", "", false
	}
	rest := strings.TrimPrefix(value, "exports")
	if strings.HasPrefix(rest, ".") {
		rest = rest[1:]
		index := strings.IndexByte(rest, ':')
		if index < 1 {
			return "", "", false
		}
		resource, api = rest[:index], rest[index+1:]
	} else if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return "", "", false
		}
		resource, ok = luaString(strings.TrimSpace(rest[1:end]))
		if !ok {
			return "", "", false
		}
		rest = rest[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		api = rest[1:]
	} else {
		return "", "", false
	}
	api = strings.TrimSpace(api)
	if index := strings.IndexByte(api, '('); index >= 0 {
		api = api[:index]
	}
	if resource == "" || api == "" || strings.ContainsAny(resource, "[]:'\"") || strings.ContainsAny(api, "[]:'\"") {
		return "", "", false
	}
	return resource, api, true
}

func luaCallee(value string) (receiver, member string) {
	value = strings.TrimSpace(value)
	if i := strings.LastIndexByte(value, ':'); i >= 0 {
		return value[:i], value[i+1:]
	}
	if i := strings.LastIndexByte(value, '.'); i >= 0 {
		return value[:i], value[i+1:]
	}
	return "", value
}
func firstIdentifier(value string) string {
	value = strings.TrimSpace(value)
	for i, r := range value {
		if i == 0 && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_') {
			j := i + 1
			for j < len(value) && ((value[j] >= 'A' && value[j] <= 'Z') || (value[j] >= 'a' && value[j] <= 'z') || (value[j] >= '0' && value[j] <= '9') || value[j] == '_') {
				j++
			}
			return value[i:j]
		}
		if i > 0 && (r == ',' || r == ' ') {
			continue
		}
	}
	return ""
}
func isIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') || (i == 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
func luaString(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return "", false
	}
	if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
		return value[1 : len(value)-1], true
	}
	return "", false
}
func nodeText(source []byte, node *sitter.Node) string {
	if node == nil {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}
func walkLua(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		walkLua(node.Child(i), visit)
	}
}
func containingSymbol(symbols []parser.Symbol, line int) string {
	best, span := "", int(^uint(0)>>1)
	for _, symbol := range symbols {
		if line >= symbol.Line && line <= symbol.EndLine && symbol.EndLine-symbol.Line < span {
			best, span = symbol.ID, symbol.EndLine-symbol.Line
		}
	}
	return best
}
