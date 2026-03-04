package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SymbolKind represents the type of code symbol.
type SymbolKind = string

const (
	KindFunction SymbolKind = "function"
	KindClass    SymbolKind = "class"
	KindMethod   SymbolKind = "method"
	KindConstant SymbolKind = "constant"
	KindType     SymbolKind = "type"
)

// Symbol represents a code symbol extracted from source via tree-sitter.
type Symbol struct {
	ID            string   `json:"id"`             // "{file_path}::{qualified_name}#{kind}"
	File          string   `json:"file"`           // Relative file path
	Name          string   `json:"name"`           // Symbol name
	QualifiedName string   `json:"qualified_name"` // Fully qualified (e.g. "MyClass.login")
	Kind          string   `json:"kind"`           // function|class|method|constant|type
	Language      string   `json:"language"`
	Signature     string   `json:"signature"`    // Full signature line(s)
	Docstring     string   `json:"docstring"`    // Extracted docstring
	Summary       string   `json:"summary"`      // One-line summary
	Decorators    []string `json:"decorators"`   // Decorators/attributes
	Keywords      []string `json:"keywords"`     // Search keywords
	Parent        string   `json:"parent"`       // Parent symbol ID
	Line          int      `json:"line"`         // Start line (1-indexed)
	EndLine       int      `json:"end_line"`     // End line (1-indexed)
	ByteOffset    int64    `json:"byte_offset"`  // Start byte in raw file
	ByteLength    int64    `json:"byte_length"`  // Byte length of full source
	ContentHash   string   `json:"content_hash"` // SHA-256 for drift detection
}

// MakeSymbolID generates a stable symbol ID.
// Format: {file_path}::{qualified_name}#{kind}
func MakeSymbolID(filePath, qualifiedName, kind string) string {
	if kind != "" {
		return fmt.Sprintf("%s::%s#%s", filePath, qualifiedName, kind)
	}
	return fmt.Sprintf("%s::%s", filePath, qualifiedName)
}

// ComputeContentHash computes SHA-256 hash of source bytes for drift detection.
func ComputeContentHash(sourceBytes []byte) string {
	h := sha256.Sum256(sourceBytes)
	return hex.EncodeToString(h[:])
}
