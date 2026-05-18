package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type exportDoc struct {
	Version  int               `json:"version"`
	Settings map[string]string `json:"settings"`
	ImdbIDs  map[string]string `json:"imdb_ids"` // media name → imdb_id
}

func (s *server) handleExport(w http.ResponseWriter, r *http.Request) {
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

	settingsApplied := 0
	for k, v := range doc.Settings {
		if err := setSetting(s.db, k, v); err == nil {
			settingsApplied++
			if k == settingWorkers {
				if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 50 {
					s.workers.Store(int32(n))
				}
			}
		}
	}

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
