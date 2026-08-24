package generic

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/lua"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

const (
	AnalyzerID             = semantic.AnalyzerGenericGraph
	KindCodeFile           = "code_file"
	KindCodeSymbol         = "code_symbol"
	KindCallSite           = "call_site"
	KindImportSite         = "import_site"
	KindReferenceSite      = "reference_site"
	RelationshipCalls      = "calls"
	RelationshipImports    = "imports"
	RelationshipReferences = "references"
)

type Analyzer struct{}

func NewAnalyzer() *Analyzer { return &Analyzer{} }

type fileFacts struct {
	input       semantic.FileInput
	root        *sitter.Node
	tree        *sitter.Tree
	fileEntity  semantic.Entity
	codeSymbols []semantic.Entity
	imports     []semantic.Entity
	calls       []semantic.Entity
	references  []semantic.Entity
	exports     map[string]struct{}
	packageName string
	bindings    *lexicalBindings
}

func (a *Analyzer) AnalyzeFile(ctx context.Context, input semantic.FileInput) (semantic.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	facts, err := analyzeFile(ctx, input)
	if err != nil {
		return semantic.Result{}, err
	}
	result := facts.result()
	if facts.tree != nil {
		facts.tree.Close()
	}
	return result, nil
}

func (a *Analyzer) AnalyzeRepository(ctx context.Context, input semantic.RepositoryInput) (semantic.Result, error) {
	paths := make([]string, 0, len(input.Files))
	for path := range input.Files {
		if supported(input.Languages[path], path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	all := make([]semantic.Entity, 0)
	for _, path := range paths {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return semantic.Result{}, ctx.Err()
			default:
			}
		}
		fileResult, err := a.AnalyzeFile(ctx, semantic.FileInput{
			Repo: input.Repo, File: path, Language: input.Languages[path], Content: input.Files[path], Symbols: input.Symbols[path],
		})
		if err != nil {
			return semantic.Result{}, err
		}
		modulePath := input.ModulePath
		if modulePath == "" {
			modulePath = modulePathFromFiles(input.Files)
		}
		for i := range fileResult.Entities {
			if fileResult.Entities[i].Kind == KindCodeFile {
				fileResult.Entities[i].Metadata["module_path"] = modulePath
			}
		}
		all = append(all, fileResult.Entities...)
	}
	return semantic.Result{Entities: all, Relationships: ResolveRelationships(all)}, nil
}

func analyzeFile(ctx context.Context, input semantic.FileInput) (fileFacts, error) {
	if !supported(input.Language, input.File) {
		return fileFacts{}, nil
	}
	language, err := languageFor(input.Language, input.File)
	if err != nil {
		return fileFacts{}, err
	}
	root, tree, err := parseTree(ctx, input.Content, language)
	if err != nil {
		return fileFacts{}, err
	}
	facts := fileFacts{input: input, root: root, tree: tree, exports: make(map[string]struct{})}
	facts.fileEntity = semantic.Entity{
		ID: semantic.StableID("generic_file", input.Repo, normalizePath(input.File)), Analyzer: AnalyzerID,
		Repo: input.Repo, File: normalizePath(input.File), Kind: KindCodeFile, Name: normalizePath(input.File),
		Framework: "", Side: "unknown", Line: 1,
		Metadata: map[string]any{"language": input.Language},
	}
	facts.addSymbols()
	switch input.Language {
	case "javascript", "typescript", "tsx":
		facts.analyzeJavaScript()
	case "lua":
		facts.analyzeLua()
	case "go":
		facts.analyzeGo()
	}
	if facts.packageName != "" {
		facts.fileEntity.Metadata["package"] = facts.packageName
	}
	return facts, nil
}

func (f fileFacts) result() semantic.Result {
	entities := make([]semantic.Entity, 0, 1+len(f.codeSymbols)+len(f.imports)+len(f.calls)+len(f.references))
	entities = append(entities, f.fileEntity)
	entities = append(entities, f.codeSymbols...)
	entities = append(entities, f.imports...)
	entities = append(entities, f.calls...)
	entities = append(entities, f.references...)
	return semantic.Result{Entities: entities}
}

func (f *fileFacts) addSymbols() {
	for _, symbol := range f.input.Symbols {
		if symbol.File != f.input.File {
			continue
		}
		metadata := map[string]any{
			"language": f.input.Language, "symbol_kind": symbol.Kind, "qualified_name": symbol.QualifiedName,
		}
		if symbol.Kind == parser.KindMethod {
			if separator := strings.LastIndexByte(symbol.QualifiedName, '.'); separator > 0 {
				metadata["class_name"] = symbol.QualifiedName[:separator]
			}
		}
		if f.input.Language == "go" && isExported(symbol.Name) {
			metadata["exports"] = []string{symbol.Name}
		}
		entity := semantic.Entity{
			ID: semantic.StableID("generic_symbol", f.input.Repo, symbol.ID), Analyzer: AnalyzerID,
			Repo: f.input.Repo, File: normalizePath(f.input.File), SymbolID: symbol.ID, Kind: KindCodeSymbol,
			Name: symbol.Name, Framework: "", Side: "unknown", Line: symbol.Line, EndLine: symbol.EndLine,
			Metadata: metadata,
		}
		f.codeSymbols = append(f.codeSymbols, entity)
	}
}

func (f *fileFacts) analyzeJavaScript() {
	f.bindings = collectJavaScriptBindings(f.root, f.input.Content)
	exports := collectJavaScriptExports(f.root, f.input.Content)
	for _, symbol := range f.codeSymbols {
		if names := exportNamesForSymbol(symbol.Name, exports); len(names) > 0 {
			symbol.Metadata["exports"] = names
		}
	}
	walk(f.root, func(node *sitter.Node) {
		switch node.Type() {
		case "import_statement":
			f.addJavaScriptImports(node)
		case "call_expression":
			f.addJavaScriptCall(node)
		case "jsx_element", "jsx_self_closing_element":
			f.addJSXReference(node)
		}
	})
}

func (f *fileFacts) addJavaScriptImports(node *sitter.Node) {
	module := firstStringChild(node, f.input.Content)
	if module == "" {
		return
	}
	clause := node.ChildByFieldName("import")
	if clause == nil {
		clause = firstNodeOfType(node, "import_clause")
	}
	if clause == nil {
		return
	}
	added := false
	walk(clause, func(child *sitter.Node) {
		if child.Type() != "import_specifier" && child.Type() != "namespace_import" && child.Type() != "identifier" {
			return
		}
		if child.Type() == "identifier" && child.Parent() != clause {
			return
		}
		imported := nodeText(f.input.Content, child.ChildByFieldName("name"))
		local := nodeText(f.input.Content, child.ChildByFieldName("alias"))
		if child.Type() == "namespace_import" {
			if child.NamedChildCount() > 0 {
				local = nodeText(f.input.Content, child.NamedChild(0))
			}
			imported = "*"
		}
		if imported == "" {
			imported = nodeText(f.input.Content, child)
		}
		if child.Type() == "identifier" {
			local = nodeText(f.input.Content, child)
			imported = "default"
		}
		if local == "" {
			local = imported
		}
		if imported == "" || local == "" {
			return
		}
		f.addImportSite(module, local, imported, child, "esmodule")
		added = true
	})
	if !added {
		// A default import is an identifier directly under import_clause.
		for i := 0; i < int(clause.NamedChildCount()); i++ {
			child := clause.NamedChild(i)
			if child.Type() == "identifier" {
				f.addImportSite(module, nodeText(f.input.Content, child), "default", child, "esmodule")
			}
		}
	}
}

func (f *fileFacts) addImportSite(module, local, imported string, node *sitter.Node, kind string) {
	line := int(node.StartPoint().Row) + 1
	entity := f.site(KindImportSite, module, line, node, map[string]any{
		"module": module, "local": local, "imported": imported, "binding_kind": kind,
	})
	f.imports = append(f.imports, entity)
}

func (f *fileFacts) addJavaScriptCall(node *sitter.Node) {
	function := node.ChildByFieldName("function")
	name, receiver, member := expressionCallee(function, f.input.Content)
	if name == "" {
		return
	}
	metadata := map[string]any{"callee": name, "receiver": receiver, "member": member, "language": f.input.Language}
	if className := containingClassName(node, f.input.Content); className != "" && receiver == "this" {
		metadata["class_name"] = className
	}
	f.annotateBinding(node, name, metadata)
	f.annotateReceiver(node, receiver, metadata)
	f.calls = append(f.calls, f.site(KindCallSite, name, int(node.StartPoint().Row)+1, node, metadata))
	if name == "require" {
		if args := node.ChildByFieldName("arguments"); args != nil {
			if module := firstStringChild(args, f.input.Content); module != "" {
				f.addImportSite(module, "", "", node, "commonjs")
			}
		}
	}
	arguments := node.ChildByFieldName("arguments")
	if arguments != nil {
		walk(arguments, func(child *sitter.Node) {
			if child.Type() != "identifier" || child == function {
				return
			}
			if child.Parent() != nil && child.Parent().Type() == "property_identifier" {
				return
			}
			metadata := map[string]any{"name": nodeText(f.input.Content, child), "language": f.input.Language}
			f.annotateBinding(child, nodeText(f.input.Content, child), metadata)
			f.references = append(f.references, f.site(KindReferenceSite, nodeText(f.input.Content, child), int(child.StartPoint().Row)+1, child, metadata))
		})
	}
}

func (f *fileFacts) addJSXReference(node *sitter.Node) {
	var nameNode *sitter.Node
	if node.Type() == "jsx_element" {
		opening := node.ChildByFieldName("open_tag")
		if opening == nil {
			opening = node.ChildByFieldName("opening_element")
		}
		if opening != nil {
			nameNode = opening.ChildByFieldName("name")
		}
	} else {
		nameNode = node.ChildByFieldName("name")
	}
	name := nodeText(f.input.Content, nameNode)
	if name == "" || strings.ContainsAny(name, ".:-") {
		return
	}
	metadata := map[string]any{"name": name, "jsx": true, "language": f.input.Language}
	f.annotateBinding(node, name, metadata)
	f.references = append(f.references, f.site(KindReferenceSite, name, int(node.StartPoint().Row)+1, node, metadata))
}

func (f *fileFacts) analyzeLua() {
	f.bindings = collectLuaBindings(f.root, f.input.Content)
	walk(f.root, func(node *sitter.Node) {
		if node.Type() != "function_call" {
			return
		}
		prefix := node.ChildByFieldName("prefix")
		name := strings.TrimSpace(nodeText(f.input.Content, prefix))
		if name == "" || strings.Contains(name, "=") {
			return
		}
		receiver, member := luaCallee(name)
		metadata := map[string]any{"callee": member, "receiver": receiver, "member": receiver != "", "language": "lua"}
		if receiver == "" {
			f.annotateBinding(node, member, metadata)
		} else {
			f.annotateReceiver(node, receiver, metadata)
		}
		f.calls = append(f.calls, f.site(KindCallSite, member, int(node.StartPoint().Row)+1, node, metadata))
		if member == "require" {
			if args := node.ChildByFieldName("args"); args != nil {
				if module := firstStringChild(args, f.input.Content); module != "" {
					f.addImportSite(module, "", "", node, "lua_require")
				}
			}
		}
	})
}

func (f *fileFacts) analyzeGo() {
	f.bindings = collectGoBindings(f.root, f.input.Content)
	f.packageName = firstNodeText(f.root, "package_identifier", f.input.Content)
	walk(f.root, func(node *sitter.Node) {
		switch node.Type() {
		case "import_spec":
			path := firstStringChild(node, f.input.Content)
			if path != "" {
				alias := nodeText(f.input.Content, node.ChildByFieldName("name"))
				f.addImportSite(path, alias, "", node, "go_import")
			}
		case "call_expression":
			function := node.ChildByFieldName("function")
			name, receiver, member := expressionCallee(function, f.input.Content)
			if name == "" {
				return
			}
			if receiver == "" {
				receiver = ""
			}
			metadata := map[string]any{"callee": member, "receiver": receiver, "member": member != name, "language": "go"}
			if receiver == "" {
				f.annotateBinding(node, name, metadata)
			} else {
				f.annotateReceiver(node, receiver, metadata)
			}
			f.calls = append(f.calls, f.site(KindCallSite, name, int(node.StartPoint().Row)+1, node, metadata))
		}
	})
}

func (f fileFacts) site(kind, name string, line int, node *sitter.Node, metadata map[string]any) semantic.Entity {
	symbolID := containingSymbolID(f.input.Symbols, line)
	metadata["source_symbol_id"] = symbolID
	return semantic.Entity{
		ID: semantic.StableID("generic_site", f.input.Repo, f.input.File, kind, strconv.Itoa(line), strconv.Itoa(int(node.StartByte())), name), Analyzer: AnalyzerID,
		Repo: f.input.Repo, File: normalizePath(f.input.File), Kind: kind, Name: name,
		Line: line, EndLine: int(node.EndPoint().Row) + 1, Dynamic: false, Metadata: metadata,
	}
}

func containingSymbolID(symbols []parser.Symbol, line int) string {
	best, span := "", int(^uint(0)>>1)
	for _, symbol := range symbols {
		if line >= symbol.Line && line <= symbol.EndLine && symbol.EndLine-symbol.Line < span {
			best, span = symbol.ID, symbol.EndLine-symbol.Line
		}
	}
	return best
}

func supported(language, file string) bool {
	if language == "" {
		language = parser.DetectLanguage(file)
	}
	switch language {
	case "javascript", "typescript", "tsx", "lua", "go":
		return true
	default:
		return false
	}
}

func languageFor(language, file string) (*sitter.Language, error) {
	if language == "" {
		language = parser.DetectLanguage(file)
	}
	switch language {
	case "javascript":
		return javascript.GetLanguage(), nil
	case "typescript":
		return typescript.GetLanguage(), nil
	case "tsx":
		return tsx.GetLanguage(), nil
	case "lua":
		return lua.GetLanguage(), nil
	case "go":
		return golang.GetLanguage(), nil
	default:
		return nil, fmt.Errorf("unsupported generic graph language %q", language)
	}
}

func parseTree(ctx context.Context, content []byte, language *sitter.Language) (*sitter.Node, *sitter.Tree, error) {
	p := sitter.NewParser()
	defer p.Close()
	p.SetLanguage(language)
	tree, err := p.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, nil, err
	}
	if tree == nil {
		return nil, nil, fmt.Errorf("empty syntax tree")
	}
	return tree.RootNode(), tree, nil
}

func walk(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		walk(node.Child(i), visit)
	}
}

func nodeText(source []byte, node *sitter.Node) string {
	if node == nil || node.StartByte() > node.EndByte() || node.EndByte() > uint32(len(source)) {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

func firstStringChild(node *sitter.Node, source []byte) string {
	var value string
	if node == nil {
		return ""
	}
	walk(node, func(child *sitter.Node) {
		if value == "" && (child.Type() == "string" || child.Type() == "interpreted_string_literal" || child.Type() == "raw_string_literal" || child.Type() == "string_literal") {
			value = decodeString(nodeText(source, child))
		}
	})
	return value
}

func decodeString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] == '`' {
		return ""
	}
	decoded, err := strconv.Unquote(value)
	if err == nil {
		return decoded
	}
	return strings.Trim(value, "\"'")
}

func firstNodeText(root *sitter.Node, typ string, source []byte) string {
	var result string
	walk(root, func(node *sitter.Node) {
		if result == "" && node.Type() == typ {
			result = nodeText(source, node)
		}
	})
	return result
}

func firstNodeOfType(root *sitter.Node, typ string) *sitter.Node {
	var result *sitter.Node
	walk(root, func(node *sitter.Node) {
		if result == nil && node.Type() == typ {
			result = node
		}
	})
	return result
}

func normalizePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func isExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func expressionCallee(node *sitter.Node, source []byte) (name, receiver, member string) {
	if node == nil {
		return "", "", ""
	}
	switch node.Type() {
	case "identifier", "property_identifier", "field_identifier", "type_identifier":
		value := nodeText(source, node)
		return value, "", value
	case "member_expression", "selector_expression":
		property := node.ChildByFieldName("property")
		if property == nil {
			property = node.ChildByFieldName("field")
		}
		object := node.ChildByFieldName("object")
		if object == nil {
			object = node.ChildByFieldName("operand")
		}
		if property == nil || object == nil {
			return "", "", ""
		}
		if strings.Contains(nodeText(source, node), "[") {
			return "", "", ""
		}
		return nodeText(source, property), nodeText(source, object), nodeText(source, property)
	case "this":
		return "", "this", ""
	default:
		return "", "", ""
	}
}

func luaCallee(value string) (receiver, member string) {
	value = strings.TrimSpace(value)
	if colon := strings.LastIndexByte(value, ':'); colon >= 0 {
		return value[:colon], value[colon+1:]
	}
	if dot := strings.LastIndexByte(value, '.'); dot >= 0 {
		return value[:dot], value[dot+1:]
	}
	return "", value
}

func containingClassName(node *sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Type() != "class_declaration" && current.Type() != "class" {
			continue
		}
		return nodeText(source, current.ChildByFieldName("name"))
	}
	return ""
}

func modulePathFromFiles(files map[string][]byte) string {
	content, ok := files["go.mod"]
	if !ok {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
}

func collectJavaScriptExports(root *sitter.Node, source []byte) map[string]string {
	result := make(map[string]string)
	walk(root, func(node *sitter.Node) {
		if node.Type() != "export_statement" {
			return
		}
		declaration := node.ChildByFieldName("declaration")
		if declaration != nil {
			name := nodeText(source, declaration.ChildByFieldName("name"))
			if name != "" {
				result[name] = name
				exportBody := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(nodeText(source, node)), "export"))
				if strings.HasPrefix(exportBody, "default") {
					result["default"] = name
				}
				return
			}
			walk(declaration, func(child *sitter.Node) {
				if child.Type() == "variable_declarator" {
					if value := nodeText(source, child.ChildByFieldName("name")); value != "" {
						result[value] = value
					}
				}
			})
			return
		}
		walk(node, func(child *sitter.Node) {
			if child.Type() != "export_specifier" {
				return
			}
			name := nodeText(source, child.ChildByFieldName("name"))
			alias := nodeText(source, child.ChildByFieldName("alias"))
			if name == "" {
				name = nodeText(source, child)
			}
			if alias == "" {
				alias = name
			}
			result[alias] = name
		})
	})
	return result
}

func exportNamesForSymbol(symbolName string, exports map[string]string) []string {
	var result []string
	for exported, local := range exports {
		if local == symbolName {
			result = append(result, exported)
		}
	}
	sort.Strings(result)
	return result
}
