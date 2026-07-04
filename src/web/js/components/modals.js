import { state } from '../state.js';
import * as api from '../api.js';
import { showToast } from '../utils.js';
import { refreshMediaAndStats, fileInfoById } from './actions.js';

let pickerFocusIndex = -1;
let imdbFocusIndex = -1;

function applyFocus(rows, index) {
  rows.forEach((r, i) => r.classList.toggle('kb-focus', i === index));
  if (index >= 0 && rows[index]) rows[index].scrollIntoView({ block: 'nearest' });
}

export function handlePickerKey(e) {
  if (document.getElementById('picker-modal')?.classList.contains('hidden')) return;
  const rows = [...document.querySelectorAll('#picker-body .picker-row')];
  if (!rows.length) return;
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    pickerFocusIndex = Math.min(pickerFocusIndex + 1, rows.length - 1);
    applyFocus(rows, pickerFocusIndex);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    pickerFocusIndex = Math.max(pickerFocusIndex - 1, 0);
    applyFocus(rows, pickerFocusIndex);
  } else if (e.key === 'Enter' && pickerFocusIndex >= 0) {
    e.preventDefault();
    rows[pickerFocusIndex]?.click();
  }
}

export function handleImdbKey(e) {
  if (document.getElementById('imdb-modal')?.classList.contains('hidden')) return;
  const rows = [...document.querySelectorAll('#imdb-picker-body .imdb-row')];
  if (!rows.length) return;
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    imdbFocusIndex = Math.min(imdbFocusIndex + 1, rows.length - 1);
    applyFocus(rows, imdbFocusIndex);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    imdbFocusIndex = Math.max(imdbFocusIndex - 1, 0);
    applyFocus(rows, imdbFocusIndex);
  } else if (e.key === 'Enter' && imdbFocusIndex >= 0) {
    e.preventDefault();
    rows[imdbFocusIndex]?.click();
  }
}

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
    pickerFocusIndex = -1;
    body.innerHTML = candidates.map((c, i) => `
      <div class="picker-row" onclick="window.app.downloadCandidate(${fileId}, ${i}, this)">
        <span class="picker-provider">${c.provider}</span>
        <span class="picker-lang">${(c.language || 'en').toUpperCase()}</span>
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
      imdbFocusIndex = -1;
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

export async function openSubtitlePreview(fileId, subName, event) {
  if (event) event.stopPropagation();
  const modal = document.getElementById('preview-modal');
  document.getElementById('preview-modal-title').textContent = subName || 'Subtitle Preview';
  const body = document.getElementById('preview-body');
  body.textContent = 'Loading…';
  modal.classList.remove('hidden');
  try {
    body.textContent = await api.apiSubtitlePreview(fileId);
  } catch {
    body.textContent = 'Failed to load subtitle.';
  }
}

export function closeSubtitlePreview() {
  document.getElementById('preview-modal').classList.add('hidden');
}

// --- NFO viewer ---

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

const NFO_TYPE_LABEL = { movie: 'Movie', tvshow: 'Series', episodedetails: 'Episode' };

function nfoArtURL(item) {
  return `/api/media/${state.nfoArtMediaId}/art?name=${encodeURIComponent(item.name)}`;
}

function renderNfoCarousel(art) {
  if (!art || !art.length) return '';
  const first = art[0];
  const nav = art.length > 1
    ? `<button class="nfo-art-nav nfo-art-prev" onclick="window.app.nfoArtStep(-1)" aria-label="Previous">‹</button>
       <button class="nfo-art-nav nfo-art-next" onclick="window.app.nfoArtStep(1)" aria-label="Next">›</button>`
    : '';
  return `
    <div class="nfo-carousel">
      <img id="nfo-art-img" class="nfo-art-img" src="${nfoArtURL(first)}" alt="${esc(first.label)}"
           onerror="window.app.onNfoArtError()">
      ${nav}
      <div class="nfo-art-caption">
        <span id="nfo-art-label">${esc(first.label)}</span>
        <span id="nfo-art-count">1 / ${art.length}</span>
      </div>
    </div>`;
}

function updateNfoArt() {
  const art = state.nfoArt || [];
  const item = art[state.nfoArtIndex];
  if (!item) return;
  const img = document.getElementById('nfo-art-img');
  const label = document.getElementById('nfo-art-label');
  const count = document.getElementById('nfo-art-count');
  if (img) { img.classList.remove('nfo-art-broken'); img.src = nfoArtURL(item); img.alt = item.label; }
  if (label) label.textContent = item.label;
  if (count) count.textContent = `${state.nfoArtIndex + 1} / ${art.length}`;
}

export function nfoArtStep(delta) {
  const art = state.nfoArt || [];
  if (art.length < 2) return;
  state.nfoArtIndex = (state.nfoArtIndex + delta + art.length) % art.length;
  updateNfoArt();
}

export function onNfoArtError() {
  const img = document.getElementById('nfo-art-img');
  if (img) img.classList.add('nfo-art-broken');
}

export function handleNfoKey(e) {
  if (document.getElementById('nfo-modal')?.classList.contains('hidden')) return;
  if (e.key === 'ArrowRight') { e.preventDefault(); nfoArtStep(1); }
  else if (e.key === 'ArrowLeft') { e.preventDefault(); nfoArtStep(-1); }
}

function renderNfoCard(nfo) {
  const d = nfo.data || {};
  const row = (k, v) => `<div class="nfo-row"><span class="nfo-k">${k}</span><span class="nfo-v">${v}</span></div>`;

  const title = d.title || d.original_title || d.show_title || '(untitled)';
  const typeLabel = NFO_TYPE_LABEL[nfo.type] || nfo.type || '';

  const bits = [];
  if (d.year) bits.push(esc(d.year));
  if (d.runtime) bits.push(esc(d.runtime) + ' min');
  if (d.mpaa) bits.push(esc(d.mpaa));
  if (d.status) bits.push(esc(d.status));
  if (d.rating) bits.push('★ ' + esc(Number(d.rating).toFixed(1)));

  const imdb = nfo.imdb_id
    ? `<a class="imdb-chip" href="https://www.imdb.com/title/tt${esc(nfo.imdb_id)}/" target="_blank">IMDb ↗</a>`
    : '';

  const genres = (d.genres || []).filter(Boolean)
    .map(g => `<span class="nfo-chip">${esc(g)}</span>`).join('');

  const rows = [];
  if (d.premiered) rows.push(row('Premiered', esc(d.premiered)));
  if (d.end_date) rows.push(row('Ended', esc(d.end_date)));
  if ((d.directors || []).length) rows.push(row('Director', d.directors.map(esc).join(', ')));
  const studios = (d.studios || []).filter(Boolean);
  if (studios.length) rows.push(row('Studio', studios.map(esc).join(', ')));
  if (d.collection && d.collection.name) rows.push(row('Collection', esc(d.collection.name)));

  const actors = (d.actors || []).filter(a => !a.type || a.type === 'Actor').slice(0, 12);
  const cast = actors.length
    ? `<div class="nfo-section-label">Cast</div>
       <div class="nfo-cast">${actors.map(a => `
         <div class="nfo-cast-item">
           <span class="nfo-actor">${esc(a.name)}</span>
           ${a.role ? `<span class="nfo-role">${esc(a.role)}</span>` : ''}
         </div>`).join('')}</div>`
    : '';

  return `
    <div id="nfo-card" class="nfo-card">
      ${renderNfoCarousel(nfo.art)}
      <div class="nfo-title">${esc(title)}</div>
      <div class="nfo-meta">
        ${typeLabel ? `<span class="nfo-type">${esc(typeLabel)}</span>` : ''}
        ${bits.length ? `<span>${bits.join(' · ')}</span>` : ''}
        ${imdb}
      </div>
      ${d.tagline ? `<p class="nfo-tagline">“${esc(d.tagline)}”</p>` : ''}
      ${d.plot ? `<p class="nfo-plot">${esc(d.plot)}</p>` : ''}
      ${genres ? `<div class="nfo-chips">${genres}</div>` : ''}
      ${rows.length ? `<div class="nfo-rows">${rows.join('')}</div>` : ''}
      ${cast}
    </div>
    <pre id="nfo-raw" class="preview-body hidden">${esc(nfo.raw)}</pre>`;
}

export async function openNfo(mediaId, event) {
  if (event) event.stopPropagation();
  const modal = document.getElementById('nfo-modal');
  const body = document.getElementById('nfo-body');
  const toggle = document.getElementById('nfo-raw-toggle');
  const m = (state.mediaData || []).find(x => (x.id || x.Id) === mediaId);
  document.getElementById('nfo-modal-title').textContent = m ? (m.name || m.Name) : 'NFO';
  if (toggle) { toggle.textContent = 'View raw XML'; toggle.classList.add('hidden'); }
  body.innerHTML = '<div class="picker-loading">Loading…</div>';
  state.nfoArt = [];
  state.nfoArtIndex = 0;
  state.nfoArtMediaId = mediaId;
  modal.classList.remove('hidden');
  try {
    const nfo = await api.apiGetNfo(mediaId);
    state.nfoArt = nfo.art || [];
    state.nfoArtIndex = 0;
    body.innerHTML = renderNfoCard(nfo);
    if (toggle) toggle.classList.remove('hidden');
  } catch (err) {
    body.innerHTML = `<div class="picker-empty">${esc(err.message || 'Failed to load NFO.')}</div>`;
  }
}

export function toggleNfoRaw() {
  const raw = document.getElementById('nfo-raw');
  const card = document.getElementById('nfo-card');
  const toggle = document.getElementById('nfo-raw-toggle');
  if (!raw) return;
  const showRaw = raw.classList.toggle('hidden') === false;
  if (card) card.classList.toggle('hidden', showRaw);
  if (toggle) toggle.textContent = showRaw ? 'View formatted' : 'View raw XML';
}

export function closeNfo() {
  document.getElementById('nfo-modal').classList.add('hidden');
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
