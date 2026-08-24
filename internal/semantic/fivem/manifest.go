package fivem

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/lua"
)

type manifestInfo struct {
	FXVersion         string
	Game              string
	UIPage            string
	ClientScripts     []string
	ServerScripts     []string
	SharedScripts     []string
	Dependencies      []string
	DependencyLines   map[string]int
	DependencySources map[string][]string
	Exports           []manifestExport
}

type manifestExport struct {
	Name string
	Side string
	Line int
}

func parseManifest(file string, source []byte) (manifestInfo, error) {
	root, err := sitter.ParseCtx(context.Background(), source, lua.GetLanguage())
	if err != nil {
		return manifestInfo{}, err
	}
	if root == nil {
		return manifestInfo{}, fmt.Errorf("empty syntax tree")
	}

	result := manifestInfo{
		DependencyLines:   make(map[string]int),
		DependencySources: make(map[string][]string),
	}
	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "function_call" {
			return
		}
		callee, args := callParts(node, source)
		values := literalArguments(args, source)
		line := int(node.StartPoint().Row) + 1
		switch callee {
		case "fx_version":
			result.FXVersion = first(values)
		case "game":
			result.Game = first(values)
		case "ui_page":
			result.UIPage = first(values)
		case "client_script", "client_scripts":
			result.ClientScripts = append(result.ClientScripts, values...)
			result.addExternalScriptDependencies(values, line)
		case "server_script", "server_scripts":
			result.ServerScripts = append(result.ServerScripts, values...)
			result.addExternalScriptDependencies(values, line)
		case "shared_script", "shared_scripts":
			result.SharedScripts = append(result.SharedScripts, values...)
			result.addExternalScriptDependencies(values, line)
		case "dependency", "dependencies":
			for _, value := range values {
				result.addDependency(value, line, "explicit_dependency")
			}
		case "export", "exports":
			result.addExports(values, "client", line)
		case "server_export", "server_exports":
			result.addExports(values, "server", line)
		}
	})
	return result, nil
}

func (m *manifestInfo) addDependency(name string, line int, source string) {
	if name == "" {
		return
	}
	m.Dependencies = appendUnique(m.Dependencies, name)
	if _, ok := m.DependencyLines[name]; !ok {
		m.DependencyLines[name] = line
	}
	m.DependencySources[name] = appendUnique(m.DependencySources[name], source)
}

func (m *manifestInfo) addExternalScriptDependencies(values []string, line int) {
	for _, value := range values {
		if !strings.HasPrefix(value, "@") {
			continue
		}
		resource := strings.TrimPrefix(value, "@")
		if slash := strings.IndexByte(resource, '/'); slash >= 0 {
			resource = resource[:slash]
		}
		m.addDependency(resource, line, "external_script_reference")
	}
}

func (m *manifestInfo) addExports(values []string, side string, line int) {
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, existing := range m.Exports {
			if existing.Name == value && existing.Side == side {
				goto next
			}
		}
		m.Exports = append(m.Exports, manifestExport{Name: value, Side: side, Line: line})
	next:
	}
}

func walkNodes(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		walkNodes(node.Child(i), visit)
	}
}

func callParts(node *sitter.Node, source []byte) (string, *sitter.Node) {
	if node == nil || node.Type() != "function_call" {
		return "", nil
	}
	args := node.ChildByFieldName("args")
	raw := strings.TrimSpace(nodeText(source, node.StartByte(), node.EndByte()))
	if open := strings.IndexByte(raw, '('); open >= 0 {
		return strings.TrimSpace(raw[:open]), args
	}
	if args != nil && args.StartByte() > node.StartByte() {
		prefix := strings.TrimSpace(nodeText(source, node.StartByte(), args.StartByte()))
		if prefix != "" {
			return prefix, args
		}
	}
	if space := strings.IndexAny(raw, " \t\r\n"); space >= 0 {
		return strings.TrimSpace(raw[:space]), args
	}
	return raw, args
}

func literalArguments(args *sitter.Node, source []byte) []string {
	if args == nil {
		return nil
	}
	if args.Type() == "string_argument" {
		if value, ok := luaString(nodeText(source, args.StartByte(), args.EndByte())); ok {
			return []string{value}
		}
		return nil
	}
	var values []string
	for _, child := range argumentNodes(args) {
		if child == nil {
			continue
		}
		if child.Type() == "string" {
			if value, ok := luaString(nodeText(source, child.StartByte(), child.EndByte())); ok {
				values = append(values, value)
			}
			continue
		}
		if child.Type() == "table_constructor" || child.Type() == "table_argument" {
			walkNodes(child, func(node *sitter.Node) {
				if node.Type() != "string" {
					return
				}
				if value, ok := luaString(nodeText(source, node.StartByte(), node.EndByte())); ok {
					values = append(values, value)
				}
			})
		}
	}
	return values
}

func argumentNodes(args *sitter.Node) []*sitter.Node {
	if args == nil {
		return nil
	}
	if args.Type() == "string_argument" {
		if args.NamedChildCount() == 0 {
			return nil
		}
		return []*sitter.Node{args.NamedChild(0)}
	}
	if args.Type() == "table_argument" {
		return []*sitter.Node{args}
	}
	result := make([]*sitter.Node, 0, args.NamedChildCount())
	for i := 0; i < int(args.NamedChildCount()); i++ {
		result = append(result, args.NamedChild(i))
	}
	return result
}

func luaString(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return "", false
	}
	if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
		decoded, err := strconv.Unquote(value)
		if err == nil {
			return decoded, true
		}
		return value[1 : len(value)-1], true
	}
	if strings.HasPrefix(value, "[[") && strings.HasSuffix(value, "]]") {
		return value[2 : len(value)-2], true
	}
	return "", false
}

func nodeText(source []byte, start, end uint32) string {
	if start > end || end > uint32(len(source)) {
		return ""
	}
	return string(source[start:end])
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
