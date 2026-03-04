package parser

// SymbolNode is a node in the symbol tree with children.
type SymbolNode struct {
	Symbol   Symbol       `json:"symbol"`
	Children []SymbolNode `json:"children,omitempty"`
}

// BuildSymbolTree builds a hierarchical tree from a flat symbol list.
// Methods become children of their parent classes.
// Returns top-level symbols (classes and standalone functions).
func BuildSymbolTree(symbols []Symbol) []SymbolNode {
	nodeMap := make(map[string]*SymbolNode, len(symbols))
	for i := range symbols {
		nodeMap[symbols[i].ID] = &SymbolNode{Symbol: symbols[i]}
	}

	// Track which nodes are children (not roots)
	isChild := make(map[string]bool)

	// First pass: build parent-child relationships
	for _, sym := range symbols {
		if sym.Parent != "" {
			if parent, ok := nodeMap[sym.Parent]; ok {
				parent.Children = append(parent.Children, *nodeMap[sym.ID])
				isChild[sym.ID] = true
			}
		}
	}

	// Second pass: collect roots (copy from nodeMap to get updated children)
	var roots []SymbolNode
	for _, sym := range symbols {
		if !isChild[sym.ID] {
			roots = append(roots, *nodeMap[sym.ID])
		}
	}

	return roots
}

// FlattenTree flattens symbol tree with depth information.
func FlattenTree(nodes []SymbolNode, depth int) []struct {
	Symbol Symbol
	Depth  int
} {
	var result []struct {
		Symbol Symbol
		Depth  int
	}
	for _, node := range nodes {
		result = append(result, struct {
			Symbol Symbol
			Depth  int
		}{node.Symbol, depth})
		result = append(result, FlattenTree(node.Children, depth+1)...)
	}
	return result
}
