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

const podnapisiBase = "https://www.podnapisi.net"

// --- podnapisiProvider implements subtitleProvider via the Podnapisi API. ---

type podnapisiProvider struct {
	hc *http.Client
}

func newPodnapisiProvider() *podnapisiProvider {
	return &podnapisiProvider{
		hc: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *podnapisiProvider) Name() string { return "podnapisi" }

func (p *podnapisiProvider) Open() error  { return nil }
func (p *podnapisiProvider) Close()       {}

func (p *podnapisiProvider) FetchSubtitle(videoPath, show string, keywords []string, imdbID, mediaType string, printMu *sync.Mutex) bool {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	season, episode, hasSE := parseSeasonEpisode(stem)

	if mediaType == "series" && !hasSE {
		return false
	}

	var results []map[string]any
	var err error
	var label string

	if mediaType == "movie" {
		label = "title"
		results, err = p.search(show, 0, 0, false)
	} else {
		label = "show+S+E"
		results, err = p.search(show, season, episode, true)
	}

	lines := []string{}
	flush := func(ok bool) bool {
		printMu.Lock()
		fmt.Printf("[podnapisi] %s\n  [%s] %d result(s)%s\n",
			filepath.Base(videoPath), label, len(results), strings.Join(lines, ""))
		printMu.Unlock()
		return ok
	}

	if err != nil {
		lines = append(lines, fmt.Sprintf("\n  [podnapisi] error: %v", err))
		return flush(false)
	}

	if len(results) == 0 {
		lines = append(lines, "\n  No subtitles found.")
		return flush(false)
	}

	// Pick first keyword-matching result
	best := results[0]
	for _, r := range results {
		rel, _ := r["release"].(string)
		if matchesKeywords(rel, keywords) {
			best = r
			break
		}
	}

	rel, _ := best["release"].(string)
	if rel == "" {
		rel, _ = best["url"].(string)
	}
	lines = append(lines, fmt.Sprintf("\n  Selected: %s", rel))

	pidVal, ok := best["pid"]
	if !ok {
		lines = append(lines, "\n  No PID in response.")
		return flush(false)
	}
	var pid string
	switch v := pidVal.(type) {
	case string:
		pid = v
	case float64:
		pid = strconv.Itoa(int(v))
	case int:
		pid = strconv.Itoa(v)
	default:
		pid = fmt.Sprintf("%v", pidVal)
	}
	dlURL := fmt.Sprintf("%s/subtitles/%s/download/", podnapisiBase, pid)

	req, _ := http.NewRequest("GET", dlURL, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
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

// search queries Podnapisi and returns the subtitle result list.
func (p *podnapisiProvider) search(show string, season, episode int, hasSE bool) ([]map[string]any, error) {
	u, _ := url.Parse(podnapisiBase + "/subtitles/search/")
	q := u.Query()
	q.Set("sK", show)
	q.Set("sL", "en")
	if hasSE {
		q.Set("sS", strconv.Itoa(season))
		q.Set("sE", strconv.Itoa(episode))
	}
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return nil, fmt.Errorf("podnapisi: expected JSON response, got %s", contentType)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed %d: %s", resp.StatusCode, data)
	}

	var result struct {
		Subtitles []map[string]any `json:"subtitles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Subtitles, nil
}
