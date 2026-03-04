package parser

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// extractDocstring extracts docstring using language-specific strategy.
func extractDocstring(node *sitter.Node, spec *LanguageSpec, src []byte) string {
	switch spec.DocstringStrategy {
	case DocstringNextSiblingString:
		return extractPythonDocstring(node, src)
	case DocstringPrecedingComment:
		return extractPrecedingComments(node, src)
	}
	return ""
}

// extractPythonDocstring extracts Python docstring from first statement in body.
func extractPythonDocstring(node *sitter.Node, src []byte) string {
	body := node.ChildByFieldName("body")
	if body == nil || body.ChildCount() == 0 {
		return ""
	}

	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child.Type() == "expression_statement" {
			// Check field "expression"
			expr := child.ChildByFieldName("expression")
			if expr != nil && expr.Type() == "string" {
				doc := string(src[expr.StartByte():expr.EndByte()])
				return stripQuotes(doc)
			}
			// Handle tree-sitter-python string format
			if child.ChildCount() > 0 {
				first := child.Child(0)
				if first.Type() == "string" || first.Type() == "concatenated_string" {
					doc := string(src[first.StartByte():first.EndByte()])
					return stripQuotes(doc)
				}
			}
		} else if child.Type() == "string" {
			// Class docstrings directly in block
			doc := string(src[child.StartByte():child.EndByte()])
			return stripQuotes(doc)
		}
	}
	return ""
}

// extractPrecedingComments extracts comments that immediately precede a node.
func extractPrecedingComments(node *sitter.Node, src []byte) string {
	var comments []string

	prev := node.PrevNamedSibling()
	for prev != nil {
		t := prev.Type()
		if t == "comment" || t == "line_comment" || t == "block_comment" {
			commentText := string(src[prev.StartByte():prev.EndByte()])
			comments = append([]string{commentText}, comments...)
			prev = prev.PrevNamedSibling()
		} else {
			break
		}
	}

	if len(comments) == 0 {
		return ""
	}

	return cleanCommentMarkers(strings.Join(comments, "\n"))
}

// stripQuotes removes surrounding quotes from a docstring.
func stripQuotes(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, `"""`) && strings.HasSuffix(text, `"""`) && len(text) >= 6 {
		return strings.TrimSpace(text[3 : len(text)-3])
	}
	if strings.HasPrefix(text, "'''") && strings.HasSuffix(text, "'''") && len(text) >= 6 {
		return strings.TrimSpace(text[3 : len(text)-3])
	}
	if strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`) && len(text) >= 2 {
		return strings.TrimSpace(text[1 : len(text)-1])
	}
	if strings.HasPrefix(text, "'") && strings.HasSuffix(text, "'") && len(text) >= 2 {
		return strings.TrimSpace(text[1 : len(text)-1])
	}
	return text
}

// cleanCommentMarkers removes comment markers from docstring text.
func cleanCommentMarkers(text string) string {
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "/**"):
			line = line[3:]
		case strings.HasPrefix(line, "/*"):
			line = line[2:]
		case strings.HasPrefix(line, "///"):
			line = line[3:]
		case strings.HasPrefix(line, "//!"):
			line = line[3:]
		case strings.HasPrefix(line, "//"):
			line = line[2:]
		case strings.HasPrefix(line, "*"):
			line = line[1:]
		}
		line = strings.TrimSuffix(line, "*/")
		cleaned = append(cleaned, strings.TrimSpace(line))
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}
