package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
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
)

type server struct {
	db      *sql.DB
	root    string
	workers atomic.Int32

	scanMu       sync.Mutex
	scanning     atomic.Bool
	scanStatus   string
	scanStatusMu sync.RWMutex
	scanCurrent  atomic.Int64
	scanTotal    atomic.Int64

	listeners   map[chan bool]bool
	listenersMu sync.Mutex

	watcher *mediaWatcher

	osProvider   *openSubtitlesProvider
	osProviderMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
}

func newServer(db *sql.DB, root string, workers int) *server {
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
	s.osProviderMu.Lock()
	if s.osProvider != nil {
		s.osProvider.Logout()
		s.osProvider = nil
	}
	s.osProviderMu.Unlock()
}
