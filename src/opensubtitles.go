package main

import (
	"bytes"
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

const (
	apiBase = "https://api.opensubtitles.com/api/v1"
	appName = "SubtitleFetcher v1.0"
)

type client struct {
	hc     *http.Client
	apiKey string
	token  string
}

func newClient(apiKey string) *client {
	return &client{
		hc:     &http.Client{Timeout: 30 * time.Second},
		apiKey: apiKey,
	}
}

func jsonBody(v any) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func (c *client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", appName)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.hc.Do(req)
}

// retryable returns true for status codes that warrant a single retry after a pause.
func retryable(code int) bool {
	return code == 429 || code == 503 || code == 502 || code == 504
}

// withRetry calls doReq, and if the response status is retryable, waits and tries once more.
func withRetry(label string, doReq func() (*http.Response, error)) (*http.Response, error) {
	resp, err := doReq()
	if err != nil {
		return nil, err
	}
	if retryable(resp.StatusCode) {
		resp.Body.Close()
		fmt.Printf("  %s — server returned %d, waiting 10s and retrying…\n", label, resp.StatusCode)
		time.Sleep(10 * time.Second)
		return doReq()
	}
	return resp, nil
}

func (c *client) login(username, password string) error {
	payload := map[string]string{"username": username, "password": password}

	doReq := func() (*http.Response, error) {
		req, _ := http.NewRequest("POST", apiBase+"/login", jsonBody(payload))
		return c.do(req)
	}

	resp, err := withRetry("login", doReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("login failed %d: %s", resp.StatusCode, data)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	c.token = result.Token
	return nil
}

func (c *client) logout() {
	req, _ := http.NewRequest("DELETE", apiBase+"/logout", nil)
	resp, err := c.do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func (c *client) search(params map[string]string) ([]map[string]any, error) {
	time.Sleep(1 * time.Second)

	u, _ := url.Parse(apiBase + "/subtitles")
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	fmt.Printf("[opensubtitles] api call: GET %s\n", u.String())

	doReq := func() (*http.Response, error) {
		req, _ := http.NewRequest("GET", u.String(), nil)
		return c.do(req)
	}

	resp, err := withRetry("search", doReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("search failed %d: %s", resp.StatusCode, data)
	}

	var result struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (c *client) requestDownload(fileID int) (string, error) {
	payload := map[string]int{"file_id": fileID}

	doReq := func() (*http.Response, error) {
		req, _ := http.NewRequest("POST", apiBase+"/download", jsonBody(payload))
		return c.do(req)
	}

	resp, err := withRetry("download", doReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return "", fmt.Errorf("download request failed %d: %s", resp.StatusCode, data)
	}

	var result struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Link, nil
}

type userInfo struct {
	UserID             int    `json:"user_id"`
	Level              string `json:"level"`
	AllowedDownloads   int    `json:"allowed_downloads"`
	RemainingDownloads int    `json:"remaining_downloads"`
	DownloadsCount     int    `json:"downloads_count"`
	VIP                bool   `json:"vip"`
}

func (c *client) getUser() (*userInfo, error) {
	req, _ := http.NewRequest("GET", apiBase+"/infos/user", nil)
	resp, err := withRetry("user", func() (*http.Response, error) { return c.do(req) })
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("user info failed %d: %s", resp.StatusCode, data)
	}
	var result struct {
		Data userInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// --- Response field accessors (encode OpenSubtitles JSON shape) ---

func attrs(sub map[string]any) map[string]any {
	a, _ := sub["attributes"].(map[string]any)
	return a
}

func featureDetails(sub map[string]any) map[string]any {
	a := attrs(sub)
	if a == nil {
		return nil
	}
	fd, _ := a["feature_details"].(map[string]any)
	return fd
}

func attrString(sub map[string]any, key string) string {
	a := attrs(sub)
	if a == nil {
		return ""
	}
	v, _ := a[key].(string)
	return v
}

func downloadCount(sub map[string]any) int {
	a := attrs(sub)
	if a == nil {
		return 0
	}
	switch v := a["download_count"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func fileID(sub map[string]any) (int, bool) {
	a := attrs(sub)
	if a == nil {
		return 0, false
	}
	files, _ := a["files"].([]any)
	if len(files) == 0 {
		return 0, false
	}
	file, _ := files[0].(map[string]any)
	if file == nil {
		return 0, false
	}
	switch v := file["file_id"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

func matchesShow(sub map[string]any, keywords []string) bool {
	fd := featureDetails(sub)
	title := ""
	if fd != nil {
		title, _ = fd["title"].(string)
	}
	text := strings.ToLower(attrString(sub, "release") + " " + title)
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// --- openSubtitlesProvider implements subtitleProvider ---

type openSubtitlesProvider struct {
	username, password, apiKey string
	cl                         *client
}

func newOpenSubtitlesProvider(username, password, apiKey string) *openSubtitlesProvider {
	return &openSubtitlesProvider{username: username, password: password, apiKey: apiKey}
}

func (p *openSubtitlesProvider) Name() string { return "opensubtitles" }

func (p *openSubtitlesProvider) Open() error {
	p.cl = newClient(p.apiKey)
	return p.cl.login(p.username, p.password)
}

func (p *openSubtitlesProvider) Close() {
	if p.cl != nil {
		p.cl.logout()
	}
}

func (p *openSubtitlesProvider) SearchSubtitles(videoPath, show string, keywords []string, imdbID, mediaType string) ([]SubtitleCandidate, error) {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	season, episode, hasSE := parseSeasonEpisode(stem)
	epTitle := episodeTitleFromStem(stem)

	var all []map[string]any
	seen := map[int]bool{}

	collect := func(res []map[string]any) {
		for _, r := range res {
			fid, ok := fileID(r)
			if !ok || seen[fid] {
				continue
			}
			seen[fid] = true
			all = append(all, r)
		}
	}

	if mediaType == "movie" {
		if imdbID != "" {
			res, _ := p.cl.search(map[string]string{"imdb_id": imdbID, "languages": "en"})
			collect(filterByShow(res, keywords, ""))
		}
		res, _ := p.cl.search(map[string]string{"query": show, "languages": "en"})
		collect(filterByShow(res, keywords, ""))
	} else if hasSE {
		if imdbID != "" {
			res, _ := p.cl.search(map[string]string{"parent_imdb_id": imdbID, "season_number": strconv.Itoa(season), "episode_number": strconv.Itoa(episode), "languages": "en"})
			collect(res)
		}
		if epTitle != "" {
			res1, _ := p.cl.search(map[string]string{"query": show + " " + epTitle, "languages": "en", "season_number": strconv.Itoa(season), "episode_number": strconv.Itoa(episode)})
			collect(filterByShow(res1, keywords, imdbID))
		}
		res2, _ := p.cl.search(map[string]string{"query": show, "languages": "en", "season_number": strconv.Itoa(season), "episode_number": strconv.Itoa(episode)})
		collect(filterByShow(res2, keywords, imdbID))
	} else {
		// specials / no S+E: broad search by show name
		if imdbID != "" {
			res, _ := p.cl.search(map[string]string{"parent_imdb_id": imdbID, "languages": "en"})
			collect(res)
		}
		res, _ := p.cl.search(map[string]string{"query": show, "languages": "en"})
		collect(filterByShow(res, keywords, imdbID))
	}

	var out []SubtitleCandidate
	for _, r := range all {
		fid, ok := fileID(r)
		if !ok {
			continue
		}
		out = append(out, SubtitleCandidate{
			Provider:  "opensubtitles",
			Name:      attrString(r, "release"),
			Downloads: downloadCount(r),
			Format:    "srt",
			Token:     fmt.Sprintf("opensubtitles:%d", fid),
		})
	}
	return out, nil
}

func (p *openSubtitlesProvider) DownloadCandidate(handle, videoPath string) (string, error) {
	fid, err := strconv.Atoi(handle)
	if err != nil {
		return "", fmt.Errorf("invalid file id: %w", err)
	}
	dlLink, err := p.cl.requestDownload(fid)
	if err != nil {
		return "", err
	}
	resp, err := http.Get(dlLink) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return "", fmt.Errorf("subtitle download failed %d: %s", resp.StatusCode, snippet)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	outPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".srt"
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return "", err
	}
	return filepath.Base(outPath), nil
}

func (p *openSubtitlesProvider) FetchSubtitle(videoPath, show string, keywords []string, imdbID, mediaType string, printMu *sync.Mutex) bool {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	season, episode, hasSE := parseSeasonEpisode(stem)
	epTitle := episodeTitleFromStem(stem)
	var lines []string

	type strategy struct {
		label string
		run   func() ([]map[string]any, error)
	}

	var strategies []strategy

	if mediaType == "movie" {
		strategies = []strategy{
			{
				label: "imdb_id",
				run: func() ([]map[string]any, error) {
					if imdbID == "" {
						return nil, nil
					}
					res, err := p.cl.search(map[string]string{
						"imdb_id":   imdbID,
						"languages": "en",
					})
					if err != nil {
						return nil, err
					}
					return filterByShow(res, keywords, ""), nil
				},
			},
			{
				label: "title",
				run: func() ([]map[string]any, error) {
					res, err := p.cl.search(map[string]string{
						"query":     show,
						"languages": "en",
					})
					if err != nil {
						return nil, err
					}
					return filterByShow(res, keywords, ""), nil
				},
			},
		}
	} else {
		strategies = []strategy{
			{
				label: "imdb+S+E",
				run: func() ([]map[string]any, error) {
					if imdbID == "" || !hasSE {
						return nil, nil
					}
					return p.cl.search(map[string]string{
						"parent_imdb_id": imdbID,
						"season_number":  strconv.Itoa(season),
						"episode_number": strconv.Itoa(episode),
						"languages":      "en",
					})
				},
			},
			{
				label: "show+ep",
				run: func() ([]map[string]any, error) {
					if !hasSE || epTitle == "" {
						return nil, nil
					}
					res, err := p.cl.search(map[string]string{
						"query":          show + " " + epTitle,
						"languages":      "en",
						"season_number":  strconv.Itoa(season),
						"episode_number": strconv.Itoa(episode),
					})
					if err != nil {
						return nil, err
					}
					return filterByShow(res, keywords, imdbID), nil
				},
			},
			{
				label: "show+S+E",
				run: func() ([]map[string]any, error) {
					if !hasSE {
						return nil, nil
					}
					res, err := p.cl.search(map[string]string{
						"query":          show,
						"languages":      "en",
						"season_number":  strconv.Itoa(season),
						"episode_number": strconv.Itoa(episode),
					})
					if err != nil {
						return nil, err
					}
					return filterByShow(res, keywords, imdbID), nil
				},
			},
			{
				label: "imdb(specials)",
				run: func() ([]map[string]any, error) {
					if hasSE || imdbID == "" {
						return nil, nil
					}
					res, err := p.cl.search(map[string]string{
						"parent_imdb_id": imdbID,
						"languages":      "en",
					})
					if err != nil {
						return nil, err
					}
					return filterByShow(res, keywords, imdbID), nil
				},
			},
			{
				label: "show(specials)",
				run: func() ([]map[string]any, error) {
					if hasSE {
						return nil, nil
					}
					res, err := p.cl.search(map[string]string{
						"query":          show,
						"languages":      "en",
					})
					if err != nil {
						return nil, err
					}
					return filterByShow(res, keywords, imdbID), nil
				},
			},
		}
	}

	var subtitles []map[string]any
	for _, strat := range strategies {
		res, err := strat.run()
		if err != nil {
			lines = append(lines, fmt.Sprintf("  [%s] error: %v", strat.label, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("  [%s] %d result(s)", strat.label, len(res)))
		if len(res) > 0 {
			subtitles = res
			break
		}
	}

	flush := func(ok bool) bool {
		printMu.Lock()
		fmt.Printf("[opensubtitles] %s\n%s\n", filepath.Base(videoPath), strings.Join(lines, "\n"))
		printMu.Unlock()
		return ok
	}

	if len(subtitles) == 0 {
		lines = append(lines, "  No subtitles found.")
		return flush(false)
	}

	best := subtitles[0]
	for _, s := range subtitles[1:] {
		if downloadCount(s) > downloadCount(best) {
			best = s
		}
	}
	lines = append(lines, fmt.Sprintf("  Selected: %s | downloads: %d", attrString(best, "release"), downloadCount(best)))

	fid, ok := fileID(best)
	if !ok {
		lines = append(lines, "  Could not get file ID.")
		return flush(false)
	}

	dlLink, err := p.cl.requestDownload(fid)
	if err != nil {
		lines = append(lines, fmt.Sprintf("  Download request failed: %v", err))
		return flush(false)
	}

	dlResp, err := http.Get(dlLink) //nolint:noctx
	if err != nil {
		lines = append(lines, fmt.Sprintf("  Failed to fetch subtitle content: %v", err))
		return flush(false)
	}
	defer dlResp.Body.Close()

	data, err := io.ReadAll(dlResp.Body)
	if err != nil {
		lines = append(lines, fmt.Sprintf("  Failed to read subtitle content: %v", err))
		return flush(false)
	}

	outPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".srt"
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		lines = append(lines, fmt.Sprintf("  Failed to write subtitle: %v", err))
		return flush(false)
	}

	lines = append(lines, fmt.Sprintf("  Saved: %s", filepath.Base(outPath)))
	return flush(true)
}
