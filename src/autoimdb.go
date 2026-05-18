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

// imdbTypeMatches reports whether an IMDB suggestion type is compatible with
// the local media type ("movie" or "series").
func imdbTypeMatches(mediaType, imdbType string) bool {
	q := strings.ToLower(imdbType)
	if mediaType == "movie" {
		return q == "feature" || q == "tv movie" || q == "video"
	}
	return q == "tv series" || q == "tv mini series" || q == "tv mini-series"
}

// autoIMDBStatus holds live progress for the current run.
type autoIMDBStatus struct {
	mu      sync.RWMutex
	Running bool
	Total   int
	Current int
	Matched int
	Skipped int
	Label   string
}

// autoIMDBSnapshot is a mutex-free copy safe to pass to encoding/json.
type autoIMDBSnapshot struct {
	Running bool   `json:"running"`
	Total   int    `json:"total"`
	Current int    `json:"current"`
	Matched int    `json:"matched"`
	Skipped int    `json:"skipped"`
	Label   string `json:"label"`
}

func (s *autoIMDBStatus) snapshot() autoIMDBSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return autoIMDBSnapshot{
		Running: s.Running,
		Total:   s.Total,
		Current: s.Current,
		Matched: s.Matched,
		Skipped: s.Skipped,
		Label:   s.Label,
	}
}

var autoIMDB autoIMDBStatus

// autoPopulateIMDB iterates all media without an IMDB ID and sets the IMDB ID
// when there is an unambiguous exact match in the IMDB suggestion API:
//   - Movies: exact name + year match required (folder must have "(YYYY)" suffix).
//   - Series: exact name match against "TV Series" or "TV Mini-Series" entries;
//     year is used as a tie-breaker when present but not required.
//
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

		acceptsType := imdbTypeMatches

		for i, e := range entries {
			if ctx.Err() != nil {
				return
			}

			autoIMDB.mu.Lock()
			autoIMDB.Current = i + 1
			autoIMDB.Label = e.name
			autoIMDB.mu.Unlock()

			skip := func(reason string) {
				fmt.Printf("[autoimdb] skip %q — %s\n", e.name, reason)
				autoIMDB.mu.Lock()
				autoIMDB.Skipped++
				autoIMDB.mu.Unlock()
			}

			parsedName, parsedYear, hasYear := parseNameAndYear(e.name)
			if !hasYear {
				// Movies require a year suffix to match unambiguously.
				if e.mediaType != "series" {
					skip("movie has no year suffix")
					continue
				}
				// For series, use the full folder name as-is.
				parsedName = e.name
			}

			suggestions, err := fetchIMDBSuggestions(parsedName)
			if err != nil {
				time.Sleep(2 * time.Second)
				suggestions, err = fetchIMDBSuggestions(parsedName)
			}
			time.Sleep(300 * time.Millisecond) // be polite to the IMDB API
			if err != nil {
				skip(fmt.Sprintf("API error: %v", err))
				continue
			}
			if len(suggestions) == 0 {
				skip("no suggestions from API")
				continue
			}

			normWant := normalizeTitle(parsedName)
			var matchID string
			var candidates []IMDBSuggestion
			for _, s := range suggestions {
				if !acceptsType(e.mediaType, s.Type) {
					continue
				}
				if normalizeTitle(s.Title) != normWant {
					continue
				}
				candidates = append(candidates, s)
			}

			if len(candidates) == 1 {
				// Single name match — accept regardless of year.
				matchID = candidates[0].ID
			} else if len(candidates) > 1 && hasYear {
				// Multiple name matches — use year to disambiguate.
				for _, c := range candidates {
					if c.Year == parsedYear {
						matchID = c.ID
						break
					}
				}
			}

			if matchID == "" {
				if len(candidates) == 0 {
					skip("no title match in suggestions")
				} else {
					skip(fmt.Sprintf("ambiguous: %d candidates with same name, no year match", len(candidates)))
				}
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
