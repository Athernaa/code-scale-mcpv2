package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePath(t *testing.T) {
	tmp := t.TempDir()

	// Valid: path inside root
	inner := filepath.Join(tmp, "subdir", "file.txt")
	if !ValidatePath(tmp, inner) {
		t.Error("expected path inside root to be valid")
	}

	// Invalid: path traversal
	if ValidatePath(tmp, filepath.Join(tmp, "..", "etc", "passwd")) {
		t.Error("expected path traversal to be invalid")
	}

	// Valid: root itself
	if !ValidatePath(tmp, tmp) {
		t.Error("expected root itself to be valid")
	}
}

func TestIsSecretFile(t *testing.T) {
	tests := []struct {
		path   string
		secret bool
	}{
		{".env", true},
		{".env.local", true},
		{"config/.env.production", true},
		{"server.pem", true},
		{"private.key", true},
		{"id_rsa", true},
		{"id_ed25519", true},
		{"credentials.json", true},
		{".htpasswd", true},
		{".netrc", true},
		{"my_secret_config.yaml", true},
		// Not secrets
		{"main.py", false},
		{"README.md", false},
		{"config.json", false},
		{"server.go", false},
	}

	for _, tt := range tests {
		got := IsSecretFile(tt.path)
		if got != tt.secret {
			t.Errorf("IsSecretFile(%q) = %v, want %v", tt.path, got, tt.secret)
		}
	}
}

func TestIsBinaryExtension(t *testing.T) {
	tests := []struct {
		path   string
		binary bool
	}{
		{"image.png", true},
		{"app.exe", true},
		{"data.pdf", true},
		{"lib.so", true},
		{"main.py", false},
		{"app.js", false},
		{"README.md", false},
	}

	for _, tt := range tests {
		got := IsBinaryExtension(tt.path)
		if got != tt.binary {
			t.Errorf("IsBinaryExtension(%q) = %v, want %v", tt.path, got, tt.binary)
		}
	}
}

func TestIsBinaryContent(t *testing.T) {
	// Text content
	if IsBinaryContent([]byte("hello world\nfoo bar")) {
		t.Error("expected text content to not be binary")
	}

	// Binary content (null byte)
	if !IsBinaryContent([]byte("hello\x00world")) {
		t.Error("expected content with null byte to be binary")
	}

	// Empty
	if IsBinaryContent([]byte{}) {
		t.Error("expected empty content to not be binary")
	}
}

func TestShouldSkipDir(t *testing.T) {
	if !ShouldSkipDir("node_modules") {
		t.Error("expected node_modules to be skipped")
	}
	if !ShouldSkipDir(".git") {
		t.Error("expected .git to be skipped")
	}
	if ShouldSkipDir("src") {
		t.Error("expected src to not be skipped")
	}
}

func TestShouldSkipFile(t *testing.T) {
	if !ShouldSkipFile("package-lock.json") {
		t.Error("expected package-lock.json to be skipped")
	}
	if !ShouldSkipFile("bundle.min.js") {
		t.Error("expected minified JS to be skipped")
	}
	if ShouldSkipFile("main.go") {
		t.Error("expected main.go to not be skipped")
	}
}

func TestSafeRepoComponent(t *testing.T) {
	tests := []struct {
		value string
		safe  bool
	}{
		{"myrepo", true},
		{"my-repo", true},
		{"my_repo.v2", true},
		{"", false},
		{".", false},
		{"..", false},
		{"path/traversal", false},
		{"back\\slash", false},
	}

	for _, tt := range tests {
		got := SafeRepoComponent(tt.value)
		if got != tt.safe {
			t.Errorf("SafeRepoComponent(%q) = %v, want %v", tt.value, got, tt.safe)
		}
	}
}

func TestSafeDecode(t *testing.T) {
	// Valid UTF-8
	s := SafeDecode([]byte("hello world"))
	if s != "hello world" {
		t.Errorf("expected 'hello world', got %q", s)
	}

	// Invalid UTF-8 should produce replacement characters
	invalid := []byte{0xFF, 0xFE, 0x68, 0x65, 0x6C, 0x6C, 0x6F}
	s = SafeDecode(invalid)
	if len(s) == 0 {
		t.Error("expected non-empty string for invalid UTF-8")
	}
}

func TestShouldExcludeFile(t *testing.T) {
	tmp := t.TempDir()

	// Create a normal file
	normalPath := filepath.Join(tmp, "main.py")
	if err := os.WriteFile(normalPath, []byte("print('hello')"), 0644); err != nil {
		t.Fatal(err)
	}

	reason := ShouldExcludeFile(normalPath, tmp, DefaultMaxFileSize)
	if reason != "" {
		t.Errorf("expected normal file to not be excluded, got %q", reason)
	}

	// Create a secret file
	secretPath := filepath.Join(tmp, ".env")
	if err := os.WriteFile(secretPath, []byte("SECRET=123"), 0644); err != nil {
		t.Fatal(err)
	}

	reason = ShouldExcludeFile(secretPath, tmp, DefaultMaxFileSize)
	if reason != string(ReasonSecretFile) {
		t.Errorf("expected secret_file reason, got %q", reason)
	}

	// Create a large file
	largePath := filepath.Join(tmp, "large.txt")
	large := make([]byte, DefaultMaxFileSize+1)
	if err := os.WriteFile(largePath, large, 0644); err != nil {
		t.Fatal(err)
	}

	reason = ShouldExcludeFile(largePath, tmp, DefaultMaxFileSize)
	if reason != string(ReasonFileTooLarge) {
		t.Errorf("expected file_too_large reason, got %q", reason)
	}

	// Binary extension
	binaryPath := filepath.Join(tmp, "image.png")
	if err := os.WriteFile(binaryPath, []byte{0x89, 0x50, 0x4E, 0x47}, 0644); err != nil {
		t.Fatal(err)
	}

	reason = ShouldExcludeFile(binaryPath, tmp, DefaultMaxFileSize)
	if reason != string(ReasonBinaryExt) {
		t.Errorf("expected binary_extension reason, got %q", reason)
	}
}
