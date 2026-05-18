package main

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var yearSuffixRe = regexp.MustCompile(`^(.+?)\s*\((\d{4})\)\s*$`)

// parseNameAndYear splits "Movie Title (2014)" → ("Movie Title", 2014, true).
func parseNameAndYear(folderName string) (name string, year int, ok bool) {
	m := yearSuffixRe.FindStringSubmatch(folderName)
	if m == nil {
		return
	}
	y, err := strconv.Atoi(m[2])
	if err != nil {
		return
	}
	return strings.TrimSpace(m[1]), y, true
}

// normalizeTitle lowercases and strips punctuation for comparison.
func normalizeTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// autoIMDBStatus holds live progress for the current run.
type autoIMDBStatus struct {
	mu      sync.RWMutex
	Running bool   `json:"running"`
	Total   int    `json:"total"`
	Current int    `json:"current"`
	Matched int    `json:"matched"`
	Skipped int    `json:"skipped"`
	Label   string `json:"label"` // name currently being processed
}

func (s *autoIMDBStatus) snapshot() autoIMDBStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return autoIMDBStatus{
		Running: s.Running,
		Total:   s.Total,
		Current: s.Current,
		Matched: s.Matched,
		Skipped: s.Skipped,
		Label:   s.Label,
	}
}

var autoIMDB autoIMDBStatus

// autoPopulateIMDB iterates all media without an IMDB ID, parses name+year from
// the folder name, and sets the ID only when there is an exact title+year match
// in the IMDB suggestion API. Entries without a year suffix are skipped.
// The provided context can be cancelled to abort the run (e.g. on shutdown).
func autoPopulateIMDB(ctx context.Context, db *sql.DB) error {
	autoIMDB.mu.Lock()
	if autoIMDB.Running {
		autoIMDB.mu.Unlock()
		return fmt.Errorf("auto-populate already running")
	}
	autoIMDB.Running = true
	autoIMDB.Total = 0
	autoIMDB.Current = 0
	autoIMDB.Matched = 0
	autoIMDB.Skipped = 0
	autoIMDB.Label = ""
	autoIMDB.mu.Unlock()

	go func() {
		defer func() {
			autoIMDB.mu.Lock()
			autoIMDB.Running = false
			autoIMDB.Label = ""
			autoIMDB.mu.Unlock()
		}()

		rows, err := db.Query(`SELECT id, name, type FROM media WHERE imdb_id = '' OR imdb_id IS NULL`)
		if err != nil {
			return
		}
		type entry struct {
			id        int64
			name      string
			mediaType string
		}
		var entries []entry
		for rows.Next() {
			var e entry
			if rows.Scan(&e.id, &e.name, &e.mediaType) == nil {
				entries = append(entries, e)
			}
		}
		rows.Close()

		autoIMDB.mu.Lock()
		autoIMDB.Total = len(entries)
		autoIMDB.mu.Unlock()

		acceptsType := func(mediaType, q string) bool {
			q = strings.ToLower(q)
			if mediaType == "movie" {
				return q == "feature"
			}
			return q == "tv series" || q == "tv mini series"
		}

		for i, e := range entries {
			if ctx.Err() != nil {
				return
			}

			autoIMDB.mu.Lock()
			autoIMDB.Current = i + 1
			autoIMDB.Label = e.name
			autoIMDB.mu.Unlock()

			parsedName, parsedYear, ok := parseNameAndYear(e.name)
			if !ok {
				autoIMDB.mu.Lock()
				autoIMDB.Skipped++
				autoIMDB.mu.Unlock()
				continue
			}

			suggestions, err := fetchIMDBSuggestions(parsedName)
			if err != nil {
				time.Sleep(2 * time.Second)
				suggestions, err = fetchIMDBSuggestions(parsedName)
			}
			time.Sleep(300 * time.Millisecond) // be polite to the IMDB API
			if err != nil || len(suggestions) == 0 {
				autoIMDB.mu.Lock()
				autoIMDB.Skipped++
				autoIMDB.mu.Unlock()
				continue
			}

			normWant := normalizeTitle(parsedName)
			var matchID string
			for _, s := range suggestions {
				if !acceptsType(e.mediaType, s.Type) {
					continue
				}
				if s.Year != parsedYear {
					continue
				}
				if normalizeTitle(s.Title) == normWant {
					matchID = s.ID
					break
				}
			}

			if matchID == "" {
				autoIMDB.mu.Lock()
				autoIMDB.Skipped++
				autoIMDB.mu.Unlock()
				continue
			}

			if _, err := db.Exec(`UPDATE media SET imdb_id=? WHERE id=?`, matchID, e.id); err == nil {
				autoIMDB.mu.Lock()
				autoIMDB.Matched++
				autoIMDB.mu.Unlock()
				fmt.Printf("[autoimdb] %q → tt%s\n", e.name, matchID)
			}
		}
	}()

	return nil
}
