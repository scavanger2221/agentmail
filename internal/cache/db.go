package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// Store is the SQLite-backed email cache.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// DefaultDBPath returns the default cache database path.
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "agentmail", "cache.db")
}

// Open opens or creates the SQLite cache database and runs migrations.
func Open(path string) (*Store, error) {
	if path == "" {
		path = DefaultDBPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{db: db}

	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	schema := `
	CREATE TABLE IF NOT EXISTS accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		email TEXT NOT NULL,
		imap_host TEXT NOT NULL,
		imap_port INTEGER NOT NULL,
		smtp_host TEXT NOT NULL,
		smtp_port INTEGER NOT NULL,
		auth_method TEXT NOT NULL,
		password_file TEXT,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS folders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		uid_validity INTEGER NOT NULL DEFAULT 0,
		last_sync_uid INTEGER NOT NULL DEFAULT 0,
		UNIQUE(account_id, name),
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS emails (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id INTEGER NOT NULL,
		folder_id INTEGER NOT NULL,
		uid INTEGER NOT NULL,
		message_id TEXT,
		subject TEXT,
		from_addr TEXT,
		to_addrs TEXT,
		date TEXT,
		flags TEXT,
		body_snippet TEXT,
		body_path TEXT,
		size INTEGER NOT NULL DEFAULT 0,
		has_attachments INTEGER NOT NULL DEFAULT 0,
		internal_date TEXT,
		is_synced INTEGER NOT NULL DEFAULT 0,
		last_sync_at TEXT NOT NULL DEFAULT (datetime('now')),
		UNIQUE(account_id, folder_id, uid),
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
		FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_emails_account_folder ON emails(account_id, folder_id);
	CREATE INDEX IF NOT EXISTS idx_emails_uid ON emails(account_id, folder_id, uid);

	CREATE TABLE IF NOT EXISTS attachments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email_id INTEGER NOT NULL,
		filename TEXT NOT NULL,
		mime_type TEXT,
		size INTEGER NOT NULL DEFAULT 0,
		storage_path TEXT,
		FOREIGN KEY (email_id) REFERENCES emails(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_attachments_email ON attachments(email_id);

	-- FTS5 full-text search
	CREATE VIRTUAL TABLE IF NOT EXISTS emails_fts USING fts5(
		subject, from_addr, body_snippet,
		content='emails', content_rowid='id',
		tokenize='porter unicode61'
	);

	-- Triggers to keep FTS in sync
	CREATE TRIGGER IF NOT EXISTS emails_ai AFTER INSERT ON emails BEGIN
		INSERT INTO emails_fts(rowid, subject, from_addr, body_snippet)
		VALUES (new.id, new.subject, new.from_addr, new.body_snippet);
	END;

	CREATE TRIGGER IF NOT EXISTS emails_ad AFTER DELETE ON emails BEGIN
		INSERT INTO emails_fts(emails_fts, rowid, subject, from_addr, body_snippet)
		VALUES ('delete', old.id, old.subject, old.from_addr, old.body_snippet);
	END;

	CREATE TRIGGER IF NOT EXISTS emails_au AFTER UPDATE ON emails BEGIN
		INSERT INTO emails_fts(emails_fts, rowid, subject, from_addr, body_snippet)
		VALUES ('delete', old.id, old.subject, old.from_addr, old.body_snippet);
		INSERT INTO emails_fts(rowid, subject, from_addr, body_snippet)
		VALUES (new.id, new.subject, new.from_addr, new.body_snippet);
	END;
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Populate FTS from existing data (if FTS was just created and is empty)
	var ftsCount int
	s.db.QueryRow("SELECT COUNT(*) FROM emails_fts").Scan(&ftsCount)
	if ftsCount == 0 {
		s.db.Exec("INSERT INTO emails_fts(rowid, subject, from_addr, body_snippet) SELECT id, subject, from_addr, body_snippet FROM emails")
	}

	return nil
}
