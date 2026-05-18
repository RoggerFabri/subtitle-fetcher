import { state } from '../state.js';
import { showToast, setFetching } from '../utils.js';
import * as api from '../api.js';
import { loadSettings } from './settings.js';
import { renderList, updateStats } from './mediaList.js';

export async function refreshData() {
  try {
    const settings = await api.apiGetSettings();
    loadSettings(settings);
    await refreshMediaAndStats();
  } catch (err) {
    console.error(err);
    showToast("Failed to load initial data from server", "error");
  }
}

export async function refreshMediaAndStats() {
  try {
    const data = await api.apiGetReport();
    state.mediaData = Array.isArray(data) ? data : (data.media || []);
    
    state.mediaData.forEach(m => {
      const files = m.files || m.Files || [];
      m.subtitles_count = files.filter(f => f.has_subtitle || f.HasSubtitle).length;
      m.total_count = files.length;
      
      if (m.total_count === 0) m.status = 'missing';
      else if (m.subtitles_count === m.total_count) m.status = 'complete';
      else if (m.subtitles_count > 0) m.status = 'partial';
      else m.status = 'missing';
    });

    renderList();

    const total = state.mediaData.reduce((acc, m) => acc + m.total_count, 0);
    const subbed = state.mediaData.reduce((acc, m) => acc + m.subtitles_count, 0);
    const stats = {
      total_files: total,
      missing: total - subbed,
      coverage: total > 0 ? Math.round((subbed / total) * 100) : 0
    };
    updateStats(stats);
  } catch (err) {
    console.error(err);
    showToast("Failed to refresh media data", "error");
  }
}

export async function triggerScan() {
  if (state.isScanning) return;

  const btn = document.getElementById('btn-scan');
  const statusEl = document.getElementById('scan-status');
  const progressBarContainer = document.getElementById('scan-progress-container');
  const progressBarFill = document.getElementById('scan-progress-bar-fill');

  try {
    state.isScanning = true;
    if (btn) btn.disabled = true;
    if (progressBarFill) progressBarFill.style.width = '0%';
    if (statusEl) statusEl.textContent = "Initiating scan…";
    if (progressBarContainer) progressBarContainer.classList.add('scan-visible');

    await api.apiPostScan();

    while (true) {
      await new Promise(r => setTimeout(r, 1000));
      let sData;
      try {
        sData = await api.apiGetScanStatus();
      } catch {
        continue;
      }
      
      if (statusEl && sData.status) statusEl.textContent = sData.status;

      if (progressBarFill && sData.total > 0) {
        const progress = (sData.current / sData.total) * 100;
        progressBarFill.style.width = `${progress}%`;
      } else if (progressBarFill) {
        progressBarFill.style.width = '0%';
      }
      
      if (!sData.running) break;
    }

    if (progressBarFill) progressBarFill.style.width = '100%';
    if (statusEl) statusEl.textContent = "Scan complete";
    showToast("Scan completed successfully", "success");
  } catch (err) {
    showToast(err.message, "error");
  } finally {
    state.isScanning = false;
    if (btn) btn.disabled = false;
    await refreshMediaAndStats();
    await new Promise(r => setTimeout(r, 900));
    if (progressBarContainer) progressBarContainer.classList.remove('scan-visible');
    setTimeout(() => {
      if (progressBarFill) progressBarFill.style.width = '0%';
      if (statusEl) statusEl.textContent = '';
    }, 400);
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
    await refreshMediaAndStats();
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
    await refreshMediaAndStats();
  } catch (err) {
    showToast(`${name ? `"${name}" S${season}` : `Season ${season}`} — fetch failed`, "error");
  } finally {
    setFetching(btn, false);
    unlockSeasonChildren(id, season);
  }
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
    await refreshMediaAndStats();
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
    await refreshMediaAndStats();
  } catch (err) {
    showToast(`${label} — delete failed: ${err.message}`, 'error');
    btn.disabled = false;
  }
}

export async function autoIMDB() {
  const btn = document.getElementById('btn-auto-imdb');
  if (btn) { btn.disabled = true; btn.textContent = '…'; }
  try {
    const data = await api.apiAutoIMDB();
    showToast(`Auto IMDB: ${data.matched} matched, ${data.skipped} skipped`, data.matched > 0 ? 'success' : 'info');
    if (data.matched > 0) await refreshMediaAndStats();
  } catch (err) {
    showToast(err.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = 'Auto IMDB'; }
  }
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
