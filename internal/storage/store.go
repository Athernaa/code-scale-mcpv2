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
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/search"
	"github.com/Athernaa/code-scale-mcpv2/internal/security"
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

	// Upsert schema version
	_, err = s.db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", CurrentSchemaVersion)
	return err
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
		// Delete existing data for re-index
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

	return tx.Commit()
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

	return tx.Commit()
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
	matched, err := filepath.Match(pattern, filePath)
	if err != nil {
		return false
	}
	if matched {
		return true
	}
	matched, _ = filepath.Match(pattern, filepath.Base(filePath))
	return matched
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
	files, err := s.getFilesLocked(repoID)
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

	if _, err := tx.Exec("DELETE FROM symbols WHERE repo_id = ?", id); err != nil {
		return fmt.Errorf("delete symbols: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM files WHERE repo_id = ?", id); err != nil {
		return fmt.Errorf("delete files: %w", err)
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
