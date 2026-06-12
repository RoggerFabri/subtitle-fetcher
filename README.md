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

State is persisted in `data/subtitles.db` (SQLite) relative to the binary. Override the directory with the `DB_PATH` environment variable.

### Scan only

```
./subtitle-fetcher --scan <root>
```

Walks `<root>` recursively, records every video file and its subtitle status in the database, then exits. Useful for updating the database without starting the web server.

## Docker

```bash
docker-compose up --build
```

The compose file mounts your media directory at `/media` inside the container (edit the volume path in `docker-compose.yaml`) and persists the database in `./data` on the host. The container runs `--serve /media` by default.

## Security

The web UI has **no authentication** and is intended for a trusted local network only. In particular:

- Provider credentials (including your OpenSubtitles password) are stored **in plaintext** in `subtitles.db`.
- `GET /api/export` returns **all settings, including credentials, in plaintext** to any client that can reach the port.

**Do not expose this server directly to the internet.** If you need remote access, place it behind a reverse proxy that enforces authentication (and TLS), or restrict it to a VPN/LAN.

Environment variables set in the container:

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `DB_PATH` | `/app/data` | Directory where `subtitles.db` is stored |

On Windows, use a WSL path for network shares (e.g. `/mnt/z/Shared/Downloads`).

## Subtitle providers

Providers are tried in configured priority order; the first to return a result wins.

| Provider | Auth required | Notes |
| --- | --- | --- |
| [OpenSubtitles](https://www.opensubtitles.com) | Username + password + API key | Largest index; VIP account removes download limits |
| [SubDL](https://subdl.com) | API key | No download quota |
| [Wyzie](https://sub.wyzie.io) | API key | IMDB ID required |

Provider credentials, order, and enable/disable toggles are configured from the Settings tab in the web UI and stored in the database.

## Supported formats

**Video:** `.mkv`, `.mp4`, `.avi`, `.mov`, `.m4v`, `.wmv`  
**Subtitles detected:** `.srt`, `.ass`, `.ssa`, `.sub`
