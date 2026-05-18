# TODO

## High priority

- [ ] **Watcher polling fallback** — inotify does not work over SMB/NFS (NAS mounts). Add a configurable polling interval (e.g. every 30 min) that scans for new files without subtitles, making auto-fetch actually useful for network-mounted libraries.
- [ ] **Bulk "Fetch All Missing"** — a single button to queue every file across the whole library that lacks a subtitle, respecting the worker semaphore. Currently requires clicking per media item.
- [ ] **Subtitle language preference** — allow users to set a preferred language (+ optional fallback). Currently hardcoded to English, making the service unusable for non-English libraries.

## UI improvements

- [ ] **Sort options** — list order is DB insertion order. Add sort by name (A–Z), coverage %, and type via a dropdown in the toolbar.
- [x] **Delete subtitle confirmation** — delete fires immediately with no undo. Add an inline confirmation or grace period to prevent accidental loss.
- [ ] **Empty library state** — fresh installs show "No media items found." which is confusing. Replace with a proper empty state and a "Run a scan to get started" CTA.
- [x] **Fetch button spinner** — buttons disable during fetch but show no loading indicator. Add a spinner or label change for clearer feedback.
- [ ] **Auto-expand after fetch** — after a successful fetch, expand the card so the user sees the result without an extra click.
- [ ] **Expand / collapse all** — add a toggle-all button next to the search bar for navigating large libraries faster.
- [x] **Settings panel heading** — heading still reads "Subtitle Providers" but the General section (workers) now sits above it. Update to reflect the full scope.
- [ ] **Keyboard navigation in subtitle picker** — arrow keys + Enter to select a candidate; currently mouse-only.
- [x] **Search clear button** — no way to clear the search input other than manually selecting and deleting. An ✕ button inside the field fixes this.
- [x] **Coverage bar in stats** — coverage is shown as a plain percentage. A thin coloured bar (green/yellow/red) would communicate library health at a glance.
- [x] **Card highlight after fetch** — briefly flash a card green after a subtitle is successfully downloaded, for satisfying visual feedback.
- [ ] **Subtitle picker language column** — the picker shows Provider / Release / Downloads / Format but no language. Add a language column so users don't have to infer it from the filename.
- [x] **Toast close button** — toasts auto-dismiss after 3.5 s but can't be manually dismissed. Add an ✕ so long error messages can be read at the user's pace.
- [x] **Settings unsaved indicator** — "Save all" button gives no indication there are pending changes. Show a subtle dot or change button state when fields are dirty.

## Medium priority

- [ ] **Fetch history / activity log** — results only appear in server logs. An in-memory ring buffer (last N events) exposed in the UI would show what the watcher/auto-fetch is doing without needing to SSH into the container.
- [ ] **Per-file fetch status** — surface "last attempted", "attempt count", and "failed reason" on files missing a subtitle, so users know whether a manual retry is worth it.
- [ ] **Retry queue** — files where all providers returned no results are silently forgotten. A scheduled re-attempt (e.g. after 24 h) would cover cases where providers are temporarily out of stock.

## Low priority / future

- [ ] **Subtitle language tag** — display the detected language of the downloaded subtitle on the subtitle row (parseable from filename or provider metadata).
- [ ] **Miniplayer** — in-browser video + subtitle preview. Straightforward for MP4/WebM; requires FFmpeg transcoding to support MKV (the most common format in mixed libraries). Deferred until transcoding support is scoped.
