package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
)

// subtitleProvider is implemented by every subtitle source.
type subtitleProvider interface {
	Name() string
	Open() error // login / init — no-op for providers without auth
	Close()      // logout / cleanup
	FetchSubtitle(videoPath, show string, keywords []string, imdbID, mediaType string, printMu *sync.Mutex) bool
}

// fetchWithProviders tries each provider in order, stopping at first success.
func fetchWithProviders(videoPath, show string, keywords []string, imdbID, mediaType string, providers []subtitleProvider, printMu *sync.Mutex) bool {
	for _, p := range providers {
		if p.FetchSubtitle(videoPath, show, keywords, imdbID, mediaType, printMu) {
			return true
		}
	}
	return false
}

// extractSRTFromZip returns the content of the first .srt file found in a ZIP archive.
func extractSRTFromZip(data []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if strings.ToLower(filepath.Ext(f.Name)) == ".srt" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("no .srt file in ZIP")
}

// containsEpisode reports whether a release name encodes the given season+episode.
// Handles the common S01E01 and 1x01 patterns.
func containsEpisode(release string, season, episode int) bool {
	r := strings.ToLower(release)
	if strings.Contains(r, fmt.Sprintf("s%02de%02d", season, episode)) {
		return true
	}
	return strings.Contains(r, fmt.Sprintf("%dx%02d", season, episode))
}

// matchesKeywords reports whether text contains any of the given keywords (case-insensitive).
func matchesKeywords(text string, keywords []string) bool {
	t := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

// filterByShow applies keyword matching and optional parent-IMDB narrowing to raw search results.
func filterByShow(results []map[string]any, keywords []string, parentIMDBID string) []map[string]any {
	var out []map[string]any
	for _, s := range results {
		if matchesShow(s, keywords) {
			out = append(out, s)
		}
	}
	if parentIMDBID != "" {
		var byIMDB []map[string]any
		for _, s := range out {
			fd := featureDetails(s)
			if fd != nil && fmt.Sprintf("%v", fd["parent_imdb_id"]) == parentIMDBID {
				byIMDB = append(byIMDB, s)
			}
		}
		if len(byIMDB) > 0 {
			return byIMDB
		}
	}
	return out
}
