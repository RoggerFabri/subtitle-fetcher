# subtitle-fetcher

Downloads English subtitles for a TV show directory using the [OpenSubtitles REST API](https://api.opensubtitles.com/api/v1).

## Requirements

```
pip install requests
```

An OpenSubtitles account and API key are required. Get one at <https://www.opensubtitles.com/consumers>.

## Usage

```
python get_subtitles.py -u <username> -p <password> -k <api-key> -d <path/to/show> [-w <workers>]
```

| Flag | Description |
|------|-------------|
| `-u` / `--username` | OpenSubtitles username |
| `-p` / `--password` | OpenSubtitles password |
| `-k` / `--api-key`  | OpenSubtitles API key |
| `-d` / `--directory`| Directory to scan (searched recursively) |
| `-w` / `--workers`  | Parallel downloads, default `5` |

Video files that already have a subtitle sidecar (`.srt`, `.ass`, `.ssa`, `.sub`) are skipped. A summary report is printed at the end.

## Supported formats

**Video:** `.mkv`, `.mp4`, `.avi`, `.mov`, `.m4v`, `.wmv`  
**Existing subtitles detected:** `.srt`, `.ass`, `.ssa`, `.sub`
