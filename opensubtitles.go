package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

func (c *client) login(username, password string) error {
	payload := map[string]string{"username": username, "password": password}

	doReq := func() (*http.Response, error) {
		req, _ := http.NewRequest("POST", apiBase+"/login", jsonBody(payload))
		return c.do(req)
	}

	resp, err := doReq()
	if err != nil {
		return err
	}
	if resp.StatusCode == 429 {
		resp.Body.Close()
		fmt.Println("Login rate limited — waiting 10s and retrying...")
		time.Sleep(10 * time.Second)
		resp, err = doReq()
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
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

	doReq := func() (*http.Response, error) {
		req, _ := http.NewRequest("GET", u.String(), nil)
		return c.do(req)
	}

	resp, err := doReq()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 429 {
		resp.Body.Close()
		fmt.Println("  Rate limited — waiting 10s and retrying...")
		time.Sleep(10 * time.Second)
		resp, err = doReq()
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

	resp, err := doReq()
	if err != nil {
		return "", err
	}
	if resp.StatusCode == 429 {
		resp.Body.Close()
		fmt.Println("  Download rate limited — waiting 10s and retrying...")
		time.Sleep(10 * time.Second)
		resp, err = doReq()
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
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
