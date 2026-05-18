# TODO

## High priority

- **Watcher polling fallback** — inotify does not work over SMB/NFS (NAS mounts). Add a configurable polling interval (e.g. every 30 min) that scans for new files without subtitles, making auto-fetch actually useful for network-mounted libraries.

- **Bulk "Fetch All Missing"** — a single button to queue every file across the whole library that lacks a subtitle, respecting the worker semaphore. Currently requires clicking per media item.

## Medium priority

- **Subtitle language preference** — allow users to set a preferred language (+ optional fallback). Currently hardcoded to English, making the service unusable for non-English libraries.

- **Fetch history / activity log** — results only appear in server logs. An in-memory ring buffer (last N events) exposed in the UI would show what the watcher/auto-fetch is doing without needing to SSH into the container.

- **Per-file fetch status** — surface "last attempted", "attempt count", and "failed reason" on files missing a subtitle, so users know whether a manual retry is worth it.

- **Retry queue** — files where all providers returned no results are silently forgotten. A scheduled re-attempt (e.g. after 24 h) would cover cases where providers are temporarily out of stock.

## Low priority / future

- **Subtitle language tag** — display the detected language of the downloaded subtitle on the subtitle row (parseable from filename or provider metadata).

- **Miniplayer** — in-browser video + subtitle preview. Straightforward for MP4/WebM; requires FFmpeg transcoding to support MKV (the most common format in mixed libraries). Deferred until transcoding support is scoped.
