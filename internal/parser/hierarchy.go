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

	// Pre-build children map for O(N) total instead of O(N²)
	childrenOf := make(map[string][]string, len(symbols))
	isChild := make(map[string]bool)
	for _, sym := range symbols {
		if sym.Parent != "" {
			if _, ok := nodeMap[sym.Parent]; ok {
				childrenOf[sym.Parent] = append(childrenOf[sym.Parent], sym.ID)
				isChild[sym.ID] = true
			}
		}
	}

	// Collect roots, then recursively build children
	var roots []SymbolNode
	for _, sym := range symbols {
		if !isChild[sym.ID] {
			roots = append(roots, buildNode(nodeMap, childrenOf, sym.ID))
		}
	}

	return roots
}

// buildNode recursively builds a SymbolNode with all descendants.
func buildNode(nodeMap map[string]*SymbolNode, childrenOf map[string][]string, id string) SymbolNode {
	node := SymbolNode{Symbol: nodeMap[id].Symbol}
	for _, childID := range childrenOf[id] {
		node.Children = append(node.Children, buildNode(nodeMap, childrenOf, childID))
	}
	return node
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
