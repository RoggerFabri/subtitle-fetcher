package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	mux.HandleFunc("GET /api/media/{id}/files", s.handleMediaFiles)
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
	mux.HandleFunc("POST /api/imdb/auto", s.handleAutoIMDB)
	mux.HandleFunc("GET /api/imdb/auto/status", s.handleAutoIMDBStatus)
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
