package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

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

func (s *server) handleAutoIMDB(w http.ResponseWriter, r *http.Request) {
	if err := autoPopulateIMDB(s.ctx, s.db); err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}
	jsonOK(w, map[string]string{"status": "started"})
}

func (s *server) handleAutoIMDBStatus(w http.ResponseWriter, r *http.Request) {
	snap := autoIMDB.snapshot()
	jsonOK(w, snap)
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
