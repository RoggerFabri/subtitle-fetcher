package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"
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
	Year           int       `json:"year,omitempty"`
	AirStatus      string    `json:"air_status,omitempty"`
	HasNFO         bool      `json:"has_nfo"`
	TotalCount     int       `json:"total_count"`
	SubtitlesCount int       `json:"subtitles_count"`
	IsNew          bool      `json:"is_new,omitempty"`       // media added since library_seen_at
	NewEpisodes    int       `json:"new_episodes,omitempty"` // files added since library_seen_at
	LastAdded      string    `json:"last_added,omitempty"`   // latest of media/file added_at; drives Recently Added order
	Files          []apiFile `json:"files,omitempty"`
}

func (s *server) handleReport(w http.ResponseWriter, r *http.Request) {
	// Timestamps are fixed-format RFC3339 UTC (Zulu), so lexical > matches
	// chronological >. The added_at != '' guard keeps pre-existing rows (which
	// have an empty added_at from the schema migration) from ever counting as new.
	seen := getSetting(s.db, "library_seen_at")
	rows, err := s.db.Query(`
		SELECT m.id, m.name, m.type, m.imdb_id, m.year, m.air_status,
		       CASE WHEN m.nfo_path != '' THEN 1 ELSE 0 END AS has_nfo,
		       COUNT(f.id),
		       SUM(CASE WHEN f.has_subtitle=1 THEN 1 ELSE 0 END),
		       CASE WHEN m.added_at != '' AND m.added_at > ? THEN 1 ELSE 0 END AS is_new,
		       SUM(CASE WHEN f.added_at != '' AND f.added_at > ? THEN 1 ELSE 0 END) AS new_eps,
		       CASE WHEN m.added_at > IFNULL(MAX(f.added_at), '') THEN m.added_at ELSE IFNULL(MAX(f.added_at), '') END AS last_added
		FROM media m
		LEFT JOIN files f ON f.media_id = m.id
		GROUP BY m.id
		ORDER BY m.type, m.name`, seen, seen)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	result := []apiMedia{}
	for rows.Next() {
		var m apiMedia
		var subCount, newEps sql.NullInt64
		var hasNFO, isNew int
		var lastAdded sql.NullString
		if err := rows.Scan(&m.ID, &m.Name, &m.Type, &m.ImdbID, &m.Year, &m.AirStatus, &hasNFO, &m.TotalCount, &subCount, &isNew, &newEps, &lastAdded); err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		m.HasNFO = hasNFO == 1
		m.SubtitlesCount = int(subCount.Int64)
		m.IsNew = isNew == 1
		m.NewEpisodes = int(newEps.Int64)
		m.LastAdded = lastAdded.String
		result = append(result, m)
	}

	jsonOK(w, result)
}

// handleMarkSeen advances the "seen everything up to here" pointer to now, so a
// subsequent /api/report reports nothing as new. Backs the "Mark all seen" button.
func (s *server) handleMarkSeen(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	if err := setSetting(s.db, "library_seen_at", now); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]any{"library_seen_at": now})
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
