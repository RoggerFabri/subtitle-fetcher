package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS media (
    id           INTEGER PRIMARY KEY,
    path         TEXT    NOT NULL UNIQUE,
    name         TEXT    NOT NULL,
    type         TEXT    NOT NULL CHECK(type IN ('movie','series')),
    last_scanned TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    id           INTEGER PRIMARY KEY,
    media_id     INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    path         TEXT    NOT NULL UNIQUE,
    season       INTEGER,
    episode      INTEGER,
    has_subtitle INTEGER NOT NULL DEFAULT 0,
    last_seen    TEXT    NOT NULL
);
`

func openDB(_ string) (*sql.DB, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	// Resolve symlinks so go run temp paths are followed to the real binary dir.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks: %w", err)
	}
	dbPath := filepath.Join(filepath.Dir(exe), "subtitles.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_busy_timeout=10000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("db pragmas: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return db, nil
}

func getSetting(db *sql.DB, key string) string {
	var val string
	db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&val)
	return val
}

func setSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func upsertMedia(db *sql.DB, path, name, typ string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO media(path, name, type, last_scanned)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			name         = excluded.name,
			last_scanned = excluded.last_scanned`,
		path, name, typ, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM media WHERE path = ?`, path).Scan(&id)
	return id, err
}

func upsertFile(db *sql.DB, mediaID int64, path string, season, episode *int, hasSubtitle bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	sub := 0
	if hasSubtitle {
		sub = 1
	}
	_, err := db.Exec(`
		INSERT INTO files(media_id, path, season, episode, has_subtitle, last_seen)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			season       = excluded.season,
			episode      = excluded.episode,
			has_subtitle = excluded.has_subtitle,
			last_seen    = excluded.last_seen`,
		mediaID, path, season, episode, sub, now)
	return err
}
