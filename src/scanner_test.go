package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func fileSub(t *testing.T, db *sql.DB, path string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT has_subtitle FROM files WHERE path = ?`, path).Scan(&n); err != nil {
		t.Fatalf("query has_subtitle for %s: %v", path, err)
	}
	return n
}

func fileExists(t *testing.T, db *sql.DB, path string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files WHERE path = ?`, path).Scan(&n); err != nil {
		t.Fatalf("count files for %s: %v", path, err)
	}
	return n > 0
}

// bumpMtime forces a directory's mtime forward so a change is detected
// regardless of the filesystem's timestamp resolution.
func bumpMtime(t *testing.T, dir string) {
	t.Helper()
	future := time.Now().Add(10 * time.Second)
	if err := os.Chtimes(dir, future, future); err != nil {
		t.Fatalf("chtimes %s: %v", dir, err)
	}
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestIncrementalScan_SkipsUnchangedAndDetectsChanges(t *testing.T) {
	db := newTestDB(t)
	root := newTestRoot(t)

	movieDir := filepath.Join(root, "Movies", "Inception (2010)")
	mustMkdir(t, movieDir)
	movieVid := filepath.Join(movieDir, "Inception.mkv")
	writeFile(t, movieVid)
	writeFile(t, filepath.Join(movieDir, "Inception.srt")) // sidecar subtitle

	seasonDir := filepath.Join(root, "Series", "Show", "Season 1")
	mustMkdir(t, seasonDir)
	writeFile(t, filepath.Join(seasonDir, "Show.S01E01.mkv"))

	scan := func() {
		if err := runScanWithProgressDB(context.Background(), db, root, 4, func(string, int, int) {}); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}

	scan()
	if got := fileSub(t, db, movieVid); got != 1 {
		t.Fatalf("after first scan: movie has_subtitle=%d, want 1", got)
	}

	// Mutate the DB to a value the disk does NOT reflect, then rescan without
	// touching disk. A skipped (unchanged) folder must preserve the mutation.
	mustExec(t, db, `UPDATE files SET has_subtitle = 0 WHERE path = ?`, movieVid)
	scan()
	if got := fileSub(t, db, movieVid); got != 0 {
		t.Fatalf("unchanged movie was re-scanned (has_subtitle=%d); expected skip to preserve 0", got)
	}

	// Add an episode and bump the season mtime — the series must be re-read.
	newEp := filepath.Join(seasonDir, "Show.S01E02.mkv")
	writeFile(t, newEp)
	bumpMtime(t, seasonDir)
	scan()
	if !fileExists(t, db, newEp) {
		t.Fatal("new episode not picked up by incremental scan")
	}
	// The untouched movie must still have been skipped.
	if got := fileSub(t, db, movieVid); got != 0 {
		t.Fatalf("movie re-scanned after an unrelated series change (has_subtitle=%d); expected 0", got)
	}
}

func TestIncrementalScan_PrunesRemovedFileInChangedFolder(t *testing.T) {
	db := newTestDB(t)
	root := newTestRoot(t)

	seasonDir := filepath.Join(root, "Series", "Show", "Season 1")
	mustMkdir(t, seasonDir)
	ep1 := filepath.Join(seasonDir, "Show.S01E01.mkv")
	ep2 := filepath.Join(seasonDir, "Show.S01E02.mkv")
	writeFile(t, ep1)
	writeFile(t, ep2)

	scan := func() {
		if err := runScanWithProgressDB(context.Background(), db, root, 4, func(string, int, int) {}); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}

	scan()
	if !fileExists(t, db, ep2) {
		t.Fatal("ep2 missing after first scan")
	}

	// Remove an episode and bump the season mtime so the folder is re-read.
	if err := os.Remove(ep2); err != nil {
		t.Fatalf("remove ep2: %v", err)
	}
	bumpMtime(t, seasonDir)
	scan()

	if fileExists(t, db, ep2) {
		t.Fatal("removed episode was not pruned")
	}
	if !fileExists(t, db, ep1) {
		t.Fatal("surviving episode was wrongly pruned")
	}
}
