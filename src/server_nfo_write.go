package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// tmdbClientFromSettings builds a TMDB client from the stored API key, or an
// error describing what to configure.
func (s *server) tmdbClientFromSettings() (*tmdbClient, error) {
	key := getSetting(s.db, settingTMDBApiKey)
	if key == "" {
		return nil, fmt.Errorf("no TMDB API key configured — open Settings and add your TMDB key")
	}
	return newTMDBClient(key), nil
}

// generateNFO fetches TMDB metadata for one media entry and writes a
// movie.nfo/tvshow.nfo (plus poster/fanart) into its folder, then records the
// derived fields back onto the media row. It returns skipped=true when the
// entry already has an NFO. tmdb is shared so bulk runs reuse one client.
func (s *server) generateNFO(id int64, tmdb *tmdbClient) (path string, skipped bool, err error) {
	var name, typ, imdbID, mediaPath, nfoPath string
	var year int
	if err := s.db.QueryRow(
		`SELECT name, type, imdb_id, path, nfo_path, year FROM media WHERE id=?`, id,
	).Scan(&name, &typ, &imdbID, &mediaPath, &nfoPath, &year); err != nil {
		return "", false, fmt.Errorf("media not found")
	}
	if nfoPath != "" {
		return nfoPath, true, nil // already has an NFO — nothing to backfill
	}

	show := showNameFromFolder(name)

	// Resolve an IMDB id if we don't have one, persisting it like the fetch
	// path does so subsequent operations reuse it.
	if imdbID == "" {
		if guess := discoverIMDBID(show, typ); guess != "" {
			imdbID = guess
			s.db.Exec(`UPDATE media SET imdb_id=? WHERE id=? AND (imdb_id='' OR imdb_id IS NULL)`, imdbID, id)
		}
	}

	kind, tmdbID, err := tmdb.resolve(imdbID, show, year, typ)
	if err != nil {
		return "", false, err
	}

	var n *nfoData
	var poster, backdrop string
	if kind == "tv" {
		t, err := tmdb.tvDetails(tmdbID)
		if err != nil {
			return "", false, err
		}
		n = nfoFromTV(t)
		poster, backdrop = t.PosterPath, t.BackdropPath
	} else {
		m, err := tmdb.movieDetails(tmdbID)
		if err != nil {
			return "", false, err
		}
		n = nfoFromMovie(m)
		poster, backdrop = m.PosterPath, m.BackdropPath
	}

	data, err := marshalNFO(n, kind)
	if err != nil {
		return "", false, err
	}

	// Filename by media type (findNFO convention): tvshow.nfo / movie.nfo.
	fileName := "movie.nfo"
	if typ == "series" {
		fileName = "tvshow.nfo"
	}
	outPath := filepath.Join(mediaPath, fileName)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return "", false, err
	}

	// Artwork is best-effort: a failed image download must not fail the NFO.
	if err := tmdb.downloadImage(poster, filepath.Join(mediaPath, "poster.jpg")); err != nil {
		fmt.Printf("[nfo] %s: poster download failed: %v\n", name, err)
	}
	if err := tmdb.downloadImage(backdrop, filepath.Join(mediaPath, "fanart.jpg")); err != nil {
		fmt.Printf("[nfo] %s: fanart download failed: %v\n", name, err)
	}

	// Persist derived fields (mirrors scanner.writeResultToDB) so has_nfo, year
	// and air_status reflect the new file without waiting for a rescan.
	s.db.Exec(`UPDATE media SET nfo_path=?, year=?, air_status=? WHERE id=?`, outPath, n.Year, n.Status, id)
	if imdbNumeric := strings.TrimPrefix(n.IMDBID, "tt"); imdbNumeric != "" {
		s.db.Exec(`UPDATE media SET imdb_id=? WHERE id=? AND (imdb_id='' OR imdb_id IS NULL)`, imdbNumeric, id)
	}

	fmt.Printf("[nfo] %s — wrote %s\n", name, outPath)
	return outPath, false, nil
}

func (s *server) handleBackfillNFO(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	tmdb, err := s.tmdbClientFromSettings()
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	path, skipped, err := s.generateNFO(id, tmdb)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"ok": true, "skipped": skipped, "path": path})
}

func (s *server) handleBackfillAllNFO(w http.ResponseWriter, r *http.Request) {
	tmdb, err := s.tmdbClientFromSettings()
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := s.db.Query(`SELECT id FROM media WHERE nfo_path='' OR nfo_path IS NULL`)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	var mu sync.Mutex
	var created, failed, skipped int
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.workers.Load())
	for _, id := range ids {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			_, sk, err := s.generateNFO(id, tmdb)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				failed++
			case sk:
				skipped++
			default:
				created++
			}
		}(id)
	}
	wg.Wait()

	fmt.Printf("[nfo] backfill done: %d created, %d skipped, %d failed\n", created, skipped, failed)
	jsonOK(w, map[string]any{"created": created, "skipped": skipped, "failed": failed})
}
