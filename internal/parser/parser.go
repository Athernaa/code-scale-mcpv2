package parser

import (
	"context"
	"path/filepath"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
)

// ParseFile parses source code and extracts symbols using tree-sitter.
func ParseFile(content []byte, filename string, language string) ([]Symbol, error) {
	spec, ok := LanguageRegistry[language]
	if !ok {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(spec.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, err
	}

	var symbols []Symbol
	walkTree(tree.RootNode(), spec, content, filename, language, &symbols, nil)
	symbols = disambiguateOverloads(symbols)
	return symbols, nil
}

// DetectLanguage detects language from file extension.
func DetectLanguage(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if lang, ok := LanguageExtensions[ext]; ok {
		return lang
	}
	return ""
}

// walkTree recursively walks the AST and extracts symbols.
func walkTree(
	node *sitter.Node,
	spec *LanguageSpec,
	src []byte,
	filename string,
	language string,
	symbols *[]Symbol,
	parentSymbol *Symbol,
) {
	nodeType := node.Type()

	// Check if this node is a symbol
	if _, ok := spec.SymbolNodeTypes[nodeType]; ok {
		sym := extractSymbol(node, spec, src, filename, language, parentSymbol)
		if sym != nil {
			*symbols = append(*symbols, *sym)
			parentSymbol = sym
		}
	}

	// Check for constant patterns (top-level only)
	if parentSymbol == nil {
		for _, cp := range spec.ConstantPatterns {
			if nodeType == cp {
				if c := extractConstant(node, spec, src, filename, language); c != nil {
					*symbols = append(*symbols, *c)
				}
			}
		}
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		walkTree(child, spec, src, filename, language, symbols, parentSymbol)
	}
}

// extractSymbol extracts a Symbol from an AST node.
func extractSymbol(
	node *sitter.Node,
	spec *LanguageSpec,
	src []byte,
	filename string,
	language string,
	parentSymbol *Symbol,
) *Symbol {
	kind := spec.SymbolNodeTypes[node.Type()]

	// Skip nodes with errors
	if node.HasError() {
		return nil
	}

	// Extract name
	name := extractName(node, spec, src)
	if name == "" {
		return nil
	}

	// Build qualified name
	qualifiedName := name
	if parentSymbol != nil {
		qualifiedName = parentSymbol.Name + "." + name
		if kind == KindFunction {
			kind = KindMethod
		}
	}

	// Build signature
	signature := buildSignature(node, src)

	// Extract docstring
	docstring := extractDocstring(node, spec, src)

	// Extract decorators
	decorators := extractDecorators(node, spec, src)

	// Compute content hash
	symbolBytes := src[node.StartByte():node.EndByte()]
	contentHash := ComputeContentHash(symbolBytes)

	parentID := ""
	if parentSymbol != nil {
		parentID = parentSymbol.ID
	}

	return &Symbol{
		ID:            MakeSymbolID(filename, qualifiedName, kind),
		File:          filename,
		Name:          name,
		QualifiedName: qualifiedName,
		Kind:          kind,
		Language:      language,
		Signature:     signature,
		Docstring:     docstring,
		Decorators:    decorators,
		Keywords:      []string{},
		Parent:        parentID,
		Line:          int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		ByteOffset:    int64(node.StartByte()),
		ByteLength:    int64(node.EndByte()) - int64(node.StartByte()),
		ContentHash:   contentHash,
	}
}

// extractName extracts the name from an AST node.
func extractName(node *sitter.Node, spec *LanguageSpec, src []byte) string {
	// Arrow functions get name from parent — skip here
	if node.Type() == "arrow_function" {
		return ""
	}

	// Handle Go type_declaration — name is in type_spec child
	if node.Type() == "type_declaration" {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "type_spec" {
				nameNode := child.ChildByFieldName("name")
				if nameNode != nil {
					return string(src[nameNode.StartByte():nameNode.EndByte()])
				}
			}
		}
		return ""
	}

	fieldName, ok := spec.NameFields[node.Type()]
	if !ok {
		return ""
	}

	nameNode := node.ChildByFieldName(fieldName)
	if nameNode != nil {
		return string(src[nameNode.StartByte():nameNode.EndByte()])
	}

	// Fallback: some grammars (e.g. Kotlin) don't use field names.
	// Search named children for type_identifier or simple_identifier.
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		switch child.Type() {
		case "type_identifier", "simple_identifier":
			return string(src[child.StartByte():child.EndByte()])
		}
	}

	return ""
}

// buildSignature builds a clean signature from AST node.
func buildSignature(node *sitter.Node, src []byte) string {
	// Find the body child to determine where signature ends
	body := node.ChildByFieldName("body")

	endByte := node.EndByte()
	if body != nil {
		endByte = body.StartByte()
	}

	sigBytes := src[node.StartByte():endByte]
	sigText := strings.TrimSpace(string(sigBytes))

	// Clean up: remove trailing '{', ':', etc.
	sigText = strings.TrimRight(sigText, "{: \n\t")

	return sigText
}

// extractDecorators extracts decorators/attributes from a node.
func extractDecorators(node *sitter.Node, spec *LanguageSpec, src []byte) []string {
	if spec.DecoratorNodeType == "" {
		return nil
	}

	var decorators []string
	prev := node.PrevNamedSibling()
	for prev != nil && prev.Type() == spec.DecoratorNodeType {
		text := strings.TrimSpace(string(src[prev.StartByte():prev.EndByte()]))
		decorators = append([]string{text}, decorators...)
		prev = prev.PrevNamedSibling()
	}

	return decorators
}

// extractConstant extracts a constant (UPPER_CASE top-level assignment).
func extractConstant(node *sitter.Node, spec *LanguageSpec, src []byte, filename string, language string) *Symbol {
	// Python: assignment node with UPPER_CASE left-hand side
	if node.Type() == "assignment" {
		left := node.ChildByFieldName("left")
		if left != nil && left.Type() == "identifier" {
			name := string(src[left.StartByte():left.EndByte()])
			if isConstantName(name) {
				sig := string(src[node.StartByte():node.EndByte()])
				if len(sig) > 100 {
					sig = sig[:100]
				}
				constBytes := src[node.StartByte():node.EndByte()]
				return &Symbol{
					ID:            MakeSymbolID(filename, name, KindConstant),
					File:          filename,
					Name:          name,
					QualifiedName: name,
					Kind:          KindConstant,
					Language:      language,
					Signature:     strings.TrimSpace(sig),
					Keywords:      []string{},
					Line:          int(node.StartPoint().Row) + 1,
					EndLine:       int(node.EndPoint().Row) + 1,
					ByteOffset:    int64(node.StartByte()),
					ByteLength:    int64(node.EndByte()) - int64(node.StartByte()),
					ContentHash:   ComputeContentHash(constBytes),
				}
			}
		}
	}

	return nil
}

// isConstantName checks if a name follows constant naming convention (UPPER_CASE).
func isConstantName(name string) bool {
	if len(name) == 0 {
		return false
	}
	// All uppercase
	allUpper := true
	for _, r := range name {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			allUpper = false
			break
		}
	}
	if allUpper {
		return true
	}
	// Starts with uppercase and contains underscore (e.g. Max_Retries)
	if len(name) > 1 && unicode.IsUpper(rune(name[0])) && strings.Contains(name, "_") {
		return true
	}
	return false
}

// disambiguateOverloads appends ordinal suffix to symbols with duplicate IDs.
func disambiguateOverloads(symbols []Symbol) []Symbol {
	// Count occurrences
	idCounts := make(map[string]int)
	for _, s := range symbols {
		idCounts[s.ID]++
	}

	// Find duplicates
	duplicated := make(map[string]bool)
	for id, count := range idCounts {
		if count > 1 {
			duplicated[id] = true
		}
	}

	if len(duplicated) == 0 {
		return symbols
	}

	// Assign ordinals
	ordinals := make(map[string]int)
	for i := range symbols {
		if duplicated[symbols[i].ID] {
			ordinals[symbols[i].ID]++
			symbols[i].ID = symbols[i].ID + "~" + itoa(ordinals[symbols[i].ID])
		}
	}

	return symbols
}

// itoa converts int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
