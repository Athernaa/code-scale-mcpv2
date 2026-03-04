package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/lua"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// DocstringStrategy defines how to extract docstrings from AST nodes.
type DocstringStrategy string

const (
	DocstringNextSiblingString DocstringStrategy = "next_sibling_string"
	DocstringPrecedingComment  DocstringStrategy = "preceding_comment"
)

// LanguageSpec defines how to extract symbols from a language's AST.
type LanguageSpec struct {
	GetLanguage        func() *sitter.Language
	SymbolNodeTypes    map[string]string // AST node type -> symbol kind
	NameFields         map[string]string // node_type -> field name for name extraction
	ParamFields        map[string]string // node_type -> field name for params
	ReturnTypeFields   map[string]string // node_type -> field name for return type
	DocstringStrategy  DocstringStrategy
	DecoratorNodeType  string   // "" if language has no decorators
	ContainerNodeTypes []string // node types that nest (e.g. class_definition)
	ConstantPatterns   []string // node types for constants
	TypePatterns       []string // node types for type definitions
}

// LanguageExtensions maps file extensions to language names.
var LanguageExtensions = map[string]string{
	".py":    "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".go":    "go",
	".rs":    "rust",
	".java":  "java",
	".php":   "php",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".cc":    "cpp",
	".cxx":   "cpp",
	".hpp":   "cpp",
	".hh":    "cpp",
	".rb":    "ruby",
	".kt":    "kotlin",
	".kts":   "kotlin",
	".swift": "swift",
	".lua":   "lua",
}

// LanguageRegistry maps language names to their specs.
var LanguageRegistry = map[string]*LanguageSpec{
	"python":     PythonSpec,
	"javascript": JavaScriptSpec,
	"typescript": TypeScriptSpec,
	"go":         GoSpec,
	"rust":       RustSpec,
	"java":       JavaSpec,
	"php":        PHPSpec,
	"c":          CSpec,
	"cpp":        CppSpec,
	"ruby":       RubySpec,
	"kotlin":     KotlinSpec,
	"swift":      SwiftSpec,
	"lua":        LuaSpec,
}

// PythonSpec defines symbol extraction rules for Python.
var PythonSpec = &LanguageSpec{
	GetLanguage: python.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_definition": "function",
		"class_definition":    "class",
	},
	NameFields: map[string]string{
		"function_definition": "name",
		"class_definition":    "name",
	},
	ParamFields: map[string]string{
		"function_definition": "parameters",
	},
	ReturnTypeFields: map[string]string{
		"function_definition": "return_type",
	},
	DocstringStrategy:  DocstringNextSiblingString,
	DecoratorNodeType:  "decorator",
	ContainerNodeTypes: []string{"class_definition"},
	ConstantPatterns:   []string{"assignment"},
	TypePatterns:       []string{"type_alias_statement"},
}

// JavaScriptSpec defines symbol extraction rules for JavaScript.
var JavaScriptSpec = &LanguageSpec{
	GetLanguage: javascript.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_declaration":           "function",
		"class_declaration":              "class",
		"method_definition":              "method",
		"arrow_function":                 "function",
		"generator_function_declaration": "function",
	},
	NameFields: map[string]string{
		"function_declaration": "name",
		"class_declaration":    "name",
		"method_definition":    "name",
	},
	ParamFields: map[string]string{
		"function_declaration": "parameters",
		"method_definition":    "parameters",
		"arrow_function":       "parameters",
	},
	ReturnTypeFields:   map[string]string{},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "",
	ContainerNodeTypes: []string{"class_declaration", "class"},
	ConstantPatterns:   []string{"lexical_declaration"},
	TypePatterns:       []string{},
}

// TypeScriptSpec defines symbol extraction rules for TypeScript.
var TypeScriptSpec = &LanguageSpec{
	GetLanguage: typescript.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_declaration":   "function",
		"class_declaration":      "class",
		"method_definition":      "method",
		"arrow_function":         "function",
		"interface_declaration":  "type",
		"type_alias_declaration": "type",
		"enum_declaration":       "type",
	},
	NameFields: map[string]string{
		"function_declaration":   "name",
		"class_declaration":      "name",
		"method_definition":      "name",
		"interface_declaration":  "name",
		"type_alias_declaration": "name",
		"enum_declaration":       "name",
	},
	ParamFields: map[string]string{
		"function_declaration": "parameters",
		"method_definition":    "parameters",
		"arrow_function":       "parameters",
	},
	ReturnTypeFields: map[string]string{
		"function_declaration": "return_type",
		"method_definition":    "return_type",
		"arrow_function":       "return_type",
	},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "decorator",
	ContainerNodeTypes: []string{"class_declaration", "class"},
	ConstantPatterns:   []string{"lexical_declaration"},
	TypePatterns:       []string{"interface_declaration", "type_alias_declaration", "enum_declaration"},
}

// GoSpec defines symbol extraction rules for Go.
var GoSpec = &LanguageSpec{
	GetLanguage: golang.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_declaration": "function",
		"method_declaration":   "method",
		"type_declaration":     "type",
	},
	NameFields: map[string]string{
		"function_declaration": "name",
		"method_declaration":   "name",
		"type_declaration":     "name",
	},
	ParamFields: map[string]string{
		"function_declaration": "parameters",
		"method_declaration":   "parameters",
	},
	ReturnTypeFields: map[string]string{
		"function_declaration": "result",
		"method_declaration":   "result",
	},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "",
	ContainerNodeTypes: []string{},
	ConstantPatterns:   []string{"const_declaration"},
	TypePatterns:       []string{"type_declaration"},
}

// RustSpec defines symbol extraction rules for Rust.
var RustSpec = &LanguageSpec{
	GetLanguage: rust.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_item": "function",
		"struct_item":   "type",
		"enum_item":     "type",
		"trait_item":    "type",
		"impl_item":     "class",
		"type_item":     "type",
	},
	NameFields: map[string]string{
		"function_item": "name",
		"struct_item":   "name",
		"enum_item":     "name",
		"trait_item":    "name",
		"type_item":     "name",
	},
	ParamFields: map[string]string{
		"function_item": "parameters",
	},
	ReturnTypeFields: map[string]string{
		"function_item": "return_type",
	},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "attribute_item",
	ContainerNodeTypes: []string{"impl_item", "trait_item"},
	ConstantPatterns:   []string{"const_item", "static_item"},
	TypePatterns:       []string{"struct_item", "enum_item", "trait_item", "type_item"},
}

// JavaSpec defines symbol extraction rules for Java.
var JavaSpec = &LanguageSpec{
	GetLanguage: java.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"method_declaration":      "method",
		"constructor_declaration": "method",
		"class_declaration":       "class",
		"interface_declaration":   "type",
		"enum_declaration":        "type",
	},
	NameFields: map[string]string{
		"method_declaration":      "name",
		"constructor_declaration": "name",
		"class_declaration":       "name",
		"interface_declaration":   "name",
		"enum_declaration":        "name",
	},
	ParamFields: map[string]string{
		"method_declaration":      "parameters",
		"constructor_declaration": "parameters",
	},
	ReturnTypeFields: map[string]string{
		"method_declaration": "type",
	},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "marker_annotation",
	ContainerNodeTypes: []string{"class_declaration", "interface_declaration", "enum_declaration"},
	ConstantPatterns:   []string{"field_declaration"},
	TypePatterns:       []string{"interface_declaration", "enum_declaration"},
}

// PHPSpec defines symbol extraction rules for PHP.
var PHPSpec = &LanguageSpec{
	GetLanguage: php.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_definition":   "function",
		"class_declaration":     "class",
		"method_declaration":    "method",
		"interface_declaration": "type",
		"trait_declaration":     "type",
		"enum_declaration":      "type",
	},
	NameFields: map[string]string{
		"function_definition":   "name",
		"class_declaration":     "name",
		"method_declaration":    "name",
		"interface_declaration": "name",
		"trait_declaration":     "name",
		"enum_declaration":      "name",
	},
	ParamFields: map[string]string{
		"function_definition": "parameters",
		"method_declaration":  "parameters",
	},
	ReturnTypeFields: map[string]string{
		"function_definition": "return_type",
		"method_declaration":  "return_type",
	},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "attribute",
	ContainerNodeTypes: []string{"class_declaration", "trait_declaration", "interface_declaration"},
	ConstantPatterns:   []string{"const_declaration"},
	TypePatterns:       []string{"interface_declaration", "trait_declaration", "enum_declaration"},
}

// CSpec defines symbol extraction rules for C.
var CSpec = &LanguageSpec{
	GetLanguage: c.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_definition": "function",
		"declaration":         "function", // function declarations
		"struct_specifier":    "type",
		"enum_specifier":      "type",
		"type_definition":     "type",
	},
	NameFields: map[string]string{
		"function_definition": "declarator",
		"struct_specifier":    "name",
		"enum_specifier":      "name",
	},
	ParamFields: map[string]string{
		"function_definition": "declarator",
	},
	ReturnTypeFields:   map[string]string{},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "",
	ContainerNodeTypes: []string{},
	ConstantPatterns:   []string{"preproc_def"},
	TypePatterns:       []string{"struct_specifier", "enum_specifier", "type_definition"},
}

// CppSpec defines symbol extraction rules for C++.
var CppSpec = &LanguageSpec{
	GetLanguage: cpp.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_definition":  "function",
		"class_specifier":      "class",
		"struct_specifier":     "type",
		"enum_specifier":       "type",
		"namespace_definition": "type",
		"template_declaration": "function",
	},
	NameFields: map[string]string{
		"function_definition":  "declarator",
		"class_specifier":      "name",
		"struct_specifier":     "name",
		"enum_specifier":       "name",
		"namespace_definition": "name",
	},
	ParamFields: map[string]string{
		"function_definition": "declarator",
	},
	ReturnTypeFields:   map[string]string{},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "",
	ContainerNodeTypes: []string{"class_specifier", "struct_specifier", "namespace_definition"},
	ConstantPatterns:   []string{"preproc_def"},
	TypePatterns:       []string{"class_specifier", "struct_specifier", "enum_specifier"},
}

// RubySpec defines symbol extraction rules for Ruby.
var RubySpec = &LanguageSpec{
	GetLanguage: ruby.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"method":           "function",
		"singleton_method": "function",
		"class":            "class",
		"module":           "type",
	},
	NameFields: map[string]string{
		"method":           "name",
		"singleton_method": "name",
		"class":            "name",
		"module":           "name",
	},
	ParamFields: map[string]string{
		"method":           "parameters",
		"singleton_method": "parameters",
	},
	ReturnTypeFields:   map[string]string{},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "",
	ContainerNodeTypes: []string{"class", "module"},
	ConstantPatterns:   []string{"assignment"},
	TypePatterns:       []string{"module"},
}

// KotlinSpec defines symbol extraction rules for Kotlin.
var KotlinSpec = &LanguageSpec{
	GetLanguage: kotlin.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_declaration":  "function",
		"class_declaration":     "class",
		"object_declaration":    "class",
		"interface_declaration": "type",
	},
	NameFields: map[string]string{
		"function_declaration":  "name",
		"class_declaration":     "name",
		"object_declaration":    "name",
		"interface_declaration": "name",
	},
	ParamFields: map[string]string{
		"function_declaration": "value_parameters",
	},
	ReturnTypeFields: map[string]string{
		"function_declaration": "return_type",
	},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "annotation",
	ContainerNodeTypes: []string{"class_declaration", "object_declaration", "interface_declaration"},
	ConstantPatterns:   []string{"property_declaration"},
	TypePatterns:       []string{"interface_declaration"},
}

// SwiftSpec defines symbol extraction rules for Swift.
var SwiftSpec = &LanguageSpec{
	GetLanguage: swift.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_declaration": "function",
		"class_declaration":    "class",
		"struct_declaration":   "type", // Note: tree-sitter-swift uses "class_declaration" for struct too in some versions
		"protocol_declaration": "type",
		"enum_declaration":     "type",
	},
	NameFields: map[string]string{
		"function_declaration": "name",
		"class_declaration":    "name",
		"struct_declaration":   "name",
		"protocol_declaration": "name",
		"enum_declaration":     "name",
	},
	ParamFields: map[string]string{
		"function_declaration": "parameters",
	},
	ReturnTypeFields: map[string]string{
		"function_declaration": "return_type",
	},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "attribute",
	ContainerNodeTypes: []string{"class_declaration", "struct_declaration", "protocol_declaration", "enum_declaration"},
	ConstantPatterns:   []string{"property_declaration"},
	TypePatterns:       []string{"struct_declaration", "protocol_declaration", "enum_declaration"},
}

// LuaSpec defines symbol extraction rules for Lua.
var LuaSpec = &LanguageSpec{
	GetLanguage: lua.GetLanguage,
	SymbolNodeTypes: map[string]string{
		"function_statement": "function",
	},
	NameFields: map[string]string{
		"function_statement": "name",
	},
	ParamFields: map[string]string{
		"function_statement": "parameters",
	},
	ReturnTypeFields:   map[string]string{},
	DocstringStrategy:  DocstringPrecedingComment,
	DecoratorNodeType:  "",
	ContainerNodeTypes: []string{},
	ConstantPatterns:   []string{},
	TypePatterns:       []string{},
}
