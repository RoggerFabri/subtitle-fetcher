import { state } from '../state.js';
import * as api from '../api.js';
import { showToast } from '../utils.js';
import { refreshMediaAndStats, fileInfoById } from './actions.js';

export async function openPicker(fileId, event) {
  if (event) event.stopPropagation();
  state.pickerFileId = fileId;
  const modal = document.getElementById('picker-modal');
  const title = document.getElementById('picker-title');
  const input = document.getElementById('picker-search-input');
  if (input) input.value = '';
  const info = fileInfoById(fileId);
  const filePath = info?.file?.path || info?.file?.Path || '';
  const fileName = filePath ? filePath.replace(/.*[/\\]/, '') : '';
  title.textContent = fileName || 'Choose Subtitle';
  modal.classList.remove('hidden');

  await doPickerSearch('');
}

export async function doPickerSearch(query) {
  const fileId = state.pickerFileId;
  const body = document.getElementById('picker-body');
  body.innerHTML = '<div class="picker-loading">Searching providers…</div>';

  try {
    const candidates = await api.apiSearchFile(fileId, query);
    if (!candidates || candidates.length === 0) {
      body.innerHTML = '<div class="picker-empty">No results found.</div>';
      return;
    }
    state.pickerCandidates = candidates;
    body.innerHTML = candidates.map((c, i) => `
      <div class="picker-row" onclick="window.app.downloadCandidate(${fileId}, ${i}, this)">
        <span class="picker-provider">${c.provider}</span>
        <span class="picker-name" title="${c.name}">${c.name}</span>
        <span class="picker-downloads">${c.downloads > 0 ? c.downloads.toLocaleString() : '—'}</span>
        <span class="picker-format">${c.format.toUpperCase()}</span>
      </div>`).join('');
  } catch (err) {
    body.innerHTML = '<div class="picker-empty">Search failed.</div>';
  }
}

export function onPickerSearchInput(val) {
  clearTimeout(state.pickerSearchTimer);
  state.pickerSearchTimer = setTimeout(() => {
    doPickerSearch(val.trim());
  }, 400);
}

export function setPickerStatus(msg) {
  const el = document.getElementById('picker-status');
  if (!el) return;
  if (!msg) { el.style.display = 'none'; el.innerHTML = ''; return; }
  el.style.display = 'flex';
  el.innerHTML = `<div class="spinner"></div><span>${msg}</span>`;
}

export async function downloadCandidate(fileId, candidateIndex, rowEl) {
  const token = (state.pickerCandidates || [])[candidateIndex]?.token;
  if (!token) return;
  rowEl.classList.add('loading');
  setPickerStatus('Downloading…');
  document.getElementById('picker-modal').classList.add('picker-busy');
  try {
    const data = await api.apiDownloadFile(fileId, token);
    if (data.downloaded) {
      rowEl.classList.remove('loading');
      document.getElementById('picker-modal').classList.remove('picker-busy');
      closePicker();
      showToast('Subtitle downloaded', 'success');
      await refreshMediaAndStats();
    } else {
      rowEl.classList.remove('loading');
      setPickerStatus('');
      document.getElementById('picker-modal').classList.remove('picker-busy');
      const errMsg = (data.error || 'unknown').slice(0, 120);
      showToast(`Download failed: ${errMsg}`, 'error');
    }
  } catch (err) {
    rowEl.classList.remove('loading');
    setPickerStatus('');
    document.getElementById('picker-modal').classList.remove('picker-busy');
    showToast('Download failed', 'error');
  }
}

export function closePicker() {
  const modal = document.getElementById('picker-modal');
  if (modal.classList.contains('picker-busy')) return;
  modal.classList.add('hidden');
  setPickerStatus('');
  clearTimeout(state.pickerSearchTimer);
}

export function openImdbPicker(mediaId, event) {
  if (event) event.stopPropagation();
  state.imdbTargetMediaId = mediaId;
  const m = (state.mediaData || []).find(m => (m.id || m.Id) === mediaId);
  document.getElementById('imdb-modal-title').textContent =
    `Set IMDB — ${m ? (m.name || m.Name) : ''}`;
  const input = document.getElementById('imdb-search-input');
  input.value = m ? (m.name || m.Name) : '';
  document.getElementById('imdb-picker-body').innerHTML = '<div class="picker-loading">Searching…</div>';
  document.getElementById('imdb-modal').classList.remove('hidden');
  onImdbSearchInput(input.value);
  setTimeout(() => input.select(), 50);
}

export function closeImdbPicker() {
  document.getElementById('imdb-modal').classList.add('hidden');
  clearTimeout(state.imdbSearchTimer);
}

export function onImdbSearchInput(val) {
  clearTimeout(state.imdbSearchTimer);
  const body = document.getElementById('imdb-picker-body');
  if (!val.trim()) { body.innerHTML = ''; return; }
  state.imdbSearchTimer = setTimeout(async () => {
    body.innerHTML = '<div class="picker-loading">Searching…</div>';
    try {
      const results = await api.apiImdbSearch(val.trim());
      if (!results || results.length === 0) {
        body.innerHTML = '<div class="picker-empty">No results.</div>';
        return;
      }
      body.innerHTML = `
        <div class="imdb-cols"><span>ID</span><span>Title</span><span style="text-align:right">Year</span><span>Type</span><span></span></div>
        ${results.map(r => `
          <div class="imdb-row" onclick="window.app.selectImdbID(${state.imdbTargetMediaId}, '${r.id}', event)">
            <span class="imdb-row-id">tt${r.id}</span>
            <span class="imdb-row-title" title="${r.title}">${r.title}</span>
            <span class="imdb-row-year">${r.year || '—'}</span>
            <span class="imdb-row-type">${r.type || ''}</span>
            <a class="imdb-row-link" href="https://www.imdb.com/title/tt${r.id}/" target="_blank" onclick="event.stopPropagation()">↗</a>
          </div>`).join('')}`;
    } catch {
      body.innerHTML = '<div class="picker-empty">Search failed.</div>';
    }
  }, 320);
}

export async function selectImdbID(mediaId, imdbId, event) {
  if (event) event.stopPropagation();
  try {
    const data = await api.apiSetImdbId(mediaId, imdbId);
    if (data.ok) {
      closeImdbPicker();
      showToast(`IMDB set to tt${imdbId}`, 'success');
      await refreshMediaAndStats();
    } else {
      showToast('Failed to save IMDB ID', 'error');
    }
  } catch {
    showToast('Failed to save IMDB ID', 'error');
  }
}
