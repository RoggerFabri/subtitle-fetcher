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
  const nfoFilter = document.querySelector('.pills[data-filter="nfo"] .pill.active')?.dataset.value || "";
  const sortBy = document.querySelector('.pills[data-filter="sort"] .pill.active')?.dataset.value || "";

  updateNewIndicators();
  renderRecentlyAdded();

  const filtered = (state.mediaData || []).filter(m => {
    if (m.total_count === 0) return false;
    const name = m.name || m.Name || "";
    const type = m.type || m.Type || "";
    const status = m.status || m.Status || "";
    const imdbId = m.imdb_id || m.ImdbID || "";
    const hasNfo = m.has_nfo || m.HasNFO || false;
    const matchesSearch = name.toLowerCase().includes(query);
    const matchesType = !typeFilter || type === typeFilter;
    const matchesStatus = !statusFilter ||
      status === statusFilter ||
      (statusFilter === 'missing' && status === 'partial');
    const matchesImdb = !imdbFilter ||
      (imdbFilter === 'yes' && imdbId) ||
      (imdbFilter === 'no' && !imdbId);
    const matchesNfo = !nfoFilter ||
      (nfoFilter === 'yes' && hasNfo) ||
      (nfoFilter === 'no' && !hasNfo);
    return matchesSearch && matchesType && matchesStatus && matchesImdb && matchesNfo;
  });

  if (sortBy === 'new') {
    // New items first (brand-new or gained episodes), most-recently-added among
    // them at the top; everything else falls back to alphabetical.
    filtered.sort((a, b) => {
      const na = hasNewContent(a), nb = hasNewContent(b);
      if (na !== nb) return na ? -1 : 1;
      if (na && nb) return (b.last_added || b.LastAdded || '').localeCompare(a.last_added || a.LastAdded || '');
      return (a.name || a.Name || '').localeCompare(b.name || b.Name || '');
    });
  } else if (sortBy === 'name') {
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
    const hasFilters = query || typeFilter || statusFilter || imdbFilter || nfoFilter;
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

    const year = m.year || m.Year || 0;
    // Movies already carry the year in their folder name; only series need it appended.
    const yearTag = (type === 'series' && year) ? `<span class="media-year">${year}</span>` : '';
    const air = m.air_status || m.AirStatus || '';
    const airBadge = air
      ? `<span class="air-badge air-${air.toLowerCase().replace(/[^a-z]+/g, '-')}">${air}</span>`
      : '';
    const newEpisodes = m.new_episodes || m.NewEpisodes || 0;
    // Brand-new movie/series → NEW; an existing show that only gained episodes → +N new.
    const newBadge = isMediaNew(m)
      ? `<span class="new-badge" title="Added since you last marked the library seen">NEW</span>`
      : (newEpisodes > 0
          ? `<span class="new-badge new-badge--eps" title="${newEpisodes} new episode${newEpisodes > 1 ? 's' : ''} since you last marked the library seen">+${newEpisodes} new</span>`
          : '');

    const hasNfo = m.has_nfo || m.HasNFO;
    const nfoBtn = hasNfo
      ? `<button class="nfo-btn" onclick="window.app.openNfo(${id}, event)">NFO</button>`
      : `<button class="nfo-btn nfo-get" id="nfo-get-${id}" onclick="window.app.backfillNfo(${id}, event)" title="Fetch metadata from TMDB">+ NFO</button>`;

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
    <div class="media-card ${isExpanded ? 'expanded' : ''}" data-id="${id}">
      <div class="media-header" onclick="window.app.toggleExpand(${id})">
        <span class="badge badge-${type}">${type}</span>
        <span class="media-name">${m.name || m.Name}${yearTag}</span>
        ${newBadge}
        ${airBadge}
        <span class="coverage">${m.subtitles_count}/${m.total_count}</span>
        <div class="dot dot-${getStatusColor(m.status)}"></div>
        ${imdbChip}
        ${nfoBtn}
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
              <button class="subtitle-prev-btn" onclick="window.app.openSubtitlePreview(${f.id}, '${subName.replace(/'/g, "\\'")}', event)">Preview</button>
              <button class="subtitle-del-btn" onclick="window.app.deleteSubtitle(${f.id}, event)">Delete</button>
            </div>
          ` : ''}
        `;
      }).join('')}
    </div>
  `;
}

// A media entry is "new" when its folder was added since the last time the
// library was marked seen (server sets is_new by comparing added_at with
// library_seen_at).
export function isMediaNew(m) {
  return !!(m.is_new || m.IsNew);
}

// True for a brand-new entry OR an existing show that gained episodes — the set
// counted by the header badge and floated to the top by the "New first" sort.
export function hasNewContent(m) {
  return isMediaNew(m) || (m.new_episodes || m.NewEpisodes || 0) > 0;
}

// Refreshes the header "N new" badge and the "Mark all seen" button visibility.
function updateNewIndicators() {
  const newItems = (state.mediaData || []).filter(m => m.total_count > 0 && hasNewContent(m)).length;

  const markBtn = document.getElementById('btn-mark-seen');
  if (markBtn) markBtn.classList.toggle('hidden', newItems === 0);

  const headerBadge = document.getElementById('header-new-badge');
  const headerCount = document.getElementById('header-new-count');
  if (headerCount) headerCount.textContent = String(newItems);
  if (headerBadge) headerBadge.classList.toggle('hidden', newItems === 0);
}

const RECENT_LIMIT = 12;

// Recently Added strip: media with a non-empty added_at (i.e. added since this
// feature/upgrade), newest first. Unlike the NEW badges, this does NOT clear on
// "Mark all seen" — it's a browse-what-arrived list, ordered by last_added
// (latest of the folder's or any episode's add time).
export function renderRecentlyAdded() {
  const section = document.getElementById('recently-added');
  const body = document.getElementById('recently-added-body');
  const countEl = document.getElementById('recently-added-count');
  if (!section || !body) return;

  const recent = (state.mediaData || [])
    .filter(m => m.total_count > 0 && (m.last_added || m.LastAdded))
    .sort((a, b) => (b.last_added || b.LastAdded || '').localeCompare(a.last_added || a.LastAdded || ''))
    .slice(0, RECENT_LIMIT);

  if (recent.length === 0) {
    section.classList.add('hidden');
    return;
  }
  section.classList.remove('hidden');
  if (countEl) countEl.textContent = String(recent.length);

  body.innerHTML = recent.map(m => {
    const id = m.id || m.Id;
    const type = m.type || m.Type;
    const name = m.name || m.Name || '';
    const newEps = m.new_episodes || m.NewEpisodes || 0;
    const tag = isMediaNew(m)
      ? '<span class="ra-tag">NEW</span>'
      : (newEps > 0 ? `<span class="ra-tag ra-tag--eps">+${newEps}</span>` : '');
    return `
      <button class="ra-chip" onclick="window.app.focusRecentMedia(${id})" title="${name.replace(/"/g, '&quot;')}">
        <span class="badge badge-${type} ra-badge">${type}</span>
        <span class="ra-name">${name}</span>
        ${tag}
      </button>`;
  }).join('');
}

// Jump to a media card from the Recently Added strip. Clears filters/search so
// the target is guaranteed visible, scrolls to it, then expands it.
export function focusRecentMedia(id) {
  const search = document.getElementById('search');
  if (search) { search.value = ''; document.getElementById('search-clear')?.classList.add('hidden'); }
  document.querySelectorAll('.pills[data-filter]').forEach(group => {
    group.querySelectorAll('.pill').forEach(p => p.classList.remove('active'));
    group.querySelector('.pill[data-value=""]')?.classList.add('active');
  });
  renderList();
  const card = document.querySelector(`#media-list .media-card[data-id="${id}"]`);
  card?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  if (!state.expandedIds.has(id)) toggleExpand(id);
}

// Activate the "New first" sort (used by the header badge).
export function showNewOnly() {
  const group = document.querySelector('.pills[data-filter="sort"]');
  if (group) {
    group.querySelectorAll('.pill').forEach(p => p.classList.remove('active'));
    group.querySelector('.pill[data-value="new"]')?.classList.add('active');
  }
  renderList();
  document.getElementById('media-list')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
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
