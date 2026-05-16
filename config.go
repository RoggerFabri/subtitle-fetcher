package main

import (
	"errors"
	"flag"
	"path/filepath"
)

type Config struct {
	Username  string
	Password  string
	APIKey    string
	Directory string
	Workers   int
}

func parseConfig() (Config, error) {
	var cfg Config
	flag.StringVar(&cfg.Username, "u", "", "OpenSubtitles username")
	flag.StringVar(&cfg.Password, "p", "", "OpenSubtitles password")
	flag.StringVar(&cfg.APIKey, "k", "", "OpenSubtitles API key")
	flag.StringVar(&cfg.Directory, "d", "", "Path to scan for video files")
	flag.IntVar(&cfg.Workers, "w", 5, "Number of parallel downloads (default: 5)")
	flag.Parse()

	if cfg.Username == "" || cfg.Password == "" || cfg.APIKey == "" || cfg.Directory == "" {
		return Config{}, errors.New("usage: subtitle-fetcher -u <username> -p <password> -k <api-key> -d <directory> [-w <workers>]")
	}

	abs, err := filepath.Abs(cfg.Directory)
	if err != nil {
		return Config{}, err
	}
	cfg.Directory = abs
	return cfg, nil
}
