package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var seasonDirRe = regexp.MustCompile(`(?i)season\s*(\d+)`)

// --- types ---

type scanEntry struct {
	dir  string
	name string
	typ  string // "movie" | "series"
}

type fileResult struct {
	path         string
	season       *int
	episode      *int
	hasSubtitle  bool
	subtitleName string
}

type scanResult struct {
	entry   scanEntry
	files   []fileResult
	scanErr error
}

// --- orchestration ---

func runScan(root string) error {
	db, err := openDB(root)
	if err != nil {
		return err
	}
	defer db.Close()

	digits := func(total int) string { return strconv.Itoa(total) }
	_ = digits

	return runScanDB(context.Background(), db, root, func(done, total int, name string) {
		fmt.Printf("\r  [%*d/%d] %-55s", len(strconv.Itoa(total)), done, total, truncate(name, 55))
	}, func() { fmt.Println() }, true)
}

// runScanWithProgress is used by the web server; progress is reported via statusFn.
func runScanWithProgress(ctx context.Context, root string, statusFn func(string, int, int)) error {
	db, err := openDB(root)
	if err != nil {
		return err
	}
	defer db.Close()
	return runScanDB(ctx, db, root, func(done, total int, name string) {
		statusFn(fmt.Sprintf("[%d/%d] %s", done, total, name), done, total)
	}, func() {}, false)
}

func runScanDB(ctx context.Context, db *sql.DB, root string, progressFn func(done, total int, name string), doneFn func(), printReport bool) error {
	entries := collectEntries(
		filepath.Join(root, "Movies"),
		filepath.Join(root, "Series"),
	)
	total := len(entries)
	if total == 0 {
		fmt.Println("No entries found.")
		return nil
	}

	workers := runtime.NumCPU() * 2
	if workers < 4 {
		workers = 4
	}

	jobs := make(chan scanEntry, total)
	results := make(chan scanResult, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				results <- scanEntryFS(e)
			}
		}()
	}

	for _, e := range entries {
		jobs <- e
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	done := 0
	for r := range results {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		done++
		progressFn(done, total, r.entry.name)
		if r.scanErr != nil {
			continue
		}
		writeResultToDB(db, r)
	}
	doneFn()

	if printReport {
		return printScanReport(db)
	}
	return nil
}

func collectEntries(moviesDir, seriesDir string) []scanEntry {
	var entries []scanEntry
	for _, pair := range []struct {
		dir string
		typ string
	}{
		{moviesDir, "movie"},
		{seriesDir, "series"},
	} {
		dirs, err := os.ReadDir(pair.dir)
		if err != nil {
			fmt.Printf("[warn] cannot read %s: %v\n", pair.dir, err)
			continue
		}
		for _, d := range dirs {
			if d.IsDir() {
				entries = append(entries, scanEntry{
					dir:  filepath.Join(pair.dir, d.Name()),
					name: d.Name(),
					typ:  pair.typ,
				})
			}
		}
	}
	return entries
}

// scanEntryFS reads the filesystem only — no DB access.
func scanEntryFS(e scanEntry) scanResult {
	var files []fileResult
	var scanErr error

	switch e.typ {
	case "movie":
		files, scanErr = scanMovieFS(e.dir)
	case "series":
		files, scanErr = scanSeriesFS(e.dir)
	}
	return scanResult{entry: e, files: files, scanErr: scanErr}
}

func scanMovieFS(dir string) ([]fileResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []fileResult
	for _, e := range entries {
		if e.IsDir() || !videoExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		sp := subtitlePath(path)
		subName := ""
		if sp != "" {
			subName = filepath.Base(sp)
		}
		files = append(files, fileResult{path: path, hasSubtitle: sp != "", subtitleName: subName})
	}
	return files, nil
}

func scanSeriesFS(dir string) ([]fileResult, error) {
	seasons, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []fileResult
	for _, s := range seasons {
		if !s.IsDir() {
			continue
		}
		seasonNum := parseSeasonFromFolder(s.Name())
		seasonDir := filepath.Join(dir, s.Name())

		eps, err := os.ReadDir(seasonDir)
		if err != nil {
			continue
		}
		for _, fe := range eps {
			if fe.IsDir() || !videoExts[strings.ToLower(filepath.Ext(fe.Name()))] {
				continue
			}
			path := filepath.Join(seasonDir, fe.Name())
			stem := strings.TrimSuffix(fe.Name(), filepath.Ext(fe.Name()))
			_, ep, hasSE := parseSeasonEpisode(stem)

			sp := subtitlePath(path)
			subName := ""
			if sp != "" {
				subName = filepath.Base(sp)
			}
			fr := fileResult{path: path, hasSubtitle: sp != "", subtitleName: subName}
			if seasonNum >= 0 {
				sn := seasonNum
				fr.season = &sn
			}
			if hasSE {
				fr.episode = &ep
			}
			files = append(files, fr)
		}
	}
	return files, nil
}

func writeResultToDB(db *sql.DB, r scanResult) error {
	mediaID, err := upsertMedia(db, r.entry.dir, r.entry.name, r.entry.typ)
	if err != nil {
		return err
	}
	for _, f := range r.files {
		if err := upsertFile(db, mediaID, f.path, f.season, f.episode, f.hasSubtitle, f.subtitleName); err != nil {
			return err
		}
	}
	return nil
}

// parseSeasonFromFolder returns the season number for known patterns,
// 0 for special-season folders (Specials, OVA, …), or -1 if unrecognised.
func parseSeasonFromFolder(name string) int {
	if m := seasonDirRe.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "specials", "special", "ova", "extras", "extra", "bonus", "bonus content":
		return 0
	}
	return -1
}

// --- report ---

type fileRecord struct {
	path        string
	season      *int
	episode     *int
	hasSubtitle bool
}

type mediaRecord struct {
	name  string
	typ   string
	files []fileRecord
}

func loadAll(db *sql.DB) ([]mediaRecord, error) {
	rows, err := db.Query(`SELECT id, name, type FROM media ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []mediaRecord
	var ids []int64
	for rows.Next() {
		var r mediaRecord
		var id int64
		if err := rows.Scan(&id, &r.name, &r.typ); err != nil {
			return nil, err
		}
		records = append(records, r)
		ids = append(ids, id)
	}
	rows.Close()

	for i, id := range ids {
		frows, err := db.Query(`
			SELECT path, season, episode, has_subtitle
			FROM files WHERE media_id = ?
			ORDER BY season, episode`, id)
		if err != nil {
			return nil, err
		}
		for frows.Next() {
			var f fileRecord
			var season, episode sql.NullInt64
			var hasSub int
			if err := frows.Scan(&f.path, &season, &episode, &hasSub); err != nil {
				frows.Close()
				return nil, err
			}
			if season.Valid {
				s := int(season.Int64)
				f.season = &s
			}
			if episode.Valid {
				e := int(episode.Int64)
				f.episode = &e
			}
			f.hasSubtitle = hasSub == 1
			records[i].files = append(records[i].files, f)
		}
		frows.Close()
	}
	return records, nil
}

func coverageMark(with, total int) string {
	switch {
	case total == 0:
		return "[ ]"
	case with == total:
		return "[+]"
	case with == 0:
		return "[x]"
	default:
		return "[~]"
	}
}

func printScanReport(db *sql.DB) error {
	records, err := loadAll(db)
	if err != nil {
		return err
	}

	var movies, series []mediaRecord
	for _, r := range records {
		if r.typ == "movie" {
			movies = append(movies, r)
		} else {
			series = append(series, r)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("  SCAN REPORT")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("\n  Movies (%d)\n", len(movies))
	fmt.Println("  " + strings.Repeat("-", 56))
	for _, m := range movies {
		total := len(m.files)
		with := countWith(m.files)
		fmt.Printf("  %s  %-44s [%d/%d]\n", coverageMark(with, total), truncate(m.name, 44), with, total)
	}

	fmt.Printf("\n  Series (%d)\n", len(series))
	fmt.Println("  " + strings.Repeat("-", 56))
	for _, s := range series {
		total := len(s.files)
		with := countWith(s.files)
		fmt.Printf("\n  %s  %s\n", coverageMark(with, total), s.name)

		seasonFiles := groupBySeason(s.files)
		var seasons []int
		for k := range seasonFiles {
			seasons = append(seasons, k)
		}
		sort.Ints(seasons)

		for _, sn := range seasons {
			files := seasonFiles[sn]
			sw := countWith(files)
			fmt.Printf("       %s  Season %-2d   %d/%d episodes\n",
				coverageMark(sw, len(files)), sn, sw, len(files))
		}
	}
	fmt.Println()
	return nil
}

func countWith(files []fileRecord) int {
	n := 0
	for _, f := range files {
		if f.hasSubtitle {
			n++
		}
	}
	return n
}

func groupBySeason(files []fileRecord) map[int][]fileRecord {
	m := map[int][]fileRecord{}
	for _, f := range files {
		if f.season != nil {
			m[*f.season] = append(m[*f.season], f)
		}
	}
	return m
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
