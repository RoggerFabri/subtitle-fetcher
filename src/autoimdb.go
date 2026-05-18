package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

var yearSuffixRe = regexp.MustCompile(`^(.+?)\s*\((\d{4})\)\s*$`)

// parseNameAndYear splits "Movie Title (2014)" → ("Movie Title", 2014, true).
// Returns ok=false when no year suffix is found.
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

// AutoIMDBResult holds the outcome of an auto-populate run.
type AutoIMDBResult struct {
	Total   int `json:"total"`
	Matched int `json:"matched"`
	Skipped int `json:"skipped"` // no year in name or no exact match found
}

var autoIMDBRunning atomic.Bool

// autoPopulateIMDB iterates all media without an IMDB ID, parses name+year from
// the folder name, and sets the ID only when there is an exact title+year match
// in the IMDB suggestion API. Entries without a year suffix are skipped.
func autoPopulateIMDB(db *sql.DB) (AutoIMDBResult, error) {
	if !autoIMDBRunning.CompareAndSwap(false, true) {
		return AutoIMDBResult{}, fmt.Errorf("auto-populate already running")
	}
	defer autoIMDBRunning.Store(false)

	rows, err := db.Query(`SELECT id, name, type FROM media WHERE imdb_id = '' OR imdb_id IS NULL`)
	if err != nil {
		return AutoIMDBResult{}, err
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

	result := AutoIMDBResult{Total: len(entries)}

	acceptsType := func(mediaType, q string) bool {
		q = strings.ToLower(q)
		if mediaType == "movie" {
			return q == "feature"
		}
		return q == "tv series" || q == "tv mini series"
	}

	for _, e := range entries {
		parsedName, parsedYear, ok := parseNameAndYear(e.name)
		if !ok {
			result.Skipped++
			continue
		}

		suggestions, err := fetchIMDBSuggestions(parsedName)
		time.Sleep(300 * time.Millisecond) // be polite to the IMDB API
		if err != nil || len(suggestions) == 0 {
			result.Skipped++
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
			result.Skipped++
			continue
		}

		if _, err := db.Exec(`UPDATE media SET imdb_id=? WHERE id=?`, matchID, e.id); err == nil {
			result.Matched++
			fmt.Printf("[autoimdb] %q → tt%s\n", e.name, matchID)
		}
	}

	return result, nil
}
