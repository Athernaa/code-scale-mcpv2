package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/syphon1c/code-scale-mcp/internal/parser"
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
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
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

// migrate runs schema migrations.
func (s *IndexStore) migrate() error {
	_, err := s.db.Exec(SchemaSQL)
	if err != nil {
		return fmt.Errorf("schema exec: %w", err)
	}

	// Insert schema version if not exists
	_, err = s.db.Exec("INSERT OR IGNORE INTO schema_version (version) VALUES (?)", CurrentSchemaVersion)
	return err
}

// SaveIndex saves symbols for a repository. Replaces existing data for the same repo.
func (s *IndexStore) SaveIndex(
	owner, name string,
	sourceType string,
	gitHead string,
	files map[string]string, // path -> content_hash
	fileLanguages map[string]string, // path -> language
	symbols []parser.Symbol,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	repo := owner + "/" + name
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Upsert repo
	var repoID int64
	err = tx.QueryRow("SELECT id FROM repos WHERE repo = ?", repo).Scan(&repoID)
	if err == sql.ErrNoRows {
		res, err := tx.Exec(
			"INSERT INTO repos (owner, name, repo, indexed_at, git_head, source_type) VALUES (?, ?, ?, ?, ?, ?)",
			owner, name, repo, now, gitHead, sourceType,
		)
		if err != nil {
			return err
		}
		repoID, _ = res.LastInsertId()
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
		if _, err := tx.Exec("UPDATE repos SET indexed_at = ?, git_head = ? WHERE id = ?", now, gitHead, repoID); err != nil {
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
		fid, _ := res.LastInsertId()
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
		decorJSON, _ := json.Marshal(sym.Decorators)
		kwJSON, _ := json.Marshal(sym.Keywords)

		_, err := symStmt.Exec(
			repoID, fileID, sym.ID, sym.File, sym.Name, sym.QualifiedName,
			sym.Kind, sym.Language, sym.Signature, sym.ContentHash,
			sym.Docstring, sym.Summary, string(decorJSON), string(kwJSON),
			sym.Parent, sym.Line, sym.EndLine, sym.ByteOffset, sym.ByteLength,
		)
		if err != nil {
			return fmt.Errorf("insert symbol %s: %w", sym.ID, err)
		}
	}

	// Update FTS index
	_, _ = tx.Exec("DELETE FROM symbols_fts")
	_, _ = tx.Exec(`INSERT INTO symbols_fts(rowid, name, qualified_name, signature, summary, docstring)
		SELECT id, name, qualified_name, signature, summary, docstring FROM symbols WHERE repo_id = ?`, repoID)

	return tx.Commit()
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
			_ = langRows.Scan(&lang, &count)
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
		for rows.Next() {
			var rid int64
			var r string
			_ = rows.Scan(&rid, &r)
			if strings.HasSuffix(r, "/"+repo) || strings.HasSuffix(r, "-"+repo) {
				return rid, nil
			}
		}
		return 0, fmt.Errorf("repository %q not indexed", repo)
	}
	return id, err
}

// GetFiles returns all files for a repo.
func (s *IndexStore) GetFiles(repoID int64) ([]struct {
	Path     string
	Language string
}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT path, language FROM files WHERE repo_id = ? ORDER BY path", repoID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var files []struct {
		Path     string
		Language string
	}
	for rows.Next() {
		var f struct {
			Path     string
			Language string
		}
		_ = rows.Scan(&f.Path, &f.Language)
		files = append(files, f)
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

// SearchSymbols searches for symbols using weighted scoring.
func (s *IndexStore) SearchSymbols(repoID int64, query string, kind string, language string, filePattern string, maxResults int) ([]parser.Symbol, []float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxResults <= 0 {
		maxResults = 10
	}

	// Get all symbols for the repo (or filtered by kind/language)
	where := "repo_id = ?"
	args := []any{repoID}
	if kind != "" {
		where += " AND kind = ?"
		args = append(args, kind)
	}
	if language != "" {
		where += " AND language = ?"
		args = append(args, language)
	}

	symbols, err := s.querySymbols("SELECT * FROM symbols WHERE "+where, args...)
	if err != nil {
		return nil, nil, err
	}

	// Apply file pattern filter
	if filePattern != "" {
		var filtered []parser.Symbol
		for _, sym := range symbols {
			if matched, _ := filepath.Match(filePattern, sym.File); matched {
				filtered = append(filtered, sym)
			}
			if matched, _ := filepath.Match(filePattern, filepath.Base(sym.File)); matched {
				filtered = append(filtered, sym)
			}
		}
		symbols = filtered
	}

	// Score symbols
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

	// Sort by score descending
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Limit results
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	var syms []parser.Symbol
	var scores []float64
	for _, r := range results {
		syms = append(syms, r.symbol)
		scores = append(scores, r.score)
	}
	return syms, scores, nil
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
func (s *IndexStore) SearchText(repoID int64, query string, filePattern string, maxResults int) ([]TextSearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxResults <= 0 {
		maxResults = 20
	}

	// Get repo info
	var owner, name string
	err := s.db.QueryRow("SELECT owner, name FROM repos WHERE id = ?", repoID).Scan(&owner, &name)
	if err != nil {
		return nil, err
	}

	contentDir := filepath.Join(s.basePath, owner+"-"+name)

	// Get files for this repo
	files, err := s.GetFiles(repoID)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	var results []TextSearchResult

	for _, f := range files {
		if filePattern != "" {
			matched1, _ := filepath.Match(filePattern, f.Path)
			matched2, _ := filepath.Match(filePattern, filepath.Base(f.Path))
			if !matched1 && !matched2 {
				continue
			}
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

	return results, nil
}

// TextSearchResult represents a text search match.
type TextSearchResult struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
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

	contentPath := filepath.Join(s.basePath, owner+"-"+name, sym.File)
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

	contentPath := filepath.Join(s.basePath, owner+"-"+name, filePath)
	return os.ReadFile(contentPath)
}

// SaveContentFile saves a raw source file to the content cache.
func (s *IndexStore) SaveContentFile(owner, name, filePath string, content []byte) error {
	dir := filepath.Join(s.basePath, owner+"-"+name, filepath.Dir(filePath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.basePath, owner+"-"+name, filePath), content, 0644)
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

	_, _ = tx.Exec("DELETE FROM symbols WHERE repo_id = ?", id)
	_, _ = tx.Exec("DELETE FROM files WHERE repo_id = ?", id)
	_, _ = tx.Exec("DELETE FROM repos WHERE id = ?", id)
	_, _ = tx.Exec("DELETE FROM symbols_fts")

	if err := tx.Commit(); err != nil {
		return err
	}

	// Remove content directory
	contentDir := filepath.Join(s.basePath, owner+"-"+name)
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
	langRows, _ := s.db.Query(
		"SELECT language, COUNT(*) FROM files WHERE repo_id = ? AND language != '' GROUP BY language", repoID)
	if langRows != nil {
		for langRows.Next() {
			var l string
			var c int
			_ = langRows.Scan(&l, &c)
			info.Languages[l] = c
		}
		_ = langRows.Close()
	}

	// Directories
	dirs := make(map[string]int)
	dirRows, _ := s.db.Query("SELECT path FROM files WHERE repo_id = ?", repoID)
	if dirRows != nil {
		for dirRows.Next() {
			var p string
			_ = dirRows.Scan(&p)
			d := filepath.Dir(p)
			if d == "." {
				d = "/"
			}
			dirs[d]++
		}
		_ = dirRows.Close()
	}

	// Symbol kinds
	kinds := make(map[string]int)
	kindRows, _ := s.db.Query(
		"SELECT kind, COUNT(*) FROM symbols WHERE repo_id = ? GROUP BY kind", repoID)
	if kindRows != nil {
		for kindRows.Next() {
			var k string
			var c int
			_ = kindRows.Scan(&k, &c)
			kinds[k] = c
		}
		_ = kindRows.Close()
	}

	return &info, dirs, kinds, nil
}

// DetectChanges compares stored file hashes with current hashes.
func (s *IndexStore) DetectChanges(repoID int64, currentHashes map[string]string) (changed, added, deleted []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stored := make(map[string]string)
	rows, _ := s.db.Query("SELECT path, content_hash FROM files WHERE repo_id = ?", repoID)
	if rows != nil {
		for rows.Next() {
			var p, h string
			_ = rows.Scan(&p, &h)
			stored[p] = h
		}
		_ = rows.Close()
	}

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
	return symbols, nil
}

// GetAllSymbols returns all symbols for a repo.
func (s *IndexStore) GetAllSymbols(repoID int64) ([]parser.Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.querySymbols("SELECT * FROM symbols WHERE repo_id = ? ORDER BY file_path, line", repoID)
}
