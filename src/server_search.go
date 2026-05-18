package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

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

	providers, err := s.loadProviders()
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
		imdbID = ""         // Ignore IMDB ID if doing a custom text search
		searchFilePath = "" // Clear path to prevent providers from inferring S/E and forcing constraints
	} else if imdbID == "" {
		imdbID = discoverIMDBID(show, m.Type)
		if imdbID != "" {
			s.db.Exec(`UPDATE media SET imdb_id=? WHERE id=?`, imdbID, m.ID)
		}
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

	providers, err := s.loadProviders()
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
