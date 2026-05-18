import { state } from '../state.js';
import { showToast, setFetching } from '../utils.js';
import * as api from '../api.js';
import { loadSettings } from './settings.js';
import { renderList, updateStats } from './mediaList.js';


export async function refreshData() {
  try {
    const [settings, data] = await Promise.all([api.apiGetSettings(), api.apiGetReport()]);
    loadSettings(settings);
    applyMediaData(data);
  } catch (err) {
    console.error(err);
    showToast("Failed to load initial data from server", "error");
  }
}

function applyMediaData(data) {
  state.mediaData = Array.isArray(data) ? data : (data.media || []);

  state.mediaData.forEach(m => {
    const id = m.id || m.Id;
    // Re-attach cached files so expanded cards stay populated after a refresh.
    if (state.fileCache.has(id)) {
      m.files = state.fileCache.get(id);
    }
    // Counts come from the server (GROUP BY); fall back to computing from files
    // for backwards-compatibility if an old response still includes them.
    if (m.total_count == null) {
      const files = m.files || [];
      m.total_count = files.length;
      m.subtitles_count = files.filter(f => f.has_subtitle || f.HasSubtitle).length;
    }

    if (m.total_count === 0) m.status = 'missing';
    else if (m.subtitles_count === m.total_count) m.status = 'complete';
    else if (m.subtitles_count > 0) m.status = 'partial';
    else m.status = 'missing';
  });

  renderList();

  const total = state.mediaData.reduce((acc, m) => acc + m.total_count, 0);
  const subbed = state.mediaData.reduce((acc, m) => acc + m.subtitles_count, 0);
  updateStats({
    total_files: total,
    missing: total - subbed,
    coverage: total > 0 ? Math.round((subbed / total) * 100) : 0
  });
}

export async function refreshMediaAndStats() {
  try {
    applyMediaData(await api.apiGetReport());

    // Re-fetch files for any currently expanded cards so their content stays fresh.
    const expandedIds = [...state.expandedIds];
    if (expandedIds.length > 0) {
      await Promise.all(expandedIds.map(async id => {
        try {
          const files = await api.apiGetMediaFiles(id);
          state.fileCache.set(id, files);
          const m = state.mediaData.find(m => (m.id || m.Id) === id);
          if (m) m.files = files;
        } catch { /* leave stale cache in place */ }
      }));
      renderList();
    }
  } catch (err) {
    console.error(err);
    showToast("Failed to refresh media data", "error");
  }
}

export async function triggerScan() {
  if (state.isScanning) return;

  const btn = document.getElementById('btn-scan');

  try {
    state.isScanning = true;
    if (btn) btn.disabled = true;

    await api.apiPostScan();

    while (true) {
      await new Promise(r => setTimeout(r, 1000));
      let sData;
      try { sData = await api.apiGetScanStatus(); } catch { continue; }

      if (btn) {
        btn.textContent = sData.total > 0 ? `${sData.current} / ${sData.total}` : '…';
        btn.title = sData.status || '';
      }

      if (!sData.running) break;
    }

    showToast("Scan completed successfully", "success");
    await refreshMediaAndStats();
  } catch (err) {
    showToast(err.message, "error");
  } finally {
    state.isScanning = false;
    if (btn) { btn.disabled = false; btn.textContent = '⟳ Scan'; btn.title = ''; }
  }
}

function lockMediaChildren(mediaId) {
  const card = document.getElementById(`fetch-media-${mediaId}`)?.closest('.media-card');
  if (!card) return;
  card.querySelectorAll('[id^="fetch-season-"], .episode-row .fetch-btn').forEach(b => { b.disabled = true; });
}

function unlockMediaChildren(mediaId) {
  const card = document.getElementById(`fetch-media-${mediaId}`)?.closest('.media-card');
  if (!card) return;
  card.querySelectorAll('[id^="fetch-season-"], .episode-row .fetch-btn').forEach(b => { b.disabled = false; });
}

function lockSeasonChildren(mediaId, season) {
  const row = document.getElementById(`fetch-season-${mediaId}-${season}`)?.closest('.season-row');
  row?.nextElementSibling?.querySelectorAll('.fetch-btn').forEach(b => { b.disabled = true; });
}

function unlockSeasonChildren(mediaId, season) {
  const row = document.getElementById(`fetch-season-${mediaId}-${season}`)?.closest('.season-row');
  row?.nextElementSibling?.querySelectorAll('.fetch-btn').forEach(b => { b.disabled = false; });
}

function flashCard(id) {
  const card = document.getElementById(`fetch-media-${id}`)?.closest('.media-card');
  if (!card) return;
  card.classList.remove('flash-success');
  void card.offsetWidth; // reflow to restart animation
  card.classList.add('flash-success');
  card.addEventListener('animationend', () => card.classList.remove('flash-success'), { once: true });
}

export async function fetchMedia(id, event) {
  if (event) event.stopPropagation();
  const btn = document.getElementById(`fetch-media-${id}`);
  const name = mediaNameById(id);
  setFetching(btn, true);
  lockMediaChildren(id);
  try {
    const data = await api.apiFetchMedia(id);
    const prefix = name ? `"${name}" — ` : '';
    const type = data.downloaded > 0 ? 'success' : 'error';
    showToast(`${prefix}${data.downloaded} downloaded, ${data.failed} failed`, type);
    state.fileCache.delete(id);
    await refreshMediaAndStats();
    if (data.downloaded > 0) flashCard(id);
  } catch (err) {
    showToast(`${name ? `"${name}" — ` : ''}fetch failed`, "error");
  } finally {
    setFetching(btn, false);
    unlockMediaChildren(id);
  }
}

export async function fetchSeason(id, season, event) {
  if (event) event.stopPropagation();
  const btn = document.getElementById(`fetch-season-${id}-${season}`);
  const name = mediaNameById(id);
  setFetching(btn, true);
  lockSeasonChildren(id, season);
  try {
    const data = await api.apiFetchSeason(id, season);
    const prefix = name ? `"${name}" S${String(season).padStart(2,'0')} — ` : `Season ${season} — `;
    const type = data.downloaded > 0 ? 'success' : 'error';
    showToast(`${prefix}${data.downloaded} downloaded, ${data.failed} failed`, type);
    state.fileCache.delete(id);
    await refreshMediaAndStats();
    if (data.downloaded > 0) flashCard(id);
  } catch (err) {
    showToast(`${name ? `"${name}" S${season}` : `Season ${season}`} — fetch failed`, "error");
  } finally {
    setFetching(btn, false);
    unlockSeasonChildren(id, season);
  }
}

export async function chooseMovieSubtitle(mediaId, event) {
  if (event) event.stopPropagation();
  let files = state.fileCache.get(mediaId);
  if (!files) {
    try {
      files = await api.apiGetMediaFiles(mediaId);
      state.fileCache.set(mediaId, files);
      const m = state.mediaData.find(m => (m.id || m.Id) === mediaId);
      if (m) m.files = files;
      renderList();
    } catch { return; }
  }
  const firstFile = files[0];
  if (!firstFile) return;
  window.app.openPicker(firstFile.id || firstFile.Id, event);
}

export async function fetchFile(id, event) {
  if (event) event.stopPropagation();
  const btn = document.getElementById(`fetch-file-${id}`);
  const info = fileInfoById(id);
  const label = info
    ? `"${info.media.name || info.media.Name}"${info.file.episode != null ? ` E${String(info.file.episode).padStart(2,'0')}` : ''}`
    : 'file';
  setFetching(btn, true);
  try {
    const data = await api.apiFetchFile(id);
    if (data.downloaded > 0) {
      showToast(`${label} — subtitle downloaded`);
    } else {
      showToast(`${label} — no subtitle found`, "error");
    }
    const mediaId = info ? (info.media.id || info.media.Id) : null;
    if (mediaId) state.fileCache.delete(mediaId);
    await refreshMediaAndStats();
    if (data.downloaded > 0 && mediaId) flashCard(mediaId);
  } catch (err) {
    showToast(`${label} — fetch failed`, "error");
  } finally {
    if (btn) {
      btn.classList.remove('fetching');
      btn.disabled = false;
    }
  }
}

export async function deleteSubtitle(id, event) {
  if (event) event.stopPropagation();
  const btn = event.currentTarget;

  if (!btn.dataset.confirming) {
    btn.dataset.confirming = '1';
    btn.dataset.label = btn.textContent;
    btn.textContent = 'Sure?';
    btn.classList.add('btn-confirm');
    btn.dataset.confirmTimer = setTimeout(() => {
      btn.textContent = btn.dataset.label;
      btn.classList.remove('btn-confirm');
      delete btn.dataset.confirming;
    }, 3000);
    return;
  }

  clearTimeout(btn.dataset.confirmTimer);
  delete btn.dataset.confirming;
  btn.classList.remove('btn-confirm');

  const info = fileInfoById(id);
  const label = info
    ? `"${info.media.name || info.media.Name}"${info.file.episode != null ? ` E${String(info.file.episode).padStart(2,'0')}` : ''}`
    : 'file';
  btn.disabled = true;
  try {
    await api.apiDeleteSubtitle(id);
    showToast(`${label} — subtitle deleted`, 'info');
    if (info) state.fileCache.delete(info.media.id || info.media.Id);
    await refreshMediaAndStats();
  } catch (err) {
    showToast(`${label} — delete failed: ${err.message}`, 'error');
    btn.disabled = false;
  }
}

export async function autoIMDB() {
  const btn = document.getElementById('btn-auto-imdb');
  try {
    await api.apiAutoIMDB();
  } catch (err) {
    showToast(err.message, 'error');
    return;
  }

  if (btn) btn.disabled = true;

  let lastMatched = 0;
  while (true) {
    await new Promise(r => setTimeout(r, 1000));
    let s;
    try { s = await api.apiAutoIMDBStatus(); } catch { continue; }

    if (btn) {
      btn.textContent = s.total > 0 ? `${s.current} / ${s.total}` : '…';
      btn.title = s.label || '';
    }

    if (lastMatched !== s.matched) {
      lastMatched = s.matched;
      refreshMediaAndStats();
    }

    if (!s.running) {
      showToast(`Auto IMDB: ${s.matched} matched, ${s.skipped} skipped`, s.matched > 0 ? 'success' : 'info');
      await refreshMediaAndStats();
      break;
    }
  }

  if (btn) { btn.disabled = false; btn.textContent = 'Auto IMDB'; btn.title = ''; }
}

export function mediaNameById(id) {
  const m = (state.mediaData || []).find(m => (m.id || m.Id) === id);
  return m ? (m.name || m.Name || null) : null;
}

export function fileInfoById(fileId) {
  for (const m of (state.mediaData || [])) {
    const f = (m.files || m.Files || []).find(f => (f.id || f.Id) === fileId);
    if (f) return { media: m, file: f };
  }
  return null;
}
