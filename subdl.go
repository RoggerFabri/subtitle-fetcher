package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const subdlAPIBase = "https://api.subdl.com/api/v1/subtitles"
const subdlDLBase = "https://dl.subdl.com/subtitle"

// --- subdlProvider implements subtitleProvider via the SubDL API. ---

type subdlProvider struct {
	apiKey string
	hc     *http.Client
}

func newSubDLProvider(apiKey string) *subdlProvider {
	return &subdlProvider{
		apiKey: apiKey,
		hc:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *subdlProvider) Name() string { return "subdl" }

func (p *subdlProvider) Open() error  { return nil }
func (p *subdlProvider) Close()       {}

func (p *subdlProvider) FetchSubtitle(videoPath, show string, keywords []string, imdbID, mediaType string, printMu *sync.Mutex) bool {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	season, episode, hasSE := parseSeasonEpisode(stem)
	epTitle := episodeTitleFromStem(stem)

	var results []map[string]any
	var err error
	var label string

	if mediaType == "movie" {
		label = "imdb_id"
		if imdbID != "" {
			results, err = p.search(map[string]string{
				"api_key":   p.apiKey,
				"languages": "EN",
				"type":      "movie",
				"imdb_id":   "tt" + imdbID,
			})
		}
		if err != nil || len(results) == 0 {
			label = "title"
			results, err = p.search(map[string]string{
				"api_key":   p.apiKey,
				"languages": "EN",
				"type":      "movie",
				"film_name": show,
			})
		}
	} else {
		if !hasSE {
			return false
		}
		if imdbID != "" {
			label = "imdb+S+E"
			results, err = p.search(map[string]string{
				"api_key":        p.apiKey,
				"languages":      "EN",
				"type":           "tv",
				"imdb_id":        "tt" + imdbID,
				"season_number":  strconv.Itoa(season),
				"episode_number": strconv.Itoa(episode),
			})
		}
		if len(results) == 0 {
			label = "show+ep"
			q := show + " " + epTitle
			results, err = p.search(map[string]string{
				"api_key":        p.apiKey,
				"languages":      "EN",
				"type":           "tv",
				"film_name":      q,
				"season_number":  strconv.Itoa(season),
				"episode_number": strconv.Itoa(episode),
			})
		}
		if len(results) == 0 {
			label = "show+S+E"
			results, err = p.search(map[string]string{
				"api_key":        p.apiKey,
				"languages":      "EN",
				"type":           "tv",
				"film_name":      show,
				"season_number":  strconv.Itoa(season),
				"episode_number": strconv.Itoa(episode),
			})
		}
	}

	lines := []string{}
	flush := func(ok bool) bool {
		printMu.Lock()
		fmt.Printf("[subdl] %s\n  [%s] %d result(s)%s\n",
			filepath.Base(videoPath), label, len(results), strings.Join(lines, ""))
		printMu.Unlock()
		return ok
	}

	if err != nil {
		lines = append(lines, fmt.Sprintf("\n  [subdl] error: %v", err))
		return flush(false)
	}

	if len(results) == 0 {
		lines = append(lines, "\n  No subtitles found.")
		return flush(false)
	}

	// Pick first keyword-matching result
	best := results[0]
	for _, r := range results {
		rel, _ := r["release_name"].(string)
		if rel == "" {
			rel, _ = r["name"].(string)
		}
		if matchesKeywords(rel, keywords) {
			best = r
			break
		}
	}

	rel, _ := best["release_name"].(string)
	if rel == "" {
		rel, _ = best["name"].(string)
	}
	lines = append(lines, fmt.Sprintf("\n  Selected: %s", rel))

	urlVal, ok := best["url"].(string)
	if !ok {
		var ks []string
		for k := range best { ks = append(ks, k) }
		lines = append(lines, fmt.Sprintf("\n  No URL in response (keys: %v)", ks))
		return flush(false)
	}
	lines = append(lines, fmt.Sprintf("\n  url field: %q", urlVal))

	// SubDL url field is already a full path like "/subtitle/abc.zip"
	dlURL := "https://dl.subdl.com" + urlVal
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

	srt, err := extractSRTFromZip(data)
	if err != nil {
		lines = append(lines, fmt.Sprintf("\n  SRT extract failed: %v", err))
		return flush(false)
	}

	outPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".srt"
	if err := os.WriteFile(outPath, srt, 0o644); err != nil {
		lines = append(lines, fmt.Sprintf("\n  Write failed: %v", err))
		return flush(false)
	}

	lines = append(lines, fmt.Sprintf("\n  Saved: %s", filepath.Base(outPath)))
	return flush(true)
}

func (p *subdlProvider) search(params map[string]string) ([]map[string]any, error) {
	u, _ := url.Parse(subdlAPIBase)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed %d: %s", resp.StatusCode, data)
	}

	var result struct {
		Status  bool             `json:"status"`
		Subtitles []map[string]any `json:"subtitles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.Status {
		return nil, fmt.Errorf("api status=false")
	}
	return result.Subtitles, nil
}
