# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Building and running

```
make build
./subtitle-fetcher -u <username> -p <password> -k <api-key> -d <directory> [-w <workers>]
```

### Makefile targets

| Target | Description |
|--------|-------------|
| `make build` | Compile to `subtitle-fetcher` / `subtitle-fetcher.exe` |
| `make clean` | Remove compiled binaries |
| `make fmt` | Format all Go source files with `gofmt` |
| `make vet` | Run `go vet` |
| `make test` | Run `go test ./...` |

- `-w` defaults to 5 parallel download workers.
- The program logs in, scans the directory recursively for video files, skips files that already have a subtitle sidecar, downloads missing ones, then logs out.

## Architecture

The program is split across several Go source files in `package main`:

- `main.go` — entry point, orchestrates the fan-out and prints the final report
- `config.go` — CLI flag parsing
- `media.go` — video file discovery and subtitle sidecar detection
- `opensubtitles.go` — OpenSubtitles API client (login, search, download, logout)
- `imdb.go` — IMDB ID lookup via the IMDB suggestion API
- `fetch.go` — `fetchSubtitle()` with three-strategy fallback

**Execution flow (top-level, imperative):**
1. Parse CLI args and build the API client with the API key + bearer token from login.
2. Derive the show name from the target folder name and look up its IMDB ID via the IMDB suggestion API.
3. Walk the directory for video files; skip any that already have a subtitle sidecar (`.srt`, `.ass`, etc.).
4. Fan out `fetchSubtitle()` calls via a semaphore-bounded goroutine pool.
5. Log out and print a summary report.

**`fetchSubtitle()` — three-strategy fallback:**
1. `imdb+S+E` — search by parent IMDB ID + season/episode (most precise).
2. `show+ep` — search by show name + episode title keywords + season/episode.
3. `show+S+E` — search by show name + season/episode only.

Each strategy result set is filtered by `matchesShow()` (keyword match against release name/title) and preferentially narrowed by parent IMDB ID match. The subtitle with the highest `download_count` is selected.

**Rate limiting:** every search sleeps 1 s before the request; both search and download calls retry once after 10 s on HTTP 429.

**Key API:** [OpenSubtitles REST API v1](https://api.opensubtitles.com/api/v1) — auth via `Api-Key` header + `Authorization: Bearer <token>`.
