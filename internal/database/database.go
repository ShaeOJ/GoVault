package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps a SQLite connection and provides all persistence operations.
type DB struct {
	conn *sql.DB
	path string
}

// Open creates or opens the SQLite database at the given path.
func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite is single-writer

	db := &DB{conn: conn, path: dbPath}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	return db, nil
}

// Size returns the total disk usage in bytes (main DB + WAL + SHM files).
func (db *DB) Size() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(db.path + suffix); err == nil {
			total += info.Size()
		}
	}
	return total
}

// Path returns the database file path.
func (db *DB) Path() string {
	return db.path
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS shares (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp  INTEGER NOT NULL,
			miner_id   TEXT    NOT NULL,
			worker     TEXT    NOT NULL DEFAULT '',
			difficulty REAL    NOT NULL,
			accepted   INTEGER NOT NULL DEFAULT 1,
			reject_reason TEXT NOT NULL DEFAULT ''
		);

		CREATE INDEX IF NOT EXISTS idx_shares_timestamp ON shares(timestamp);
		CREATE INDEX IF NOT EXISTS idx_shares_miner     ON shares(miner_id);

		CREATE TABLE IF NOT EXISTS blocks (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			height    INTEGER NOT NULL,
			hash      TEXT    NOT NULL,
			miner_id  TEXT    NOT NULL DEFAULT '',
			worker    TEXT    NOT NULL DEFAULT '',
			difficulty REAL   NOT NULL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_blocks_timestamp ON blocks(timestamp);

		CREATE TABLE IF NOT EXISTS hashrate_history (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			hashrate  REAL    NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_hashrate_timestamp ON hashrate_history(timestamp);

		CREATE TABLE IF NOT EXISTS miner_sessions (
			session_id      TEXT PRIMARY KEY,
			worker          TEXT    NOT NULL DEFAULT '',
			ip_address      TEXT    NOT NULL DEFAULT '',
			connected_at    INTEGER NOT NULL,
			disconnected_at INTEGER NOT NULL DEFAULT 0,
			shares_accepted INTEGER NOT NULL DEFAULT 0,
			shares_rejected INTEGER NOT NULL DEFAULT 0,
			best_difficulty REAL    NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS cumulative_stats (
			id              INTEGER PRIMARY KEY CHECK (id = 1),
			total_accepted  INTEGER NOT NULL DEFAULT 0,
			total_rejected  INTEGER NOT NULL DEFAULT 0,
			best_difficulty REAL    NOT NULL DEFAULT 0,
			blocks_found    INTEGER NOT NULL DEFAULT 0
		);

		INSERT OR IGNORE INTO cumulative_stats (id, total_accepted, total_rejected, best_difficulty, blocks_found)
		VALUES (1, 0, 0, 0, 0);

		CREATE TABLE IF NOT EXISTS worker_diffs (
			worker     TEXT PRIMARY KEY,
			difficulty REAL NOT NULL,
			updated_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return err
	}

	// Add session_diff column (safe for existing DBs — ignores "duplicate column" error)
	db.conn.Exec(`ALTER TABLE shares ADD COLUMN session_diff REAL NOT NULL DEFAULT 0`)

	// Composite index for per-miner hashrate history queries
	db.conn.Exec(`CREATE INDEX IF NOT EXISTS idx_shares_miner_ts ON shares(miner_id, timestamp)`)

	return nil
}
