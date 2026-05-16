package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func discoverIMDBID(showName string) string {
	query := strings.ToLower(strings.ReplaceAll(showName, " ", "_"))
	if len(query) == 0 {
		return ""
	}
	first := string([]rune(query)[0])
	imdbURL := fmt.Sprintf("https://v3.sg.media-imdb.com/suggestion/%s/%s.json", first, query)

	req, _ := http.NewRequest("GET", imdbURL, nil)
	req.Header.Set("User-Agent", appName)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		D []struct {
			ID string `json:"id"`
			L  string `json:"l"`
			Q  string `json:"q"`
		} `json:"d"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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
	for _, item := range result.D {
		q := strings.ToLower(item.Q)
		if q != "tv series" && q != "tv mini series" {
			continue
		}
		title := strings.ToLower(item.L)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(title, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = strings.TrimPrefix(item.ID, "tt")
		}
	}
	return best
}
