package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/pathmatch"
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/search"
	"github.com/Athernaa/code-scale-mcpv2/internal/security"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/snippet"
	_ "modernc.org/sqlite"
)

// IndexStore manages the SQLite-backed code index.
type IndexStore struct {
	db       *sql.DB
	basePath string // for raw content files
	mu       sync.RWMutex
}

// RepoInfo contains metadata about an indexed repository.
type RepoInfo struct {
	ID          int64          `json:"id"`
	Owner       string         `json:"owner"`
	Name        string         `json:"name"`
	Repo        string         `json:"repo"`
	IndexedAt   string         `json:"indexed_at"`
	GitHead     string         `json:"git_head"`
	SourceType  string         `json:"source_type"`
	FileCount   int            `json:"file_count"`
	SymbolCount int            `json:"symbol_count"`
	Languages   map[string]int `json:"languages"`
}

// NewIndexStore creates a new index store with SQLite backend.
func NewIndexStore(basePath string) (*IndexStore, error) {
	if basePath == "" {
		basePath = os.Getenv("CODE_INDEX_PATH")
	}
	if basePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home dir: %w", err)
		}
		basePath = filepath.Join(home, ".code-index")
	}

	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("cannot create base path: %w", err)
	}

	dbPath := filepath.Join(basePath, "code-scale.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	store := &IndexStore{db: db, basePath: basePath}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *IndexStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection.
func (s *IndexStore) DB() *sql.DB {
	return s.db
}

// BasePath returns the base path for content files.
func (s *IndexStore) BasePath() string {
	return s.basePath
}

func (s *IndexStore) contentDir(owner, name string) (string, error) {
	return repository.ContentDir(s.basePath, owner, name)
}

// migrate runs schema migrations.
func (s *IndexStore) migrate() error {
	_, err := s.db.Exec(SchemaSQL)
	if err != nil {
		return fmt.Errorf("schema exec: %w", err)
	}

	// Check current schema version
	var currentVersion int
	err = s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		currentVersion = 0
	}

	// Apply incremental migrations
	if currentVersion < 2 {
		if _, err := s.db.Exec(MigrateV2SQL); err != nil {
			return fmt.Errorf("migrate v2: %w", err)
		}
	}
	if currentVersion < 3 {
		if _, err := s.db.Exec(MigrateV3SQL); err != nil {
			// Column may already exist from a partial migration
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migrate v3: %w", err)
			}
		}
	}
	if currentVersion < 4 {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migrate v4: %w", err)
		}
		if _, err := tx.Exec(MigrateV4SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate v4: %w", err)
		}
		// Bootstrap the external-content index once. Subsequent writes are
		// synchronized by the triggers installed above.
		if _, err := tx.Exec("INSERT INTO symbols_fts(symbols_fts) VALUES('rebuild')"); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("bootstrap FTS index: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migrate v4: %w", err)
		}
	}
	if currentVersion < 5 {
		if _, err := s.db.Exec(MigrateV5SQL); err != nil {
			return fmt.Errorf("migrate v5: %w", err)
		}
		if err := s.bootstrapFileFTS(); err != nil {
			return fmt.Errorf("bootstrap file text index: %w", err)
		}
	}
	if currentVersion < 6 {
		if _, err := s.db.Exec(MigrateV6SQL); err != nil {
			return fmt.Errorf("migrate v6: %w", err)
		}
	}
	if currentVersion < 7 {
		for _, table := range []string{"semantic_entities", "semantic_relationships"} {
			if ok, err := tableHasColumn(s.db, table, "analyzer"); err != nil {
				return fmt.Errorf("inspect v7 %s: %w", table, err)
			} else if !ok {
				if _, err := s.db.Exec("ALTER TABLE " + table + " ADD COLUMN analyzer TEXT NOT NULL DEFAULT 'fivem'"); err != nil {
					return fmt.Errorf("migrate v7 %s: %w", table, err)
				}
			}
		}
		if _, err := s.db.Exec(MigrateV7SQL); err != nil {
			return fmt.Errorf("migrate v7 indexes: %w", err)
		}
	}
	if currentVersion < 8 {
		if _, err := s.db.Exec(MigrateV8SQL); err != nil {
			return fmt.Errorf("migrate v8: %w", err)
		}
	}
	if currentVersion < 9 {
		columns := map[string]string{
			"files_discovered_total":      "INTEGER NOT NULL DEFAULT 0",
			"files_indexed":               "INTEGER NOT NULL DEFAULT 0",
			"index_truncated":             "INTEGER NOT NULL DEFAULT 0",
			"incomplete":                  "INTEGER NOT NULL DEFAULT 0",
			"resources_with_semantics":    "INTEGER NOT NULL DEFAULT 0",
			"resources_without_semantics": "INTEGER NOT NULL DEFAULT 0",
		}
		for column, definition := range columns {
			present, err := tableHasColumn(s.db, "workspaces", column)
			if err != nil {
				return fmt.Errorf("inspect v9 %s: %w", column, err)
			}
			if !present {
				if _, err := s.db.Exec("ALTER TABLE workspaces ADD COLUMN " + column + " " + definition); err != nil {
					return fmt.Errorf("migrate v9 %s: %w", column, err)
				}
			}
		}
	}
	if currentVersion < 10 {
		if _, err := s.db.Exec(MigrateV10SQL); err != nil {
			return fmt.Errorf("migrate v10: %w", err)
		}
	}

	// Upsert schema version
	_, err = s.db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", CurrentSchemaVersion)
	return err
}

func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// SavedWatch represents a persisted folder watch.
type SavedWatch struct {
	Path      string `json:"path"`
	Repo      string `json:"repo"`
	CreatedAt string `json:"created_at"`
}

// SaveWatch persists a folder watch to the database.
func (s *IndexStore) SaveWatch(path, repo string) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO watches (path, repo, created_at) VALUES (?, ?, ?)",
		path, repo, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// DeleteWatch removes a persisted folder watch.
func (s *IndexStore) DeleteWatch(path string) error {
	_, err := s.db.Exec("DELETE FROM watches WHERE path = ?", path)
	return err
}

// ListSavedWatches returns all persisted folder watches.
func (s *IndexStore) ListSavedWatches() ([]SavedWatch, error) {
	rows, err := s.db.Query("SELECT path, repo, created_at FROM watches ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var watches []SavedWatch
	for rows.Next() {
		var w SavedWatch
		if err := rows.Scan(&w.Path, &w.Repo, &w.CreatedAt); err != nil {
			return nil, err
		}
		watches = append(watches, w)
	}
	return watches, rows.Err()
}

// ReplaceRepoIndex performs a destructive full replacement of a repository index.
func (s *IndexStore) ReplaceRepoIndex(
	owner, name string,
	sourceType string,
	gitHead string,
	files map[string]string, // path -> content_hash
	fileLanguages map[string]string, // path -> language
	symbols []parser.Symbol,
	sourcePaths ...string, // optional: original source path for local repos
) error {
	if !security.SafeRepoComponent(owner) || !security.SafeRepoComponent(name) {
		return fmt.Errorf("invalid repository component: owner=%q name=%q", owner, name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	repo := owner + "/" + name
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Resolve optional source path
	sourcePath := ""
	if len(sourcePaths) > 0 {
		sourcePath = sourcePaths[0]
	}

	// Upsert repo
	var repoID int64
	err = tx.QueryRow("SELECT id FROM repos WHERE repo = ?", repo).Scan(&repoID)
	if err == sql.ErrNoRows {
		res, err := tx.Exec(
			"INSERT INTO repos (owner, name, repo, indexed_at, git_head, source_type, source_path) VALUES (?, ?, ?, ?, ?, ?, ?)",
			owner, name, repo, now, gitHead, sourceType, sourcePath,
		)
		if err != nil {
			return err
		}
		repoID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("get repo ID: %w", err)
		}
	} else if err != nil {
		return err
	} else {
		// Semantic analyzers own their own rows and replace them after the
		// generic symbol transaction completes. Do not erase another analyzer's
		// facts as part of a repository source replacement.
		if _, err := tx.Exec("DELETE FROM files_fts WHERE repo_id = ?", repoID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM symbols WHERE repo_id = ?", repoID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM files WHERE repo_id = ?", repoID); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE repos SET indexed_at = ?, git_head = ?, source_type = ?, source_path = ? WHERE id = ?", now, gitHead, sourceType, sourcePath, repoID); err != nil {
			return err
		}
	}

	// Insert files
	fileIDMap := make(map[string]int64)
	fileStmt, err := tx.Prepare("INSERT INTO files (repo_id, path, language, content_hash) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer func() { _ = fileStmt.Close() }()

	for path, hash := range files {
		lang := fileLanguages[path]
		res, err := fileStmt.Exec(repoID, path, lang, hash)
		if err != nil {
			return fmt.Errorf("insert file %s: %w", path, err)
		}
		fid, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("get file ID for %s: %w", path, err)
		}
		fileIDMap[path] = fid
	}

	// Insert symbols
	symStmt, err := tx.Prepare(`INSERT INTO symbols
		(repo_id, file_id, symbol_id, file_path, name, qualified_name, kind, language,
		 signature, content_hash, docstring, summary, decorators, keywords, parent_id,
		 line, end_line, byte_offset, byte_length)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = symStmt.Close() }()

	for _, sym := range symbols {
		fileID := fileIDMap[sym.File]
		decorJSON, err := json.Marshal(sym.Decorators)
		if err != nil {
			return fmt.Errorf("marshal decorators for %s: %w", sym.ID, err)
		}
		kwJSON, err := json.Marshal(sym.Keywords)
		if err != nil {
			return fmt.Errorf("marshal keywords for %s: %w", sym.ID, err)
		}

		_, err = symStmt.Exec(
			repoID, fileID, sym.ID, sym.File, sym.Name, sym.QualifiedName,
			sym.Kind, sym.Language, sym.Signature, sym.ContentHash,
			sym.Docstring, sym.Summary, string(decorJSON), string(kwJSON),
			sym.Parent, sym.Line, sym.EndLine, sym.ByteOffset, sym.ByteLength,
		)
		if err != nil {
			return fmt.Errorf("insert symbol %s: %w", sym.ID, err)
		}
	}
	if err := s.syncFileFTSTx(tx, repoID, owner, name, fileIDMap); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *IndexStore) bootstrapFileFTS() error {
	rows, err := s.db.Query("SELECT f.id, f.repo_id, f.path, r.owner, r.name FROM files f JOIN repos r ON r.id = f.repo_id")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var fileID, repoID int64
		var path, owner, name string
		if err := rows.Scan(&fileID, &repoID, &path, &owner, &name); err != nil {
			return err
		}
		contentDir, err := s.contentDir(owner, name)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(contentDir, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		if _, err := s.db.Exec("INSERT OR REPLACE INTO files_fts(rowid, repo_id, path, content) VALUES (?, ?, ?, ?)", fileID, repoID, path, string(data)); err != nil {
			return err
		}
	}
	return rows.Err()
}

// SaveIndex is retained for compatibility. New code should use
// ReplaceRepoIndex to make its destructive semantics explicit.
func (s *IndexStore) SaveIndex(
	owner, name string,
	sourceType string,
	gitHead string,
	files map[string]string,
	fileLanguages map[string]string,
	symbols []parser.Symbol,
	sourcePaths ...string,
) error {
	return s.ReplaceRepoIndex(owner, name, sourceType, gitHead, files, fileLanguages, symbols, sourcePaths...)
}

// UpsertFileIndex transactionally replaces one file and only that file's
// symbols. Unrelated files and symbols in the repository are preserved.
func (s *IndexStore) UpsertFileIndex(
	owner, name string,
	sourceType string,
	gitHead string,
	filePath string,
	contentHash string,
	language string,
	symbols []parser.Symbol,
	sourcePaths ...string,
) error {
	if !security.SafeRepoComponent(owner) || !security.SafeRepoComponent(name) {
		return fmt.Errorf("invalid repository component: owner=%q name=%q", owner, name)
	}
	if filePath == "" || filepath.IsAbs(filePath) || filepath.Clean(filePath) == ".." || strings.HasPrefix(filepath.Clean(filePath), ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid file path: %q", filePath)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	repo := owner + "/" + name
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var repoID int64
	if err := tx.QueryRow("SELECT id FROM repos WHERE repo = ?", repo).Scan(&repoID); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("repository %q not indexed", repo)
		}
		return err
	}
	if len(sourcePaths) > 0 {
		_, err = tx.Exec("UPDATE repos SET indexed_at = ?, git_head = ?, source_type = ?, source_path = ? WHERE id = ?", now, gitHead, sourceType, sourcePaths[0], repoID)
	} else {
		_, err = tx.Exec("UPDATE repos SET indexed_at = ?, git_head = ?, source_type = ? WHERE id = ?", now, gitHead, sourceType, repoID)
	}
	if err != nil {
		return err
	}

	var fileID int64
	err = tx.QueryRow("SELECT id FROM files WHERE repo_id = ? AND path = ?", repoID, filePath).Scan(&fileID)
	if err == sql.ErrNoRows {
		res, insertErr := tx.Exec("INSERT INTO files (repo_id, path, language, content_hash) VALUES (?, ?, ?, ?)", repoID, filePath, language, contentHash)
		if insertErr != nil {
			return fmt.Errorf("insert file %s: %w", filePath, insertErr)
		}
		fileID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("get file ID for %s: %w", filePath, err)
		}
	} else if err != nil {
		return err
	} else {
		if _, err := tx.Exec("UPDATE files SET language = ?, content_hash = ? WHERE id = ?", language, contentHash, fileID); err != nil {
			return fmt.Errorf("update file %s: %w", filePath, err)
		}
	}

	if _, err := tx.Exec("DELETE FROM symbols WHERE repo_id = ? AND file_id = ?", repoID, fileID); err != nil {
		return fmt.Errorf("delete symbols for %s: %w", filePath, err)
	}
	for _, sym := range symbols {
		if sym.File != filePath {
			return fmt.Errorf("symbol %q belongs to %q, expected %q", sym.ID, sym.File, filePath)
		}
	}
	if err := insertSymbolsTx(tx, repoID, map[string]int64{filePath: fileID}, symbols); err != nil {
		return err
	}
	if err := s.syncFileFTSTx(tx, repoID, owner, name, map[string]int64{filePath: fileID}); err != nil {
		return err
	}

	return tx.Commit()
}

// UpsertFileIndexWithContent updates the cache and index as one recoverable
// operation. The new bytes are staged first; if the database update fails, the
// previous cache file is restored so SQLite offsets continue to describe the
// bytes returned by GetSymbolContent.
func (s *IndexStore) UpsertFileIndexWithContent(
	owner, name, sourceType, gitHead, filePath, contentHash, language string,
	symbols []parser.Symbol, content []byte, sourcePaths ...string,
) error {
	if err := s.SaveContentFile(owner, name, filePath+".codescale-tmp", content); err != nil {
		return err
	}
	contentDir, err := s.contentDir(owner, name)
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(contentDir, filepath.FromSlash(filePath+".codescale-tmp"))
	contentPath := filepath.Join(contentDir, filepath.FromSlash(filePath))
	backupPath := filepath.Join(contentDir, filepath.FromSlash(filePath+".codescale-backup"))
	oldContent, oldErr := os.ReadFile(contentPath)
	oldExists := oldErr == nil
	if oldExists {
		_ = os.Remove(backupPath)
		if err := os.Rename(contentPath, backupPath); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
	}
	if err := os.Rename(tmpPath, contentPath); err != nil {
		if oldExists {
			_ = os.Rename(backupPath, contentPath)
		}
		return err
	}

	err = s.UpsertFileIndex(owner, name, sourceType, gitHead, filePath, contentHash, language, symbols, sourcePaths...)
	if err != nil {
		_ = os.Remove(contentPath)
		if oldExists {
			_ = os.WriteFile(contentPath, oldContent, 0644)
		}
		_ = os.Remove(backupPath)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func (s *IndexStore) syncFileFTSTx(tx *sql.Tx, repoID int64, owner, name string, fileIDs map[string]int64) error {
	contentDir, err := s.contentDir(owner, name)
	if err != nil {
		return err
	}
	for filePath, fileID := range fileIDs {
		if _, err := tx.Exec("DELETE FROM files_fts WHERE rowid = ?", fileID); err != nil {
			return err
		}
		contentPath := filepath.Join(contentDir, filepath.FromSlash(filePath))
		data, readErr := os.ReadFile(contentPath)
		if readErr != nil {
			continue
		}
		if _, err := tx.Exec("INSERT INTO files_fts(rowid, repo_id, path, content) VALUES (?, ?, ?, ?)", fileID, repoID, filePath, string(data)); err != nil {
			return fmt.Errorf("update file text index for %s: %w", filePath, err)
		}
	}
	return nil
}

func insertSymbolsTx(tx *sql.Tx, repoID int64, fileIDMap map[string]int64, symbols []parser.Symbol) error {
	symStmt, err := tx.Prepare(`INSERT INTO symbols
		(repo_id, file_id, symbol_id, file_path, name, qualified_name, kind, language,
		 signature, content_hash, docstring, summary, decorators, keywords, parent_id,
		 line, end_line, byte_offset, byte_length)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = symStmt.Close() }()

	for _, sym := range symbols {
		fileID, ok := fileIDMap[sym.File]
		if !ok {
			return fmt.Errorf("no file row for symbol %s", sym.ID)
		}
		decorJSON, err := json.Marshal(sym.Decorators)
		if err != nil {
			return fmt.Errorf("marshal decorators for %s: %w", sym.ID, err)
		}
		kwJSON, err := json.Marshal(sym.Keywords)
		if err != nil {
			return fmt.Errorf("marshal keywords for %s: %w", sym.ID, err)
		}
		if _, err := symStmt.Exec(
			repoID, fileID, sym.ID, sym.File, sym.Name, sym.QualifiedName,
			sym.Kind, sym.Language, sym.Signature, sym.ContentHash,
			sym.Docstring, sym.Summary, string(decorJSON), string(kwJSON),
			sym.Parent, sym.Line, sym.EndLine, sym.ByteOffset, sym.ByteLength,
		); err != nil {
			return fmt.Errorf("insert symbol %s: %w", sym.ID, err)
		}
	}
	return nil
}

// ListRepos returns all indexed repositories.
func (s *IndexStore) ListRepos() ([]RepoInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT r.id, r.owner, r.name, r.repo, r.indexed_at, r.git_head, r.source_type,
		       (SELECT COUNT(*) FROM files WHERE repo_id = r.id) as file_count,
		       (SELECT COUNT(*) FROM symbols WHERE repo_id = r.id) as symbol_count
		FROM repos r ORDER BY r.indexed_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var repos []RepoInfo
	for rows.Next() {
		var r RepoInfo
		if err := rows.Scan(&r.ID, &r.Owner, &r.Name, &r.Repo, &r.IndexedAt,
			&r.GitHead, &r.SourceType, &r.FileCount, &r.SymbolCount); err != nil {
			return nil, err
		}
		r.Languages = make(map[string]int)
		repos = append(repos, r)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch languages per repo
	for i := range repos {
		langRows, err := s.db.Query(
			"SELECT language, COUNT(*) FROM files WHERE repo_id = ? AND language != '' GROUP BY language",
			repos[i].ID,
		)
		if err != nil {
			continue
		}
		for langRows.Next() {
			var lang string
			var count int
			if err := langRows.Scan(&lang, &count); err != nil {
				continue
			}
			repos[i].Languages[lang] = count
		}
		_ = langRows.Close()
	}

	return repos, nil
}

// GetRepoID returns the repo ID for a repo string, or 0 if not found.
func (s *IndexStore) GetRepoID(repo string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var id int64
	err := s.db.QueryRow("SELECT id FROM repos WHERE repo = ?", repo).Scan(&id)
	if err == sql.ErrNoRows {
		// Try suffix match
		rows, err := s.db.Query("SELECT id, repo FROM repos")
		if err != nil {
			return 0, err
		}
		defer func() { _ = rows.Close() }()
		type repoMatch struct {
			id   int64
			repo string
		}
		var matches []repoMatch
		for rows.Next() {
			var rid int64
			var r string
			if err := rows.Scan(&rid, &r); err != nil {
				continue
			}
			if strings.HasSuffix(r, "/"+repo) || strings.HasSuffix(r, "-"+repo) {
				matches = append(matches, repoMatch{rid, r})
			}
		}
		if len(matches) == 1 {
			return matches[0].id, nil
		}
		if len(matches) > 1 {
			names := make([]string, len(matches))
			for i, m := range matches {
				names[i] = m.repo
			}
			return 0, fmt.Errorf("ambiguous repository %q, matches: %s", repo, strings.Join(names, ", "))
		}
		return 0, fmt.Errorf("repository %q not indexed", repo)
	}
	return id, err
}

// FileInfo contains file path and language.
type FileInfo struct {
	Path     string
	Language string
}

// GetFiles returns all files for a repo.
func (s *IndexStore) GetFiles(repoID int64) ([]FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getFilesLocked(repoID)
}

// FileExists checks one indexed repository-relative path without loading the
// repository's complete file list. Query-time consumers use this for bounded
// scope validation.
func (s *IndexStore) FileExists(repoID int64, filePath string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var exists int
	err := s.db.QueryRow("SELECT 1 FROM files WHERE repo_id = ? AND path = ? LIMIT 1", repoID, filePath).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil && exists == 1, err
}

// FilesExist checks a bounded set of indexed paths in one query. Missing
// paths are omitted, allowing callers to enforce source-backed invariants
// without materializing every file in a repository.
func (s *IndexStore) FilesExist(repoID int64, filePaths []string) (map[string]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	unique := make(map[string]struct{}, len(filePaths))
	for _, filePath := range filePaths {
		if filePath != "" {
			unique[filePath] = struct{}{}
		}
	}
	result := make(map[string]bool, len(unique))
	if len(unique) == 0 {
		return result, nil
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, 0, len(paths)+1)
	args = append(args, repoID)
	for _, path := range paths {
		args = append(args, path)
	}
	rows, err := s.db.Query("SELECT path FROM files WHERE repo_id = ? AND path IN ("+placeholders+")", args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		result[path] = true
	}
	return result, rows.Err()
}

// CountFiles returns the current indexed file count without loading file
// metadata. It is used for truthful empty-index health reporting.
func (s *IndexStore) CountFiles(repoID int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM files WHERE repo_id = ?", repoID).Scan(&count)
	return count, err
}

// ReplaceSemanticIndex is the compatibility wrapper for the original FiveM
// semantic API. New analyzers must use the analyzer-scoped operation.
func (s *IndexStore) ReplaceSemanticIndex(repoID int64, result semantic.Result) error {
	return s.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, result)
}

// ReplaceSemanticIndexForAnalyzer replaces only one analyzer's records.
func (s *IndexStore) ReplaceSemanticIndexForAnalyzer(repoID int64, analyzer string, result semantic.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := deleteSemanticIndexTx(tx, repoID, analyzer); err != nil {
		return err
	}
	if err := insertSemanticEntitiesTx(tx, repoID, analyzer, result.Entities); err != nil {
		return err
	}
	if err := insertSemanticRelationshipsTx(tx, repoID, analyzer, result.Relationships); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceSemanticFile removes semantic entities belonging to one file and
// inserts its replacement. Relationships are rebuilt separately from the
// complete entity set so cross-file links remain correct.
func (s *IndexStore) ReplaceSemanticFile(repoID int64, filePath string, entities []semantic.Entity) error {
	return s.ReplaceSemanticFileForAnalyzer(repoID, semantic.AnalyzerFiveM, filePath, entities)
}

// ReplaceSemanticFileForAnalyzer replaces one analyzer's facts for one file.
func (s *IndexStore) ReplaceSemanticFileForAnalyzer(repoID int64, analyzer, filePath string, entities []semantic.Entity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM semantic_relationships
		WHERE repo_id = ? AND analyzer = ? AND (file_path = ? OR from_entity_id IN
		(SELECT id FROM semantic_entities WHERE repo_id = ? AND analyzer = ? AND file_path = ?)
		OR to_entity_id IN (SELECT id FROM semantic_entities WHERE repo_id = ? AND analyzer = ? AND file_path = ?))`, repoID, analyzer, filePath, repoID, analyzer, filePath, repoID, analyzer, filePath); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM semantic_entities WHERE repo_id = ? AND analyzer = ? AND file_path = ?", repoID, analyzer, filePath); err != nil {
		return err
	}
	if err := insertSemanticEntitiesTx(tx, repoID, analyzer, entities); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceSemanticResourceForAnalyzer replaces facts owned by one resource
// path. It is intentionally narrower than repository replacement so a normal
// workspace source edit cannot erase unrelated resources or analyzers.
func (s *IndexStore) ReplaceSemanticResourceForAnalyzer(repoID int64, analyzer, resourcePath string, result semantic.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	selector := `repo_id = ? AND analyzer = ? AND json_extract(metadata, '$.source_resource_path') = ?`
	if _, err := tx.Exec(`DELETE FROM semantic_relationships WHERE repo_id=? AND analyzer=? AND (from_entity_id IN (SELECT id FROM semantic_entities WHERE `+selector+`) OR to_entity_id IN (SELECT id FROM semantic_entities WHERE `+selector+`))`, repoID, analyzer, repoID, analyzer, resourcePath, repoID, analyzer, resourcePath); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM semantic_entities WHERE `+selector, repoID, analyzer, resourcePath); err != nil {
		return err
	}
	if err := insertSemanticEntitiesTx(tx, repoID, analyzer, result.Entities); err != nil {
		return err
	}
	if err := insertSemanticRelationshipsTx(tx, repoID, analyzer, result.Relationships); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceSemanticRelationships replaces only relationship edges, preserving
// all entity records. It is used after an incremental file semantic update.
func (s *IndexStore) ReplaceSemanticRelationships(repoID int64, relationships []semantic.Relationship) error {
	return s.ReplaceSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveM, relationships)
}

// ReplaceSemanticRelationshipsForAnalyzer replaces one analyzer's edges.
func (s *IndexStore) ReplaceSemanticRelationshipsForAnalyzer(repoID int64, analyzer string, relationships []semantic.Relationship) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM semantic_relationships WHERE repo_id = ? AND analyzer = ?", repoID, analyzer); err != nil {
		return err
	}
	if err := insertSemanticRelationshipsTx(tx, repoID, analyzer, relationships); err != nil {
		return err
	}
	return tx.Commit()
}

// GetSemanticEntities returns all semantic entities for a repository in a
// stable order suitable for deterministic relationship resolution.
func (s *IndexStore) GetSemanticEntities(repoID int64) ([]semantic.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getSemanticEntitiesLocked(repoID, "")
}

func (s *IndexStore) GetSemanticEntitiesForAnalyzer(repoID int64, analyzer string) ([]semantic.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getSemanticEntitiesLocked(repoID, analyzer)
}

// GetSemanticEntityByID returns one semantic endpoint and its owning analyzer.
// It is used by graph tools to avoid guessing an analyzer for an explicit ID.
func (s *IndexStore) GetSemanticEntityByID(repoID int64, entityID string) (semantic.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repoName := repoNameLocked(s.db, repoID)
	row := s.db.QueryRow(`SELECT id, analyzer, file_path, symbol_id, kind, name, framework, side, line, end_line, dynamic, metadata
		FROM semantic_entities WHERE repo_id = ? AND id = ?`, repoID, entityID)
	return scanSemanticEntity(row, repoName)
}

func (s *IndexStore) GetSemanticEntityBySymbolID(repoID int64, analyzer, symbolID string) (semantic.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repoName := repoNameLocked(s.db, repoID)
	rows, err := s.db.Query(`SELECT id, analyzer, file_path, symbol_id, kind, name, framework, side, line, end_line, dynamic, metadata
		FROM semantic_entities WHERE repo_id = ? AND analyzer = ? AND symbol_id = ? ORDER BY id`, repoID, analyzer, symbolID)
	if err != nil {
		return semantic.Entity{}, err
	}
	defer func() { _ = rows.Close() }()
	var matches []semantic.Entity
	for rows.Next() {
		entity, err := scanSemanticEntity(rows, repoName)
		if err != nil {
			return semantic.Entity{}, err
		}
		matches = append(matches, entity)
	}
	if err := rows.Err(); err != nil {
		return semantic.Entity{}, err
	}
	if len(matches) == 0 {
		return semantic.Entity{}, fmt.Errorf("symbol %q has no %s graph entity", symbolID, analyzer)
	}
	if len(matches) > 1 {
		return semantic.Entity{}, fmt.Errorf("symbol %q maps to multiple %s graph entities", symbolID, analyzer)
	}
	return matches[0], nil
}

// GetSemanticRelationships returns all stored semantic edges for verification
// and maintenance operations. Query tools should prefer TraceSemantic.
func (s *IndexStore) GetSemanticRelationships(repoID int64) ([]semantic.Relationship, error) {
	return s.GetSemanticRelationshipsForAnalyzer(repoID, "")
}

func (s *IndexStore) GetSemanticRelationshipsForAnalyzer(repoID int64, analyzer string) ([]semantic.Relationship, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repoName := repoNameLocked(s.db, repoID)
	rows, err := s.db.Query(`SELECT id, analyzer, from_entity_id, to_entity_id, kind, name, dynamic, confidence, file_path, line
		FROM semantic_relationships WHERE repo_id = ? AND (? = '' OR analyzer = ?) ORDER BY id`, repoID, analyzer, analyzer)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []semantic.Relationship
	for rows.Next() {
		var relationship semantic.Relationship
		var dynamic int
		if err := rows.Scan(&relationship.ID, &relationship.Analyzer, &relationship.FromEntityID, &relationship.ToEntityID, &relationship.Kind, &relationship.Name, &dynamic, &relationship.Confidence, &relationship.File, &relationship.Line); err != nil {
			return nil, err
		}
		relationship.Repo = repoName
		relationship.Dynamic = dynamic != 0
		result = append(result, relationship)
	}
	return result, rows.Err()
}

func (s *IndexStore) getSemanticEntitiesLocked(repoID int64, analyzer string) ([]semantic.Entity, error) {
	repoName := repoNameLocked(s.db, repoID)
	rows, err := s.db.Query(`SELECT id, analyzer, file_path, symbol_id, kind, name, framework, side, line, end_line, dynamic, metadata
		FROM semantic_entities WHERE repo_id = ? AND (? = '' OR analyzer = ?) ORDER BY file_path, line, id`, repoID, analyzer, analyzer)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []semantic.Entity
	for rows.Next() {
		entity := semantic.Entity{Repo: ""}
		var dynamic int
		var metadata string
		if err := rows.Scan(&entity.ID, &entity.Analyzer, &entity.File, &entity.SymbolID, &entity.Kind, &entity.Name, &entity.Framework, &entity.Side, &entity.Line, &entity.EndLine, &dynamic, &metadata); err != nil {
			return nil, err
		}
		entity.Repo = repoName
		entity.Dynamic = dynamic != 0
		if metadata != "" {
			if err := json.Unmarshal([]byte(metadata), &entity.Metadata); err != nil {
				return nil, fmt.Errorf("decode semantic metadata: %w", err)
			}
		}
		result = append(result, entity)
	}
	return result, rows.Err()
}

// SearchSemantic performs a compact indexed SQL search without reading source
// files. Query, kind, and side are optional filters.
func (s *IndexStore) SearchSemantic(repoID int64, query, kind, side string, maxResults int) ([]semantic.Entity, bool, error) {
	return s.SearchSemanticWithOptions(repoID, query, kind, side, "", false, maxResults)
}

func (s *IndexStore) SearchSemanticWithOptions(repoID int64, query, kind, side, analyzer string, includeInternal bool, maxResults int) ([]semantic.Entity, bool, error) {
	return s.SearchSemanticWithResourceTargetOptions(repoID, query, kind, side, analyzer, "", "", includeInternal, maxResults)
}

func (s *IndexStore) SearchSemanticWithResourceOptions(repoID int64, query, kind, side, analyzer, resource string, includeInternal bool, maxResults int) ([]semantic.Entity, bool, error) {
	return s.SearchSemanticWithResourceTargetOptions(repoID, query, kind, side, analyzer, resource, "", includeInternal, maxResults)
}

func (s *IndexStore) SearchSemanticWithResourceTargetOptions(repoID int64, query, kind, side, analyzer, resource, targetResource string, includeInternal bool, maxResults int) ([]semantic.Entity, bool, error) {
	return s.SearchSemanticWithResourceTargetFrameworkOptions(repoID, query, kind, side, analyzer, resource, targetResource, "", includeInternal, maxResults)
}

// SearchSemanticWithResourceTargetFrameworkOptions adds a first-class
// framework filter while preserving the existing owner/target resource
// distinction in metadata filters.
func (s *IndexStore) SearchSemanticWithResourceTargetFrameworkOptions(repoID int64, query, kind, side, analyzer, resource, targetResource, framework string, includeInternal bool, maxResults int) ([]semantic.Entity, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if maxResults <= 0 {
		maxResults = 20
	}
	if maxResults > 200 {
		maxResults = 200
	}
	repoName := repoNameLocked(s.db, repoID)
	queryText := `SELECT id, analyzer, file_path, symbol_id, kind, name, framework, side, line, end_line, dynamic, metadata
		FROM semantic_entities WHERE repo_id = ?`
	args := []any{repoID}
	if analyzer != "" {
		queryText += " AND analyzer = ?"
		args = append(args, analyzer)
	} else if !includeInternal {
		queryText += " AND analyzer != ?"
		args = append(args, semantic.AnalyzerGenericGraph)
	}
	if query != "" {
		queryText += " AND lower(name) LIKE '%' || lower(?) || '%'"
		args = append(args, query)
	}
	if kind != "" {
		queryText += " AND kind = ?"
		args = append(args, kind)
	}
	if side != "" {
		queryText += " AND side = ?"
		args = append(args, side)
	}
	if framework != "" {
		queryText += " AND framework = ?"
		args = append(args, framework)
	}
	if resource != "" {
		queryText += " AND COALESCE(json_extract(metadata, '$.source_resource'), json_extract(metadata, '$.resource')) = ?"
		args = append(args, resource)
	}
	if targetResource != "" {
		queryText += " AND json_extract(metadata, '$.target_resource') = ?"
		args = append(args, targetResource)
	}
	queryText += " ORDER BY file_path, line, id LIMIT ?"
	args = append(args, maxResults+1)
	rows, err := s.db.Query(queryText, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	var result []semantic.Entity
	for rows.Next() {
		entity, err := scanSemanticEntity(rows, repoName)
		if err != nil {
			return nil, false, err
		}
		result = append(result, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(result) > maxResults
	if truncated {
		result = result[:maxResults]
	}
	return result, truncated, nil
}

// SearchSemanticExact returns semantic entities whose name is exactly query.
// Normalized operation metadata has a separate query path so ordinary exact
// name lookups can use the existing (repo_id, name) index without forcing a
// JSON expression into every query.
func (s *IndexStore) SearchSemanticExact(repoID int64, query string, maxResults int) ([]semantic.Entity, bool, error) {
	return s.searchSemanticExact(repoID, query, "name", nil, maxResults)
}

// SearchSemanticOperationExact returns entities whose normalized operation is
// exactly query. Callers should use this only for operation-shaped hints.
func (s *IndexStore) SearchSemanticOperationExact(repoID int64, query string, maxResults int) ([]semantic.Entity, bool, error) {
	return s.searchSemanticExact(repoID, query, "operation", nil, maxResults)
}

// SearchSemanticExactByKinds returns exact-name semantic entities restricted
// to the supplied kinds. It is used for declaration-scoped ambiguity checks so
// mixed usage/flow rows cannot make a declaration subset appear truncated.
func (s *IndexStore) SearchSemanticExactByKinds(repoID int64, query string, kinds []string, maxResults int) ([]semantic.Entity, bool, error) {
	return s.searchSemanticExact(repoID, query, "name", kinds, maxResults)
}

func (s *IndexStore) searchSemanticExact(repoID int64, query, field string, kinds []string, maxResults int) ([]semantic.Entity, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if maxResults <= 0 {
		maxResults = 20
	}
	if maxResults > 200 {
		maxResults = 200
	}
	repoName := repoNameLocked(s.db, repoID)
	where := "name = ?"
	if field == "operation" {
		where = "json_extract(metadata, '$.operation') = ?"
	}
	queryText := `SELECT id, analyzer, file_path, symbol_id, kind, name, framework, side, line, end_line, dynamic, metadata
		FROM semantic_entities WHERE repo_id = ? AND ` + where
	args := []any{repoID, query}
	if len(kinds) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
		queryText += " AND kind IN (" + placeholders + ")"
		for _, kind := range kinds {
			args = append(args, kind)
		}
	}
	queryText += " ORDER BY file_path, line, id LIMIT ?"
	args = append(args, maxResults+1)
	rows, err := s.db.Query(queryText, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]semantic.Entity, 0, maxResults+1)
	for rows.Next() {
		entity, scanErr := scanSemanticEntity(rows, repoName)
		if scanErr != nil {
			return nil, false, scanErr
		}
		result = append(result, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(result) > maxResults
	if truncated {
		result = result[:maxResults]
	}
	return result, truncated, nil
}

// TraceSemantic is the compatibility wrapper for FiveM relationship tracing.
func (s *IndexStore) TraceSemantic(repoID int64, entityID, direction string, depth, maxResults int) ([]semantic.TraceEdge, bool, error) {
	return s.TraceSemanticWithOptions(repoID, entityID, semantic.AnalyzerFiveM, direction, nil, depth, maxResults)
}

// TraceSemanticWithOptions traverses only indexed adjacency rows for the
// current frontier. It never materializes the complete repository graph.
func (s *IndexStore) TraceSemanticWithOptions(repoID int64, entityID, analyzer, direction string, relationshipKinds []string, depth, maxResults int) ([]semantic.TraceEdge, bool, error) {
	return s.traceSemanticWithOptions(repoID, entityID, analyzer, direction, relationshipKinds, depth, maxResults, false)
}

// TraceSemanticRankedWithOptions is the planner-facing traversal variant. It
// keeps the same bounded adjacency traversal as TraceSemanticWithOptions but
// asks SQLite to place high-value relationship kinds before low-value
// references/imports. This prevents a bounded result window from hiding a
// relevant call merely because its stable ID sorts later.
func (s *IndexStore) TraceSemanticRankedWithOptions(repoID int64, entityID, analyzer, direction string, relationshipKinds []string, depth, maxResults int) ([]semantic.TraceEdge, bool, error) {
	return s.traceSemanticWithOptions(repoID, entityID, analyzer, direction, relationshipKinds, depth, maxResults, true)
}

func (s *IndexStore) traceSemanticWithOptions(repoID int64, entityID, analyzer, direction string, relationshipKinds []string, depth, maxResults int, ranked bool) ([]semantic.TraceEdge, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if depth <= 0 {
		depth = 2
	}
	if depth > 3 {
		depth = 3
	}
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}
	var entityAnalyzer string
	if err := s.db.QueryRow("SELECT analyzer FROM semantic_entities WHERE repo_id = ? AND id = ?", repoID, entityID).Scan(&entityAnalyzer); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, fmt.Errorf("semantic entity %q not found in analyzer %q", entityID, analyzer)
		}
		return nil, false, err
	}
	if entityAnalyzer != analyzer && analyzer != semantic.AnalyzerFiveMWorkspace {
		return nil, false, fmt.Errorf("semantic entity %q belongs to analyzer %q, not %q", entityID, entityAnalyzer, analyzer)
	}
	if direction != "incoming" && direction != "outgoing" && direction != "both" {
		direction = "both"
	}
	type queueItem struct {
		id    string
		depth int
	}
	queue := []queueItem{{entityID, 0}}
	visited := map[string]bool{entityID: true}
	var result []semantic.TraceEdge
	for len(queue) > 0 && len(result) < maxResults+1 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= depth {
			continue
		}
		edges, err := s.querySemanticEdgesLocked(repoID, analyzer, direction, relationshipKinds, []string{current.id}, ranked)
		if err != nil {
			return nil, false, err
		}
		ids := make([]string, 0, len(edges)*2)
		for _, edge := range edges {
			ids = append(ids, edge.FromEntityID, edge.ToEntityID)
		}
		// Workspace edges may intentionally connect workspace-owned relationship
		// facts to per-resource FiveM entities. The edge itself remains scoped by
		// analyzer; endpoint lookup is scoped only by repository and stable ID.
		endpointMap, err := s.semanticEntitiesByIDsLocked(repoID, ids)
		if err != nil {
			return nil, false, err
		}
		for _, relationship := range edges {
			from, fromOK := endpointMap[relationship.FromEntityID]
			to, toOK := endpointMap[relationship.ToEntityID]
			if !fromOK || !toOK {
				continue
			}
			edge := semantic.TraceEdge{Relationship: relationship, From: from, To: &to, Depth: current.depth + 1}
			result = append(result, edge)
			if direction == "incoming" {
				if !visited[from.ID] {
					visited[from.ID] = true
					queue = append(queue, queueItem{from.ID, current.depth + 1})
				}
			} else if direction == "outgoing" {
				if !visited[to.ID] {
					visited[to.ID] = true
					queue = append(queue, queueItem{to.ID, current.depth + 1})
				}
			} else {
				nextID := to.ID
				if relationship.ToEntityID == current.id {
					nextID = from.ID
				}
				if !visited[nextID] {
					visited[nextID] = true
					queue = append(queue, queueItem{nextID, current.depth + 1})
				}
			}
			if len(result) >= maxResults+1 {
				break
			}
		}
	}
	truncated := len(result) > maxResults
	if truncated {
		result = result[:maxResults]
	}
	return result, truncated, nil
}

func (s *IndexStore) querySemanticEdgesLocked(repoID int64, analyzer, direction string, kinds, frontier []string, ranked bool) ([]semantic.Relationship, error) {
	if len(frontier) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(frontier)), ",")
	condition := "from_entity_id IN (" + placeholders + ")"
	if direction == "incoming" {
		condition = "to_entity_id IN (" + placeholders + ")"
	} else if direction == "both" {
		condition = "(from_entity_id IN (" + placeholders + ") OR to_entity_id IN (" + placeholders + "))"
	}
	args := []any{repoID, analyzer}
	for _, id := range frontier {
		args = append(args, id)
	}
	if direction == "both" {
		for _, id := range frontier {
			args = append(args, id)
		}
	}
	query := `SELECT id, analyzer, from_entity_id, to_entity_id, kind, name, dynamic, confidence, file_path, line
		FROM semantic_relationships WHERE repo_id = ? AND analyzer = ? AND ` + condition
	if len(kinds) > 0 {
		kindPlaceholders := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
		query += " AND kind IN (" + kindPlaceholders + ")"
		for _, kind := range kinds {
			args = append(args, kind)
		}
	}
	if ranked {
		query += ` ORDER BY CASE
			WHEN kind IN ('calls', 'framework_calls', 'framework_object_call', 'provided_by', 'cross_resource_event', 'cross_resource_callback', 'cross_resource_export') THEN 0
			WHEN kind IN ('triggers', 'handles', 'registers', 'uses_export', 'defines') THEN 1
			WHEN kind = 'references' THEN 2
			WHEN kind = 'imports' THEN 3
			ELSE 4 END, id`
	} else {
		query += " ORDER BY id"
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []semantic.Relationship
	for rows.Next() {
		var relationship semantic.Relationship
		var dynamic int
		if err := rows.Scan(&relationship.ID, &relationship.Analyzer, &relationship.FromEntityID, &relationship.ToEntityID, &relationship.Kind, &relationship.Name, &dynamic, &relationship.Confidence, &relationship.File, &relationship.Line); err != nil {
			return nil, err
		}
		relationship.Repo = repoNameLocked(s.db, repoID)
		relationship.Dynamic = dynamic != 0
		result = append(result, relationship)
	}
	return result, rows.Err()
}

func (s *IndexStore) semanticEntitiesByIDsLocked(repoID int64, ids []string) (map[string]semantic.Entity, error) {
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			unique[id] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return map[string]semantic.Entity{}, nil
	}
	values := make([]string, 0, len(unique))
	for id := range unique {
		values = append(values, id)
	}
	query := `SELECT id, analyzer, file_path, symbol_id, kind, name, framework, side, line, end_line, dynamic, metadata
		FROM semantic_entities WHERE repo_id = ? AND id IN (` + strings.TrimRight(strings.Repeat("?,", len(values)), ",") + ")"
	args := []any{repoID}
	for _, id := range values {
		args = append(args, id)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make(map[string]semantic.Entity, len(values))
	repoName := repoNameLocked(s.db, repoID)
	for rows.Next() {
		entity, err := scanSemanticEntity(rows, repoName)
		if err != nil {
			return nil, err
		}
		result[entity.ID] = entity
	}
	return result, rows.Err()
}

func deleteSemanticIndexTx(tx *sql.Tx, repoID int64, analyzer string) error {
	if _, err := tx.Exec("DELETE FROM semantic_relationships WHERE repo_id = ? AND analyzer = ?", repoID, analyzer); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM semantic_entities WHERE repo_id = ? AND analyzer = ?", repoID, analyzer)
	return err
}

func insertSemanticEntitiesTx(tx *sql.Tx, repoID int64, analyzer string, entities []semantic.Entity) error {
	stmt, err := tx.Prepare(`INSERT INTO semantic_entities
		(id, repo_id, analyzer, file_path, symbol_id, kind, name, framework, side, line, end_line, dynamic, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, entity := range entities {
		if entity.ID == "" || entity.Kind == "" {
			return fmt.Errorf("invalid semantic entity %q", entity.ID)
		}
		metadata, err := json.Marshal(entity.Metadata)
		if err != nil {
			return fmt.Errorf("marshal semantic metadata for %s: %w", entity.ID, err)
		}
		dynamic := 0
		if entity.Dynamic {
			dynamic = 1
		}
		rowAnalyzer := entity.Analyzer
		if rowAnalyzer == "" {
			rowAnalyzer = analyzer
		}
		if rowAnalyzer != analyzer {
			return fmt.Errorf("semantic entity %s belongs to analyzer %q, not %q", entity.ID, rowAnalyzer, analyzer)
		}
		if _, err := stmt.Exec(entity.ID, repoID, rowAnalyzer, entity.File, entity.SymbolID, entity.Kind, entity.Name, entity.Framework, entity.Side, entity.Line, entity.EndLine, dynamic, string(metadata)); err != nil {
			return fmt.Errorf("insert semantic entity %s: %w", entity.ID, err)
		}
	}
	return nil
}

func insertSemanticRelationshipsTx(tx *sql.Tx, repoID int64, analyzer string, relationships []semantic.Relationship) error {
	stmt, err := tx.Prepare(`INSERT INTO semantic_relationships
		(id, repo_id, analyzer, from_entity_id, to_entity_id, kind, name, dynamic, confidence, file_path, line)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, relationship := range relationships {
		dynamic := 0
		if relationship.Dynamic {
			dynamic = 1
		}
		rowAnalyzer := relationship.Analyzer
		if rowAnalyzer == "" {
			rowAnalyzer = analyzer
		}
		if rowAnalyzer != analyzer {
			return fmt.Errorf("semantic relationship %s belongs to analyzer %q, not %q", relationship.ID, rowAnalyzer, analyzer)
		}
		if _, err := stmt.Exec(relationship.ID, repoID, rowAnalyzer, relationship.FromEntityID, relationship.ToEntityID, relationship.Kind, relationship.Name, dynamic, relationship.Confidence, relationship.File, relationship.Line); err != nil {
			return fmt.Errorf("insert semantic relationship %s: %w", relationship.ID, err)
		}
	}
	return nil
}

func repoNameLocked(db *sql.DB, repoID int64) string {
	var repo string
	_ = db.QueryRow("SELECT repo FROM repos WHERE id = ?", repoID).Scan(&repo)
	return repo
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type semanticScanner interface {
	Scan(dest ...any) error
}

func scanSemanticEntity(scanner semanticScanner, repoName string) (semantic.Entity, error) {
	entity := semantic.Entity{}
	var dynamic int
	var metadata string
	if err := scanner.Scan(&entity.ID, &entity.Analyzer, &entity.File, &entity.SymbolID, &entity.Kind, &entity.Name, &entity.Framework, &entity.Side, &entity.Line, &entity.EndLine, &dynamic, &metadata); err != nil {
		return semantic.Entity{}, err
	}
	entity.Repo = repoName
	entity.Dynamic = dynamic != 0
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &entity.Metadata); err != nil {
			return semantic.Entity{}, err
		}
	}
	return entity, nil
}

// getFilesLocked returns all files for a repo. Caller must hold s.mu (read or write).
func (s *IndexStore) getFilesLocked(repoID int64) ([]FileInfo, error) {
	rows, err := s.db.Query("SELECT path, language FROM files WHERE repo_id = ? ORDER BY path", repoID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var files []FileInfo
	for rows.Next() {
		var f FileInfo
		if err := rows.Scan(&f.Path, &f.Language); err != nil {
			return nil, fmt.Errorf("scan file row: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

// GetSymbolsByFile returns symbols for a specific file.
func (s *IndexStore) GetSymbolsByFile(repoID int64, filePath string) ([]parser.Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.querySymbols("SELECT * FROM symbols WHERE repo_id = ? AND file_path = ? ORDER BY line", repoID, filePath)
}

// GetSymbolByID returns a single symbol by its symbol_id.
func (s *IndexStore) GetSymbolByID(repoID int64, symbolID string) (*parser.Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	symbols, err := s.querySymbols("SELECT * FROM symbols WHERE repo_id = ? AND symbol_id = ?", repoID, symbolID)
	if err != nil {
		return nil, err
	}
	if len(symbols) == 0 {
		return nil, fmt.Errorf("symbol %q not found", symbolID)
	}
	return &symbols[0], nil
}

// SearchSymbolsExact returns exact, case-sensitive symbol-name matches from
// the indexed symbol table. It intentionally does not fall back to fuzzy or
// full-text matching; callers such as the context planner use ambiguity as a
// signal rather than choosing a lexical near-match.
func (s *IndexStore) SearchSymbolsExact(repoID int64, query, filePath string, maxResults int) ([]parser.Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if maxResults <= 0 || maxResults > 200 {
		maxResults = 200
	}
	where := "repo_id = ? AND (name = ? OR qualified_name = ?)"
	args := []any{repoID, query, query}
	if filePath != "" {
		where += " AND file_path = ?"
		args = append(args, filePath)
	}
	args = append(args, maxResults)
	return s.querySymbols("SELECT * FROM symbols WHERE "+where+" ORDER BY file_path, line, symbol_id LIMIT ?", args...)
}

// GetSymbolsByIDs hydrates a bounded set of parser symbols in one indexed
// query. Missing IDs are omitted so callers can enforce current-index
// invariants without accepting stale symbol references.
func (s *IndexStore) GetSymbolsByIDs(repoID int64, symbolIDs []string) (map[string]parser.Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	unique := make(map[string]struct{}, len(symbolIDs))
	for _, id := range symbolIDs {
		if id != "" {
			unique[id] = struct{}{}
		}
	}
	result := make(map[string]parser.Symbol, len(unique))
	if len(unique) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, repoID)
	for _, id := range ids {
		args = append(args, id)
	}
	symbols, err := s.querySymbols("SELECT * FROM symbols WHERE repo_id = ? AND symbol_id IN ("+placeholders+") ORDER BY file_path, line, symbol_id", args...)
	if err != nil {
		return nil, err
	}
	for _, symbol := range symbols {
		result[symbol.ID] = symbol
	}
	return result, nil
}

// MatchTier indicates which search layer produced a result.
type MatchTier string

const (
	MatchTierFTS5      MatchTier = "fts5"
	MatchTierSubstring MatchTier = "substring"
	MatchTierFuzzy     MatchTier = "fuzzy"
)

// ScoredSymbol wraps a symbol with its score and match tier.
type ScoredSymbol struct {
	Symbol parser.Symbol
	Score  float64
	Tier   MatchTier
}

// SearchSymbols searches for symbols using a 3-layer fallback: FTS5 BM25, substring, fuzzy.
func (s *IndexStore) SearchSymbols(repoID int64, query string, kind string, language string, filePattern string, maxResults int) ([]parser.Symbol, []float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scored, err := s.searchSymbolsLayered(repoID, query, kind, language, filePattern, maxResults)
	if err != nil {
		return nil, nil, err
	}

	var syms []parser.Symbol
	var scores []float64
	for _, r := range scored {
		syms = append(syms, r.Symbol)
		scores = append(scores, r.Score)
	}
	return syms, scores, nil
}

// SearchSymbolsWithTier is like SearchSymbols but also returns the match tier per result.
func (s *IndexStore) SearchSymbolsWithTier(repoID int64, query string, kind string, language string, filePattern string, maxResults int) ([]ScoredSymbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchSymbolsLayered(repoID, query, kind, language, filePattern, maxResults)
}

func (s *IndexStore) searchSymbolsLayered(repoID int64, query string, kind string, language string, filePattern string, maxResults int) ([]ScoredSymbol, error) {
	seen := make(map[string]bool)
	var results []ScoredSymbol

	// Layer 1: FTS5 BM25 search
	ftsResults, err := s.searchFTS5(repoID, query, kind, language, filePattern, maxResults)
	if err == nil {
		for _, r := range ftsResults {
			if !seen[r.Symbol.ID] {
				seen[r.Symbol.ID] = true
				results = append(results, r)
			}
		}
	}

	// Layer 2: Substring scoring (fills remaining slots)
	if len(results) < maxResults {
		subResults, err := s.searchSubstring(repoID, query, kind, language, filePattern, maxResults-len(results))
		if err == nil {
			for _, r := range subResults {
				if !seen[r.Symbol.ID] {
					seen[r.Symbol.ID] = true
					results = append(results, r)
				}
			}
		}
	}

	// Layer 3: Fuzzy matching (fills remaining slots)
	if len(results) < maxResults {
		fuzzyResults, err := s.searchFuzzy(repoID, query, kind, language, filePattern, maxResults-len(results))
		if err == nil {
			for _, r := range fuzzyResults {
				if !seen[r.Symbol.ID] {
					seen[r.Symbol.ID] = true
					results = append(results, r)
				}
			}
		}
	}

	return results, nil
}

// searchFTS5 queries the FTS5 index with BM25 ranking.
func (s *IndexStore) searchFTS5(repoID int64, query string, kind string, language string, filePattern string, limit int) ([]ScoredSymbol, error) {
	// Sanitize query for FTS5 MATCH syntax: escape special chars, quote terms
	ftsQuery := sanitizeFTS5Query(query)
	if ftsQuery == "" {
		return nil, nil
	}

	// BM25 weights: name=10, qualified_name=5, signature=3, summary=2, docstring=1
	sqlQuery := `
		SELECT s.*, bm25(symbols_fts, 10, 5, 3, 2, 1) as rank
		FROM symbols_fts f
		JOIN symbols s ON f.rowid = s.id
		WHERE symbols_fts MATCH ? AND s.repo_id = ?`
	args := []any{ftsQuery, repoID}

	if kind != "" {
		sqlQuery += " AND s.kind = ?"
		args = append(args, strings.ToLower(kind))
	}
	if language != "" {
		sqlQuery += " AND s.language = ?"
		args = append(args, strings.ToLower(language))
	}

	sqlQuery += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []ScoredSymbol
	for rows.Next() {
		var sym parser.Symbol
		var id, rid, fileID int64
		var decorJSON, kwJSON string
		var rank float64

		err := rows.Scan(
			&id, &rid, &fileID,
			&sym.ID, &sym.File, &sym.Name, &sym.QualifiedName,
			&sym.Kind, &sym.Language, &sym.Signature, &sym.ContentHash,
			&sym.Docstring, &sym.Summary, &decorJSON, &kwJSON,
			&sym.Parent, &sym.Line, &sym.EndLine, &sym.ByteOffset, &sym.ByteLength,
			&rank,
		)
		if err != nil {
			return nil, err
		}

		_ = json.Unmarshal([]byte(decorJSON), &sym.Decorators)
		_ = json.Unmarshal([]byte(kwJSON), &sym.Keywords)
		if sym.Decorators == nil {
			sym.Decorators = []string{}
		}
		if sym.Keywords == nil {
			sym.Keywords = []string{}
		}

		// Apply file pattern filter
		if filePattern != "" && !matchFilePattern(sym.File, filePattern) {
			continue
		}

		// BM25 returns negative scores (lower = better), normalize to positive
		results = append(results, ScoredSymbol{Symbol: sym, Score: -rank, Tier: MatchTierFTS5})
	}

	return results, rows.Err()
}

// searchSubstring uses the existing in-memory substring scoring.
func (s *IndexStore) searchSubstring(repoID int64, query string, kind string, language string, filePattern string, limit int) ([]ScoredSymbol, error) {
	where := "repo_id = ?"
	args := []any{repoID}
	if kind != "" {
		where += " AND kind = ?"
		args = append(args, strings.ToLower(kind))
	}
	if language != "" {
		where += " AND language = ?"
		args = append(args, strings.ToLower(language))
	}

	symbols, err := s.querySymbols("SELECT * FROM symbols WHERE "+where, args...)
	if err != nil {
		return nil, err
	}

	if filePattern != "" {
		var filtered []parser.Symbol
		for _, sym := range symbols {
			if matchFilePattern(sym.File, filePattern) {
				filtered = append(filtered, sym)
			}
		}
		symbols = filtered
	}

	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	type scored struct {
		symbol parser.Symbol
		score  float64
	}
	var results []scored

	for _, sym := range symbols {
		score := scoreSymbol(sym, queryLower, queryWords)
		if score > 0 {
			results = append(results, scored{sym, score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	var out []ScoredSymbol
	for _, r := range results {
		out = append(out, ScoredSymbol{Symbol: r.symbol, Score: r.score, Tier: MatchTierSubstring})
	}
	return out, nil
}

// searchFuzzy uses Levenshtein distance on symbol names.
func (s *IndexStore) searchFuzzy(repoID int64, query string, kind string, language string, filePattern string, limit int) ([]ScoredSymbol, error) {
	where := "repo_id = ?"
	args := []any{repoID}
	if kind != "" {
		where += " AND kind = ?"
		args = append(args, strings.ToLower(kind))
	}
	if language != "" {
		where += " AND language = ?"
		args = append(args, strings.ToLower(language))
	}

	symbols, err := s.querySymbols("SELECT * FROM symbols WHERE "+where, args...)
	if err != nil {
		return nil, err
	}

	if filePattern != "" {
		var filtered []parser.Symbol
		for _, sym := range symbols {
			if matchFilePattern(sym.File, filePattern) {
				filtered = append(filtered, sym)
			}
		}
		symbols = filtered
	}

	queryLower := strings.ToLower(query)
	threshold := search.FuzzyThreshold(len(queryLower))

	type scored struct {
		symbol   parser.Symbol
		distance int
	}
	var results []scored

	for _, sym := range symbols {
		nameLower := strings.ToLower(sym.Name)
		dist := search.LevenshteinDistance(queryLower, nameLower)
		if dist <= threshold {
			results = append(results, scored{sym, dist})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].distance < results[j].distance
	})

	if len(results) > limit {
		results = results[:limit]
	}

	var out []ScoredSymbol
	for _, r := range results {
		// Convert distance to a score (lower distance = higher score)
		score := float64(threshold-r.distance+1) / float64(threshold+1) * 5
		out = append(out, ScoredSymbol{Symbol: r.symbol, Score: score, Tier: MatchTierFuzzy})
	}
	return out, nil
}

// sanitizeFTS5Query escapes special FTS5 characters and formats as a valid MATCH query.
func sanitizeFTS5Query(query string) string {
	// Split into words and quote each to avoid FTS5 syntax errors
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}
	var parts []string
	for _, w := range words {
		// Remove FTS5 special chars
		clean := strings.NewReplacer(
			"\"", "", "*", "", "(", "", ")", "",
			":", "", "^", "", "{", "", "}", "",
		).Replace(w)
		if clean != "" {
			parts = append(parts, "\""+clean+"\"")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
}

// matchFilePattern checks if a file path matches a glob pattern.
func matchFilePattern(filePath string, pattern string) bool {
	return pathmatch.Match(pattern, filePath)
}

// GetSymbolCountsByFile returns only the aggregate counts needed by file-tree
// views, avoiding materializing every symbol in Go.
func (s *IndexStore) GetSymbolCountsByFile(repoID int64) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT file_path, COUNT(*) FROM symbols WHERE repo_id = ? GROUP BY file_path", repoID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int)
	for rows.Next() {
		var path string
		var count int
		if err := rows.Scan(&path, &count); err != nil {
			return nil, err
		}
		counts[path] = count
	}
	return counts, rows.Err()
}

// scoreSymbol computes weighted relevance score for a symbol against a query.
func scoreSymbol(sym parser.Symbol, queryLower string, queryWords []string) float64 {
	var score float64
	nameLower := strings.ToLower(sym.Name)
	sigLower := strings.ToLower(sym.Signature)
	summaryLower := strings.ToLower(sym.Summary)
	docLower := strings.ToLower(sym.Docstring)

	// Name matching
	if nameLower == queryLower {
		score += 20
	} else if strings.Contains(nameLower, queryLower) {
		score += 10
	}
	for _, w := range queryWords {
		if strings.Contains(nameLower, w) {
			score += 5
		}
	}

	// Signature matching
	if strings.Contains(sigLower, queryLower) {
		score += 8
	}
	for _, w := range queryWords {
		if strings.Contains(sigLower, w) {
			score += 2
		}
	}

	// Summary matching
	if strings.Contains(summaryLower, queryLower) {
		score += 5
	}
	for _, w := range queryWords {
		if strings.Contains(summaryLower, w) {
			score += 1
		}
	}

	// Keywords matching
	for _, kw := range sym.Keywords {
		kwLower := strings.ToLower(kw)
		for _, w := range queryWords {
			if strings.Contains(kwLower, w) {
				score += 3
			}
		}
	}

	// Docstring matching
	for _, w := range queryWords {
		if strings.Contains(docLower, w) {
			score += 1
		}
	}

	return score
}

// SearchText performs full-text search across cached file contents.
// When contextLines > 0, returns consolidated snippet windows instead of individual lines.
func (s *IndexStore) SearchText(repoID int64, query string, filePattern string, maxResults int, contextLines int) ([]TextSearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get repo info
	var owner, name string
	err := s.db.QueryRow("SELECT owner, name FROM repos WHERE id = ?", repoID).Scan(&owner, &name)
	if err != nil {
		return nil, err
	}

	contentDir, err := s.contentDir(owner, name)
	if err != nil {
		return nil, err
	}

	// Get files for this repo (use locked version since we already hold RLock)
	files, err := s.textSearchFilesLocked(repoID, query, filePattern)
	if err != nil {
		return nil, err
	}

	var results []TextSearchResult

	if contextLines > 0 {
		// Snippet mode: return merged context windows
		for _, f := range files {
			if filePattern != "" && !matchFilePattern(f.Path, filePattern) {
				continue
			}

			contentPath := filepath.Join(contentDir, f.Path)
			data, err := os.ReadFile(contentPath)
			if err != nil {
				continue
			}

			snippets := snippet.ExtractSnippets(string(data), f.Path, query, contextLines, maxResults-len(results))
			for _, s := range snippets {
				results = append(results, TextSearchResult{
					File:      s.File,
					Line:      s.MatchLine,
					StartLine: s.StartLine,
					EndLine:   s.EndLine,
					Text:      s.Text,
				})
				if len(results) >= maxResults {
					return results, nil
				}
			}
		}
	} else {
		// Legacy mode: individual matching lines
		queryLower := strings.ToLower(query)
		for _, f := range files {
			if filePattern != "" && !matchFilePattern(f.Path, filePattern) {
				continue
			}

			contentPath := filepath.Join(contentDir, f.Path)
			data, err := os.ReadFile(contentPath)
			if err != nil {
				continue
			}

			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					text := strings.TrimSpace(line)
					if len(text) > 200 {
						text = text[:200]
					}
					results = append(results, TextSearchResult{
						File: f.Path,
						Line: i + 1,
						Text: text,
					})
					if len(results) >= maxResults {
						return results, nil
					}
				}
			}
		}
	}

	return results, nil
}

// textSearchFilesLocked uses FTS only as a cheap candidate selector. Queries
// containing punctuation are scanned exactly because FTS tokenization cannot
// safely preserve literals such as namespace:event:name.
func (s *IndexStore) textSearchFilesLocked(repoID int64, query, filePattern string) ([]FileInfo, error) {
	allFiles, err := s.getFilesLocked(repoID)
	if err != nil {
		return nil, err
	}
	filtered := make([]FileInfo, 0, len(allFiles))
	for _, file := range allFiles {
		if filePattern == "" || matchFilePattern(file.Path, filePattern) {
			filtered = append(filtered, file)
		}
	}
	for _, r := range query {
		if !(r == ' ' || r == '\t' || r == '\r' || r == '\n' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return filtered, nil
		}
	}
	ftsQuery := sanitizeFTS5Query(query)
	if ftsQuery == "" {
		return filtered, nil
	}
	rows, err := s.db.Query("SELECT path FROM files_fts WHERE files_fts MATCH ? AND repo_id = ?", ftsQuery, repoID)
	if err != nil {
		return filtered, nil
	}
	defer func() { _ = rows.Close() }()
	candidates := make(map[string]bool)
	for rows.Next() {
		var path string
		if scanErr := rows.Scan(&path); scanErr == nil {
			candidates[path] = true
		}
	}
	if len(candidates) == 0 {
		return filtered, nil
	}
	result := make([]FileInfo, 0, len(candidates))
	for _, file := range filtered {
		if candidates[file.Path] {
			result = append(result, file)
		}
	}
	return result, nil
}

// TextSearchResult represents a text search match.
type TextSearchResult struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Text      string `json:"text"`
}

// GetSymbolContent reads the raw source for a symbol using byte-offset seeking.
func (s *IndexStore) GetSymbolContent(repoID int64, symbolID string) (string, error) {
	sym, err := s.GetSymbolByID(repoID, symbolID)
	if err != nil {
		return "", err
	}

	var owner, name string
	err = s.db.QueryRow("SELECT owner, name FROM repos WHERE id = ?", repoID).Scan(&owner, &name)
	if err != nil {
		return "", err
	}

	repoDir, err := s.contentDir(owner, name)
	if err != nil {
		return "", err
	}
	contentPath := filepath.Join(repoDir, sym.File)

	// Prevent path traversal
	rel, err := filepath.Rel(filepath.Clean(repoDir), filepath.Clean(contentPath))
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path traversal detected: %s", sym.File)
	}

	f, err := os.Open(contentPath)
	if err != nil {
		return "", fmt.Errorf("cannot open content file: %w", err)
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, sym.ByteLength)
	_, err = f.ReadAt(buf, sym.ByteOffset)
	if err != nil {
		return "", fmt.Errorf("cannot read symbol content: %w", err)
	}

	return string(buf), nil
}

// GetFileContent reads the full file content from the content cache.
func (s *IndexStore) GetFileContent(repoID int64, filePath string) ([]byte, error) {
	var owner, name string
	err := s.db.QueryRow("SELECT owner, name FROM repos WHERE id = ?", repoID).Scan(&owner, &name)
	if err != nil {
		return nil, err
	}

	repoDir, err := s.contentDir(owner, name)
	if err != nil {
		return nil, err
	}
	contentPath := filepath.Join(repoDir, filePath)

	// Prevent path traversal
	cleanPath := filepath.Clean(contentPath)
	cleanRepo := filepath.Clean(repoDir)
	rel, err := filepath.Rel(cleanRepo, cleanPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("path traversal detected: %s", filePath)
	}

	return os.ReadFile(contentPath)
}

// SaveContentFile saves a raw source file to the content cache.
func (s *IndexStore) SaveContentFile(owner, name, filePath string, content []byte) error {
	if !security.SafeRepoComponent(owner) || !security.SafeRepoComponent(name) {
		return fmt.Errorf("invalid repository component: owner=%q name=%q", owner, name)
	}
	repoDir, err := s.contentDir(owner, name)
	if err != nil {
		return err
	}
	fullPath := filepath.Join(repoDir, filePath)

	// Prevent path traversal
	cleanPath := filepath.Clean(fullPath)
	cleanRepo := filepath.Clean(repoDir)
	rel, err := filepath.Rel(cleanRepo, cleanPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path traversal detected: %s", filePath)
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, content, 0644)
}

// DeleteFileFromIndex removes a single file and its symbols from a repository's index.
func (s *IndexStore) DeleteFileFromIndex(owner, name, filePath string) error {
	if !security.SafeRepoComponent(owner) || !security.SafeRepoComponent(name) {
		return fmt.Errorf("invalid repository component: owner=%q name=%q", owner, name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	repo := owner + "/" + name
	var repoID int64
	err := s.db.QueryRow("SELECT id FROM repos WHERE repo = ?", repo).Scan(&repoID)
	if err != nil {
		return fmt.Errorf("repository %q not indexed", repo)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Delete symbols for this file
	if _, err := tx.Exec("DELETE FROM symbols WHERE repo_id = ? AND file_path = ?", repoID, filePath); err != nil {
		return fmt.Errorf("delete symbols for %s: %w", filePath, err)
	}
	// Preserve the historical DeleteFileFromIndex contract for the FiveM
	// analyzer while keeping generic graph rows owned by its own lifecycle.
	if _, err := tx.Exec(`DELETE FROM semantic_relationships
		WHERE repo_id = ? AND analyzer = ? AND (file_path = ? OR from_entity_id IN
		(SELECT id FROM semantic_entities WHERE repo_id = ? AND analyzer = ? AND file_path = ?)
		OR to_entity_id IN (SELECT id FROM semantic_entities WHERE repo_id = ? AND analyzer = ? AND file_path = ?))`, repoID, semantic.AnalyzerFiveM, filePath, repoID, semantic.AnalyzerFiveM, filePath, repoID, semantic.AnalyzerFiveM, filePath); err != nil {
		return fmt.Errorf("delete FiveM semantic relationships for %s: %w", filePath, err)
	}
	if _, err := tx.Exec("DELETE FROM semantic_entities WHERE repo_id = ? AND analyzer = ? AND file_path = ?", repoID, semantic.AnalyzerFiveM, filePath); err != nil {
		return fmt.Errorf("delete FiveM semantic entities for %s: %w", filePath, err)
	}
	var fileID int64
	if err := tx.QueryRow("SELECT id FROM files WHERE repo_id = ? AND path = ?", repoID, filePath).Scan(&fileID); err == nil {
		if _, err := tx.Exec("DELETE FROM files_fts WHERE rowid = ?", fileID); err != nil {
			return fmt.Errorf("delete file text index for %s: %w", filePath, err)
		}
	}
	// Delete the file record
	if _, err := tx.Exec("DELETE FROM files WHERE repo_id = ? AND path = ?", repoID, filePath); err != nil {
		return fmt.Errorf("delete file %s: %w", filePath, err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Remove content file (with path traversal check)
	repoDir, err := s.contentDir(owner, name)
	if err != nil {
		return err
	}
	contentPath := filepath.Join(repoDir, filePath)
	rel, relErr := filepath.Rel(filepath.Clean(repoDir), filepath.Clean(contentPath))
	if relErr == nil && !strings.HasPrefix(rel, "..") {
		_ = os.Remove(contentPath)
	}

	return nil
}

// DeleteFilesUnderPrefix removes indexed files and all analyzer facts owned by
// a removed directory. It is used by workspace watchers for resource removal.
func (s *IndexStore) DeleteFilesUnderPrefix(owner, name, prefix string) error {
	if !security.SafeRepoComponent(owner) || !security.SafeRepoComponent(name) || filepath.IsAbs(prefix) || prefix == ".." || strings.HasPrefix(filepath.ToSlash(prefix), "../") {
		return fmt.Errorf("invalid file prefix")
	}
	prefix = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(prefix)), "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	var repoID int64
	var err error
	err = s.db.QueryRow("SELECT id FROM repos WHERE repo = ?", owner+"/"+name).Scan(&repoID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	pattern := prefix + "/%"
	if _, err = tx.Exec(`DELETE FROM semantic_relationships WHERE repo_id=? AND (file_path=? OR file_path LIKE ? OR from_entity_id IN (SELECT id FROM semantic_entities WHERE repo_id=? AND (file_path=? OR file_path LIKE ?)) OR to_entity_id IN (SELECT id FROM semantic_entities WHERE repo_id=? AND (file_path=? OR file_path LIKE ?)))`, repoID, prefix, pattern, repoID, prefix, pattern, repoID, prefix, pattern); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM semantic_entities WHERE repo_id=? AND (file_path=? OR file_path LIKE ?)", repoID, prefix, pattern); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM symbols WHERE repo_id=? AND (file_path=? OR file_path LIKE ?)", repoID, prefix, pattern); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT id FROM files WHERE repo_id=? AND (path=? OR path LIKE ?)", repoID, prefix, pattern)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.Exec("DELETE FROM files_fts WHERE rowid=?", id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM files WHERE repo_id=? AND (path=? OR path LIKE ?)", repoID, prefix, pattern); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	dir, err := s.contentDir(owner, name)
	if err == nil {
		target := filepath.Join(dir, filepath.FromSlash(prefix))
		rel, _ := filepath.Rel(filepath.Clean(dir), filepath.Clean(target))
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			_ = os.RemoveAll(target)
		}
	}
	return nil
}

// DeleteIndex removes all data for a repository.
func (s *IndexStore) DeleteIndex(repo string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var id int64
	var owner, name string
	err := s.db.QueryRow("SELECT id, owner, name FROM repos WHERE repo = ?", repo).Scan(&id, &owner, &name)
	if err != nil {
		return fmt.Errorf("repository %q not indexed", repo)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM semantic_relationships WHERE repo_id = ?", id); err != nil {
		return fmt.Errorf("delete semantic relationships: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM semantic_entities WHERE repo_id = ?", id); err != nil {
		return fmt.Errorf("delete semantic entities: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM symbols WHERE repo_id = ?", id); err != nil {
		return fmt.Errorf("delete symbols: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM files WHERE repo_id = ?", id); err != nil {
		return fmt.Errorf("delete files: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM files_fts WHERE repo_id = ?", id); err != nil {
		return fmt.Errorf("delete file text index: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM repos WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete repo: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Remove content directory
	contentDir, err := s.contentDir(owner, name)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(contentDir)

	return nil
}

// GetRepoOutline returns aggregate stats for a repository.
func (s *IndexStore) GetRepoOutline(repoID int64) (*RepoInfo, map[string]int, map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var info RepoInfo
	err := s.db.QueryRow(`
		SELECT r.id, r.owner, r.name, r.repo, r.indexed_at, r.git_head, r.source_type,
		       (SELECT COUNT(*) FROM files WHERE repo_id = r.id) as file_count,
		       (SELECT COUNT(*) FROM symbols WHERE repo_id = r.id) as symbol_count
		FROM repos r WHERE r.id = ?`, repoID).Scan(
		&info.ID, &info.Owner, &info.Name, &info.Repo, &info.IndexedAt,
		&info.GitHead, &info.SourceType, &info.FileCount, &info.SymbolCount)
	if err != nil {
		return nil, nil, nil, err
	}

	// Languages
	info.Languages = make(map[string]int)
	langRows, err := s.db.Query(
		"SELECT language, COUNT(*) FROM files WHERE repo_id = ? AND language != '' GROUP BY language", repoID)
	if err != nil {
		return nil, nil, nil, err
	}
	for langRows.Next() {
		var l string
		var c int
		if err := langRows.Scan(&l, &c); err != nil {
			continue
		}
		info.Languages[l] = c
	}
	_ = langRows.Close()

	// Directories
	dirs := make(map[string]int)
	dirRows, err := s.db.Query("SELECT path FROM files WHERE repo_id = ?", repoID)
	if err != nil {
		return nil, nil, nil, err
	}
	for dirRows.Next() {
		var p string
		if err := dirRows.Scan(&p); err != nil {
			continue
		}
		d := filepath.Dir(p)
		if d == "." {
			d = "/"
		}
		dirs[d]++
	}
	_ = dirRows.Close()

	// Symbol kinds
	kinds := make(map[string]int)
	kindRows, err := s.db.Query(
		"SELECT kind, COUNT(*) FROM symbols WHERE repo_id = ? GROUP BY kind", repoID)
	if err != nil {
		return nil, nil, nil, err
	}
	for kindRows.Next() {
		var k string
		var c int
		if err := kindRows.Scan(&k, &c); err != nil {
			continue
		}
		kinds[k] = c
	}
	_ = kindRows.Close()

	return &info, dirs, kinds, nil
}

// DetectChanges compares stored file hashes with current hashes.
func (s *IndexStore) DetectChanges(repoID int64, currentHashes map[string]string) (changed, added, deleted []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stored := make(map[string]string)
	rows, err := s.db.Query("SELECT path, content_hash FROM files WHERE repo_id = ?", repoID)
	if err != nil {
		return
	}
	for rows.Next() {
		var p, h string
		if err := rows.Scan(&p, &h); err != nil {
			continue
		}
		stored[p] = h
	}
	_ = rows.Close()

	for path, hash := range currentHashes {
		if oldHash, ok := stored[path]; ok {
			if oldHash != hash {
				changed = append(changed, path)
			}
		} else {
			added = append(added, path)
		}
	}

	for path := range stored {
		if _, ok := currentHashes[path]; !ok {
			deleted = append(deleted, path)
		}
	}

	return
}

// querySymbols is a helper to query and scan symbols.
func (s *IndexStore) querySymbols(query string, args ...any) ([]parser.Symbol, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var symbols []parser.Symbol
	for rows.Next() {
		var sym parser.Symbol
		var id, repoID, fileID int64
		var decorJSON, kwJSON string

		err := rows.Scan(
			&id, &repoID, &fileID,
			&sym.ID, &sym.File, &sym.Name, &sym.QualifiedName,
			&sym.Kind, &sym.Language, &sym.Signature, &sym.ContentHash,
			&sym.Docstring, &sym.Summary, &decorJSON, &kwJSON,
			&sym.Parent, &sym.Line, &sym.EndLine, &sym.ByteOffset, &sym.ByteLength,
		)
		if err != nil {
			return nil, err
		}

		_ = json.Unmarshal([]byte(decorJSON), &sym.Decorators)
		_ = json.Unmarshal([]byte(kwJSON), &sym.Keywords)
		if sym.Decorators == nil {
			sym.Decorators = []string{}
		}
		if sym.Keywords == nil {
			sym.Keywords = []string{}
		}

		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return symbols, nil
}

// GetAllSymbols returns all symbols for a repo.
func (s *IndexStore) GetAllSymbols(repoID int64) ([]parser.Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.querySymbols("SELECT * FROM symbols WHERE repo_id = ? ORDER BY file_path, line", repoID)
}

// CleanupStale removes indexed data for local repos whose source directories no longer exist,
// and orphaned content directories that don't correspond to any indexed repo.
func (s *IndexStore) CleanupStale() (removedRepos []string, removedDirs []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: Remove repos with stale source paths
	rows, err := s.db.Query("SELECT id, repo, owner, name, source_type, source_path FROM repos")
	if err != nil {
		return nil, nil, fmt.Errorf("query repos: %w", err)
	}

	type repoRecord struct {
		id         int64
		repo       string
		owner      string
		name       string
		sourceType string
		sourcePath string
	}
	var repos []repoRecord
	for rows.Next() {
		var r repoRecord
		if err := rows.Scan(&r.id, &r.repo, &r.owner, &r.name, &r.sourceType, &r.sourcePath); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		repos = append(repos, r)
	}
	_ = rows.Close()

	// Track valid content dirs
	validDirs := make(map[string]bool)

	for _, r := range repos {
		if dir, dirErr := s.contentDir(r.owner, r.name); dirErr == nil {
			validDirs[filepath.Base(dir)] = true
		}

		// Only check local repos with a recorded source path
		if r.sourceType != "local" || r.sourcePath == "" {
			continue
		}

		if _, statErr := os.Stat(r.sourcePath); os.IsNotExist(statErr) {
			// Source directory gone — clean up
			if delErr := s.deleteRepoLocked(r.id, r.owner, r.name); delErr == nil {
				removedRepos = append(removedRepos, r.repo)
				if dir, dirErr := s.contentDir(r.owner, r.name); dirErr == nil {
					delete(validDirs, filepath.Base(dir))
				}
			}
		}
	}

	// Phase 2: Remove orphaned content directories
	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return removedRepos, nil, nil // Non-fatal
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip known non-content dirs and the database file
		if name == "." || name == ".." || strings.HasSuffix(name, ".db") || strings.HasSuffix(name, "-journal") || strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
			continue
		}
		if !validDirs[name] {
			dirPath := filepath.Join(s.basePath, name)
			if rmErr := os.RemoveAll(dirPath); rmErr == nil {
				removedDirs = append(removedDirs, name)
			}
		}
	}

	return removedRepos, removedDirs, nil
}

// deleteRepoLocked removes a repo and its data. Caller must hold s.mu write lock.
func (s *IndexStore) deleteRepoLocked(repoID int64, owner, name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM symbols WHERE repo_id = ?", repoID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM files WHERE repo_id = ?", repoID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM repos WHERE id = ?", repoID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	contentDir, err := s.contentDir(owner, name)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(contentDir)
	return nil
}
