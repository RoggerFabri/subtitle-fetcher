package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cl := newClient(cfg.APIKey)
	if err := cl.login(cfg.Username, cfg.Password); err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Logged in as %s\n\n", cfg.Username)
	defer cl.logout()

	videoFiles := collectVideoFiles(cfg.Directory)
	fmt.Printf("Found %d video file(s)\n", len(videoFiles))

	show := showNameFromFolder(cfg.Directory)
	var keywords []string
	for _, w := range strings.Fields(show) {
		if len(w) > 3 {
			keywords = append(keywords, strings.ToLower(w))
		}
	}
	fmt.Printf("Show: %q\n", show)

	parentIMDBID := discoverIMDBID(show)
	fmt.Printf("IMDB ID: %s\n\n", parentIMDBID)

	var (
		printMu    sync.Mutex
		statsMu    sync.Mutex
		existing   []string
		downloaded []string
		notFound   []string
		toFetch    []string
	)

	for _, video := range videoFiles {
		if hasSubtitle(video) {
			fmt.Printf("[skip] %s\n", filepath.Base(video))
			existing = append(existing, video)
		} else {
			toFetch = append(toFetch, video)
		}
	}

	type result struct {
		path string
		ok   bool
	}

	sem := make(chan struct{}, cfg.Workers)
	results := make(chan result, len(toFetch))

	var wg sync.WaitGroup
	for _, video := range toFetch {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			sem <- struct{}{}
			ok := fetchSubtitle(v, cl, show, keywords, parentIMDBID, &printMu)
			<-sem
			results <- result{path: v, ok: ok}
		}(video)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		statsMu.Lock()
		if r.ok {
			downloaded = append(downloaded, r.path)
		} else {
			notFound = append(notFound, r.path)
		}
		statsMu.Unlock()
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("  REPORT")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("\n  Downloaded (%d):\n", len(downloaded))
	for _, v := range downloaded {
		fmt.Printf("    + %s\n", filepath.Base(v))
	}
	fmt.Printf("\n  Already had subtitles (%d):\n", len(existing))
	for _, v := range existing {
		fmt.Printf("    = %s\n", filepath.Base(v))
	}
	fmt.Printf("\n  Not found (%d):\n", len(notFound))
	for _, v := range notFound {
		fmt.Printf("    x %s\n", filepath.Base(v))
	}
	fmt.Println()
}
