package parser

import (
	"errors"
	"os"
	"testing"
)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	return data
}

func TestParsePython(t *testing.T) {
	src := readFixture(t, "../../testdata/python/sample.py")
	symbols, err := ParseFile(src, "sample.py", "python")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}

	// Expect: MAX_RETRIES (constant), UserService (class), get_user (method), delete_user (method), authenticate (function)
	names := make(map[string]string) // name -> kind
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	assertSymbol(t, names, "MAX_RETRIES", KindConstant)
	assertSymbol(t, names, "UserService", KindClass)
	assertSymbol(t, names, "get_user", KindMethod)
	assertSymbol(t, names, "delete_user", KindMethod)
	assertSymbol(t, names, "authenticate", KindFunction)

	// Check qualified names
	for _, s := range symbols {
		if s.Name == "get_user" {
			if s.QualifiedName != "UserService.get_user" {
				t.Errorf("expected qualified name UserService.get_user, got %s", s.QualifiedName)
			}
			if s.Parent == "" {
				t.Error("expected get_user to have a parent")
			}
		}
	}

	// Check docstrings
	for _, s := range symbols {
		if s.Name == "authenticate" && s.Docstring == "" {
			t.Error("expected authenticate to have a docstring")
		}
		if s.Name == "UserService" && s.Docstring == "" {
			t.Error("expected UserService to have a docstring")
		}
	}

	// Check byte offsets are set
	for _, s := range symbols {
		if s.ByteLength == 0 {
			t.Errorf("symbol %s has zero byte length", s.Name)
		}
		if s.Line == 0 {
			t.Errorf("symbol %s has zero line number", s.Name)
		}
	}

	// Check symbol IDs
	for _, s := range symbols {
		if s.ID == "" {
			t.Errorf("symbol %s has empty ID", s.Name)
		}
	}

	// Check content hash
	for _, s := range symbols {
		if s.ContentHash == "" {
			t.Errorf("symbol %s has empty content hash", s.Name)
		}
	}
}

func TestParseGo(t *testing.T) {
	src := readFixture(t, "../../testdata/go/sample.go")
	symbols, err := ParseFile(src, "sample.go", "go")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	names := make(map[string]string)
	for _, s := range symbols {
		names[s.Name] = s.Kind
	}

	assertSymbol(t, names, "User", KindType)
	assertSymbol(t, names, "GetUser", KindFunction)
	assertSymbol(t, names, "Authenticate", KindFunction)

	// Check docstrings for Go
	for _, s := range symbols {
		if s.Name == "GetUser" && s.Docstring == "" {
			t.Error("expected GetUser to have a docstring")
		}
		if s.Name == "User" && s.Docstring == "" {
			t.Error("expected User to have a docstring")
		}
	}
}

func TestParseJavaScript(t *testing.T) {
	src := readFixture(t, "../../testdata/javascript/sample.js")
	symbols, err := ParseFile(src, "sample.js", "javascript")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}

	// Verify we got function and class symbols
	hasFunction := false
	hasClass := false
	for _, s := range symbols {
		if s.Kind == KindFunction {
			hasFunction = true
		}
		if s.Kind == KindClass {
			hasClass = true
		}
	}
	if !hasFunction {
		t.Error("expected at least one function symbol")
	}
	if !hasClass {
		t.Error("expected at least one class symbol")
	}
}

func TestParseTypeScript(t *testing.T) {
	src := readFixture(t, "../../testdata/typescript/sample.ts")
	symbols, err := ParseFile(src, "sample.ts", "typescript")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
}

func TestParseRust(t *testing.T) {
	src := readFixture(t, "../../testdata/rust/sample.rs")
	symbols, err := ParseFile(src, "sample.rs", "rust")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
}

func TestParseJava(t *testing.T) {
	src := readFixture(t, "../../testdata/java/Sample.java")
	symbols, err := ParseFile(src, "Sample.java", "java")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
}

func TestParsePHP(t *testing.T) {
	src := readFixture(t, "../../testdata/php/sample.php")
	symbols, err := ParseFile(src, "sample.php", "php")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
}

func TestParseC(t *testing.T) {
	src := readFixture(t, "../../testdata/c/sample.c")
	symbols, err := ParseFile(src, "sample.c", "c")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	t.Logf("C symbols: %d", len(symbols))
	for _, s := range symbols {
		t.Logf("  %s (%s): %s", s.Name, s.Kind, s.Signature[:min2(60, len(s.Signature))])
	}
}

func TestParseCpp(t *testing.T) {
	src := readFixture(t, "../../testdata/cpp/sample.cpp")
	symbols, err := ParseFile(src, "sample.cpp", "cpp")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	t.Logf("C++ symbols: %d", len(symbols))
	for _, s := range symbols {
		t.Logf("  %s (%s)", s.Name, s.Kind)
	}
}

func TestParseRuby(t *testing.T) {
	src := readFixture(t, "../../testdata/ruby/sample.rb")
	symbols, err := ParseFile(src, "sample.rb", "ruby")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	t.Logf("Ruby symbols: %d", len(symbols))
	for _, s := range symbols {
		t.Logf("  %s (%s)", s.Name, s.Kind)
	}
}

func TestParseKotlin(t *testing.T) {
	src := readFixture(t, "../../testdata/kotlin/sample.kt")
	symbols, err := ParseFile(src, "sample.kt", "kotlin")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	t.Logf("Kotlin symbols: %d", len(symbols))
	for _, s := range symbols {
		t.Logf("  %s (%s)", s.Name, s.Kind)
	}
}

func TestParseSwift(t *testing.T) {
	src := readFixture(t, "../../testdata/swift/sample.swift")
	symbols, err := ParseFile(src, "sample.swift", "swift")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	// Swift grammar may not extract all symbols depending on tree-sitter version
	t.Logf("Swift symbols: %d", len(symbols))
	for _, s := range symbols {
		t.Logf("  %s (%s)", s.Name, s.Kind)
	}
}

func TestParseLua(t *testing.T) {
	src := readFixture(t, "../../testdata/lua/sample.lua")
	symbols, err := ParseFile(src, "sample.lua", "lua")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}
	t.Logf("Lua symbols: %d", len(symbols))
	for _, s := range symbols {
		t.Logf("  %s (%s)", s.Name, s.Kind)
	}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestParseUnsupportedLanguage(t *testing.T) {
	symbols, err := ParseFile([]byte("hello"), "test.txt", "unknown")
	if err == nil {
		t.Fatalf("expected error for unsupported language, got nil")
	}
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("expected ErrUnsupportedLanguage, got: %v", err)
	}
	if symbols != nil {
		t.Errorf("expected nil symbols for unsupported language, got %d symbols", len(symbols))
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"main.py", "python"},
		{"app.js", "javascript"},
		{"app.jsx", "javascript"},
		{"index.ts", "typescript"},
		{"index.tsx", "typescript"},
		{"main.go", "go"},
		{"lib.rs", "rust"},
		{"App.java", "java"},
		{"index.php", "php"},
		{"readme.txt", ""},
	}

	for _, tt := range tests {
		got := DetectLanguage(tt.filename)
		if got != tt.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestDisambiguateOverloads(t *testing.T) {
	symbols := []Symbol{
		{ID: "test.py::foo#function", Name: "foo"},
		{ID: "test.py::foo#function", Name: "foo"},
		{ID: "test.py::bar#function", Name: "bar"},
	}

	result := disambiguateOverloads(symbols)
	if result[0].ID != "test.py::foo#function~1" {
		t.Errorf("expected ~1 suffix, got %s", result[0].ID)
	}
	if result[1].ID != "test.py::foo#function~2" {
		t.Errorf("expected ~2 suffix, got %s", result[1].ID)
	}
	if result[2].ID != "test.py::bar#function" {
		t.Errorf("expected no suffix for bar, got %s", result[2].ID)
	}
}

func TestMakeSymbolID(t *testing.T) {
	id := MakeSymbolID("src/main.py", "UserService.login", "method")
	if id != "src/main.py::UserService.login#method" {
		t.Errorf("unexpected ID: %s", id)
	}

	id2 := MakeSymbolID("src/main.py", "UserService", "")
	if id2 != "src/main.py::UserService" {
		t.Errorf("unexpected ID without kind: %s", id2)
	}
}

func TestComputeContentHash(t *testing.T) {
	hash := ComputeContentHash([]byte("hello world"))
	if hash == "" {
		t.Error("expected non-empty hash")
	}
	if len(hash) != 64 { // SHA-256 hex
		t.Errorf("expected 64 char hash, got %d", len(hash))
	}

	// Same input should produce same hash
	hash2 := ComputeContentHash([]byte("hello world"))
	if hash != hash2 {
		t.Error("expected same hash for same input")
	}
}

func makeHierarchyFixture() []Symbol {
	parentID := "test.py::MyClass#class"
	return []Symbol{
		{ID: parentID, Name: "MyClass", Kind: KindClass},
		{ID: "test.py::MyClass.method1#method", Name: "method1", Kind: KindMethod, Parent: parentID},
		{ID: "test.py::MyClass.method2#method", Name: "method2", Kind: KindMethod, Parent: parentID},
		{ID: "test.py::standalone#function", Name: "standalone", Kind: KindFunction},
	}
}

func TestBuildSymbolTree(t *testing.T) {
	tree := BuildSymbolTree(makeHierarchyFixture())
	if len(tree) != 2 {
		t.Fatalf("expected 2 root nodes, got %d", len(tree))
	}
	if tree[0].Symbol.Name != "MyClass" {
		t.Errorf("expected MyClass as first root, got %s", tree[0].Symbol.Name)
	}
	if len(tree[0].Children) != 2 {
		t.Errorf("expected 2 children for MyClass, got %d", len(tree[0].Children))
	}
	if tree[1].Symbol.Name != "standalone" {
		t.Errorf("expected standalone as second root, got %s", tree[1].Symbol.Name)
	}
}

func TestFlattenSymbols(t *testing.T) {
	flat := FlattenSymbols(makeHierarchyFixture())

	if len(flat) != 4 {
		t.Fatalf("expected 4 flat nodes, got %d", len(flat))
	}

	// MyClass at depth 0
	if flat[0].Symbol.Name != "MyClass" || flat[0].Depth != 0 {
		t.Errorf("expected MyClass at depth 0, got %s at depth %d", flat[0].Symbol.Name, flat[0].Depth)
	}
	// method1 at depth 1
	if flat[1].Symbol.Name != "method1" || flat[1].Depth != 1 {
		t.Errorf("expected method1 at depth 1, got %s at depth %d", flat[1].Symbol.Name, flat[1].Depth)
	}
	// method2 at depth 1
	if flat[2].Symbol.Name != "method2" || flat[2].Depth != 1 {
		t.Errorf("expected method2 at depth 1, got %s at depth %d", flat[2].Symbol.Name, flat[2].Depth)
	}
	// standalone at depth 0
	if flat[3].Symbol.Name != "standalone" || flat[3].Depth != 0 {
		t.Errorf("expected standalone at depth 0, got %s at depth %d", flat[3].Symbol.Name, flat[3].Depth)
	}
}

func TestFlattenSymbolsEmpty(t *testing.T) {
	flat := FlattenSymbols(nil)
	if len(flat) != 0 {
		t.Errorf("expected empty result, got %d items", len(flat))
	}
}

func assertSymbol(t *testing.T, names map[string]string, name string, kind string) {
	t.Helper()
	got, ok := names[name]
	if !ok {
		t.Errorf("expected symbol %q not found", name)
		return
	}
	if got != kind {
		t.Errorf("symbol %q: expected kind %q, got %q", name, kind, got)
	}
}
