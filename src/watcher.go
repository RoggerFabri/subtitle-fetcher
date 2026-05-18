package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watchDebounce = 5 * time.Second

type pendingFile struct {
	timer *time.Timer
}

type mediaWatcher struct {
	s        *server
	watcher  *fsnotify.Watcher
	mu       sync.Mutex
	pending  map[string]*pendingFile
	stopCh   chan struct{}
	doneCh   chan struct{}
	debounce time.Duration
	fetchFn  func(files []apiFile, media apiMedia) map[string]any
}

func newMediaWatcher(s *server) (*mediaWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fsnotify.NewWatcher: %w", err)
	}
	mw := &mediaWatcher{
		s:        s,
		watcher:  fw,
		pending:  make(map[string]*pendingFile),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		debounce: watchDebounce,
	}
	mw.fetchFn = s.fetchSubtitlesForFiles
	return mw, nil
}

func (w *mediaWatcher) Start() {
	go w.loop()
	// Only watch the root itself so newly created Movies/ or Series/ directories
	// are picked up. Skip the recursive addDirTree walk — it traverses the entire
	// media library over the network mount on startup, competing with the scanner.
	if err := w.watcher.Add(w.s.root); err != nil {
		fmt.Printf("[watch] warn: cannot watch root %s: %v\n", w.s.root, err)
	}
	fmt.Printf("[watch] ready\n")
}

func (w *mediaWatcher) Stop() {
	close(w.stopCh)
	<-w.doneCh
	w.watcher.Close()
}

func (w *mediaWatcher) addDirTree(dir string) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	if err := w.watcher.Add(dir); err != nil {
		fmt.Printf("[watch] warn: cannot watch %s: %v\n", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			w.addDirTree(filepath.Join(dir, e.Name()))
		}
	}
}

func (w *mediaWatcher) loop() {
	defer close(w.doneCh)
	for {
		select {
		case <-w.stopCh:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[watch] error: %v\n", err)
		}
	}
}

func (w *mediaWatcher) handleEvent(event fsnotify.Event) {
	if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) {
		return
	}

	path := filepath.Clean(event.Name)
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	if info.IsDir() {
		w.addDirTree(path)
		return
	}

	if !videoExts[strings.ToLower(filepath.Ext(path))] {
		return
	}

	w.scheduleProcess(path)
}

func (w *mediaWatcher) scheduleProcess(videoPath string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if p, exists := w.pending[videoPath]; exists {
		p.timer.Reset(w.debounce)
		return
	}

	t := time.AfterFunc(w.debounce, func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[watch] panic processing %s: %v\n", filepath.Base(videoPath), r)
			}
		}()
		w.processFile(videoPath)
		w.mu.Lock()
		delete(w.pending, videoPath)
		w.mu.Unlock()
	})
	w.pending[videoPath] = &pendingFile{timer: t}
}

func (w *mediaWatcher) processFile(videoPath string) {
	if _, err := os.Stat(videoPath); err != nil {
		return
	}

	mediaDir, mediaType, ok := resolveMediaDir(w.s.root, videoPath)
	if !ok {
		return
	}

	fmt.Printf("[watch] detected %s (%s)\n", filepath.Base(videoPath), mediaType)

	mediaID, err := upsertMedia(w.s.db, mediaDir, filepath.Base(mediaDir), mediaType)
	if err != nil {
		fmt.Printf("[watch] upsertMedia error: %v\n", err)
		return
	}

	var season, episode *int
	if mediaType == "series" {
		stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
		if s, ep, ok := parseSeasonEpisode(stem); ok {
			season = &s
			episode = &ep
		} else {
			sn := parseSeasonFromFolder(filepath.Base(filepath.Dir(videoPath)))
			if sn >= 0 {
				season = &sn
			}
		}
	}

	sp := subtitlePath(videoPath)
	hasSub := sp != ""
	subName := ""
	if hasSub {
		subName = filepath.Base(sp)
	}

	if err := upsertFile(w.s.db, mediaID, videoPath, season, episode, hasSub, subName); err != nil {
		fmt.Printf("[watch] upsertFile error: %v\n", err)
		return
	}

	if hasSub {
		fmt.Printf("[watch] subtitle already present for %s, skipping fetch\n", filepath.Base(videoPath))
		return
	}

	var imdbID string
	w.s.db.QueryRow(`SELECT imdb_id FROM media WHERE id=?`, mediaID).Scan(&imdbID)

	f := apiFile{
		Path:        videoPath,
		Name:        filepath.Base(videoPath),
		Season:      season,
		Episode:     episode,
		HasSubtitle: false,
	}
	m := apiMedia{
		ID:     mediaID,
		Name:   filepath.Base(mediaDir),
		Type:   mediaType,
		ImdbID: imdbID,
	}

	result := w.fetchFn([]apiFile{f}, m)
	fmt.Printf("[watch] fetch result for %s: %v\n", filepath.Base(videoPath), result)
}

// resolveMediaDir returns the media directory (one level under Movies/ or Series/),
// the media type, and whether the path is under a known subtree.
func resolveMediaDir(root, videoPath string) (mediaDir, mediaType string, ok bool) {
	rel, err := filepath.Rel(root, videoPath)
	if err != nil {
		return
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return
	}
	switch parts[0] {
	case "Movies":
		mediaDir = filepath.Join(root, "Movies", parts[1])
		mediaType = "movie"
		ok = true
	case "Series":
		mediaDir = filepath.Join(root, "Series", parts[1])
		mediaType = "series"
		ok = true
	}
	return
}
