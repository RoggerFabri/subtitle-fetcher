package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nfoResponse is the payload returned by GET /api/media/{id}/nfo: the parsed
// metadata alongside the raw XML so the viewer can offer a formatted card and a
// raw-XML toggle, plus the art images found in the media folder.
type nfoResponse struct {
	Type   string    `json:"type"`              // movie | tvshow | episodedetails
	ImdbID string    `json:"imdb_id,omitempty"` // numeric, no "tt" prefix
	Path   string    `json:"path"`
	Data   *nfoData  `json:"data,omitempty"`
	Art    []artItem `json:"art,omitempty"`
	Raw    string    `json:"raw"`
}

func (s *server) handleMediaNFO(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	var nfoPath, mediaPath string
	if err := s.db.QueryRow(`SELECT nfo_path, path FROM media WHERE id=?`, id).Scan(&nfoPath, &mediaPath); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if nfoPath == "" {
		jsonError(w, "no NFO for this media", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(nfoPath)
	if err != nil {
		jsonError(w, "read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := nfoResponse{Path: nfoPath, Raw: string(data), Art: discoverArt(mediaPath)}
	if n, err := parseNFOBytes(data); err == nil {
		resp.Data = n
		resp.Type = n.Root
		resp.ImdbID = n.imdbID()
	}
	jsonOK(w, resp)
}

// handleMediaArt serves an image file from a media folder. Only bare filenames
// (no path separators or "..") with an image extension are accepted, and the
// resolved path must stay inside the media folder — so this can't be used to
// read arbitrary files off disk.
func (s *server) handleMediaArt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" || name != filepath.Base(name) || !imageExts[strings.ToLower(filepath.Ext(name))] {
		jsonError(w, "bad art name", http.StatusBadRequest)
		return
	}
	var mediaPath string
	if err := s.db.QueryRow(`SELECT path FROM media WHERE id=?`, id).Scan(&mediaPath); err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	full := filepath.Join(mediaPath, name)
	// Defence in depth: confirm the cleaned path is still a direct child of the
	// media folder before touching the filesystem.
	if rel, err := filepath.Rel(mediaPath, full); err != nil || rel != name {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		jsonError(w, "art not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, full)
}
