package generic

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

type scopeKey struct {
	start uint32
	end   uint32
	typ   string
}

type lexicalBinding struct {
	name  string
	scope scopeKey
	kind  string
}

type lexicalBindings struct {
	byScope map[scopeKey][]lexicalBinding
	imports map[string]bool
}

func newLexicalBindings(root *sitter.Node) *lexicalBindings {
	return &lexicalBindings{byScope: make(map[scopeKey][]lexicalBinding), imports: make(map[string]bool)}
}

func (b *lexicalBindings) add(name string, scope *sitter.Node, kind string) {
	name = strings.TrimSpace(name)
	if name == "" || scope == nil {
		return
	}
	key := scopeKey{start: scope.StartByte(), end: scope.EndByte(), typ: scope.Type()}
	b.byScope[key] = append(b.byScope[key], lexicalBinding{name: name, scope: key, kind: kind})
	if kind == "import" {
		b.imports[name] = true
	}
}

func (b *lexicalBindings) nearest(node *sitter.Node, name string) (kind string, shadowed bool) {
	if b == nil || node == nil {
		return "", false
	}
	for current := node; current != nil; current = current.Parent() {
		key := scopeKey{start: current.StartByte(), end: current.EndByte(), typ: current.Type()}
		matches := b.byScope[key]
		if len(matches) == 0 {
			continue
		}
		local := false
		for _, binding := range matches {
			if binding.name != name {
				continue
			}
			if kind == "" {
				kind = binding.kind
			}
			if binding.kind != "import" {
				local = true
			}
		}
		if kind != "" {
			return kind, local
		}
	}
	return "", false
}

func (f *fileFacts) annotateBinding(node *sitter.Node, name string, metadata map[string]any) {
	if f.bindings == nil || name == "" {
		return
	}
	kind, shadowed := f.bindings.nearest(node, name)
	if kind == "" {
		metadata["binding_found"] = false
		return
	}
	metadata["binding_found"] = true
	metadata["binding_kind"] = kind
	if !shadowed {
		return
	}
	if f.bindings.imports[name] {
		metadata["import_shadowed"] = true
	}
	if kind != "function" && kind != "class" {
		metadata["local_binding_shadowed"] = true
	}
}

func (f *fileFacts) annotateReceiver(node *sitter.Node, receiver string, metadata map[string]any) {
	if receiver == "" || receiver == "this" {
		return
	}
	if f.bindings == nil {
		return
	}
	if kind, shadowed := f.bindings.nearest(node, receiver); kind != "" {
		metadata["receiver_binding_found"] = true
		metadata["receiver_binding_kind"] = kind
		if !shadowed {
			return
		}
		if f.bindings.imports[receiver] {
			metadata["receiver_import_shadowed"] = true
		}
		metadata["receiver_shadowed"] = true
	} else {
		metadata["receiver_binding_found"] = false
	}
}

func addPatternBindingsFromSource(bindings *lexicalBindings, pattern, scope *sitter.Node, kind string, source []byte) {
	if pattern == nil || scope == nil {
		return
	}
	walk(pattern, func(node *sitter.Node) {
		if node.Type() == "identifier" || node.Type() == "shorthand_property_identifier_pattern" || node.Type() == "shorthand_property_identifier" {
			bindings.add(nodeText(source, node), scope, kind)
		}
	})
}

func addJavaScriptParameterBindings(bindings *lexicalBindings, parameters, scope *sitter.Node, source []byte) {
	if parameters == nil || scope == nil {
		return
	}
	if parameters.Type() == "identifier" {
		bindings.add(nodeText(source, parameters), scope, "parameter")
		return
	}
	for i := 0; i < int(parameters.NamedChildCount()); i++ {
		parameter := parameters.NamedChild(i)
		pattern := parameter
		if field := parameter.ChildByFieldName("pattern"); field != nil {
			pattern = field
		} else if field := parameter.ChildByFieldName("name"); field != nil {
			pattern = field
		}
		addPatternBindingsFromSource(bindings, pattern, scope, "parameter", source)
	}
}

func addGoParameterBindings(bindings *lexicalBindings, parameters, scope *sitter.Node, source []byte) {
	if parameters == nil || scope == nil {
		return
	}
	for i := 0; i < int(parameters.NamedChildCount()); i++ {
		parameter := parameters.NamedChild(i)
		if name := parameter.ChildByFieldName("name"); name != nil {
			addPatternBindingsFromSource(bindings, name, scope, "parameter", source)
			continue
		}
		if parameter.Type() == "identifier" {
			bindings.add(nodeText(source, parameter), scope, "parameter")
		}
	}
}

func nearestScope(node *sitter.Node, language string) *sitter.Node {
	for current := node; current != nil; current = current.Parent() {
		if isScopeNode(current, language) {
			return current
		}
	}
	return nil
}

func isScopeNode(node *sitter.Node, language string) bool {
	if node == nil {
		return false
	}
	switch language {
	case "javascript", "typescript", "tsx":
		switch node.Type() {
		case "program", "statement_block", "function_declaration", "function_expression", "arrow_function", "class_body", "catch_clause", "method_definition", "method_signature":
			return true
		}
	case "lua":
		switch node.Type() {
		case "program", "function_statement", "function_body", "do_statement", "if_statement", "for_statement", "while_statement", "repeat_statement":
			return true
		}
	case "go":
		switch node.Type() {
		case "source_file", "function_declaration", "method_declaration", "block":
			return true
		}
	}
	return false
}

func collectJavaScriptBindings(root *sitter.Node, source []byte) *lexicalBindings {
	bindings := newLexicalBindings(root)
	program := root
	walk(root, func(node *sitter.Node) {
		switch node.Type() {
		case "import_specifier":
			name := nodeText(source, node.ChildByFieldName("alias"))
			if name == "" {
				name = nodeText(source, node.ChildByFieldName("name"))
			}
			bindings.add(name, program, "import")
		case "namespace_import":
			if node.NamedChildCount() > 0 {
				bindings.add(nodeText(source, node.NamedChild(0)), program, "import")
			}
		case "identifier":
			if node.Parent() != nil && node.Parent().Type() == "import_clause" {
				bindings.add(nodeText(source, node), program, "import")
			}
		case "function_declaration":
			bindings.add(nodeText(source, node.ChildByFieldName("name")), nearestScope(node.Parent(), "javascript"), "function")
			addJavaScriptParameterBindings(bindings, javascriptParameters(node), node, source)
		case "function_expression", "arrow_function", "method_definition", "method_signature":
			if node.Type() == "function_expression" {
				bindings.add(nodeText(source, node.ChildByFieldName("name")), node, "function")
			}
			addJavaScriptParameterBindings(bindings, javascriptParameters(node), node, source)
		case "class_declaration":
			bindings.add(nodeText(source, node.ChildByFieldName("name")), nearestScope(node.Parent(), "javascript"), "class")
		case "variable_declarator":
			scope := nearestScope(node.Parent(), "javascript")
			if node.Parent() != nil && node.Parent().Type() == "variable_declaration" {
				scope = nearestJavaScriptFunctionScope(node.Parent())
			}
			addPatternBindingsFromSource(bindings, node.ChildByFieldName("name"), scope, "variable", source)
		case "catch_clause":
			addPatternBindingsFromSource(bindings, node.ChildByFieldName("parameter"), node, "catch", source)
		}
	})
	return bindings
}

func javascriptParameters(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if parameters := node.ChildByFieldName("parameters"); parameters != nil {
		return parameters
	}
	if parameter := node.ChildByFieldName("parameter"); parameter != nil {
		return parameter
	}
	return nil
}

func nearestJavaScriptFunctionScope(node *sitter.Node) *sitter.Node {
	for current := node; current != nil; current = current.Parent() {
		switch current.Type() {
		case "program", "function_declaration", "function_expression", "arrow_function", "method_definition", "method_signature":
			return current
		}
	}
	return nil
}

func collectLuaBindings(root *sitter.Node, source []byte) *lexicalBindings {
	bindings := newLexicalBindings(root)
	walk(root, func(node *sitter.Node) {
		switch node.Type() {
		case "function_statement":
			bindings.add(nodeText(source, node.ChildByFieldName("name")), nearestScope(node.Parent(), "lua"), "function")
			addPatternBindingsFromSource(bindings, firstNodeOfType(node, "parameter_list"), node, "parameter", source)
		case "variable_declaration":
			name := node.ChildByFieldName("name")
			if name == nil {
				name = firstNodeOfType(node, "variable_declarator")
			}
			addPatternBindingsFromSource(bindings, name, nearestScope(node.Parent(), "lua"), "variable", source)
		}
	})
	return bindings
}

func collectGoBindings(root *sitter.Node, source []byte) *lexicalBindings {
	bindings := newLexicalBindings(root)
	walk(root, func(node *sitter.Node) {
		switch node.Type() {
		case "import_spec":
			name := nodeText(source, node.ChildByFieldName("name"))
			if name == "" {
				module := firstStringChild(node, source)
				name = filepath.Base(strings.TrimSuffix(module, "/"))
			}
			if name != "" && name != "_" && name != "." {
				bindings.add(name, root, "import")
			}
		case "function_declaration":
			bindings.add(nodeText(source, node.ChildByFieldName("name")), nearestScope(node.Parent(), "go"), "function")
			addGoParameterBindings(bindings, node.ChildByFieldName("parameters"), node, source)
		case "method_declaration":
			addGoParameterBindings(bindings, node.ChildByFieldName("receiver"), node, source)
			addGoParameterBindings(bindings, node.ChildByFieldName("parameters"), node, source)
		case "short_var_declaration":
			addPatternBindingsFromSource(bindings, node.ChildByFieldName("left"), nearestScope(node.Parent(), "go"), "variable", source)
		case "var_spec", "const_spec":
			addPatternBindingsFromSource(bindings, node.ChildByFieldName("name"), nearestScope(node.Parent(), "go"), "variable", source)
		}
	})
	return bindings
}
