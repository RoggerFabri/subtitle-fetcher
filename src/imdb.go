package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IMDBSuggestion is one result from the IMDB suggestion API.
type IMDBSuggestion struct {
	ID    string `json:"id"`    // numeric, without "tt" prefix
	Title string `json:"title"`
	Year  int    `json:"year"`
	Type  string `json:"type"` // "feature", "tv series", "tv mini series", etc.
}

func fetchIMDBSuggestions(query string) ([]IMDBSuggestion, error) {
	q := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(query), " ", "_"))
	if q == "" {
		return nil, nil
	}
	first := string([]rune(q)[0])
	url := fmt.Sprintf("https://v3.sg.media-imdb.com/suggestion/%s/%s.json", first, q)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", appName)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw struct {
		D []struct {
			ID string `json:"id"`
			L  string `json:"l"`
			Y  int    `json:"y"`
			Q  string `json:"q"`
		} `json:"d"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]IMDBSuggestion, 0, len(raw.D))
	for _, item := range raw.D {
		if !strings.HasPrefix(item.ID, "tt") {
			continue // skip people, companies, etc.
		}
		out = append(out, IMDBSuggestion{
			ID:    strings.TrimPrefix(item.ID, "tt"),
			Title: item.L,
			Year:  item.Y,
			Type:  item.Q,
		})
	}
	return out, nil
}

// discoverIMDBID picks the best-matching IMDB ID for a show/movie name.
func discoverIMDBID(showName, mediaType string) string {
	suggestions, err := fetchIMDBSuggestions(showName)
	if err != nil || len(suggestions) == 0 {
		return ""
	}

	var keywords []string
	for _, w := range strings.Fields(showName) {
		if len(w) > 3 {
			keywords = append(keywords, strings.ToLower(w))
		}
	}

	best := ""
	bestScore := -1
	for _, item := range suggestions {
		if !imdbTypeMatches(mediaType, item.Type) {
			continue
		}
		title := strings.ToLower(item.Title)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(title, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = item.ID
		}
	}
	return best
}
