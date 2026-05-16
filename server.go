package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

	// Podnapisi keys
	settingPodEnabled = "podnapisi_enabled"
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
}

func newServer(db *sql.DB, root string, workers int) *server {
	s := &server{
		db:        db,
		root:      root,
		workers:   workers,
		listeners: make(map[chan bool]bool),
	}
	go s.watchWeb()
	return s
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// Serve static files directly from the "web" directory.
	// We use http.Dir to ensure the server reads from disk every time a file is requested.
	fs := http.FileServer(http.Dir("web"))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			// Disable caching so changes to JS/CSS are reflected immediately on reload
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
			fs.ServeHTTP(w, r)
		}
	}))

	mux.HandleFunc("GET /api/report", s.handleReport)
	mux.HandleFunc("GET /api/hot-reload", s.handleHotReload)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleSaveSettings)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/health/test", s.handleTestConnection)
	mux.HandleFunc("POST /api/health/provider-test", s.handleTestProvider)
	mux.HandleFunc("POST /api/scan", s.handleScan)
	mux.HandleFunc("GET /api/scan/status", s.handleScanStatus)
	mux.HandleFunc("POST /api/fetch/media/{id}", s.handleFetchMedia)
	mux.HandleFunc("POST /api/fetch/season/{id}/{season}", s.handleFetchSeason)
	mux.HandleFunc("POST /api/fetch/file/{id}", s.handleFetchFile)
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
	lastMod := time.Now()
	for {
		time.Sleep(1 * time.Second)
		changed := false
		_ = filepath.Walk("web", func(path string, info os.FileInfo, err error) error {
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
		if err := runScanWithProgress(s.root, func(status string, done, total int) { // Modified signature
			s.setScanStatus(status)
			s.scanCurrent.Store(int64(done)) // Update current
			s.scanTotal.Store(int64(total))  // Update total
			fmt.Printf("\r[scan] %s", status)
		}); err != nil {
			fmt.Printf("\n[scan] error: %v\n", err)
			s.setScanStatus("error: " + err.Error())
			return
		}
		fmt.Printf("\n[scan] done  elapsed=%s\n", time.Since(start).Round(time.Millisecond))
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
	ID          int64  `json:"id"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	Season      *int   `json:"season"`
	Episode     *int   `json:"episode"`
	HasSubtitle bool   `json:"has_subtitle"`
}

type apiMedia struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Type  string    `json:"type"`
	Files []apiFile `json:"files"`
}

func (s *server) handleReport(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, name, type FROM media ORDER BY type, name`)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	result := []apiMedia{}
	for rows.Next() {
		var m apiMedia
		if err := rows.Scan(&m.ID, &m.Name, &m.Type); err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		result = append(result, m)
	}
	rows.Close()

	for i := range result {
		frows, err := s.db.Query(`
			SELECT id, path, season, episode, has_subtitle
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
			if err := frows.Scan(&f.ID, &f.Path, &season, &episode, &hasSub); err != nil {
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
		settingProviderOrder: "opensubtitles,subdl,podnapisi",
		settingOSEnabled:     "1",
		settingSubDLEnabled:  "1",
		settingPodEnabled:    "1",
	}
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	defaults := providerDefaults()
	for k := range defaults {
		if getSetting(s.db, k) == "" {
			setSetting(s.db, k, defaults[k])
		}
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
		case "podnapisi":
			// no credentials
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
	for _, name := range []string{"opensubtitles", "subdl", "podnapisi"} {
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
				}
			}
		}
	}

	jsonOK(w, map[string]string{"status": "ok"})
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
		case "podnapisi":
			p["configured"] = true
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
	case "podnapisi":
		p := newPodnapisiProvider()
		res, err := p.search("test", 0, 0, false)
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{
			"ok":      true,
			"results": len(res),
		})
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
		case "podnapisi":
			out = append(out, newPodnapisiProvider())
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
	imdbID := discoverIMDBID(show, media.Type)

	pending := 0
	for _, f := range files {
		if !f.HasSubtitle {
			pending++
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
				s.db.Exec(`UPDATE files SET has_subtitle=1 WHERE id=?`, file.ID)
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
	if err := s.db.QueryRow(`SELECT id, name, type FROM media WHERE id=?`, id).Scan(&m.ID, &m.Name, &m.Type); err != nil {
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
	if err := s.db.QueryRow(`SELECT id, name, type FROM media WHERE id=?`, id).Scan(&m.ID, &m.Name, &m.Type); err != nil {
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
	s.db.QueryRow(`SELECT id, name, type FROM media WHERE id=?`, mediaID).Scan(&m.ID, &m.Name, &m.Type)

	jsonOK(w, s.fetchSubtitlesForFiles([]apiFile{f}, m))
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
