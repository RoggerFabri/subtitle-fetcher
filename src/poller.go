package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type mediaPoller struct {
	s        *server
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newMediaPoller(s *server, interval time.Duration) *mediaPoller {
	return &mediaPoller{
		s:        s,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

func (p *mediaPoller) Start() {
	go p.loop()
	fmt.Printf("[autoscan] started, interval=%s\n", p.interval)
}

func (p *mediaPoller) Stop() {
	close(p.stopCh)
	<-p.doneCh
}

func (p *mediaPoller) loop() {
	defer close(p.doneCh)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.runPoll()
		}
	}
}

func (p *mediaPoller) runPoll() {
	if !p.s.scanning.CompareAndSwap(false, true) {
		fmt.Printf("[autoscan] scan already in progress, skipping\n")
		return
	}
	defer p.s.scanning.Store(false)
	defer p.s.scanCurrent.Store(0)
	defer p.s.scanTotal.Store(0)

	// Snapshot all file paths known before the scan.
	knownPaths := p.knownFilePaths()

	fmt.Printf("[autoscan] scanning for new files\n")
	p.s.setScanSource("autoscan")
	p.s.setScanStatus("running")
	var lastLog time.Time
	if err := runScanWithProgressDB(p.s.ctx, p.s.db, p.s.root, int(p.s.workers.Load()), func(status string, done, total int) {
		p.s.setScanStatus(status)
		p.s.scanCurrent.Store(int64(done))
		p.s.scanTotal.Store(int64(total))
		now := time.Now()
		if done == total || now.Sub(lastLog) >= 5*time.Second {
			lastLog = now
			fmt.Printf("[autoscan] %s\n", status)
		}
	}); err != nil {
		fmt.Printf("[autoscan] scan error: %v\n", err)
		p.s.setScanStatus("error: " + err.Error())
		return
	}
	p.s.setScanStatus("done")

	if p.s.ctx.Err() != nil {
		return
	}

	p.fetchNewFiles(knownPaths)
}

func (p *mediaPoller) knownFilePaths() map[string]struct{} {
	rows, err := p.s.db.Query(`SELECT path FROM files`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	paths := make(map[string]struct{})
	for rows.Next() {
		var path string
		if rows.Scan(&path) == nil {
			paths[path] = struct{}{}
		}
	}
	return paths
}

func (p *mediaPoller) fetchNewFiles(knownPaths map[string]struct{}) {
	rows, err := p.s.db.Query(`
		SELECT DISTINCT m.id, m.name, m.type, COALESCE(m.imdb_id, '')
		FROM media m
		JOIN files f ON f.media_id = m.id
		WHERE f.has_subtitle = 0
	`)
	if err != nil {
		fmt.Printf("[autoscan] DB query error: %v\n", err)
		return
	}
	var mediaList []apiMedia
	for rows.Next() {
		var m apiMedia
		if err := rows.Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID); err != nil {
			rows.Close()
			fmt.Printf("[autoscan] DB scan error: %v\n", err)
			return
		}
		mediaList = append(mediaList, m)
	}
	rows.Close()

	var totalDownloaded, totalFailed, skipped atomic.Int32
	workers := int(p.s.workers.Load())
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, m := range mediaList {
		if p.s.ctx.Err() != nil {
			break
		}
		files, err := p.s.fetchFilesFromDB(
			`SELECT id, path, season, episode, has_subtitle FROM files WHERE media_id=? AND has_subtitle=0`, m.ID)
		if err != nil {
			fmt.Printf("[autoscan] DB error loading files for %s: %v\n", m.Name, err)
			continue
		}
		var newFiles []apiFile
		for _, f := range files {
			if _, known := knownPaths[f.Path]; !known {
				newFiles = append(newFiles, f)
			} else {
				skipped.Add(1)
			}
		}
		if len(newFiles) == 0 {
			continue
		}
		wg.Add(1)
		go func(media apiMedia, filesToFetch []apiFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result := p.s.fetchSubtitlesForFiles(filesToFetch, media)
			if dl, ok := result["downloaded"].(int); ok {
				totalDownloaded.Add(int32(dl))
			}
			if fl, ok := result["failed"].(int); ok {
				totalFailed.Add(int32(fl))
			}
		}(m, newFiles)
	}
	wg.Wait()

	fmt.Printf("[autoscan] done — %d downloaded, %d failed, %d existing skipped\n",
		totalDownloaded.Load(), totalFailed.Load(), skipped.Load())
}
