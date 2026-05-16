package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// filterByShow applies keyword matching and optional parent-IMDB narrowing to raw search results.
func filterByShow(results []map[string]any, keywords []string, parentIMDBID string) []map[string]any {
	var out []map[string]any
	for _, s := range results {
		if matchesShow(s, keywords) {
			out = append(out, s)
		}
	}
	if parentIMDBID != "" {
		var byIMDB []map[string]any
		for _, s := range out {
			fd := featureDetails(s)
			if fd != nil && fmt.Sprintf("%v", fd["parent_imdb_id"]) == parentIMDBID {
				byIMDB = append(byIMDB, s)
			}
		}
		if len(byIMDB) > 0 {
			return byIMDB
		}
	}
	return out
}

// fetchSubtitle attempts three search strategies in order, picks the subtitle with the highest
// download count, and writes it as a .srt sidecar next to videoPath.
func fetchSubtitle(videoPath string, cl *client, show string, keywords []string, parentIMDBID string, printMu *sync.Mutex) bool {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	season, episode, hasSE := parseSeasonEpisode(stem)
	epTitle := episodeTitleFromStem(stem)
	var lines []string

	type strategy struct {
		label string
		run   func() ([]map[string]any, error)
	}

	strategies := []strategy{
		{
			label: "imdb+S+E",
			run: func() ([]map[string]any, error) {
				if parentIMDBID == "" || !hasSE {
					return nil, nil
				}
				return cl.search(map[string]string{
					"parent_imdb_id": parentIMDBID,
					"season_number":  strconv.Itoa(season),
					"episode_number": strconv.Itoa(episode),
					"languages":      "en",
				})
			},
		},
		{
			label: "show+ep",
			run: func() ([]map[string]any, error) {
				if !hasSE {
					return nil, nil
				}
				res, err := cl.search(map[string]string{
					"query":          show + " " + epTitle,
					"languages":      "en",
					"season_number":  strconv.Itoa(season),
					"episode_number": strconv.Itoa(episode),
				})
				if err != nil {
					return nil, err
				}
				return filterByShow(res, keywords, parentIMDBID), nil
			},
		},
		{
			label: "show+S+E",
			run: func() ([]map[string]any, error) {
				if !hasSE {
					return nil, nil
				}
				res, err := cl.search(map[string]string{
					"query":          show,
					"languages":      "en",
					"season_number":  strconv.Itoa(season),
					"episode_number": strconv.Itoa(episode),
				})
				if err != nil {
					return nil, err
				}
				return filterByShow(res, keywords, parentIMDBID), nil
			},
		},
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
		fmt.Printf("[fetch] %s\n%s\n", filepath.Base(videoPath), strings.Join(lines, "\n"))
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

	dlLink, err := cl.requestDownload(fid)
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
