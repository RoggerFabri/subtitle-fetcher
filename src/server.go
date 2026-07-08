package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Setting keys — every provider's credentials are consistently prefixed.
const (
	settingProviderOrder = "provider_order"
	settingWorkers       = "workers"

	// OpenSubtitles keys
	settingOSEnabled  = "opensubtitles_enabled"
	settingOSUsername = "opensubtitles_username"
	settingOSPassword = "opensubtitles_password"
	settingOSApiKey   = "opensubtitles_api_key"

	// SubDL keys
	settingSubDLEnabled = "subdl_enabled"
	settingSubDLApiKey  = "subdl_api_key"

	// Wyzie keys
	settingWyzieEnabled = "wyzie_enabled"
	settingWyzieApiKey  = "wyzie_api_key"

	// TMDB metadata (used to backfill NFOs, not a subtitle provider)
	settingTMDBApiKey = "tmdb_api_key"

	settingAutoScanInterval = "auto_scan_interval"
)

type server struct {
	db      *sql.DB
	root    string
	workers atomic.Int32

	scanMu       sync.Mutex
	scanning     atomic.Bool
	scanStatus   string
	scanSource   string // "manual" | "poll"
	scanStatusMu sync.RWMutex
	scanCurrent  atomic.Int64
	scanTotal    atomic.Int64

	listeners   map[chan bool]bool
	listenersMu sync.Mutex

	watcher *mediaWatcher

	poller   *mediaPoller
	pollerMu sync.Mutex

	osProvider   *openSubtitlesProvider
	osProviderMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
}

func newServer(db *sql.DB, root string, workers int, autoScanInterval time.Duration) *server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &server{
		db:        db,
		root:      root,
		listeners: make(map[chan bool]bool),
		ctx:       ctx,
		cancel:    cancel,
	}
	// Seed defaults so settings are available before the first HTTP request.
	for k, v := range providerDefaults() {
		if getSetting(db, k) == "" {
			setSetting(db, k, v)
		}
	}
	// DB-stored value takes precedence over the CLI flag.
	if stored := getSetting(db, settingWorkers); stored != "" {
		if n, err := strconv.Atoi(stored); err == nil && n >= 1 && n <= 50 {
			workers = n
		}
	}
	s.workers.Store(int32(workers))

	// DB-stored poll interval takes precedence over CLI flag.
	if stored := getSetting(db, settingAutoScanInterval); stored != "" && stored != "0" {
		if d, err := time.ParseDuration(stored); err == nil && d >= time.Minute {
			autoScanInterval = d
		}
	}
	if autoScanInterval >= time.Minute {
		s.poller = newMediaPoller(s, autoScanInterval)
		s.poller.Start()
	}

	go s.watchWeb()

	if mw, err := newMediaWatcher(s); err != nil {
		fmt.Printf("[watch] warning: cannot start watcher: %v\n", err)
	} else {
		s.watcher = mw
		fmt.Printf("[watch] starting, scanning %s/{Movies,Series} in background\n", root)
		mw.Start()
	}

	return s
}

func (s *server) Shutdown() {
	s.cancel()
	if s.watcher != nil {
		s.watcher.Stop()
	}
	s.pollerMu.Lock()
	if s.poller != nil {
		s.poller.Stop()
		s.poller = nil
	}
	s.pollerMu.Unlock()
	s.osProviderMu.Lock()
	if s.osProvider != nil {
		s.osProvider.Logout()
		s.osProvider = nil
	}
	s.osProviderMu.Unlock()
}

func (s *server) updatePoller(interval time.Duration) {
	s.pollerMu.Lock()
	defer s.pollerMu.Unlock()
	if s.poller != nil {
		s.poller.Stop()
		s.poller = nil
	}
	if interval >= time.Minute {
		s.poller = newMediaPoller(s, interval)
		s.poller.Start()
	}
}
