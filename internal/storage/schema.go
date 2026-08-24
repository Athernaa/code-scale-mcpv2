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

// CurrentSchemaVersion is the current schema version.
const CurrentSchemaVersion = 4
