package fivem

import (
	"context"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/lua"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

type luaCall struct {
	Node      *sitter.Node
	Callee    string
	Args      *sitter.Node
	Arguments []*sitter.Node
	Source    []byte
}

func analyzeLuaFile(ctx context.Context, input semantic.FileInput) (semantic.Result, error) {
	root, err := sitter.ParseCtx(ctx, input.Content, lua.GetLanguage())
	if err != nil {
		return semantic.Result{}, err
	}
	if root == nil {
		return semantic.Result{}, nil
	}

	result := semantic.Result{}
	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "function_call" {
			return
		}
		callee, args := callParts(node, input.Content)
		if args == nil {
			return
		}
		call := luaCall{Node: node, Callee: callee, Args: args, Source: input.Content}
		call.Arguments = argumentNodes(args)
		entities := analyzeCall(call, input)
		result.Entities = append(result.Entities, entities...)
	})
	return result, nil
}

func analyzeCall(call luaCall, input semantic.FileInput) []semantic.Entity {
	callee := call.Callee
	switch callee {
	case "RegisterNetEvent":
		return eventEntities(call, input, KindEventRegistration, true)
	case "AddEventHandler":
		return eventEntities(call, input, KindEventHandler, false)
	case "TriggerEvent", "TriggerServerEvent", "TriggerClientEvent", "TriggerLatentServerEvent", "TriggerLatentClientEvent":
		return eventEntities(call, input, KindEventTrigger, false)
	case "RegisterNUICallback":
		return namedEntities(call, input, KindNUICallback, "nui_callback")
	case "RegisterCommand":
		return namedEntities(call, input, KindCommandRegistration, "command_registration")
	case "exports":
		return exportDefinition(call, input)
	case "lib.callback.register":
		return namedEntities(call, input, KindCallbackRegistration, "callback_registration")
	case "lib.callback", "lib.callback.await", "lib.callback.call", "lib.callback.trigger":
		return namedEntities(call, input, KindCallbackCall, callee)
	default:
		if resource, name, ok := exportCall(callee); ok {
			return []semantic.Entity{makeEntity(call, input, KindExportCall, name, false, map[string]any{
				"resource":  resource,
				"operation": "export_call",
			})}
		}
	}
	return nil
}

func eventEntities(call luaCall, input semantic.FileInput, kind string, allowInlineHandler bool) []semantic.Entity {
	name, literal := firstArgument(call, 0)
	metadata := map[string]any{"operation": call.Callee}
	if !literal {
		metadata["expression"] = name
	}
	entity := makeEntity(call, input, kind, name, !literal, metadata)
	result := []semantic.Entity{entity}
	if allowInlineHandler && hasFunctionArgument(call, 1) {
		result = append(result, makeEntity(call, input, KindEventHandler, name, !literal, map[string]any{
			"operation": "RegisterNetEvent_inline",
		}))
	}
	return result
}

func namedEntities(call luaCall, input semantic.FileInput, kind, operation string) []semantic.Entity {
	name, literal := firstArgument(call, 0)
	metadata := map[string]any{"operation": operation}
	if !literal {
		metadata["expression"] = name
	}
	return []semantic.Entity{makeEntity(call, input, kind, name, !literal, metadata)}
}

func exportDefinition(call luaCall, input semantic.FileInput) []semantic.Entity {
	name, literal := firstArgument(call, 0)
	if !literal || !hasCallableArgument(call, 1) {
		return nil
	}
	return []semantic.Entity{makeEntity(call, input, KindExportDefinition, name, false, map[string]any{
		"resource":  input.Resource,
		"operation": "exports",
	})}
}

func makeEntity(call luaCall, input semantic.FileInput, kind, name string, dynamic bool, metadata map[string]any) semantic.Entity {
	line := int(call.Node.StartPoint().Row) + 1
	endLine := int(call.Node.EndPoint().Row) + 1
	symbolID := containingSymbol(input.Symbols, line)
	metadata["byte_offset"] = int(call.Node.StartByte())
	entity := semantic.Entity{
		Analyzer:  semantic.AnalyzerFiveM,
		Repo:      input.Repo,
		File:      input.File,
		SymbolID:  symbolID,
		Kind:      kind,
		Name:      name,
		Framework: FrameworkFiveM,
		Side:      semantic.NormalizeSide(input.Side),
		Line:      line,
		EndLine:   endLine,
		Dynamic:   dynamic,
		Metadata:  metadata,
	}
	entity.ID = semantic.StableID("semantic", input.Repo, input.File, kind, name, strconv.Itoa(line), strconv.Itoa(int(call.Node.StartByte())))
	return entity
}

func containingSymbol(symbols []parser.Symbol, line int) string {
	best := ""
	bestSpan := int(^uint(0) >> 1)
	for _, symbol := range symbols {
		if line < symbol.Line || line > symbol.EndLine {
			continue
		}
		span := symbol.EndLine - symbol.Line
		if span < bestSpan {
			best = symbol.ID
			bestSpan = span
		}
	}
	return best
}

func firstArgument(call luaCall, index int) (string, bool) {
	if index >= len(call.Arguments) {
		return "", false
	}
	node := call.Arguments[index]
	if node.Type() != "string" {
		return nodeText(call.Source, node.StartByte(), node.EndByte()), false
	}
	value, ok := luaString(nodeText(call.Source, node.StartByte(), node.EndByte()))
	return value, ok
}

func hasFunctionArgument(call luaCall, index int) bool {
	return index < len(call.Arguments) && call.Arguments[index].Type() == "function"
}

func hasCallableArgument(call luaCall, index int) bool {
	if index >= len(call.Arguments) {
		return false
	}
	argument := call.Arguments[index].Type()
	return argument == "function" || argument == "identifier"
}

func exportCall(callee string) (resource, name string, ok bool) {
	if !strings.HasPrefix(callee, "exports") {
		return "", "", false
	}
	rest := strings.TrimPrefix(callee, "exports")
	if rest == "" || (!strings.HasPrefix(rest, ".") && !strings.HasPrefix(rest, "[")) {
		return "", "", false
	}
	if strings.HasPrefix(rest, ".") {
		rest = rest[1:]
		end := strings.IndexByte(rest, ':')
		if end < 0 {
			return "", "", false
		}
		resource = rest[:end]
		name = rest[end+1:]
	} else {
		close := strings.Index(rest, "]")
		if close < 0 {
			return "", "", false
		}
		resourceValue := strings.TrimSpace(rest[1:close])
		resource, ok = luaString(resourceValue)
		if !ok {
			return "", "", false
		}
		rest = rest[close+1:]
		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		name = rest[1:]
	}
	if resource == "" || name == "" || strings.ContainsAny(resource, "[]:'\"") || strings.ContainsAny(name, "[]:'\"") {
		return "", "", false
	}
	return resource, name, true
}

// ResolveRelationships links exact literal event and callback names only.
// Dynamic entities are intentionally excluded from all target resolution.
func ResolveRelationships(entities []semantic.Entity) []semantic.Relationship {
	var triggers, handlers, registrations, callbackCalls, callbackRegistrations []semantic.Entity
	var resources, dependencies []semantic.Entity
	for _, entity := range entities {
		switch entity.Kind {
		case KindEventTrigger:
			triggers = append(triggers, entity)
		case KindEventHandler:
			handlers = append(handlers, entity)
		case KindEventRegistration:
			registrations = append(registrations, entity)
		case KindCallbackCall:
			callbackCalls = append(callbackCalls, entity)
		case KindCallbackRegistration:
			callbackRegistrations = append(callbackRegistrations, entity)
		case KindManifestResource:
			resources = append(resources, entity)
		case KindManifestDependency:
			dependencies = append(dependencies, entity)
		}
	}
	result := make([]semantic.Relationship, 0)
	for _, resource := range resources {
		for _, dependency := range dependencies {
			if resource.Repo == dependency.Repo {
				result = append(result, relationship(resource, dependency, RelationshipDependsOn))
			}
		}
	}
	for _, trigger := range triggers {
		if trigger.Dynamic || trigger.Name == "" {
			continue
		}
		for _, handler := range handlers {
			if handler.Dynamic || handler.Name != trigger.Name {
				continue
			}
			operation, _ := trigger.Metadata["operation"].(string)
			if operation == "TriggerEvent" {
				if !compatibleLocalEventSide(trigger, handler) {
					continue
				}
			} else if !networkHandlerBackedByRegistration(trigger, handler, registrations) {
				continue
			}
			result = append(result, relationship(trigger, handler, RelationshipTriggers))
		}
	}
	for _, handler := range handlers {
		if handler.Dynamic || handler.Name == "" {
			continue
		}
		for _, registration := range registrations {
			if registration.Name == handler.Name && !registration.Dynamic && registration.Repo == handler.Repo && compatibleHandlerRegistrationSide(handler, registration) {
				result = append(result, relationship(handler, registration, RelationshipRegisters))
			}
		}
	}
	for _, call := range callbackCalls {
		if call.Dynamic || call.Name == "" {
			continue
		}
		for _, registration := range callbackRegistrations {
			if registration.Dynamic || registration.Name != call.Name || !compatibleCallbackSide(call, registration) {
				continue
			}
			result = append(result, relationship(call, registration, RelationshipCalls))
		}
	}
	return result
}

func compatibleLocalEventSide(trigger, handler semantic.Entity) bool {
	return sidesCompatible(semantic.NormalizeSide(trigger.Side), semantic.NormalizeSide(handler.Side))
}

func networkHandlerBackedByRegistration(trigger, handler semantic.Entity, registrations []semantic.Entity) bool {
	target := networkTargetSide(trigger)
	if target == "" || !sidesCompatible(target, semantic.NormalizeSide(handler.Side)) {
		return false
	}
	for _, registration := range registrations {
		if registration.Dynamic || registration.Name != trigger.Name || registration.Repo != trigger.Repo {
			continue
		}
		if sidesCompatible(target, semantic.NormalizeSide(registration.Side)) {
			return true
		}
	}
	return false
}

func networkTargetSide(trigger semantic.Entity) string {
	operation, _ := trigger.Metadata["operation"].(string)
	switch operation {
	case "TriggerServerEvent", "TriggerLatentServerEvent":
		return "server"
	case "TriggerClientEvent", "TriggerLatentClientEvent":
		return "client"
	default:
		return ""
	}
}

func compatibleHandlerRegistrationSide(handler, registration semantic.Entity) bool {
	return sidesCompatible(semantic.NormalizeSide(handler.Side), semantic.NormalizeSide(registration.Side))
}

func sidesCompatible(expected, actual string) bool {
	if expected == "unknown" || actual == "unknown" {
		return expected == actual
	}
	return expected == actual || expected == "shared" || actual == "shared"
}

func compatibleCallbackSide(call, registration semantic.Entity) bool {
	side := semantic.NormalizeSide(call.Side)
	if side == "client" {
		return semantic.NormalizeSide(registration.Side) == "server" || semantic.NormalizeSide(registration.Side) == "shared"
	}
	if side == "server" {
		return semantic.NormalizeSide(registration.Side) == "client" || semantic.NormalizeSide(registration.Side) == "shared"
	}
	return false
}

func relationship(from, to semantic.Entity, kind string) semantic.Relationship {
	return semantic.Relationship{
		Analyzer:     semantic.AnalyzerFiveM,
		ID:           semantic.StableID("relationship", from.ID, to.ID, kind),
		Repo:         from.Repo,
		FromEntityID: from.ID,
		ToEntityID:   to.ID,
		Kind:         kind,
		Name:         from.Name,
		Confidence:   1,
		File:         from.File,
		Line:         from.Line,
	}
}
