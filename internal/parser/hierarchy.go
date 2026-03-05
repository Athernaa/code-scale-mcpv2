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

// FlatSymbolNode is a symbol with its nesting depth in the hierarchy.
type FlatSymbolNode struct {
	Symbol Symbol `json:"symbol"`
	Depth  int    `json:"depth"`
}

// FlattenSymbols computes a flat list with depth directly from a symbol slice,
// without building an intermediate tree. Depth is derived from parent chains.
func FlattenSymbols(symbols []Symbol) []FlatSymbolNode {
	// Build parent-child relationships
	childrenOf := make(map[string][]int, len(symbols))
	idxByID := make(map[string]int, len(symbols))
	isChild := make(map[string]bool)
	for i, sym := range symbols {
		idxByID[sym.ID] = i
		if sym.Parent != "" {
			childrenOf[sym.Parent] = append(childrenOf[sym.Parent], i)
			isChild[sym.ID] = true
		}
	}

	result := make([]FlatSymbolNode, 0, len(symbols))
	for _, sym := range symbols {
		if !isChild[sym.ID] {
			flattenInto(&result, symbols, childrenOf, sym.ID, idxByID, 0)
		}
	}
	return result
}

func flattenInto(out *[]FlatSymbolNode, symbols []Symbol, childrenOf map[string][]int, id string, idxByID map[string]int, depth int) {
	idx := idxByID[id]
	*out = append(*out, FlatSymbolNode{Symbol: symbols[idx], Depth: depth})
	for _, childIdx := range childrenOf[id] {
		flattenInto(out, symbols, childrenOf, symbols[childIdx].ID, idxByID, depth+1)
	}
}
