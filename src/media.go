package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	videoExts    = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".m4v": true, ".wmv": true}
	subtitleExts = []string{".srt", ".ass", ".ssa", ".sub"}

	seRegex        = regexp.MustCompile(`[Ss](\d+)[Ee](\d+)`)
	afterSERegex   = regexp.MustCompile(`[Ss]\d+[Ee]\d+(.*)`)
	seasonFolderRe = regexp.MustCompile(`(?i)^season\s*\d+$`)
	nonWordRe      = regexp.MustCompile(`[^\w\s]`)
	parensRe       = regexp.MustCompile(`\(.*?\)|\[.*?\]`)
	multiSpaceRe   = regexp.MustCompile(`\s+`)
)

func hasSubtitle(videoPath string) bool {
	ext := filepath.Ext(videoPath)
	base := videoPath[:len(videoPath)-len(ext)]
	for _, sext := range subtitleExts {
		if _, err := os.Stat(base + sext); err == nil {
			return true
		}
	}
	return false
}

func subtitlePath(videoPath string) string {
	ext := filepath.Ext(videoPath)
	base := videoPath[:len(videoPath)-len(ext)]
	for _, sext := range subtitleExts {
		p := base + sext
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// dirEntryNameSet maps each non-directory entry's lowercased name to its
// original name. Building this once from a ReadDir result lets the scanner
// detect subtitle sidecars with a map lookup instead of one os.Stat per
// candidate extension — a large win on network-mounted libraries where every
// stat is a round-trip.
func dirEntryNameSet(entries []os.DirEntry) map[string]string {
	names := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names[strings.ToLower(e.Name())] = e.Name()
	}
	return names
}

// subtitleNameFor returns the sidecar subtitle base name for videoName, looked
// up case-insensitively against names (from dirEntryNameSet), or "" if none.
func subtitleNameFor(videoName string, names map[string]string) string {
	ext := filepath.Ext(videoName)
	stem := strings.ToLower(videoName[:len(videoName)-len(ext)])
	for _, sext := range subtitleExts {
		if actual, ok := names[stem+sext]; ok {
			return actual
		}
	}
	return ""
}

func parseSeasonEpisode(name string) (season, episode int, ok bool) {
	m := seRegex.FindStringSubmatch(name)
	if m == nil {
		return
	}
	season, _ = strconv.Atoi(m[1])
	episode, _ = strconv.Atoi(m[2])
	ok = true
	return
}

func showNameFromFolder(dir string) string {
	folder := filepath.Base(dir)
	if seasonFolderRe.MatchString(folder) {
		folder = filepath.Base(filepath.Dir(dir))
	}
	name := nonWordRe.ReplaceAllString(folder, " ")
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(name, " "))
}

func episodeTitleFromStem(stem string) string {
	m := afterSERegex.FindStringSubmatch(stem)
	if m == nil {
		return ""
	}
	rest := parensRe.ReplaceAllString(m[1], " ")
	rest = nonWordRe.ReplaceAllString(rest, " ")
	words := strings.Fields(rest)
	if len(words) > 0 && len(words[0]) <= 2 {
		words = words[1:]
	}
	return strings.Join(words, " ")
}

func collectVideoFiles(dir string) []string {
	var files []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if videoExts[strings.ToLower(filepath.Ext(path))] {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}
