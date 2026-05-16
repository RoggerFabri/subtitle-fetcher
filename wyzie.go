package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const wyzieBase = "https://sub.wyzie.io/search"

type wyzieProvider struct {
	apiKey string
	hc     *http.Client
}

func newWyzieProvider(apiKey string) *wyzieProvider {
	return &wyzieProvider{apiKey: apiKey, hc: &http.Client{Timeout: 30 * time.Second}}
}

func (p *wyzieProvider) Name() string { return "wyzie" }
func (p *wyzieProvider) Open() error  { return nil }
func (p *wyzieProvider) Close()       {}

func (p *wyzieProvider) FetchSubtitle(videoPath, show string, keywords []string, imdbID, mediaType string, printMu *sync.Mutex) bool {
	if imdbID == "" {
		return false
	}

	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	season, episode, hasSE := parseSeasonEpisode(stem)

	// For specials/OVA: no reliable S+E or season=0 — search without season/episode constraints.
	isSpecial := mediaType == "series" && (!hasSE || season == 0)

	var results []map[string]any
	var err error
	if isSpecial {
		results, err = p.search(imdbID, 0, 0, false)
	} else {
		if mediaType == "series" && !hasSE {
			return false
		}
		results, err = p.search(imdbID, season, episode, mediaType == "series")
	}

	lines := []string{}
	flush := func(ok bool) bool {
		printMu.Lock()
		fmt.Printf("[wyzie] %s\n  %d result(s)%s\n", filepath.Base(videoPath), len(results), strings.Join(lines, ""))
		printMu.Unlock()
		return ok
	}

	if err != nil {
		lines = append(lines, fmt.Sprintf("\n  error: %v", err))
		return flush(false)
	}
	if len(results) == 0 {
		lines = append(lines, "\n  No subtitles found.")
		return flush(false)
	}

	// Narrow to results that encode the correct episode number, then apply keyword matching.
	candidates := results
	if mediaType == "series" && hasSE && !isSpecial {
		var epMatched []map[string]any
		for _, r := range results {
			rel, _ := r["release"].(string)
			if containsEpisode(rel, season, episode) {
				epMatched = append(epMatched, r)
			}
		}
		if len(epMatched) > 0 {
			candidates = epMatched
		}
	}
	best := candidates[0]
	for _, r := range candidates {
		rel, _ := r["release"].(string)
		if matchesKeywords(rel, keywords) {
			best = r
			break
		}
	}

	rel, _ := best["release"].(string)
	lines = append(lines, fmt.Sprintf("\n  Selected: %s", rel))

	rawURL, _ := best["url"].(string)
	if rawURL == "" {
		lines = append(lines, "\n  No url in response.")
		return flush(false)
	}

	// Ensure SRT format on download URL
	dlURL := rawURL
	if !strings.Contains(dlURL, "format=") {
		sep := "?"
		if strings.Contains(dlURL, "?") {
			sep = "&"
		}
		dlURL += sep + "format=srt&encoding=utf-8"
	}
	lines = append(lines, fmt.Sprintf("\n  Downloading: %s", dlURL))

	resp, err := p.hc.Get(dlURL)
	if err != nil {
		lines = append(lines, fmt.Sprintf("\n  Download failed: %v", err))
		return flush(false)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		lines = append(lines, fmt.Sprintf("\n  Download failed: HTTP %d", resp.StatusCode))
		return flush(false)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		lines = append(lines, fmt.Sprintf("\n  Read failed: %v", err))
		return flush(false)
	}

	outPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".srt"
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		lines = append(lines, fmt.Sprintf("\n  Write failed: %v", err))
		return flush(false)
	}

	lines = append(lines, fmt.Sprintf("\n  Saved: %s", filepath.Base(outPath)))
	return flush(true)
}

func (p *wyzieProvider) search(imdbID string, season, episode int, hasSE bool) ([]map[string]any, error) {
	u, _ := url.Parse(wyzieBase)
	q := u.Query()
	q.Set("id", "tt"+imdbID)
	q.Set("language", "en")
	q.Set("format", "srt")
	q.Set("key", p.apiKey)
	if hasSE {
		q.Set("season", fmt.Sprintf("%d", season))
		q.Set("episode", fmt.Sprintf("%d", episode))
	}
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", u.String(), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search failed %d (url=%s): %s", resp.StatusCode, u.String(), body)
	}

	var results []map[string]any
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode failed (url=%s, body=%q): %w", u.String(), truncate(string(body), 200), err)
	}
	return results, nil
}
