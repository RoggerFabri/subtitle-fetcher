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
	sig     string // directory signature captured this scan (empty when skipped)
	skipped bool   // true when the folder was unchanged and not re-read
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

	return runScanDB(context.Background(), db, root, 0, func(done, total int, name string) {
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
	return runScanDB(ctx, db, root, 0, func(done, total int, name string) {
		statusFn(fmt.Sprintf("[%d/%d] %s", done, total, name), done, total)
	}, func() {}, false)
}

// runScanWithProgressDB is like runScanWithProgress but reuses an existing DB
// connection and accepts a explicit worker count (0 = use default NumCPU*2).
func runScanWithProgressDB(ctx context.Context, db *sql.DB, root string, scanWorkers int, statusFn func(string, int, int)) error {
	return runScanDB(ctx, db, root, scanWorkers, func(done, total int, name string) {
		statusFn(fmt.Sprintf("[%d/%d] %s", done, total, name), done, total)
	}, func() {}, false)
}

func runScanDB(ctx context.Context, db *sql.DB, root string, scanWorkers int, progressFn func(done, total int, name string), doneFn func(), printReport bool) error {
	entries := collectEntries(
		filepath.Join(root, "Movies"),
		filepath.Join(root, "Series"),
	)
	total := len(entries)
	if total == 0 {
		fmt.Println("No entries found.")
		return nil
	}

	// Scanning is network-I/O bound: workers spend almost all their time
	// blocked on ReadDir/stat round-trips over SMB/NFS, so we oversubscribe well
	// past the core count to overlap that latency. This floor is independent of
	// the download-oriented worker setting (which is far lower by default).
	floor := runtime.NumCPU() * 2
	if floor < 16 {
		floor = 16
	}
	workers := scanWorkers
	if workers < floor {
		workers = floor
	}

	priorSigs, priorFilesByMedia := loadPriorScanState(db)

	jobs := make(chan scanEntry, total)
	results := make(chan scanResult, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				results <- scanEntryFS(e, priorSigs)
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

	seenMedia := make(map[string]bool, total)
	seenFiles := make(map[string]bool)

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
		seenMedia[r.entry.dir] = true
		if r.skipped {
			// Folder unchanged since last scan — keep its rows and mark its
			// known files as seen so pruneStale doesn't delete them.
			for _, p := range priorFilesByMedia[r.entry.dir] {
				seenFiles[p] = true
			}
			continue
		}
		for _, f := range r.files {
			seenFiles[f.path] = true
		}
		writeResultToDB(db, r)
	}
	doneFn()

	if removedMedia, removedFiles, err := pruneStale(db, seenMedia, seenFiles); err != nil {
		fmt.Printf("[warn] prune failed: %v\n", err)
	} else if removedMedia > 0 || removedFiles > 0 {
		fmt.Printf("[scan] pruned %d media, %d files no longer on disk\n", removedMedia, removedFiles)
	}

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

// scanEntryFS reads the filesystem only — no DB access. When the folder's
// current directory signature matches priorSigs (i.e. nothing was added or
// removed since the last scan), it returns skipped=true without reading the
// (potentially large, network-bound) folder contents.
func scanEntryFS(e scanEntry, priorSigs map[string]string) scanResult {
	prior := priorSigs[e.dir]
	var files []fileResult
	var sig string
	var skipped bool
	var scanErr error

	switch e.typ {
	case "movie":
		files, sig, skipped, scanErr = scanMovieFS(e.dir, prior)
	case "series":
		files, sig, skipped, scanErr = scanSeriesFS(e.dir, prior)
	}
	return scanResult{entry: e, files: files, sig: sig, skipped: skipped, scanErr: scanErr}
}

// dirSig formats a directory's mtime into a comparable signature token.
func dirSig(info os.FileInfo) string {
	return strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

// scanMovieFS reads a movie folder. The signature is the folder's own mtime,
// which changes whenever a video or subtitle file is added or removed.
func scanMovieFS(dir, priorSig string) ([]fileResult, string, bool, error) {
	di, err := os.Stat(dir)
	if err != nil {
		return nil, "", false, err
	}
	sig := dirSig(di)
	if priorSig != "" && priorSig == sig {
		return nil, sig, true, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", false, err
	}
	names := dirEntryNameSet(entries)
	var files []fileResult
	for _, e := range entries {
		if e.IsDir() || !videoExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		subName := subtitleNameFor(e.Name(), names)
		files = append(files, fileResult{
			path:         filepath.Join(dir, e.Name()),
			hasSubtitle:  subName != "",
			subtitleName: subName,
		})
	}
	return files, sig, false, nil
}

// scanSeriesFS reads a series folder. A series' files live one level deeper, in
// season folders, so the show-root mtime alone is not enough — adding an
// episode changes the season folder's mtime, not the show root's. The signature
// is therefore composed from every season folder's name and mtime, which also
// captures whole seasons being added, removed, or renamed.
func scanSeriesFS(dir, priorSig string) ([]fileResult, string, bool, error) {
	seasons, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", false, err
	}

	type seasonDir struct{ name, path string }
	var sdirs []seasonDir
	var sigParts []string
	for _, s := range seasons {
		if !s.IsDir() {
			continue
		}
		mt := ""
		if info, err := s.Info(); err == nil {
			mt = dirSig(info)
		}
		sigParts = append(sigParts, s.Name()+"="+mt)
		sdirs = append(sdirs, seasonDir{name: s.Name(), path: filepath.Join(dir, s.Name())})
	}
	sort.Strings(sigParts)
	sig := strings.Join(sigParts, ";")
	if priorSig != "" && priorSig == sig {
		return nil, sig, true, nil
	}

	var files []fileResult
	for _, sd := range sdirs {
		seasonNum := parseSeasonFromFolder(sd.name)

		eps, err := os.ReadDir(sd.path)
		if err != nil {
			continue
		}
		names := dirEntryNameSet(eps)
		for _, fe := range eps {
			if fe.IsDir() || !videoExts[strings.ToLower(filepath.Ext(fe.Name()))] {
				continue
			}
			path := filepath.Join(sd.path, fe.Name())
			stem := strings.TrimSuffix(fe.Name(), filepath.Ext(fe.Name()))
			_, ep, hasSE := parseSeasonEpisode(stem)

			subName := subtitleNameFor(fe.Name(), names)
			fr := fileResult{path: path, hasSubtitle: subName != "", subtitleName: subName}
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
	return files, sig, false, nil
}

// loadPriorScanState reads, in one pass each, the per-media directory
// signatures and the file paths known from the previous scan. The signatures
// drive the incremental skip; the file map lets the result loop mark a skipped
// folder's files as still-present so they survive pruning.
func loadPriorScanState(db *sql.DB) (sigs map[string]string, filesByMedia map[string][]string) {
	sigs = make(map[string]string)
	filesByMedia = make(map[string][]string)

	if rows, err := db.Query(`SELECT path, scan_sig FROM media`); err == nil {
		for rows.Next() {
			var path, sig string
			if rows.Scan(&path, &sig) == nil {
				sigs[path] = sig
			}
		}
		rows.Close()
	}

	if rows, err := db.Query(`SELECT m.path, f.path FROM media m JOIN files f ON f.media_id = m.id`); err == nil {
		for rows.Next() {
			var mPath, fPath string
			if rows.Scan(&mPath, &fPath) == nil {
				filesByMedia[mPath] = append(filesByMedia[mPath], fPath)
			}
		}
		rows.Close()
	}
	return sigs, filesByMedia
}

func writeResultToDB(db *sql.DB, r scanResult) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	mediaID, err := upsertMedia(tx, r.entry.dir, r.entry.name, r.entry.typ)
	if err != nil {
		tx.Rollback()
		return err
	}
	// Persist the directory signature so the next scan can skip this folder if
	// nothing changes on disk.
	if _, err := tx.Exec(`UPDATE media SET scan_sig = ? WHERE id = ?`, r.sig, mediaID); err != nil {
		tx.Rollback()
		return err
	}
	for _, f := range r.files {
		if err := upsertFile(tx, mediaID, f.path, f.season, f.episode, f.hasSubtitle, f.subtitleName); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
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
