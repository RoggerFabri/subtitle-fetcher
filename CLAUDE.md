# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Running the script

```
python get_subtitles.py -u <username> -p <password> -k <api-key> -d <directory> [-w <workers>]
```

- `-w` defaults to 5 parallel download workers.
- The script logs in, scans the directory recursively for video files, skips files that already have a subtitle sidecar, downloads missing ones, then logs out.

## Architecture

Everything lives in a single script (`get_subtitles.py`). There is no package structure.

**Execution flow (top-level, imperative):**
1. Parse CLI args and build a `requests.Session` with the API key + bearer token from login.
2. Derive the show name from the target folder name and look up its IMDB ID via the IMDB suggestion API.
3. Walk the directory for video files; skip any that already have a subtitle sidecar (`.srt`, `.ass`, etc.).
4. Fan out `fetch_subtitle()` calls via `ThreadPoolExecutor`.
5. Log out and print a summary report.

**`fetch_subtitle()` — three-strategy fallback:**
1. `imdb+S+E` — search by parent IMDB ID + season/episode (most precise).
2. `show+ep` — search by show name + episode title keywords + season/episode.
3. `show+S+E` — search by show name + season/episode only.

Each strategy result set is filtered by `matches_show()` (keyword match against release name/title) and preferentially narrowed by parent IMDB ID match. The subtitle with the highest `download_count` is selected.

**Rate limiting:** every search sleeps 1 s before the request; both search and download calls retry once after 10 s on HTTP 429.

**Key API:** [OpenSubtitles REST API v1](https://api.opensubtitles.com/api/v1) — auth via `Api-Key` header + `Authorization: Bearer <token>`.
