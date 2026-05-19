package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	// scan mode
	ScanRoot string

	// serve mode
	ServeRoot    string
	Port         int
	AutoScanInterval time.Duration

	// fetch mode
	Username  string
	Password  string
	APIKey    string
	Directory string
	Workers   int
}

func (c Config) scanMode() bool  { return c.ScanRoot != "" }
func (c Config) serveMode() bool { return c.ServeRoot != "" }

func parseConfig() (Config, error) {
	var cfg Config
	flag.StringVar(&cfg.ScanRoot, "scan", "", "Root folder to scan for subtitle coverage (e.g. Z:\\Shared\\Downloads)")
	flag.StringVar(&cfg.ServeRoot, "serve", "", "Root folder to serve the web UI for (e.g. Z:\\Shared\\Downloads)")
	
	defaultPort := 8080
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			defaultPort = p
		}
	}
	flag.IntVar(&cfg.Port, "port", defaultPort, "Port for the web server (used with --serve)")
	flag.DurationVar(&cfg.AutoScanInterval, "auto-scan", 0, "Auto-scan interval for NAS/SMB mounts where inotify does not work (e.g. 30m, 1h; 0 = disabled)")
	flag.StringVar(&cfg.Username, "u", "", "OpenSubtitles username")
	flag.StringVar(&cfg.Password, "p", "", "OpenSubtitles password")
	flag.StringVar(&cfg.APIKey, "k", "", "OpenSubtitles API key")
	flag.StringVar(&cfg.Directory, "d", "", "Path to fetch subtitles for")
	flag.IntVar(&cfg.Workers, "w", 5, "Number of parallel downloads (default: 5)")
	flag.Parse()

	if cfg.AutoScanInterval == 0 {
		if env := os.Getenv("AUTO_SCAN_INTERVAL"); env != "" {
			if d, err := time.ParseDuration(env); err == nil {
				cfg.AutoScanInterval = d
			}
		}
	}

	if cfg.ScanRoot != "" {
		abs, err := filepath.Abs(cfg.ScanRoot)
		if err != nil {
			return Config{}, err
		}
		cfg.ScanRoot = abs
		return cfg, nil
	}

	if cfg.ServeRoot != "" {
		abs, err := filepath.Abs(cfg.ServeRoot)
		if err != nil {
			return Config{}, err
		}
		cfg.ServeRoot = abs
		return cfg, nil
	}

	if cfg.Username == "" || cfg.Password == "" || cfg.APIKey == "" || cfg.Directory == "" {
		return Config{}, errors.New("usage:\n" +
			"  subtitle-fetcher --serve <root> [--port 8080]\n" +
			"  subtitle-fetcher --scan <root>\n" +
			"  subtitle-fetcher -u <user> -p <pass> -k <key> -d <dir> [-w <workers>]")
	}

	abs, err := filepath.Abs(cfg.Directory)
	if err != nil {
		return Config{}, err
	}
	cfg.Directory = abs
	return cfg, nil
}
