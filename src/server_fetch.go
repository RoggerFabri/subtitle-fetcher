package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
)

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

	providers, err := s.loadProviders()
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
	sem := make(chan struct{}, s.workers.Load())

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

func (s *server) handleSubtitlePreview(w http.ResponseWriter, r *http.Request) {
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
		jsonError(w, "no subtitle on disk", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(sp)
	if err != nil {
		jsonError(w, "read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
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
