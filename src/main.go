package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if cfg.scanMode() {
		if err := runScan(cfg.ScanRoot); err != nil {
			fmt.Fprintf(os.Stderr, "Scan failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if cfg.serveMode() {
		db, err := openDB(cfg.ServeRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DB error: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()
		srv := newServer(db, cfg.ServeRoot, cfg.Workers, cfg.AutoScanInterval)
		addr := fmt.Sprintf(":%d", cfg.Port)
		fmt.Printf("Serving at http://localhost%s\n", addr)
		fmt.Printf("Root:     %s\n", cfg.ServeRoot)
		exe, _ := os.Executable()
		dbDir := filepath.Join(filepath.Dir(exe), "data")
		if env := os.Getenv("DB_PATH"); env != "" {
			dbDir = env
		}
		fmt.Printf("Database: %s\n", filepath.Join(dbDir, "subtitles.db"))
		fmt.Printf("Workers:  %d\n", srv.workers.Load())
		srv.pollerMu.Lock()
		if srv.poller != nil {
			fmt.Printf("Poll:     every %s\n", srv.poller.interval)
		} else {
			fmt.Printf("Poll:     disabled\n")
		}
		srv.pollerMu.Unlock()
		fmt.Println()
		httpServer := &http.Server{Addr: addr, Handler: srv.routes()}

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-quit
			fmt.Println("\nShutting down...")
			srv.Shutdown()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			httpServer.Shutdown(ctx)
		}()

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

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

	parentIMDBID := discoverIMDBID(show, "series")
	fmt.Printf("IMDB ID: %s\n\n", parentIMDBID)

	prov := newOpenSubtitlesProvider(cfg.Username, cfg.Password, cfg.APIKey)
	if err := prov.Open(); err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}
	defer prov.Close()

	providers := []subtitleProvider{prov}

	fmt.Printf("Logged in as %s\n\n", cfg.Username)

	toFetch := []string{}
	existing := []string{}
	for _, video := range videoFiles {
		if hasSubtitle(video) {
			fmt.Printf("[skip] %s\n", filepath.Base(video))
			existing = append(existing, video)
		} else {
			toFetch = append(toFetch, video)
		}
	}

	pending := len(toFetch)
	fmt.Printf("%d file(s) to fetch\n\n", pending)

	var statsMu sync.Mutex
	downloaded := []string{}
	notFound := []string{}
	sem := make(chan struct{}, cfg.Workers)
	var wg sync.WaitGroup

	for _, video := range toFetch {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var printMu sync.Mutex
			ok := fetchWithProviders(v, show, keywords, parentIMDBID, "series", providers, &printMu)

			statsMu.Lock()
			if ok {
				downloaded = append(downloaded, v)
			} else {
				notFound = append(notFound, v)
			}
			statsMu.Unlock()
		}(video)
	}

	wg.Wait()

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
