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
		isSpecial := !hasSE || season == 0
		if isSpecial {
			// Specials/OVA: no reliable S+E — search without season/episode constraints.
			if imdbID != "" {
				label = "imdb(specials)"
				results, err = p.search(map[string]string{
					"api_key":   p.apiKey,
					"languages": "EN",
					"type":      "tv",
					"imdb_id":   "tt" + imdbID,
				})
			}
			if len(results) == 0 {
				label = "show+ep(specials)"
				q := show
				if epTitle != "" {
					q += " " + epTitle
				}
				results, err = p.search(map[string]string{
					"api_key":   p.apiKey,
					"languages": "EN",
					"type":      "tv",
					"film_name": q,
				})
			}
		} else {
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
			if len(results) == 0 && epTitle != "" {
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

	// Narrow to results that encode the correct episode number, then apply keyword matching.
	isSpecial := !hasSE || season == 0
	candidates := results
	if hasSE && !isSpecial {
		var epMatched []map[string]any
		for _, r := range results {
			rel, _ := r["release_name"].(string)
			if rel == "" {
				rel, _ = r["name"].(string)
			}
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
	lines = append(lines, fmt.Sprintf("\n  ZIP bytes: %d (Content-Length: %d, Encoding: %q)",
		len(data), resp.ContentLength, resp.Header.Get("Content-Encoding")))

	srt, err := extractSRTFromZip(data)
	if err != nil {
		lines = append(lines, fmt.Sprintf("\n  SRT extract failed: %v", err))
		return flush(false)
	}
	lines = append(lines, fmt.Sprintf("\n  SRT bytes: %d", len(srt)))

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

	maskURL := u.String()
	if strings.Contains(maskURL, "api_key=") {
		maskURL = strings.Replace(maskURL, p.apiKey, "***", 1)
	}
	fmt.Printf("[subdl] api call: GET %s\n", maskURL)

	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		resp.Body.Close()
		fmt.Printf("[subdl] rate limited (%d), retrying in 3s…\n", resp.StatusCode)
		time.Sleep(3 * time.Second)
		req2, _ := http.NewRequest("GET", u.String(), nil)
		req2.Header.Set("Accept", "application/json")
		resp, err = p.hc.Do(req2)
		if err != nil {
			return nil, err
		}
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

func (p *subdlProvider) SearchSubtitles(videoPath, show string, keywords []string, imdbID, mediaType string) ([]SubtitleCandidate, error) {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	season, episode, hasSE := parseSeasonEpisode(stem)
	epTitle := episodeTitleFromStem(stem)
	isSpecial := mediaType != "movie" && (!hasSE || season == 0)

	seen := map[string]bool{}
	var all []map[string]any

	add := func(results []map[string]any) {
		for _, r := range results {
			u, _ := r["url"].(string)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			all = append(all, r)
		}
	}

	if mediaType == "movie" {
		if imdbID != "" {
			res, _ := p.search(map[string]string{"api_key": p.apiKey, "languages": "EN", "type": "movie", "imdb_id": "tt" + imdbID})
			add(res)
		}
		res, _ := p.search(map[string]string{"api_key": p.apiKey, "languages": "EN", "type": "movie", "film_name": show})
		add(res)
	} else if isSpecial {
		if imdbID != "" {
			res, _ := p.search(map[string]string{"api_key": p.apiKey, "languages": "EN", "type": "tv", "imdb_id": "tt" + imdbID})
			add(res)
		}
		q := show
		if epTitle != "" {
			q += " " + epTitle
		}
		res, _ := p.search(map[string]string{"api_key": p.apiKey, "languages": "EN", "type": "tv", "film_name": q})
		add(res)
	} else {
		if imdbID != "" {
			res, _ := p.search(map[string]string{"api_key": p.apiKey, "languages": "EN", "type": "tv", "imdb_id": "tt" + imdbID, "season_number": strconv.Itoa(season), "episode_number": strconv.Itoa(episode)})
			add(res)
		}
		if epTitle != "" {
			res1, _ := p.search(map[string]string{"api_key": p.apiKey, "languages": "EN", "type": "tv", "film_name": show + " " + epTitle, "season_number": strconv.Itoa(season), "episode_number": strconv.Itoa(episode)})
			add(res1)
		}
		res2, _ := p.search(map[string]string{"api_key": p.apiKey, "languages": "EN", "type": "tv", "film_name": show, "season_number": strconv.Itoa(season), "episode_number": strconv.Itoa(episode)})
		add(res2)
	}

	var out []SubtitleCandidate
	for _, r := range all {
		name, _ := r["release_name"].(string)
		if name == "" {
			name, _ = r["name"].(string)
		}
		u, _ := r["url"].(string)
		lang, _ := r["language"].(string)
		if lang == "" {
			lang = "en"
		}
		out = append(out, SubtitleCandidate{Provider: "subdl", Language: lang, Name: name, Format: "srt", Token: "subdl:" + u})
	}
	return out, nil
}

func (p *subdlProvider) DownloadCandidate(handle, videoPath string) (string, error) {
	dlURL := "https://dl.subdl.com" + handle
	resp, err := p.hc.Get(dlURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	srt, err := extractSRTFromZip(data)
	if err != nil {
		return "", err
	}
	outPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".srt"
	if err := os.WriteFile(outPath, srt, 0o644); err != nil {
		return "", err
	}
	return filepath.Base(outPath), nil
}
