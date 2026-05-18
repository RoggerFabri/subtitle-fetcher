package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DB_PATH", dir)
	db, err := openDB(dir)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range []string{"Movies", "Series"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	return root
}

// newTestWatcher creates a watcher backed by real fsnotify on a temp root,
// with a short debounce and a capturable fetchFn.
func newTestWatcher(t *testing.T, root string, db *sql.DB, fetchFn func([]apiFile, apiMedia) map[string]any) *mediaWatcher {
	t.Helper()
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	srv := &server{db: db, root: root}
	srv.workers.Store(1)
	mw := &mediaWatcher{
		s:        srv,
		watcher:  fw,
		pending:  make(map[string]*pendingFile),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		debounce: 80 * time.Millisecond,
		fetchFn:  fetchFn,
	}
	t.Cleanup(mw.Stop)

	// Register directories and start the event loop synchronously so the
	// watcher is ready before the test creates any files.
	for _, sub := range []string{"Movies", "Series"} {
		mw.addDirTree(filepath.Join(root, sub))
	}
	if err := mw.watcher.Add(root); err != nil {
		t.Fatalf("watch root: %v", err)
	}
	go mw.loop()
	return mw
}

// waitFor polls cond every 10 ms for up to timeout, failing if it never becomes true.
func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("fake video"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ── resolveMediaDir ───────────────────────────────────────────────────────────

func TestResolveMediaDir(t *testing.T) {
	root := "/media"

	tests := []struct {
		name         string
		videoPath    string
		wantMediaDir string
		wantType     string
		wantOK       bool
	}{
		{
			name:         "movie direct child",
			videoPath:    "/media/Movies/Interstellar (2014)/Interstellar.mkv",
			wantMediaDir: "/media/Movies/Interstellar (2014)",
			wantType:     "movie",
			wantOK:       true,
		},
		{
			name:         "series with season folder",
			videoPath:    "/media/Series/Breaking Bad/Season 1/S01E01.mkv",
			wantMediaDir: "/media/Series/Breaking Bad",
			wantType:     "series",
			wantOK:       true,
		},
		{
			name:         "series without season folder",
			videoPath:    "/media/Series/Miniseries/S01E01.mkv",
			wantMediaDir: "/media/Series/Miniseries",
			wantType:     "series",
			wantOK:       true,
		},
		{
			name:      "outside Movies and Series",
			videoPath: "/media/Downloads/something.mkv",
			wantOK:    false,
		},
		{
			name:      "directly in root",
			videoPath: "/media/movie.mkv",
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mediaDir, mediaType, ok := resolveMediaDir(root, tc.videoPath)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if filepath.ToSlash(mediaDir) != tc.wantMediaDir {
				t.Errorf("mediaDir = %q, want %q", mediaDir, tc.wantMediaDir)
			}
			if mediaType != tc.wantType {
				t.Errorf("mediaType = %q, want %q", mediaType, tc.wantType)
			}
		})
	}
}

// ── integration tests ─────────────────────────────────────────────────────────

func TestWatcherDetectsNewMovieFile(t *testing.T) {
	root := newTestRoot(t)
	db := newTestDB(t)

	var fetchCalls atomic.Int32
	mw := newTestWatcher(t, root, db, func(files []apiFile, m apiMedia) map[string]any {
		fetchCalls.Add(1)
		return map[string]any{"downloaded": 0, "failed": 0}
	})
	_ = mw

	movieDir := filepath.Join(root, "Movies", "Interstellar (2014)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Give fsnotify time to register the new directory before dropping the file.
	time.Sleep(150 * time.Millisecond)

	videoPath := filepath.Join(movieDir, "Interstellar.mkv")
	touch(t, videoPath)

	waitFor(t, 2*time.Second, "fetchFn called", func() bool {
		return fetchCalls.Load() == 1
	})

	// Verify the file row was inserted in the DB.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM files WHERE path = ?`, videoPath).Scan(&count)
	if count != 1 {
		t.Errorf("files row count = %d, want 1", count)
	}

	// Verify the media row was inserted with correct type.
	var mediaType string
	db.QueryRow(`SELECT type FROM media WHERE name = 'Interstellar (2014)'`).Scan(&mediaType)
	if mediaType != "movie" {
		t.Errorf("media type = %q, want %q", mediaType, "movie")
	}
}

func TestWatcherDetectsNewSeriesFile(t *testing.T) {
	root := newTestRoot(t)
	db := newTestDB(t)

	var mu sync.Mutex
	var capturedFile apiFile
	mw := newTestWatcher(t, root, db, func(files []apiFile, m apiMedia) map[string]any {
		mu.Lock()
		capturedFile = files[0]
		mu.Unlock()
		return map[string]any{"downloaded": 0, "failed": 0}
	})
	_ = mw

	// Add the season directory to the watcher first (simulates watcher picking it up).
	seasonDir := filepath.Join(root, "Series", "Breaking Bad", "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Give fsnotify a moment to register the new directory via the Create event.
	time.Sleep(150 * time.Millisecond)

	videoPath := filepath.Join(seasonDir, "Breaking.Bad.S01E01.mkv")
	touch(t, videoPath)

	waitFor(t, 2*time.Second, "fetchFn called", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return capturedFile.Path != ""
	})

	mu.Lock()
	f := capturedFile
	mu.Unlock()

	if f.Season == nil || *f.Season != 1 {
		t.Errorf("season = %v, want 1", f.Season)
	}
	if f.Episode == nil || *f.Episode != 1 {
		t.Errorf("episode = %v, want 1", f.Episode)
	}

	var mediaType string
	db.QueryRow(`SELECT type FROM media WHERE name = 'Breaking Bad'`).Scan(&mediaType)
	if mediaType != "series" {
		t.Errorf("media type = %q, want %q", mediaType, "series")
	}
}

func TestWatcherIgnoresNonVideoFile(t *testing.T) {
	root := newTestRoot(t)
	db := newTestDB(t)

	var fetchCalls atomic.Int32
	mw := newTestWatcher(t, root, db, func(files []apiFile, m apiMedia) map[string]any {
		fetchCalls.Add(1)
		return map[string]any{}
	})
	_ = mw

	touch(t, filepath.Join(root, "Movies", "SomeMovie", "info.nfo"))
	touch(t, filepath.Join(root, "Movies", "SomeMovie", "cover.jpg"))

	// Wait longer than the debounce to confirm nothing fires.
	time.Sleep(300 * time.Millisecond)

	if n := fetchCalls.Load(); n != 0 {
		t.Errorf("fetchFn called %d times, want 0", n)
	}
}

func TestWatcherSkipsFetchWhenSubtitlePresent(t *testing.T) {
	root := newTestRoot(t)
	db := newTestDB(t)

	var fetchCalls atomic.Int32
	mw := newTestWatcher(t, root, db, func(files []apiFile, m apiMedia) map[string]any {
		fetchCalls.Add(1)
		return map[string]any{}
	})
	_ = mw

	dir := filepath.Join(root, "Movies", "Alien (1979)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	videoPath := filepath.Join(dir, "Alien.mkv")
	srtPath := filepath.Join(dir, "Alien.srt")
	touch(t, videoPath)
	touch(t, srtPath)

	// Wait for debounce + processing.
	time.Sleep(300 * time.Millisecond)

	if n := fetchCalls.Load(); n != 0 {
		t.Errorf("fetchFn called %d times, want 0 (subtitle already present)", n)
	}

	// The file should still be in the DB with has_subtitle = 1.
	var hasSub int
	db.QueryRow(`SELECT has_subtitle FROM files WHERE path = ?`, videoPath).Scan(&hasSub)
	if hasSub != 1 {
		t.Errorf("has_subtitle = %d, want 1", hasSub)
	}
}

func TestWatcherDebouncesMultipleEvents(t *testing.T) {
	root := newTestRoot(t)
	db := newTestDB(t)

	var fetchCalls atomic.Int32
	mw := newTestWatcher(t, root, db, func(files []apiFile, m apiMedia) map[string]any {
		fetchCalls.Add(1)
		return map[string]any{}
	})

	videoPath := filepath.Join(root, "Movies", "Dune (2021)", "Dune.mkv")
	touch(t, videoPath)

	// Simulate multiple write events for the same file (large copy in progress).
	for i := 0; i < 5; i++ {
		time.Sleep(20 * time.Millisecond)
		mw.scheduleProcess(videoPath)
	}

	waitFor(t, 2*time.Second, "fetchFn called once", func() bool {
		return fetchCalls.Load() == 1
	})

	// Extra wait to confirm it doesn't fire a second time.
	time.Sleep(200 * time.Millisecond)

	if n := fetchCalls.Load(); n != 1 {
		t.Errorf("fetchFn called %d times, want exactly 1", n)
	}
}
