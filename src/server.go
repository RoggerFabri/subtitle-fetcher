package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Setting keys — every provider's credentials are consistently prefixed.
const (
	settingProviderOrder = "provider_order"

	// OpenSubtitles keys (renamed from bare "username"/"password"/"api_key")
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
	workers int

	scanMu       sync.Mutex
	scanning     atomic.Bool
	scanStatus   string
	scanStatusMu sync.RWMutex
	scanCurrent  atomic.Int64 // New: current item being scanned
	scanTotal    atomic.Int64 // New: total items to scan

	listeners   map[chan bool]bool
	listenersMu sync.Mutex

	watcher *mediaWatcher
}

func newServer(db *sql.DB, root string, workers int) *server {
	s := &server{
		db:        db,
		root:      root,
		workers:   workers,
		listeners: make(map[chan bool]bool),
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
	if s.watcher != nil {
		s.watcher.Stop()
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// Serve static files conditionally: from disk if available (for dev), otherwise from embedded FS (prod).
	var fsHandler http.Handler
	if _, err := os.Stat("src/web"); err == nil {
		fsHandler = http.FileServer(http.Dir("src/web"))
	} else if _, err := os.Stat("web"); err == nil {
		fsHandler = http.FileServer(http.Dir("web"))
	} else {
		sub, _ := fs.Sub(webFS, "web")
		fsHandler = http.FileServer(http.FS(sub))
	}

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			// Disable caching so changes to JS/CSS are reflected immediately on reload
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
			fsHandler.ServeHTTP(w, r)
		}
	}))

	mux.HandleFunc("GET /api/report", s.handleReport)
	mux.HandleFunc("GET /api/hot-reload", s.handleHotReload)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleSaveSettings)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/health/test", s.handleTestConnection)
	mux.HandleFunc("POST /api/health/provider-test", s.handleTestProvider)
	mux.HandleFunc("POST /api/scan", s.handleScan)
	mux.HandleFunc("GET /api/scan/status", s.handleScanStatus)
	mux.HandleFunc("POST /api/fetch/media/{id}", s.handleFetchMedia)
	mux.HandleFunc("POST /api/fetch/season/{id}/{season}", s.handleFetchSeason)
	mux.HandleFunc("POST /api/fetch/file/{id}", s.handleFetchFile)
	mux.HandleFunc("DELETE /api/subtitle/{id}", s.handleDeleteSubtitle)
	mux.HandleFunc("POST /api/search/file/{id}", s.handleSearchFile)
	mux.HandleFunc("POST /api/download/file/{id}", s.handleDownloadCandidate)
	mux.HandleFunc("GET /api/imdb/search", s.handleIMDBSearch)
	mux.HandleFunc("PUT /api/media/{id}/imdb", s.handleSetMediaIMDB)
	return logMiddleware(mux)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	body   strings.Builder
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status >= 400 {
		r.body.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/scan/status" || r.URL.Path == "/api/hot-reload" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start).Round(time.Millisecond)
		if rec.status >= 400 {
			body := strings.TrimSpace(rec.body.String())
			fmt.Printf("[http] %-6s %-40s %d  %s  — %s\n", r.Method, r.URL.Path, rec.status, elapsed, body)
		} else {
			fmt.Printf("[http] %-6s %-40s %d  %s\n", r.Method, r.URL.Path, rec.status, elapsed)
		}
	})
}

func (s *server) watchWeb() {
	watchDir := ""
	if _, err := os.Stat("src/web"); err == nil {
		watchDir = "src/web"
	} else if _, err := os.Stat("web"); err == nil {
		watchDir = "web"
	} else {
		// running embedded, no hot-reload needed
		return
	}

	lastMod := time.Now()
	for {
		time.Sleep(1 * time.Second)
		changed := false
		_ = filepath.Walk(watchDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && info.ModTime().After(lastMod) {
				changed = true
				lastMod = info.ModTime()
			}
			return nil
		})
		if changed {
			fmt.Println("[dev] web changes detected, triggering browser reload")
			s.listenersMu.Lock()
			for ch := range s.listeners {
				select {
				case ch <- true:
				default:
				}
			}
			s.listenersMu.Unlock()
		}
	}
}

// --- scan ---

func (s *server) setScanStatus(msg string) {
	s.scanStatusMu.Lock()
	s.scanStatus = msg
	s.scanStatusMu.Unlock()
}

func (s *server) getScanStatus() string {
	s.scanStatusMu.RLock()
	defer s.scanStatusMu.RUnlock()
	return s.scanStatus
}

func (s *server) handleScan(w http.ResponseWriter, r *http.Request) {
	if !s.scanning.CompareAndSwap(false, true) {
		jsonError(w, "scan already in progress", http.StatusConflict)
		return
	}
	go func() {
		defer s.scanning.Store(false)
		defer s.scanCurrent.Store(0) // Reset on finish
		defer s.scanTotal.Store(0)   // Reset on finish
		fmt.Printf("[scan] started  root=%s\n", s.root)
		start := time.Now()
		s.setScanStatus("running")
		var lastLog time.Time
		if err := runScanWithProgress(s.root, func(status string, done, total int) {
			s.setScanStatus(status)
			s.scanCurrent.Store(int64(done))
			s.scanTotal.Store(int64(total))
			now := time.Now()
			if done == total || now.Sub(lastLog) >= time.Second {
				lastLog = now
				fmt.Printf("[scan] %s\n", status)
			}
		}); err != nil {
			fmt.Printf("[scan] error: %v\n", err)
			s.setScanStatus("error: " + err.Error())
			return
		}
		fmt.Printf("[scan] done  elapsed=%s\n", time.Since(start).Round(time.Millisecond))
		s.setScanStatus("done")
	}()
	jsonOK(w, map[string]string{"status": "started"})
}

func (s *server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{
		"running": s.scanning.Load(),
		"status":  s.getScanStatus(),
		"current": s.scanCurrent.Load(), // New
		"total":   s.scanTotal.Load(),   // New
	})
}

// --- report ---

type apiFile struct {
	ID           int64  `json:"id"`
	Path         string `json:"path"`
	Name         string `json:"name"`
	Season       *int   `json:"season"`
	Episode      *int   `json:"episode"`
	HasSubtitle  bool   `json:"has_subtitle"`
	SubtitleName string `json:"subtitle_name,omitempty"`
}

type apiMedia struct {
	ID     int64     `json:"id"`
	Name   string    `json:"name"`
	Type   string    `json:"type"`
	ImdbID string    `json:"imdb_id,omitempty"`
	Files  []apiFile `json:"files"`
}

func (s *server) handleReport(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, name, type, imdb_id FROM media ORDER BY type, name`)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	result := []apiMedia{}
	for rows.Next() {
		var m apiMedia
		if err := rows.Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID); err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		result = append(result, m)
	}
	rows.Close()

	for i := range result {
		frows, err := s.db.Query(`
			SELECT id, path, season, episode, has_subtitle, subtitle_name
			FROM files WHERE media_id = ?
			ORDER BY season, episode`, result[i].ID)
		if err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		for frows.Next() {
			var f apiFile
			var season, episode sql.NullInt64
			var hasSub int
			if err := frows.Scan(&f.ID, &f.Path, &season, &episode, &hasSub, &f.SubtitleName); err != nil {
				frows.Close()
				jsonError(w, err.Error(), 500)
				return
			}
			if season.Valid {
				v := int(season.Int64)
				f.Season = &v
			}
			if episode.Valid {
				v := int(episode.Int64)
				f.Episode = &v
			}
			f.HasSubtitle = hasSub == 1
			f.Name = baseName(f.Path)
			result[i].Files = append(result[i].Files, f)
		}
		frows.Close()
		if result[i].Files == nil {
			result[i].Files = []apiFile{}
		}
	}

	jsonOK(w, result)
}

// --- settings ---

func providerDefaults() map[string]string {
	return map[string]string{
		settingProviderOrder: "opensubtitles,subdl,wyzie",
		settingOSEnabled:     "1",
		settingSubDLEnabled:  "1",
		settingWyzieEnabled:  "1",
	}
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	defaults := providerDefaults()
	for k := range defaults {
		if getSetting(s.db, k) == "" {
			setSetting(s.db, k, defaults[k])
		}
	}
	// Migrate: replace any legacy "podnapisi" entry in the stored order with "wyzie"
	if order := getSetting(s.db, settingProviderOrder); strings.Contains(order, "podnapisi") {
		setSetting(s.db, settingProviderOrder, strings.ReplaceAll(order, "podnapisi", "wyzie"))
	}

	orderStr := getSetting(s.db, "provider_order")
	if orderStr == "" {
		orderStr = defaults["provider_order"]
	}
	order := strings.Split(orderStr, ",")

	providers := map[string]any{}
	for _, name := range order {
		p := map[string]any{
			"enabled": getSetting(s.db, name+"_enabled") != "0",
		}
		switch name {
		case "opensubtitles":
			// Return raw values: empty string = unconfigured.
			// Credentials are never echoed back as masked strings — that causes form saddling.
			p["username"] = getSetting(s.db, settingOSUsername)
			p["password"] = ""
			p["api_key"] = getSetting(s.db, settingOSApiKey)
		case "subdl":
			p["api_key"] = getSetting(s.db, settingSubDLApiKey)
		case "wyzie":
			p["api_key"] = getSetting(s.db, settingWyzieApiKey)
		}
		providers[name] = p
	}
	providers[settingProviderOrder] = order

	jsonOK(w, providers)
}

func maskOrHidden(s string) string {
	if s == "" {
		return ""
	}
	return maskSecret(s)
}

func (s *server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Provider order
	if raw, ok := body["provider_order"]; ok {
		var order []string
		if err := json.Unmarshal(raw, &order); err == nil {
			setSetting(s.db, settingProviderOrder, strings.Join(order, ","))
		}
	}

	// Per-provider sections
	for _, name := range []string{"opensubtitles", "subdl", "wyzie"} {
		enabledKey := name + "_enabled"
		if raw, ok := body[name]; ok {
			var sec map[string]any
			if err := json.Unmarshal(raw, &sec); err == nil {
				if v, ok := sec["enabled"].(bool); ok {
					if v {
						setSetting(s.db, enabledKey, "1")
					} else {
						setSetting(s.db, enabledKey, "0")
					}
				}
				switch name {
				case "opensubtitles":
					if v, ok := sec["openSubtitles_api_key"].(string); ok && v != "" {
						setSetting(s.db, settingOSApiKey, v)
					}
					if v, ok := sec["password"].(string); ok && v != "" {
						setSetting(s.db, settingOSPassword, v)
					}
					if v, ok := sec["username"].(string); ok && v != "" {
						setSetting(s.db, settingOSUsername, v)
					}
				case "subdl":
					if v, ok := sec["api_key"].(string); ok && v != "" {
						setSetting(s.db, settingSubDLApiKey, v)
					}
				case "wyzie":
					if v, ok := sec["api_key"].(string); ok && v != "" {
						setSetting(s.db, settingWyzieApiKey, v)
					}
				}
			}
		}
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// --- export / import ---

type exportDoc struct {
	Version  int               `json:"version"`
	Settings map[string]string `json:"settings"`
	ImdbIDs  map[string]string `json:"imdb_ids"` // media name → imdb_id
}

func (s *server) handleExport(w http.ResponseWriter, r *http.Request) {
	// Collect all settings.
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	settings := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		settings[k] = v
	}
	rows.Close()

	// Collect IMDB IDs keyed by media name (portable across machines).
	mrows, err := s.db.Query(`SELECT name, imdb_id FROM media WHERE imdb_id != ''`)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	imdbIDs := map[string]string{}
	for mrows.Next() {
		var name, id string
		mrows.Scan(&name, &id)
		imdbIDs[name] = id
	}
	mrows.Close()

	doc := exportDoc{Version: 1, Settings: settings, ImdbIDs: imdbIDs}
	b, _ := json.MarshalIndent(doc, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="subtitle-fetcher-export.json"`)
	w.Write(b)
}

func (s *server) handleImport(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		jsonError(w, "invalid file", http.StatusBadRequest)
		return
	}
	if doc.Version != 1 {
		jsonError(w, fmt.Sprintf("unsupported export version %d", doc.Version), http.StatusBadRequest)
		return
	}

	// Apply settings.
	settingsApplied := 0
	for k, v := range doc.Settings {
		if err := setSetting(s.db, k, v); err == nil {
			settingsApplied++
		}
	}

	// Apply IMDB IDs — match by media name, skip unknowns.
	imdbApplied := 0
	for name, id := range doc.ImdbIDs {
		res, err := s.db.Exec(`UPDATE media SET imdb_id=? WHERE name=? AND (imdb_id='' OR imdb_id=?)`, id, name, id)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				imdbApplied++
			}
		}
	}

	jsonOK(w, map[string]any{
		"settings_applied": settingsApplied,
		"imdb_applied":     imdbApplied,
		"imdb_total":       len(doc.ImdbIDs),
	})
}

// --- health ---

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	defaults := providerDefaults()
	for k := range defaults {
		if getSetting(s.db, k) == "" {
			setSetting(s.db, k, defaults[k])
		}
	}

	orderStr := getSetting(s.db, settingProviderOrder)
	if orderStr == "" {
		orderStr = defaults[settingProviderOrder]
	}
	order := strings.Split(orderStr, ",")

	providers := map[string]any{}
	for _, name := range order {
		enabled := getSetting(s.db, name+"_enabled") != "0"
		p := map[string]any{
			"configured": false,
			"enabled":    enabled,
		}
		switch name {
		case "opensubtitles":
			u := getSetting(s.db, settingOSUsername)
			pw := getSetting(s.db, settingOSPassword)
			k := getSetting(s.db, settingOSApiKey)
			p["configured"] = u != "" && pw != "" && k != ""
			p["username"] = u
		case "subdl":
			k := getSetting(s.db, settingSubDLApiKey)
			p["configured"] = k != ""
		case "wyzie":
			p["configured"] = getSetting(s.db, settingWyzieApiKey) != ""
		}
		providers[name] = p
	}

	jsonOK(w, map[string]any{
		"providers":      providers,
		"provider_order": order,
	})
}

func (s *server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	// Legacy: test OpenSubtitles via login + user-info
	providers, err := loadProviders(s.db)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(providers) == 0 {
		jsonOK(w, map[string]any{"ok": false, "error": "no providers configured"})
		return
	}
	// Find the opensubtitles provider if available
	for _, p := range providers {
		if p.Name() == "opensubtitles" {
			if err := p.Open(); err != nil {
				jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			defer p.Close()
			type userInfo struct {
				Level              string `json:"level"`
				AllowedDownloads   int    `json:"allowed_downloads"`
				RemainingDownloads int    `json:"remaining_downloads"`
				DownloadsCount     int    `json:"downloads_count"`
				VIP                bool   `json:"vip"`
			}
			// Use opensubtitles.go client getUser via cast — instead request it directly
			// We reuse the existing client since opensubtitles is always first in providers slice
			cl, err2 := s.makeClient()
			if err2 != nil {
				jsonOK(w, map[string]any{"ok": false, "error": err2.Error()})
				return
			}
			defer cl.logout()
			info, err3 := cl.getUser()
			if err3 != nil {
				jsonOK(w, map[string]any{"ok": false, "error": err3.Error()})
				return
			}
			jsonOK(w, map[string]any{
				"ok":                  true,
				"level":               info.Level,
				"remaining_downloads": info.RemainingDownloads,
				"allowed_downloads":   info.AllowedDownloads,
				"vip":                 info.VIP,
			})
			return
		}
	}
	jsonOK(w, map[string]any{"ok": false, "error": "opensubtitles not configured"})
}

// handleTestProvider tests a specific provider by name
func (s *server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		jsonError(w, "missing provider param", http.StatusBadRequest)
		return
	}

	switch providerName {
	case "opensubtitles":
		cl, err := s.makeClient()
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		defer cl.logout()
		info, err := cl.getUser()
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{
			"ok":                  true,
			"level":               info.Level,
			"remaining_downloads": info.RemainingDownloads,
			"allowed_downloads":   info.AllowedDownloads,
			"vip":                 info.VIP,
		})
	case "subdl":
		k := getSetting(s.db, settingSubDLApiKey)
		if k == "" {
			jsonOK(w, map[string]any{"ok": false, "error": "API key not configured"})
			return
		}
		p := newSubDLProvider(k)
		res, err := p.search(map[string]string{
			"api_key":       k,
			"languages":     "EN",
			"type":          "movie",
			"film_name":     "test",
			"subs_per_page": "1",
		})
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{
			"ok":      true,
			"results": len(res),
		})
	case "wyzie":
		k := getSetting(s.db, settingWyzieApiKey)
		if k == "" {
			jsonOK(w, map[string]any{"ok": false, "error": "API key not configured"})
			return
		}
		p := newWyzieProvider(k)
		res, err := p.search("0816692", 0, 0, false) // Interstellar
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{"ok": true, "results": len(res)})
	default:
		jsonError(w, "unknown provider: "+providerName, http.StatusBadRequest)
	}
}

// --- provider loading ---

func loadProviders(db *sql.DB) ([]subtitleProvider, error) {
	defaults := providerDefaults()
	for k := range defaults {
		if getSetting(db, k) == "" {
			setSetting(db, k, defaults[k])
		}
	}

	order := getSetting(db, settingProviderOrder)
	if strings.Contains(order, "podnapisi") {
		order = strings.ReplaceAll(order, "podnapisi", "wyzie")
		setSetting(db, settingProviderOrder, order)
	}
	if order == "" {
		order = defaults[settingProviderOrder]
	}

	var out []subtitleProvider
	for _, name := range strings.Split(order, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if getSetting(db, name+"_enabled") == "0" {
			continue
		}
		switch name {
		case "opensubtitles":
			u, pw, k := getSetting(db, settingOSUsername),
				getSetting(db, settingOSPassword),
				getSetting(db, settingOSApiKey)
			if u != "" && pw != "" && k != "" {
				out = append(out, newOpenSubtitlesProvider(u, pw, k))
			}
		case "subdl":
			if k := getSetting(db, settingSubDLApiKey); k != "" {
				out = append(out, newSubDLProvider(k))
			}
		case "wyzie":
			if k := getSetting(db, settingWyzieApiKey); k != "" {
				out = append(out, newWyzieProvider(k))
			}
		}
	}
	return out, nil
}

// --- fetch helpers ---

// makeClient reads the OpenSubtitles prefixed keys.
func (s *server) makeClient() (*client, error) {
	username := getSetting(s.db, settingOSUsername)
	password := getSetting(s.db, settingOSPassword)
	apiKey := getSetting(s.db, settingOSApiKey)
	if username == "" || password == "" || apiKey == "" {
		return nil, fmt.Errorf("credentials not configured — open Settings")
	}
	cl := newClient(apiKey)
	if err := cl.login(username, password); err != nil {
		return nil, err
	}
	return cl, nil
}

func (s *server) fetchFilesFromDB(query string, args ...any) ([]apiFile, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []apiFile
	for rows.Next() {
		var f apiFile
		var season, episode sql.NullInt64
		var hasSub int
		if err := rows.Scan(&f.ID, &f.Path, &season, &episode, &hasSub); err != nil {
			return nil, err
		}
		if season.Valid {
			v := int(season.Int64)
			f.Season = &v
		}
		if episode.Valid {
			v := int(episode.Int64)
			f.Episode = &v
		}
		f.HasSubtitle = hasSub == 1
		f.Name = baseName(f.Path)
		files = append(files, f)
	}
	return files, nil
}

func (s *server) fetchSubtitlesForFiles(files []apiFile, media apiMedia) map[string]any {
	pending := 0
	for _, f := range files {
		if !f.HasSubtitle {
			pending++
		}
	}
	if pending == 0 {
		return map[string]any{"downloaded": 0, "failed": 0}
	}

	providers, err := loadProviders(s.db)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if len(providers) == 0 {
		return map[string]any{"error": "no providers configured — open Settings and add credentials"}
	}

	for _, p := range providers {
		if err := p.Open(); err != nil {
			fmt.Printf("  [%s] open error: %v\n", p.Name(), err)
		}
	}

	show := showNameFromFolder(media.Name)
	keywords := buildKeywords(show)
	imdbID := media.ImdbID
	if imdbID == "" {
		imdbID = discoverIMDBID(show, media.Type)
		if imdbID != "" {
			s.db.Exec(`UPDATE media SET imdb_id=? WHERE id=?`, imdbID, media.ID)
			media.ImdbID = imdbID
		}
	}


	fmt.Printf("[fetch] %s — %d file(s) to fetch, providers=%v\n", media.Name, pending, func() []string {
		names := make([]string, len(providers))
		for i, p := range providers {
			names[i] = p.Name()
		}
		return names
	}())

	var mu sync.Mutex
	var ok, failed int
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.workers)

	for _, f := range files {
		if f.HasSubtitle {
			continue
		}
		wg.Add(1)
		go func(file apiFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var printMu sync.Mutex
			success := fetchWithProviders(file.Path, show, keywords, imdbID, media.Type, providers, &printMu)

			mu.Lock()
			defer mu.Unlock()
			if success {
				ok++
				subName := ""
				if sp := subtitlePath(file.Path); sp != "" {
					subName = baseName(sp)
				}
				s.db.Exec(`UPDATE files SET has_subtitle=1, subtitle_name=? WHERE id=?`, subName, file.ID)
			} else {
				failed++
			}
		}(f)
	}
	wg.Wait()

	for _, p := range providers {
		p.Close()
	}

	fmt.Printf("[fetch] %s — done: %d downloaded, %d failed\n", media.Name, ok, failed)
	return map[string]any{"downloaded": ok, "failed": failed}
}

// --- fetch routes ---

func (s *server) handleFetchMedia(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	var m apiMedia
	if err := s.db.QueryRow(`SELECT id, name, type, imdb_id FROM media WHERE id=?`, id).Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	files, err := s.fetchFilesFromDB(`SELECT id, path, season, episode, has_subtitle FROM files WHERE media_id=? AND has_subtitle=0`, id)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, s.fetchSubtitlesForFiles(files, m))
}

func (s *server) handleFetchSeason(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	season, err2 := strconv.Atoi(r.PathValue("season"))
	if err != nil || err2 != nil {
		jsonError(w, "bad params", http.StatusBadRequest)
		return
	}
	var m apiMedia
	if err := s.db.QueryRow(`SELECT id, name, type, imdb_id FROM media WHERE id=?`, id).Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	files, err := s.fetchFilesFromDB(`SELECT id, path, season, episode, has_subtitle FROM files WHERE media_id=? AND season=? AND has_subtitle=0`, id, season)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, s.fetchSubtitlesForFiles(files, m))
}

func (s *server) handleFetchFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	var f apiFile
	var mediaID int64
	var season, episode sql.NullInt64
	var hasSub int
	err = s.db.QueryRow(`SELECT f.id, f.path, f.season, f.episode, f.has_subtitle, f.media_id FROM files f WHERE f.id=?`, id).
		Scan(&f.ID, &f.Path, &season, &episode, &hasSub, &mediaID)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if season.Valid {
		v := int(season.Int64)
		f.Season = &v
	}
	if episode.Valid {
		v := int(episode.Int64)
		f.Episode = &v
	}
	f.HasSubtitle = hasSub == 1
	f.Name = baseName(f.Path)

	var m apiMedia
	s.db.QueryRow(`SELECT id, name, type, imdb_id FROM media WHERE id=?`, mediaID).Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID)

	jsonOK(w, s.fetchSubtitlesForFiles([]apiFile{f}, m))
}

func (s *server) handleDeleteSubtitle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	var path string
	if err := s.db.QueryRow(`SELECT path FROM files WHERE id=?`, id).Scan(&path); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	sp := subtitlePath(path)
	if sp == "" {
		jsonError(w, "no subtitle found on disk", http.StatusNotFound)
		return
	}
	if err := os.Remove(sp); err != nil {
		jsonError(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.db.Exec(`UPDATE files SET has_subtitle=0, subtitle_name='' WHERE id=?`, id)
	jsonOK(w, map[string]any{"deleted": baseName(sp)})
}

func (s *server) handleSearchFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}

	var filePath string
	var mediaID int64
	if err := s.db.QueryRow(`SELECT path, media_id FROM files WHERE id=?`, id).Scan(&filePath, &mediaID); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	var m apiMedia
	s.db.QueryRow(`SELECT id, name, type, imdb_id FROM media WHERE id=?`, mediaID).Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID)

	providers, err := loadProviders(s.db)
	if err != nil || len(providers) == 0 {
		jsonOK(w, []SubtitleCandidate{})
		return
	}
	for _, p := range providers {
		p.Open()
	}
	defer func() {
		for _, p := range providers {
			p.Close()
		}
	}()

	show := showNameFromFolder(m.Name)
	keywords := buildKeywords(show)
	imdbID := m.ImdbID

	searchFilePath := filePath
	if q := r.URL.Query().Get("q"); q != "" {
		show = q
		keywords = buildKeywords(q)
		imdbID = "" // Ignore IMDB ID if doing a custom text search
		searchFilePath = "" // Clear path to prevent providers from inferring S/E and forcing constraints
	} else if imdbID == "" {
		imdbID = discoverIMDBID(show, m.Type)
		if imdbID != "" {
			s.db.Exec(`UPDATE media SET imdb_id=? WHERE id=?`, imdbID, m.ID)
		}
	}

	type result struct {
		candidates []SubtitleCandidate
	}
	ch := make(chan []SubtitleCandidate, len(providers))
	for _, p := range providers {
		go func(p subtitleProvider) {
			c, _ := p.SearchSubtitles(searchFilePath, show, keywords, imdbID, m.Type)
			ch <- c
		}(p)
	}

	var all []SubtitleCandidate
	for range providers {
		all = append(all, <-ch...)
	}

	// Sort by downloads descending.
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].Downloads > all[j-1].Downloads; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	if all == nil {
		all = []SubtitleCandidate{}
	}
	jsonOK(w, all)
}

func (s *server) handleDownloadCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		jsonError(w, "missing token", http.StatusBadRequest)
		return
	}

	sep := strings.IndexByte(body.Token, ':')
	if sep < 0 {
		jsonError(w, "invalid token", http.StatusBadRequest)
		return
	}
	providerName := body.Token[:sep]
	handle := body.Token[sep+1:]

	var filePath string
	if err := s.db.QueryRow(`SELECT path FROM files WHERE id=?`, id).Scan(&filePath); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	providers, err := loadProviders(s.db)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	var chosen subtitleProvider
	for _, p := range providers {
		if p.Name() == providerName {
			chosen = p
			break
		}
	}
	if chosen == nil {
		jsonError(w, "provider not configured: "+providerName, http.StatusBadRequest)
		return
	}

	if err := chosen.Open(); err != nil {
		jsonError(w, "provider open failed: "+err.Error(), 500)
		return
	}
	defer chosen.Close()

	subName, err := chosen.DownloadCandidate(handle, filePath)
	if err != nil {
		jsonError(w, "download failed: "+err.Error(), 500)
		return
	}

	s.db.Exec(`UPDATE files SET has_subtitle=1, subtitle_name=? WHERE id=?`, subName, id)
	jsonOK(w, map[string]any{"downloaded": true, "subtitle_name": subName})
}

// --- IMDB ---

func (s *server) handleIMDBSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		jsonOK(w, []IMDBSuggestion{})
		return
	}
	results, err := fetchIMDBSuggestions(q)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if results == nil {
		results = []IMDBSuggestion{}
	}
	jsonOK(w, results)
}

func (s *server) handleSetMediaIMDB(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	var body struct {
		ImdbID string `json:"imdb_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "bad body", http.StatusBadRequest)
		return
	}
	if _, err := s.db.Exec(`UPDATE media SET imdb_id=? WHERE id=?`, body.ImdbID, id); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]any{"ok": true, "imdb_id": body.ImdbID})
}

// --- helpers ---

func buildKeywords(show string) []string {
	var kw []string
	for _, w := range strings.Fields(show) {
		if len(w) > 3 {
			kw = append(kw, strings.ToLower(w))
		}
	}
	return kw
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	fmt.Printf("[error] %s\n", msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *server) handleHotReload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan bool)
	s.listenersMu.Lock()
	s.listeners[ch] = true
	s.listenersMu.Unlock()

	defer func() {
		s.listenersMu.Lock()
		delete(s.listeners, ch)
		s.listenersMu.Unlock()
	}()

	select {
	case <-ch:
		fmt.Fprintf(w, "data: reload\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	case <-r.Context().Done():
	}
}
