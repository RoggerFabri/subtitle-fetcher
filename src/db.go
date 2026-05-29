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
    imdb_id      TEXT    NOT NULL DEFAULT '',
    last_scanned TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    id            INTEGER PRIMARY KEY,
    media_id      INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    path          TEXT    NOT NULL UNIQUE,
    season        INTEGER,
    episode       INTEGER,
    has_subtitle  INTEGER NOT NULL DEFAULT 0,
    subtitle_name TEXT    NOT NULL DEFAULT '',
    last_seen     TEXT    NOT NULL
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

	dbDir := filepath.Join(filepath.Dir(exe), "data")
	if envDb := os.Getenv("DB_PATH"); envDb != "" {
		dbDir = envDb
	}
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	dbPath := filepath.Join(dbDir, "subtitles.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_busy_timeout=10000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	// synchronous=NORMAL is safe with WAL (no corruption risk, only the last
	// committed txn can be lost on power loss) and avoids an fsync per commit —
	// a major speedup for the thousands of small writes a scan produces.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("db pragmas: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	// Migrations — no-op if column already exists.
	db.Exec(`ALTER TABLE files ADD COLUMN subtitle_name TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE media ADD COLUMN imdb_id TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE media ADD COLUMN scan_sig TEXT NOT NULL DEFAULT ''`)
	return db, nil
}

// dbExecer is satisfied by both *sql.DB and *sql.Tx, letting the upsert
// helpers run either standalone or batched inside a scan transaction.
type dbExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
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

func upsertMedia(db dbExecer, path, name, typ string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO media(path, name, type, last_scanned)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			name         = excluded.name,
			last_scanned = excluded.last_scanned`,
		// imdb_id is intentionally excluded — preserve user-set or auto-discovered value.
		path, name, typ, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM media WHERE path = ?`, path).Scan(&id)
	return id, err
}

// pruneStale removes media rows (and their files via CASCADE) whose paths were
// not seen in the current scan, and removes individual file rows for files that
// no longer exist on disk within a still-present media folder.
func pruneStale(db *sql.DB, seenMedia, seenFiles map[string]bool) (int, int, error) {
	mediaRows, err := db.Query(`SELECT id, path FROM media`)
	if err != nil {
		return 0, 0, err
	}
	type idPath struct {
		id   int64
		path string
	}
	var allMedia []idPath
	for mediaRows.Next() {
		var r idPath
		if err := mediaRows.Scan(&r.id, &r.path); err != nil {
			mediaRows.Close()
			return 0, 0, err
		}
		allMedia = append(allMedia, r)
	}
	mediaRows.Close()

	removedMedia := 0
	for _, m := range allMedia {
		if !seenMedia[m.path] {
			if _, err := db.Exec(`DELETE FROM media WHERE id = ?`, m.id); err != nil {
				return removedMedia, 0, err
			}
			removedMedia++
		}
	}

	fileRows, err := db.Query(`SELECT id, path FROM files`)
	if err != nil {
		return removedMedia, 0, err
	}
	var allFiles []idPath
	for fileRows.Next() {
		var r idPath
		if err := fileRows.Scan(&r.id, &r.path); err != nil {
			fileRows.Close()
			return removedMedia, 0, err
		}
		allFiles = append(allFiles, r)
	}
	fileRows.Close()

	removedFiles := 0
	for _, f := range allFiles {
		if !seenFiles[f.path] {
			if _, err := db.Exec(`DELETE FROM files WHERE id = ?`, f.id); err != nil {
				return removedMedia, removedFiles, err
			}
			removedFiles++
		}
	}
	return removedMedia, removedFiles, nil
}

func upsertFile(db dbExecer, mediaID int64, path string, season, episode *int, hasSubtitle bool, subtitleName string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	sub := 0
	if hasSubtitle {
		sub = 1
	}
	_, err := db.Exec(`
		INSERT INTO files(media_id, path, season, episode, has_subtitle, subtitle_name, last_seen)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			season        = excluded.season,
			episode       = excluded.episode,
			has_subtitle  = excluded.has_subtitle,
			subtitle_name = excluded.subtitle_name,
			last_seen     = excluded.last_seen`,
		mediaID, path, season, episode, sub, subtitleName, now)
	return err
}
