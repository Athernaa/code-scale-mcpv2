package security

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// DefaultMaxFileSize is the default maximum file size (500KB).
const DefaultMaxFileSize = 500 * 1024

// ValidatePath checks that target path resolves within root directory.
// Prevents path traversal attacks.
func ValidatePath(root, target string) bool {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

// IsSymlinkEscape checks if a symlink points outside the root directory.
func IsSymlinkEscape(root, path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return true
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return true
	}
	return !ValidatePath(root, resolved)
}

// SecretPatterns are glob patterns matching known secret files.
var SecretPatterns = []string{
	"*.env",
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*.credentials",
	"*.keystore",
	"*.jks",
	"*.token",
	"*secret*",
	"id_rsa",
	"id_rsa.*",
	"id_ed25519",
	"id_ed25519.*",
	"id_dsa",
	"id_ecdsa",
	".htpasswd",
	".netrc",
	".npmrc",
	".pypirc",
	"credentials.json",
	"service-account*.json",
	"*.secrets",
}

// IsSecretFile checks if a file path matches known secret file patterns.
func IsSecretFile(filePath string) bool {
	name := strings.ToLower(filepath.Base(filePath))
	pathLower := strings.ToLower(filePath)

	for _, pattern := range SecretPatterns {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, pathLower); matched {
			return true
		}
	}
	return false
}

// BinaryExtensions is the set of known binary file extensions.
var BinaryExtensions = map[string]bool{
	// Executables
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true, ".out": true,
	// Object files
	".o": true, ".obj": true, ".a": true, ".lib": true,
	// Archives
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true,
	// Images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".ico": true, ".svg": true,
	".webp": true, ".tiff": true, ".tif": true,
	// Media
	".mp3": true, ".mp4": true, ".avi": true, ".mov": true, ".mkv": true, ".wav": true, ".flac": true,
	".ogg": true, ".webm": true,
	// Documents
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	// Compiled / bytecode
	".pyc": true, ".pyo": true, ".class": true, ".wasm": true,
	// Database
	".db": true, ".sqlite": true, ".sqlite3": true,
	// Fonts
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	// Other
	".jar": true, ".war": true, ".ear": true,
}

// IsBinaryExtension checks if a file has a known binary extension.
func IsBinaryExtension(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return BinaryExtensions[ext]
}

// IsBinaryContent detects binary content by checking for null bytes in the first 8KB.
func IsBinaryContent(data []byte) bool {
	limit := 8192
	if len(data) < limit {
		limit = len(data)
	}
	return bytes.ContainsRune(data[:limit], 0)
}

// IsBinaryFile checks if a file is binary (extension + content sniffing).
func IsBinaryFile(filePath string) bool {
	if IsBinaryExtension(filePath) {
		return true
	}
	f, err := os.Open(filePath)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil {
		return true
	}
	return IsBinaryContent(buf[:n])
}

// SafeDecode decodes bytes to string, replacing invalid UTF-8 with U+FFFD.
func SafeDecode(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		b.WriteRune(r)
		data = data[size:]
	}
	return b.String()
}

// SkipPatterns are directory/file names to always skip during discovery.
var SkipPatterns = []string{
	"node_modules",
	"vendor",
	".git",
	"build",
	"dist",
	"__pycache__",
	".tox",
	".mypy_cache",
	".pytest_cache",
	".venv",
	"venv",
	".eggs",
	"target", // Rust
}

// SkipFiles are file names/patterns to always skip.
var SkipFiles = []string{
	"package-lock.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	"poetry.lock",
	"Cargo.lock",
	"go.sum",
	"composer.lock",
}

// ShouldSkipDir checks if a directory name should be skipped.
func ShouldSkipDir(name string) bool {
	for _, pattern := range SkipPatterns {
		if name == pattern {
			return true
		}
	}
	return false
}

// ShouldSkipFile checks if a file name should be skipped.
func ShouldSkipFile(name string) bool {
	for _, pattern := range SkipFiles {
		if name == pattern {
			return true
		}
	}
	// Skip minified files
	if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") {
		return true
	}
	return false
}

// SafeRepoComponent validates an owner or repo name component.
var safeComponentRegex = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// SafeRepoComponent checks if a string is safe to use as a repo component (no path traversal).
func SafeRepoComponent(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	if strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	return safeComponentRegex.MatchString(value)
}

// DeniedRootPaths are system directories that should never be indexed.
var DeniedRootPaths = []string{
	"/etc",
	"/var",
	"/usr",
	"/bin",
	"/sbin",
	"/boot",
	"/dev",
	"/proc",
	"/sys",
	"/lib",
	"/lib64",
	"/root",
	"/System",
	"/Library",
	"/private",
	"C:\\Windows",
	"C:\\Program Files",
	"C:\\Program Files (x86)",
}

// IsAllowedRootPath checks that the given absolute path is not a sensitive system directory.
// Returns false if the path is or is inside a denied root.
func IsAllowedRootPath(absPath string) bool {
	cleaned := filepath.Clean(absPath)

	// Check allowed roots from environment (if set, only allow those)
	if allowed := os.Getenv("CODE_SCALE_ALLOWED_ROOTS"); allowed != "" {
		for _, root := range strings.Split(allowed, string(os.PathListSeparator)) {
			root = filepath.Clean(root)
			if cleaned == root || strings.HasPrefix(cleaned, root+string(os.PathSeparator)) {
				return true
			}
		}
		return false
	}

	// Default: deny known system paths
	for _, denied := range DeniedRootPaths {
		denied = filepath.Clean(denied)
		if cleaned == denied || strings.HasPrefix(cleaned, denied+string(os.PathSeparator)) {
			return false
		}
	}

	// Also deny the filesystem root itself
	if cleaned == "/" || cleaned == `C:\` {
		return false
	}

	return true
}

// ExcludeReason represents why a file was excluded.
type ExcludeReason string

const (
	ReasonSymlinkEscape ExcludeReason = "symlink_escape"
	ReasonPathTraversal ExcludeReason = "path_traversal"
	ReasonOutsideRoot   ExcludeReason = "outside_root"
	ReasonSecretFile    ExcludeReason = "secret_file"
	ReasonFileTooLarge  ExcludeReason = "file_too_large"
	ReasonUnreadable    ExcludeReason = "unreadable"
	ReasonBinaryExt     ExcludeReason = "binary_extension"
)

// ShouldExcludeFile runs all security checks. Returns reason if excluded, empty string if ok.
func ShouldExcludeFile(filePath, root string, maxFileSize int64) string {
	// Symlink escape
	if IsSymlinkEscape(root, filePath) {
		return string(ReasonSymlinkEscape)
	}

	// Path traversal
	if !ValidatePath(root, filePath) {
		return string(ReasonPathTraversal)
	}

	// Relative path for pattern matching
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return string(ReasonOutsideRoot)
	}
	rel = filepath.ToSlash(rel)

	// Secret detection
	if IsSecretFile(rel) {
		return string(ReasonSecretFile)
	}

	// File size
	info, err := os.Stat(filePath)
	if err != nil {
		return string(ReasonUnreadable)
	}
	if info.Size() > maxFileSize {
		return string(ReasonFileTooLarge)
	}

	// Binary extension
	if IsBinaryExtension(rel) {
		return string(ReasonBinaryExt)
	}

	return ""
}
