import { state } from '../state.js';
import { getStatusColor } from '../utils.js';
import { apiGetMediaFiles } from '../api.js';

export async function toggleExpand(id) {
  if (state.expandedIds.has(id)) {
    state.expandedIds.delete(id);
    renderList();
    return;
  }

  state.expandedIds.add(id);

  if (!state.fileCache.has(id)) {
    renderList(); // show loading state immediately
    try {
      const files = await apiGetMediaFiles(id);
      state.fileCache.set(id, files);
      const m = state.mediaData.find(m => (m.id || m.Id) === id);
      if (m) m.files = files;
    } catch {
      state.expandedIds.delete(id);
    }
  }

  renderList();
}

export function toggleSeasonExpand(mediaId, seasonNum, event) {
  if (event) event.stopPropagation();
  const key = `${mediaId}-${seasonNum}`;
  if (state.expandedSeasonIds.has(key)) {
    state.expandedSeasonIds.delete(key);
  } else {
    state.expandedSeasonIds.add(key);
  }
  renderList();
}

function restoreFetchingState() {
  state.fetchingMediaIds.forEach(id => {
    const btn = document.getElementById(`fetch-media-${id}`);
    if (btn) { btn.disabled = true; btn.classList.add('fetching'); btn.textContent = '…'; }
    const card = btn?.closest('.media-card');
    card?.querySelectorAll('[id^="fetch-season-"], .episode-row .fetch-btn').forEach(b => { b.disabled = true; });
  });
  state.fetchingSeasonKeys.forEach(key => {
    const btn = document.getElementById(`fetch-season-${key}`);
    if (btn) { btn.disabled = true; btn.classList.add('fetching'); btn.textContent = '…'; }
    const row = btn?.closest('.season-row');
    row?.nextElementSibling?.querySelectorAll('.fetch-btn').forEach(b => { b.disabled = true; });
  });
  state.fetchingFileIds.forEach(id => {
    const btn = document.getElementById(`fetch-file-${id}`);
    if (btn) { btn.disabled = true; btn.classList.add('fetching'); btn.textContent = '…'; }
  });
}

export function renderList() {
  const container = document.getElementById('media-list');
  if (!container) return;

  const query = document.getElementById('search')?.value.toLowerCase() || "";
  const typeFilter = document.querySelector('.pills[data-filter="type"] .pill.active')?.dataset.value || "";
  const statusFilter = document.querySelector('.pills[data-filter="status"] .pill.active')?.dataset.value || "";
  const imdbFilter = document.querySelector('.pills[data-filter="imdb"] .pill.active')?.dataset.value || "";
  const sortBy = document.querySelector('.pills[data-filter="sort"] .pill.active')?.dataset.value || "";

  const filtered = (state.mediaData || []).filter(m => {
    if (m.total_count === 0) return false;
    const name = m.name || m.Name || "";
    const type = m.type || m.Type || "";
    const status = m.status || m.Status || "";
    const imdbId = m.imdb_id || m.ImdbID || "";
    const matchesSearch = name.toLowerCase().includes(query);
    const matchesType = !typeFilter || type === typeFilter;
    const matchesStatus = !statusFilter ||
      status === statusFilter ||
      (statusFilter === 'missing' && status === 'partial');
    const matchesImdb = !imdbFilter ||
      (imdbFilter === 'yes' && imdbId) ||
      (imdbFilter === 'no' && !imdbId);
    return matchesSearch && matchesType && matchesStatus && matchesImdb;
  });

  if (sortBy === 'name') {
    filtered.sort((a, b) => (a.name || a.Name || '').localeCompare(b.name || b.Name || ''));
  } else if (sortBy === 'coverage') {
    filtered.sort((a, b) => {
      const pA = a.total_count > 0 ? a.subtitles_count / a.total_count : 0;
      const pB = b.total_count > 0 ? b.subtitles_count / b.total_count : 0;
      return pA - pB; // lowest coverage first — shows what needs attention
    });
  } else if (sortBy === 'type') {
    filtered.sort((a, b) => {
      const tA = a.type || a.Type || '';
      const tB = b.type || b.Type || '';
      if (tA !== tB) return tA.localeCompare(tB); // movie before series
      return (a.name || a.Name || '').localeCompare(b.name || b.Name || '');
    });
  }

  if (filtered.length === 0) {
    const hasData = (state.mediaData || []).some(m => m.total_count > 0);
    const hasFilters = query || typeFilter || statusFilter || imdbFilter;
    if (!hasData) {
      container.innerHTML = `
        <div class="empty-state">
          <div class="empty-state-icon">📂</div>
          <div class="empty-state-title">Your library is empty</div>
          <div class="empty-state-body">Point the app at your media folder and run a scan to get started.</div>
          <button class="empty-state-btn" onclick="window.app.triggerScan()">⟳ Run a scan</button>
        </div>`;
    } else {
      container.innerHTML = `<div class="empty-state empty-state--filters"><div class="empty-state-title">No results</div><div class="empty-state-body">${hasFilters ? 'No media matches the current filters.' : 'Nothing to show.'}</div></div>`;
    }
    return;
  }

  container.innerHTML = filtered.map(m => {
    const id = m.id || m.Id;
    const isExpanded = state.expandedIds.has(id);
    const type = m.type || m.Type;
    const label = type === 'series' ? 'Fetch All' : 'Fetch';
    const imdbId = m.imdb_id || m.ImdbID || '';
    const imdbChip = imdbId
      ? `<a class="imdb-chip" href="https://www.imdb.com/title/tt${imdbId}/" target="_blank" onclick="event.stopPropagation()">IMDb ↗</a>`
      : `<span class="imdb-chip unset" onclick="window.app.openImdbPicker(${id}, event)">+ IMDb</span>`;

    let chooseBtn = '';
    let fetchBtn = `<button class="fetch-btn" id="fetch-media-${id}" onclick="window.app.fetchMedia(${id}, event)">${label}</button>`;
    if (type === 'movie') {
      if (m.status === 'complete') {
        fetchBtn = '';
      } else {
        const files = m.files || m.Files || [];
        const firstFile = files[0];
        const firstFileId = firstFile ? (firstFile.id || firstFile.Id) : null;
        chooseBtn = firstFileId
          ? `<button class="fetch-btn" onclick="window.app.openPicker(${firstFileId}, event)">Choose</button>`
          : `<button class="fetch-btn" onclick="window.app.chooseMovieSubtitle(${id}, event)">Choose</button>`;
      }
    }

    return `
    <div class="media-card ${isExpanded ? 'expanded' : ''}">
      <div class="media-header" onclick="window.app.toggleExpand(${id})">
        <span class="badge badge-${type}">${type}</span>
        <span class="media-name">${m.name || m.Name}</span>
        <span class="coverage">${m.subtitles_count}/${m.total_count}</span>
        <div class="dot dot-${getStatusColor(m.status)}"></div>
        ${imdbChip}
        ${fetchBtn}
        ${chooseBtn}
        <span class="chevron">${isExpanded ? '▾' : '▸'}</span>
      </div>
      ${isExpanded ? renderMediaBody(m) : ''}
    </div>
  `}).join('');

  restoreFetchingState();
}

export function renderMediaBody(m) {
  const type = m.type || m.Type;
  const id = m.id || m.Id;

  if (!state.fileCache.has(id)) {
    return `<div class="media-body"><div class="files-loading">Loading…</div></div>`;
  }

  const files = m.files || m.Files || [];

  if (type === 'movie') {
    return `
      <div class="media-body">
        <div class="episode-list">
          ${files.map(f => {
            const hasSub = f.has_subtitle || f.HasSubtitle;
            const name = f.name || f.Name || f.path || f.Path || '';
            const subName = f.subtitle_name || f.SubtitleName || '';
            return `
              <div class="episode-row">
                <span class="ep-label">${name}</span>
                <div class="dot dot-${hasSub ? 'green' : 'red'}"></div>
                ${!hasSub ? `<button class="fetch-btn" id="fetch-file-${f.id}" onclick="window.app.fetchFile(${f.id}, event)">Fetch</button><button class="fetch-btn" onclick="window.app.openPicker(${f.id}, event)">Choose</button>` : ''}
              </div>
              ${hasSub && subName ? `
                <div class="subtitle-row">
                  <span class="subtitle-filename">${subName}</span>
                  <button class="subtitle-prev-btn" onclick="window.app.openSubtitlePreview(${f.id}, '${subName.replace(/'/g, "\\'")}', event)">Preview</button>
                  <button class="subtitle-del-btn" onclick="window.app.deleteSubtitle(${f.id}, event)">Delete</button>
                </div>
              ` : ''}
            `;
          }).join('')}
        </div>
      </div>
    `;
  }

  const seasonsMap = {};
  files.forEach(f => {
    const sNum = (f.season != null) ? f.season : ((f.Season != null) ? f.Season : null);
    const key = sNum != null ? sNum : 'unknown';
    if (!seasonsMap[key]) seasonsMap[key] = { number: sNum, key, files: [] };
    seasonsMap[key].files.push(f);
  });
  const seasonsArr = Object.values(seasonsMap).sort((a, b) => {
    if (a.number == null) return 1;
    if (b.number == null) return -1;
    return a.number - b.number;
  });

  return `
    <div class="media-body">
      ${seasonsArr.map(s => {
        const seasonKey = `${id}-${s.key}`;
        const isSeasonExpanded = state.expandedSeasonIds.has(seasonKey);
        const withSub = s.files.filter(f => f.has_subtitle || f.HasSubtitle).length;
        const seasonLabel = s.number === 0 ? 'Specials' : s.number != null ? `Season ${s.number}` : 'Unknown';
        return `
          <div class="season-row" onclick="window.app.toggleSeasonExpand(${id}, '${s.key}', event)">
            <span class="chevron" style="margin-right:8px; width:12px; display:inline-block">${isSeasonExpanded ? '▾' : '▸'}</span>
            <span class="season-label">${seasonLabel}</span>
            <span class="coverage">${withSub}/${s.files.length}</span>
            ${s.number != null ? `<button class="fetch-btn" id="fetch-season-${id}-${s.key}" onclick="window.app.fetchSeason(${id}, ${s.number}, event)">Fetch Season</button>` : `<span style="font-size:11px;color:var(--muted)">Rescan to fix</span>`}
          </div>
          ${isSeasonExpanded ? renderEpisodes(id, s) : ''}
        `;
      }).join('')}
    </div>
  `;
}

export function renderEpisodes(mediaId, season) {
  return `
    <div class="episode-list">
      ${season.files.map(f => {
        const epNum = f.episode !== undefined ? f.episode : (f.Episode || 0);
        const name = f.name || f.Name || `Episode ${epNum}`;
        const hasSub = f.has_subtitle || f.HasSubtitle;
        const subName = f.subtitle_name || f.SubtitleName || '';
        const epLabel = epNum > 0 ? `E${epNum.toString().padStart(2, '0')} — ` : '';
        return `
          <div class="episode-row">
            <span class="ep-label">${epLabel}${name}</span>
            <div class="dot dot-${hasSub ? 'green' : 'red'}"></div>
            ${!hasSub ? `<button class="fetch-btn" id="fetch-file-${f.id}" onclick="window.app.fetchFile(${f.id}, event)">Fetch</button><button class="fetch-btn" onclick="window.app.openPicker(${f.id}, event)">Choose</button>` : ''}
          </div>
          ${hasSub && subName ? `
            <div class="subtitle-row">
              <span class="subtitle-filename">${subName}</span>
              <button class="subtitle-del-btn" onclick="window.app.deleteSubtitle(${f.id}, event)">Delete</button>
            </div>
          ` : ''}
        `;
      }).join('')}
    </div>
  `;
}

export function updateStats(stats) {
  const el = document.getElementById('stats');
  if (!el || !stats) return;
  const movies = (state.mediaData || []).filter(m => (m.type || m.Type) === 'movie').length;
  const series = (state.mediaData || []).filter(m => (m.type || m.Type) === 'series').length;
  const pct = stats.coverage;
  const barColor = pct >= 80 ? 'var(--green)' : pct >= 40 ? 'var(--yellow)' : 'var(--red)';
  el.innerHTML = `
    <div class="stat"><div class="stat-label">Total Files</div><div class="stat-value">${stats.total_files}</div></div>
    <div class="stat"><div class="stat-label">Movies</div><div class="stat-value">${movies}</div></div>
    <div class="stat"><div class="stat-label">Series</div><div class="stat-value">${series}</div></div>
    <div class="stat stat-coverage">
      <div class="stat-label">Coverage</div>
      <div class="stat-value">${pct}%</div>
      <div class="coverage-bar-track"><div class="coverage-bar-fill" style="width:${pct}%;background:${barColor}"></div></div>
    </div>
    <div class="stat"><div class="stat-label">Missing</div><div class="stat-value" style="color:var(--red)">${stats.missing}</div></div>
  `;
}
