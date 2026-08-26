# Security Controls

code-scale-mcp indexes source code from local folders and GitHub repositories via tree-sitter AST parsing. This document describes the security controls that protect against common risks when handling arbitrary codebases.

## Trust and network boundary

The default stdio workflow is local-first. Indexing, parsing, semantic
analysis, planning, context assembly, and SQLite/cache access do not require a
remote service.

Network access is still possible when explicitly used:

* `index_repo` calls the GitHub API and may read a repository's remote
  `.gitignore`.
* AI summaries are opt-in through `use_ai_summaries`. `ANTHROPIC_API_KEY`
  uses Anthropic; `GOOGLE_API_KEY` uses Google when Anthropic is not set. The
  summarizer sends symbol kind/signature prompts to the selected provider.
* SSE/HTTP is opt-in through `--transport=sse`; `CODE_SCALE_AUTH_TOKEN`
  enables bearer authentication.

No remote telemetry service is required. `CODE_SCALE_TELEMETRY=full` changes
the local response metadata and token tracker, not the network boundary.

---

## Path Traversal Prevention

All user-supplied paths are validated before any file is read or written.

* **`security.ValidatePath(root, target)`** resolves both paths to absolute form via `filepath.Abs` + `filepath.Clean` and verifies the target is a descendant of `root` using `filepath.Rel`.
* Applied during file discovery and again before each file read (defense in depth).
* Paths such as `../../etc/passwd` or absolute paths outside the repository root are rejected.

---

## Symlink Escape Protection

Symlinks can be used to escape the repository root and read arbitrary files.

* **Default:** symlinks are checked during file discovery via `os.Lstat`.
* **`security.IsSymlinkEscape(root, path)`** resolves the symlink target with `filepath.EvalSymlinks` and validates it against the repository root. Escaping symlinks are rejected.
* If a symlink cannot be resolved, it is treated as an escape (fail-closed).

---

## Allowed Root Enforcement

Indexing is restricted to safe directory trees.

* **`CODE_SCALE_ALLOWED_ROOTS`** — when set, only paths under the specified colon-separated roots (`;` on Windows) can be indexed. All other paths are denied.
* **Default deny list** — when `CODE_SCALE_ALLOWED_ROOTS` is not set, known system directories are denied: `/etc`, `/usr`, `/var`, `/root`, `/bin`, `/sbin`, `/boot`, `/dev`, `/proc`, `/sys`, `/lib`, `/lib64`, `/System`, `/Library`, `/private`, `C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`.
* The filesystem root (`/` or `C:\`) is always denied.
* **`security.IsAllowedRootPath(absPath)`** enforces these checks before any indexing or watching operation begins.

---

## Default Ignore Policy

Files are filtered through multiple layers during discovery:

1. **SkipPatterns** — directories always excluded: `node_modules`, `vendor`, `.git`, `build`, `dist`, `__pycache__`, `.tox`, `.mypy_cache`, `.pytest_cache`, `.venv`, `venv`, `.eggs`, `target`.
2. **SkipFiles** — files always excluded: `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `poetry.lock`, `Cargo.lock`, `go.sum`, `composer.lock`, and minified files (`*.min.js`, `*.min.css`).
3. **`.gitignore`** — respected for local discovery and GitHub repositories.
4. **`extra_ignore_patterns`** — user-configurable additional ignore patterns passed to the `index_folder` tool.

---

## Secret Exclusion

Files matching known secret patterns are excluded during indexing.

**Excluded patterns include:**

* Environment files: `.env`, `.env.*`, `*.env`
* Certificates / keys: `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.keystore`, `*.jks`
* SSH keys: `id_rsa`, `id_rsa.*`, `id_ed25519`, `id_ed25519.*`, `id_dsa`, `id_ecdsa`
* Credentials: `credentials.json`, `service-account*.json`, `*.credentials`
* Auth files: `.htpasswd`, `.netrc`, `.npmrc`, `.pypirc`
* Generic secret indicators: `*secret*`, `*.secrets`, `*.token`

When a secret file is detected, it is excluded with reason `secret_file`. Secret files are never stored in the index or cached content.

---

## File Size Limits

* **Default maximum:** 500 KB per file (`DefaultMaxFileSize`).
* Files exceeding the limit are skipped during discovery with reason `file_too_large`.

---

## Binary File Detection

Binary files are excluded using a two-stage check:

1. **Extension-based detection** — common binary extensions including executables (`.exe`, `.dll`, `.so`, `.dylib`), archives (`.zip`, `.tar`, `.gz`), images (`.png`, `.jpg`, `.gif`), media (`.mp3`, `.mp4`), documents (`.pdf`, `.docx`), compiled bytecode (`.pyc`, `.class`, `.wasm`), databases (`.db`, `.sqlite`), fonts (`.ttf`, `.woff`), and more.
2. **Content-based detection** — files containing null bytes within the first 8 KB are treated as binary and skipped, even if the extension suggests source code.

---

## Encoding Safety

* **`security.SafeDecode(data)`** decodes bytes to string, replacing invalid UTF-8 sequences with the Unicode replacement character (U+FFFD) instead of panicking or producing garbled output.
* All file content reads go through safe decoding to ensure robust handling of mixed-encoding codebases.

---

## Storage Safety

* Index storage defaults to `~/.code-index/`.
* The storage path can be overridden using the `CODE_INDEX_PATH` environment variable.
* Repository identifiers are validated via **`security.SafeRepoComponent()`** — only alphanumeric characters, dots, hyphens, and underscores are allowed. Path separators, empty strings, `.`, and `..` are rejected, preventing path injection in storage locations.
* Index data is stored in SQLite with FTS5 for full-text search.

---

## SSE Transport Authentication

When running in SSE/HTTP mode, authentication can be enforced:

* **`CODE_SCALE_AUTH_TOKEN`** — when set, all SSE requests must include an `Authorization: Bearer <token>` header. Requests without a valid token receive `401 Unauthorized`.
* When unset, SSE mode runs without authentication and a warning is logged at startup.
* stdio transport (default) does not require authentication as it communicates directly with the parent process.

---

## Summary of Controls

| Control                     | Location                           | Default                        |
| --------------------------- | ---------------------------------- | ------------------------------ |
| Path traversal validation   | `security.ValidatePath()`          | Always enabled                 |
| Symlink escape protection   | `security.IsSymlinkEscape()`       | Always enabled (fail-closed)   |
| Allowed root enforcement    | `security.IsAllowedRootPath()`     | System dirs denied by default  |
| Secret file exclusion       | `security.IsSecretFile()`          | Always enabled                 |
| Binary file detection       | `security.IsBinaryFile()`          | Always enabled                 |
| File size limit             | `security.ShouldExcludeFile()`     | 500 KB                         |
| Directory skip patterns     | `security.ShouldSkipDir()`         | Always enabled                 |
| File skip patterns          | `security.ShouldSkipFile()`        | Always enabled                 |
| UTF-8 safe decode           | `security.SafeDecode()`            | Always enabled                 |
| Repo component validation   | `security.SafeRepoComponent()`     | Always enabled                 |
| SSE bearer authentication   | `authMiddleware()`                 | Opt-in via `CODE_SCALE_AUTH_TOKEN` |
| `.gitignore` respect        | Local and GitHub discovery         | Enabled                         |
