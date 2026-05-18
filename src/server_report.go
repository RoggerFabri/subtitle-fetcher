package main

import (
	"database/sql"
	"net/http"
	"strconv"
)

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
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	ImdbID         string    `json:"imdb_id,omitempty"`
	TotalCount     int       `json:"total_count"`
	SubtitlesCount int       `json:"subtitles_count"`
	Files          []apiFile `json:"files,omitempty"`
}

func (s *server) handleReport(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT m.id, m.name, m.type, m.imdb_id,
		       COUNT(f.id),
		       SUM(CASE WHEN f.has_subtitle=1 THEN 1 ELSE 0 END)
		FROM media m
		LEFT JOIN files f ON f.media_id = m.id
		GROUP BY m.id
		ORDER BY m.type, m.name`)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	result := []apiMedia{}
	for rows.Next() {
		var m apiMedia
		var subCount sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID, &m.TotalCount, &subCount); err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		m.SubtitlesCount = int(subCount.Int64)
		result = append(result, m)
	}

	jsonOK(w, result)
}

func (s *server) handleMediaFiles(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := s.db.Query(`
		SELECT id, path, season, episode, has_subtitle, subtitle_name
		FROM files WHERE media_id = ?
		ORDER BY season, episode`, id)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	files := []apiFile{}
	for rows.Next() {
		var f apiFile
		var season, episode sql.NullInt64
		var hasSub int
		if err := rows.Scan(&f.ID, &f.Path, &season, &episode, &hasSub, &f.SubtitleName); err != nil {
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
		files = append(files, f)
	}

	jsonOK(w, files)
}
