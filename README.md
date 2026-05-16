# subtitle-fetcher

Downloads English subtitles for a TV show directory using the [OpenSubtitles REST API](https://api.opensubtitles.com/api/v1).

## Requirements

Go 1.22 or later. No external dependencies — stdlib only.

An OpenSubtitles account and API key are required. Get one at <https://www.opensubtitles.com/consumers>.

## Build

```
make build
```

Or directly:

```
go build -o subtitle-fetcher .
```

## Usage

```
./subtitle-fetcher -u <username> -p <password> -k <api-key> -d <path/to/show> [-w <workers>]
```

| Flag | Description |
|------|-------------|
| `-u` | OpenSubtitles username |
| `-p` | OpenSubtitles password |
| `-k` | OpenSubtitles API key |
| `-d` | Directory to scan (searched recursively) |
| `-w` | Parallel downloads, default `5` |

Video files that already have a subtitle sidecar (`.srt`, `.ass`, `.ssa`, `.sub`) are skipped. A summary report is printed at the end.

## Supported formats

**Video:** `.mkv`, `.mp4`, `.avi`, `.mov`, `.m4v`, `.wmv`  
**Existing subtitles detected:** `.srt`, `.ass`, `.ssa`, `.sub`
