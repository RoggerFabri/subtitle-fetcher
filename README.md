# subtitle-fetcher

Downloads English subtitles for a media library and exposes a web UI for browsing coverage, fetching missing subtitles, and managing provider credentials.

## Requirements

Go 1.22 or later.

## Build

```
make build
```

Or directly:

```
go build -o subtitle-fetcher ./src
```

## Modes

### Web UI (recommended)

```
./subtitle-fetcher --serve <root> [--port 8080]
```

Starts an HTTP server at `http://localhost:8080`. The UI lets you:

- Scan the library for video files and subtitle coverage
- Browse movies and series with per-episode status
- Fetch missing subtitles from multiple providers (auto or interactive picker)
- Manage subtitle provider credentials and priority order
- Set and override IMDB IDs per title for more accurate results
- Delete subtitle sidecars
- Export/import all settings and IMDB IDs as JSON

State is persisted in `data/subtitles.db` (SQLite) next to the binary. The path can be overridden with the `DB_PATH` environment variable.

### CLI batch fetch (legacy)

```
./subtitle-fetcher -u <username> -p <password> -k <api-key> -d <directory> [-w <workers>]
```

Walks `<directory>` recursively, skips files that already have a subtitle sidecar, and downloads missing subtitles via OpenSubtitles only.

| Flag | Description |
| --- | --- |
| `-u` | OpenSubtitles username |
| `-p` | OpenSubtitles password |
| `-k` | OpenSubtitles API key |
| `-d` | Directory to scan |
| `-w` | Parallel downloads, default `5` |

## Docker

```bash
docker-compose up --build
```

The `docker-compose.yaml` mounts your media directory (set the volume path) and persists the database in `./data`. On Windows, use a WSL path for network shares (e.g. `/mnt/z/Shared/Downloads`).

The `DB_PATH` environment variable is set to `/app/data` inside the container so the database lands in the mounted volume.

## Subtitle providers

Providers are tried in configured priority order; the first to return a result wins.

| Provider | Auth required | Notes |
| --- | --- | --- |
| [OpenSubtitles](https://www.opensubtitles.com) | Username + password + API key | Largest index; VIP account removes download limits |
| [SubDL](https://subdl.com) | API key | No download quota |
| [Wyzie](https://sub.wyzie.io) | None | IMDB ID required |

Provider credentials, order, and enable/disable toggles are configured from the Settings tab in the web UI and stored in the database.

## Supported formats

**Video:** `.mkv`, `.mp4`, `.avi`, `.mov`, `.m4v`, `.wmv`  
**Subtitles detected:** `.srt`, `.ass`, `.ssa`, `.sub`
