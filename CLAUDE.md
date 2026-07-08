# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Building and running

```bash
make build

# Web UI (primary mode)
./subtitle-fetcher --serve <root> [--port 8080]

# Legacy CLI batch fetch
./subtitle-fetcher -u <username> -p <password> -k <api-key> -d <directory> [-w <workers>]
```

### Makefile targets

| Target | Description |
| --- | --- |
| `make build` | Compile to `subtitle-fetcher` / `subtitle-fetcher.exe` |
| `make clean` | Remove compiled binaries |
| `make fmt` | Format all Go source files with `gofmt` |
| `make vet` | Run `go vet` |
| `make test` | Run `go test ./...` |

## Repository layout

```text
src/                   Go source files (package main)
src/web/               Static frontend
src/web/index.html     Single-page shell + modal scaffolding
src/web/style.css      Dark theme styles
src/web/js/            ES module frontend
  main.js              Entry point; wires DOM events, exposes window.app
  api.js               All fetch() calls to the backend
  state.js             Shared mutable state (providerOrder, mediaData, …)
  utils.js             showToast, shared helpers
  components/
    mediaList.js       Library tab rendering
    settings.js        Provider cards, export/import
    actions.js         Fetch, delete, scan actions
    modals.js          Subtitle picker + IMDB picker + NFO viewer
data/                  Runtime data (gitignored)
  subtitles.db         SQLite database
Dockerfile
docker-compose.yaml
```

## Architecture

### Go source files (`src/`, `package main`)

| File | Responsibility |
| --- | --- |
| `main.go` | Entry point; dispatches to serve, scan, or legacy CLI mode |
| `config.go` | CLI flag parsing; defines `Config` and mode helpers |
| `server.go` | HTTP server, all API route handlers, `apiMedia`/`apiFile` types |
| `db.go` | SQLite schema, migrations, `upsertMedia`, `upsertFile`; respects `DB_PATH` env var |
| `scanner.go` | Recursive FS walk; writes scan results to DB |
| `media.go` | Video file discovery, subtitle sidecar detection, `subtitlePath` |
| `fetch.go` | `subtitleProvider` interface, `SubtitleCandidate` struct, `fetchWithProviders`, ZIP extraction |
| `opensubtitles.go` | OpenSubtitles REST API client + `subtitleProvider` implementation |
| `subdl.go` | SubDL API client + `subtitleProvider` implementation |
| `wyzie.go` | Wyzie API client + `subtitleProvider` implementation |
| `podnapisi.go` | Podnapisi provider (stub) |
| `imdb.go` | IMDB suggestion API; `discoverIMDBID`, `fetchIMDBSuggestions`, `IMDBSuggestion` |
| `nfo.go` | Kodi/Jellyfin `.nfo` parser (`parseNFO`, `nfoData`, `findNFO`, `parseMediaNFO`); extracts IMDB id, year, airing status at scan time |
| `server_nfo.go` | `GET /api/media/{id}/nfo` — parsed metadata + raw XML for the viewer modal |
| `ui.go` | `//go:embed web` + hot-reload SSE for dev |

### Database (`data/subtitles.db`)

Stored in `data/` next to the binary by default. Override with `DB_PATH` env var (used by Docker to point at the mounted volume).

- `media(id, path, name, type, imdb_id, last_scanned, scan_sig, nfo_path, year, air_status, added_at)` — one row per movie/series folder. `nfo_path`/`year`/`air_status` are populated from the folder's `.nfo` during scan (`air_status` = series Continuing/Ended). `added_at` is set once, on first insert, and never updated — the basis for "new since last seen".
- `files(id, media_id, path, season, episode, has_subtitle, subtitle_name, last_seen, added_at)` — one row per video file. `added_at` (insert-only) marks when an episode first appeared, driving the per-show "+N new" episode badge.
- `settings(key, value)` — provider credentials, order, toggles, and `library_seen_at` (the "seen everything up to here" timestamp for new-item detection).

### Incremental scanning

`scanner.go` skips folders whose contents are unchanged since the last scan. Each media folder stores a directory signature in `media.scan_sig`:

- **Movie:** the folder's own mtime (changes when a video/subtitle is added or removed).
- **Series:** a composite of every season folder's name + mtime (the show-root mtime alone misses episode-level changes one level down).

On rescan, `scanEntryFS` computes the current signature and, if it matches `scan_sig`, returns `skipped` without reading the (network-bound) folder contents. Skipped folders still have their known file paths re-marked as "seen" via `loadPriorScanState` so `pruneStale` doesn't delete them. The signature relies on the filesystem updating directory mtime on entry add/remove (standard POSIX / CIFS / NFS behavior).

Subtitle sidecars are detected from the directory listing (`dirEntryNameSet` + `subtitleNameFor`), not per-file `os.Stat` calls — important over SMB/NFS where each stat is a round-trip.

Scan worker count has an I/O-bound floor (`max(NumCPU*2, 16)`) independent of the download-oriented `workers` setting, since scan workers spend most of their time blocked on filesystem round-trips.

### Subtitle provider interface

```go
type subtitleProvider interface {
    Name() string
    Open() error
    Close()
    FetchSubtitle(videoPath, show string, keywords []string, imdbID, mediaType string, printMu *sync.Mutex) bool
    SearchSubtitles(videoPath, show string, keywords []string, imdbID, mediaType string) ([]SubtitleCandidate, error)
    DownloadCandidate(handle, videoPath string) (string, error)
}
```

`FetchSubtitle` is used by the auto-fetch path (stops at first success). `SearchSubtitles` + `DownloadCandidate` power the interactive picker modal.

Download tokens use the format `"provider:handle"` (e.g. `"subdl:/subtitle/abc.zip"`, `"opensubtitles:12345"`), keeping the frontend opaque to provider internals.

### IMDB ID caching

During a scan, if the media folder has an `.nfo`, its IMDB id (from `<imdbid>` / `<imdb_id>`, or `<id>` only when `tt`-prefixed) seeds `media.imdb_id` when empty — offline and exact, so no API guess is needed. When there's no NFO, `discoverIMDBID` (IMDB suggestion API) is the fallback and is called at most once per media entry; the result is stored in `media.imdb_id` and reused on all subsequent fetches. Users can override it via the IMDB picker (`PUT /api/media/{id}/imdb`). The column is never overwritten during a rescan.

### New-since-last-seen detection

Both `upsertMedia` and `upsertFile` write `added_at` only on the initial INSERT (never in the `ON CONFLICT` update), so it records when an entry first appeared — via either the scanner or the fsnotify watcher, since both funnel through those helpers. A `library_seen_at` timestamp in `settings` is the "seen up to here" pointer, seeded to now on first migration (so a pre-existing library — whose rows have an empty `added_at` after the `ALTER TABLE` — is never flagged; only genuinely new additions afterward are). `/api/report` computes `is_new` (media added after the pointer) and `new_episodes` (count of files added after it) with a lexical `added_at > library_seen_at` compare (safe because timestamps are fixed-format RFC3339 Zulu). The frontend renders a NEW badge / "+N new" sub-badge, a header "N new" count, a Recently Added strip, and a "New first" sort option; **Mark all seen** (`POST /api/seen`) advances the pointer to now, clearing the NEW markers. On a brand-new (empty) DB the first scan flags the whole library once — a one-time baseline the user clears with Mark all seen.

### Key API routes

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/report` | Full media + file list with subtitle status (incl. `year`, `air_status`, `has_nfo`, `is_new`, `new_episodes`) |
| `POST` | `/api/seen` | Advance `library_seen_at` to now — clears all NEW markers ("Mark all seen") |
| `GET` | `/api/media/{id}/nfo` | Parsed NFO metadata + raw XML for the viewer modal |
| `POST` | `/api/scan` | Trigger FS scan |
| `GET` | `/api/scan/status` | Scan progress |
| `POST` | `/api/fetch/media/{id}` | Fetch all missing subtitles for a media entry |
| `POST` | `/api/fetch/season/{id}/{season}` | Fetch missing subtitles for one season |
| `POST` | `/api/fetch/file/{id}` | Fetch subtitle for one file |
| `POST` | `/api/search/file/{id}` | Return all candidates from all providers (picker) |
| `POST` | `/api/download/file/{id}` | Download a specific candidate by token |
| `DELETE` | `/api/subtitle/{id}` | Delete subtitle file + clear DB |
| `GET` | `/api/imdb/search?q=` | Search IMDB suggestions |
| `PUT` | `/api/media/{id}/imdb` | Persist user-chosen IMDB ID |
| `GET` | `/api/settings` | Get provider settings |
| `POST` | `/api/settings` | Save provider settings |
| `GET` | `/api/export` | Download settings + IMDB IDs as JSON |
| `POST` | `/api/import` | Apply exported JSON; matches IMDB IDs by media name |

### Rate limiting

OpenSubtitles: every search sleeps 1 s; HTTP 429/502/503/504 retries once after 10 s.
