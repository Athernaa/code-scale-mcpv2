package storage

// SchemaSQL contains the DDL for the code-scale-mcp index database.
const SchemaSQL = `
CREATE TABLE IF NOT EXISTS repos (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    owner       TEXT NOT NULL,
    name        TEXT NOT NULL,
    repo        TEXT NOT NULL UNIQUE,
    indexed_at  TEXT NOT NULL,
    git_head    TEXT DEFAULT '',
    source_type TEXT DEFAULT 'github'
);

CREATE TABLE IF NOT EXISTS files (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id      INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    path         TEXT NOT NULL,
    language     TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    UNIQUE(repo_id, path)
);

CREATE TABLE IF NOT EXISTS symbols (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id        INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    file_id        INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    symbol_id      TEXT NOT NULL,
    file_path      TEXT NOT NULL,
    name           TEXT NOT NULL,
    qualified_name TEXT NOT NULL,
    kind           TEXT NOT NULL,
    language       TEXT NOT NULL DEFAULT '',
    signature      TEXT NOT NULL DEFAULT '',
    content_hash   TEXT DEFAULT '',
    docstring      TEXT DEFAULT '',
    summary        TEXT DEFAULT '',
    decorators     TEXT DEFAULT '[]',
    keywords       TEXT DEFAULT '[]',
    parent_id      TEXT DEFAULT '',
    line           INTEGER DEFAULT 0,
    end_line       INTEGER DEFAULT 0,
    byte_offset    INTEGER DEFAULT 0,
    byte_length    INTEGER DEFAULT 0,
    UNIQUE(repo_id, symbol_id)
);

CREATE INDEX IF NOT EXISTS idx_symbols_name     ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_kind     ON symbols(kind);
CREATE INDEX IF NOT EXISTS idx_symbols_language ON symbols(language);
CREATE INDEX IF NOT EXISTS idx_symbols_file     ON symbols(file_path);
CREATE INDEX IF NOT EXISTS idx_symbols_repo     ON symbols(repo_id);
CREATE INDEX IF NOT EXISTS idx_files_repo       ON files(repo_id);

CREATE TABLE IF NOT EXISTS semantic_entities (
	 id          TEXT PRIMARY KEY,
	 repo_id     INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
	 analyzer    TEXT NOT NULL DEFAULT 'fivem',
    file_path   TEXT NOT NULL,
    symbol_id   TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    framework   TEXT NOT NULL DEFAULT '',
    side        TEXT NOT NULL DEFAULT 'unknown',
    line        INTEGER NOT NULL DEFAULT 0,
    end_line    INTEGER NOT NULL DEFAULT 0,
    dynamic     INTEGER NOT NULL DEFAULT 0,
    metadata    TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS semantic_relationships (
	 id             TEXT PRIMARY KEY,
	 repo_id        INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
	 analyzer       TEXT NOT NULL DEFAULT 'fivem',
    from_entity_id TEXT NOT NULL,
    to_entity_id   TEXT NOT NULL DEFAULT '',
    kind           TEXT NOT NULL,
    name           TEXT NOT NULL DEFAULT '',
    dynamic        INTEGER NOT NULL DEFAULT 0,
    confidence     REAL NOT NULL DEFAULT 0,
    file_path      TEXT NOT NULL DEFAULT '',
    line           INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_semantic_entities_repo ON semantic_entities(repo_id);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_kind ON semantic_entities(repo_id, kind);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_name ON semantic_entities(repo_id, name);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_file ON semantic_entities(repo_id, file_path);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_symbol ON semantic_entities(repo_id, symbol_id);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_framework ON semantic_entities(repo_id, framework);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_repo ON semantic_relationships(repo_id);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_from ON semantic_relationships(repo_id, from_entity_id);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_to ON semantic_relationships(repo_id, to_entity_id);

CREATE VIRTUAL TABLE IF NOT EXISTS symbols_fts USING fts5(
    name, qualified_name, signature, summary, docstring,
    content='symbols', content_rowid='id'
);

CREATE TABLE IF NOT EXISTS token_savings (
    id                 INTEGER PRIMARY KEY CHECK(id = 1),
    total_tokens_saved INTEGER DEFAULT 0,
    anon_id            TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY
);
`

// MigrateV2SQL adds the watches table for persisting folder watches across restarts.
const MigrateV2SQL = `
CREATE TABLE IF NOT EXISTS watches (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,
    repo       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
`

// MigrateV3SQL adds source_path to repos for stale cleanup tracking.
const MigrateV3SQL = `
ALTER TABLE repos ADD COLUMN source_path TEXT DEFAULT '';
`

// MigrateV4SQL keeps the external-content FTS5 table synchronized with
// symbols incrementally. The delete command must include the OLD indexed
// column values, as required by FTS5 external-content tables.
const MigrateV4SQL = `
CREATE TRIGGER IF NOT EXISTS symbols_ai AFTER INSERT ON symbols BEGIN
    INSERT INTO symbols_fts(rowid, name, qualified_name, signature, summary, docstring)
    VALUES (new.id, new.name, new.qualified_name, new.signature, new.summary, new.docstring);
END;

CREATE TRIGGER IF NOT EXISTS symbols_ad AFTER DELETE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, qualified_name, signature, summary, docstring)
    VALUES ('delete', old.id, old.name, old.qualified_name, old.signature, old.summary, old.docstring);
END;

CREATE TRIGGER IF NOT EXISTS symbols_au AFTER UPDATE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, qualified_name, signature, summary, docstring)
    VALUES ('delete', old.id, old.name, old.qualified_name, old.signature, old.summary, old.docstring);
    INSERT INTO symbols_fts(rowid, name, qualified_name, signature, summary, docstring)
    VALUES (new.id, new.name, new.qualified_name, new.signature, new.summary, new.docstring);
END;
`

// MigrateV5SQL adds a file-level content index used only to shortlist text
// search candidates. Exact substring matching remains the final authority.
const MigrateV5SQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS files_fts USING fts5(
    repo_id UNINDEXED, path UNINDEXED, content
);
`

// MigrateV6SQL adds generic semantic entities and relationships. FiveM is the
// first analyzer, but the schema deliberately stores framework and kind as
// strings for future analyzers.
const MigrateV6SQL = `
CREATE TABLE IF NOT EXISTS semantic_entities (
    id          TEXT PRIMARY KEY,
    repo_id     INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    file_path   TEXT NOT NULL,
    symbol_id   TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    framework   TEXT NOT NULL DEFAULT '',
    side        TEXT NOT NULL DEFAULT 'unknown',
    line        INTEGER NOT NULL DEFAULT 0,
    end_line    INTEGER NOT NULL DEFAULT 0,
    dynamic     INTEGER NOT NULL DEFAULT 0,
    metadata    TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS semantic_relationships (
    id             TEXT PRIMARY KEY,
    repo_id        INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    from_entity_id TEXT NOT NULL,
    to_entity_id   TEXT NOT NULL DEFAULT '',
    kind           TEXT NOT NULL,
    name           TEXT NOT NULL DEFAULT '',
    dynamic        INTEGER NOT NULL DEFAULT 0,
    confidence     REAL NOT NULL DEFAULT 0,
    file_path      TEXT NOT NULL DEFAULT '',
    line           INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_semantic_entities_repo ON semantic_entities(repo_id);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_kind ON semantic_entities(repo_id, kind);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_name ON semantic_entities(repo_id, name);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_file ON semantic_entities(repo_id, file_path);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_symbol ON semantic_entities(repo_id, symbol_id);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_repo ON semantic_relationships(repo_id);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_from ON semantic_relationships(repo_id, from_entity_id);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_to ON semantic_relationships(repo_id, to_entity_id);
`

// MigrateV7SQL makes semantic storage analyzer-aware. Existing Phase 3 rows
// are FiveM rows because FiveM was the only analyzer before this migration.
const MigrateV7SQL = `
CREATE INDEX IF NOT EXISTS idx_semantic_entities_analyzer ON semantic_entities(repo_id, analyzer);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_analyzer_symbol ON semantic_entities(repo_id, analyzer, symbol_id);
CREATE INDEX IF NOT EXISTS idx_semantic_entities_analyzer_kind ON semantic_entities(repo_id, analyzer, kind);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_analyzer ON semantic_relationships(repo_id, analyzer);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_analyzer_from ON semantic_relationships(repo_id, analyzer, from_entity_id);
CREATE INDEX IF NOT EXISTS idx_semantic_relationships_analyzer_to ON semantic_relationships(repo_id, analyzer, to_entity_id);
`

// MigrateV8SQL adds persistent workspace/resource metadata. Resource names are
// intentionally not unique: duplicate names are valid discovery results but
// must remain ambiguous during cross-resource resolution.
const MigrateV8SQL = `
CREATE TABLE IF NOT EXISTS workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id INTEGER NOT NULL UNIQUE REFERENCES repos(id) ON DELETE CASCADE,
    root_path TEXT NOT NULL,
    kind TEXT NOT NULL,
    indexed_at TEXT NOT NULL,
    files_discovered_total INTEGER NOT NULL DEFAULT 0,
    files_indexed INTEGER NOT NULL DEFAULT 0,
    index_truncated INTEGER NOT NULL DEFAULT 0,
    incomplete INTEGER NOT NULL DEFAULT 0,
    resources_with_semantics INTEGER NOT NULL DEFAULT 0,
    resources_without_semantics INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS workspace_resources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    manifest_path TEXT NOT NULL,
    manifest_type TEXT NOT NULL DEFAULT '',
    enabled_state TEXT NOT NULL DEFAULT 'unknown',
    start_order INTEGER NOT NULL DEFAULT 0,
    group_path TEXT NOT NULL DEFAULT '',
    UNIQUE(workspace_id, relative_path)
);
CREATE TABLE IF NOT EXISTS workspace_configs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    UNIQUE(workspace_id, path)
);
CREATE INDEX IF NOT EXISTS idx_workspaces_repo ON workspaces(repo_id);
CREATE INDEX IF NOT EXISTS idx_workspace_resources_workspace ON workspace_resources(workspace_id);
CREATE INDEX IF NOT EXISTS idx_workspace_resources_name ON workspace_resources(workspace_id, name);
CREATE INDEX IF NOT EXISTS idx_workspace_configs_workspace ON workspace_configs(workspace_id);
`

// MigrateV10SQL adds the framework query index. Framework facts remain in the
// analyzer-scoped semantic tables; this index only makes framework filtering
// cheap without scanning metadata JSON.
const MigrateV10SQL = `
CREATE INDEX IF NOT EXISTS idx_semantic_entities_framework ON semantic_entities(repo_id, framework);
`

// CurrentSchemaVersion is the current schema version.
const CurrentSchemaVersion = 10
