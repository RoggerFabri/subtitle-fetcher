
// State management
window.mediaData = [];
window.providerOrder = [];
window.providerEnabled = {};
window.providerFields = {};
window.providerConfigured = {};
window.providerDefs = [];
window.isScanning = false;
window.expandedIds = new Set();
window.expandedSeasonIds = new Set();

// Hot-reload: listen for server-sent file-change events and reload the page.
(function () {
  function connect() {
    const es = new EventSource('/api/hot-reload');
    es.onmessage = () => location.reload();
    es.onerror = () => { es.close(); setTimeout(connect, 2000); };
  }
  connect();
})();

// Initialize event listeners
document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('btn-scan')?.addEventListener('click', () => typeof triggerScan === 'function' && triggerScan());
  document.getElementById('btn-settings-gear')?.addEventListener('click', () => showTab('settings'));
  document.getElementById('tab-library')?.addEventListener('click', () => showTab('library'));
  document.getElementById('tab-settings')?.addEventListener('click', () => showTab('settings'));
  document.getElementById('search')?.addEventListener('input', renderList);
  document.getElementById('search')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      refreshData();
    }
  });
  document.getElementById('filter-type')?.addEventListener('change', renderList);
  document.getElementById('filter-status')?.addEventListener('change', renderList);
  document.querySelector('.save-all-btn')?.addEventListener('click', () => window.saveAllProviders());

  // Set initial view state
  showTab('library');

  // Initial data load
  refreshData();
});

/**
 * Switches between panels and updates tab styling
 */
window.showTab = function(name) {
  document.querySelectorAll('[id^="panel-"]').forEach(p => {
    p.classList.toggle('hidden', p.id !== `panel-${name}`);
  });
  document.querySelectorAll('.tab').forEach(t => {
    t.classList.toggle('active', t.id === `tab-${name}`);
  });
};

/**
 * Updates the UI with settings received from the server
 */
function loadSettings(s) {
  if (!s) return;

  // Sync order from server
  if (s.provider_order && Array.isArray(s.provider_order)) {
    window.providerOrder = s.provider_order;
  }

  // Sync enabled flags and field values
  for (const name of window.providerOrder) {
    if (s[name]) {
      if (s[name].enabled !== undefined) {
        window.providerEnabled[name] = Boolean(s[name].enabled);
      }
      window.providerFields[name] = { ...(window.providerFields[name] || {}), ...s[name] };
    }
  }
  renderSettings();
}

async function refreshData() {
  try {
    const settingsRes = await fetch('/api/settings');
    if (settingsRes.ok) {
      const settings = await settingsRes.json();
      loadSettings(settings);
    }
    await refreshMediaAndStats();
  } catch (err) {
    console.error(err);
    showToast("Failed to load initial data from server", "error");
  }
}

// New helper function to refresh only media and stats
async function refreshMediaAndStats() {
  try {
    const mediaRes = await fetch('/api/report');
    if (mediaRes.ok) {
      const data = await mediaRes.json();
      window.mediaData = Array.isArray(data) ? data : (data.media || []);
      
      window.mediaData.forEach(m => {
        const files = m.files || m.Files || [];
        m.subtitles_count = files.filter(f => f.has_subtitle || f.HasSubtitle).length;
        m.total_count = files.length;
        
        if (m.total_count === 0) m.status = 'missing';
        else if (m.subtitles_count === m.total_count) m.status = 'complete';
        else if (m.subtitles_count > 0) m.status = 'partial';
        else m.status = 'missing';
      });

      renderList();

      const total = window.mediaData.reduce((acc, m) => acc + m.total_count, 0);
      const subbed = window.mediaData.reduce((acc, m) => acc + m.subtitles_count, 0);
      const stats = {
        total_files: total,
        missing: total - subbed,
        coverage: total > 0 ? Math.round((subbed / total) * 100) : 0
      };
      updateStats(stats);
    }
  } catch (err) {
    console.error(err);
    showToast("Failed to refresh media data", "error");
  }
}
window.triggerScan = async function() {
  if (window.isScanning) return;

  const btn = document.getElementById('btn-scan');
  const statusEl = document.getElementById('scan-status');
  const progressBarContainer = document.getElementById('scan-progress-container');
  const progressBarFill = document.getElementById('scan-progress-bar-fill');

  try {

    window.isScanning = true;
    if (btn) btn.disabled = true;
    if (progressBarFill) progressBarFill.style.width = '0%';
    if (statusEl) statusEl.textContent = "Initiating scan…";
    if (progressBarContainer) progressBarContainer.classList.add('scan-visible');

    const res = await fetch('/api/scan', { method: 'POST' });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || "Scan failed to start");
    }

    // Poll for progress until the server reports 'running: false'
    while (true) {
      await new Promise(r => setTimeout(r, 1000));
      const sRes = await fetch('/api/scan/status');
      if (!sRes.ok) continue;

      const sData = await sRes.json();
      if (statusEl && sData.status) statusEl.textContent = sData.status; // Update status text

      // Update progress bar
      if (progressBarFill && sData.total > 0) {
        const progress = (sData.current / sData.total) * 100;
        progressBarFill.style.width = `${progress}%`;
      } else if (progressBarFill) {
        progressBarFill.style.width = '0%'; // Reset if total is 0
      }
      
      // The Go backend returns 'running', not 'scanning'
      if (!sData.running) break;
    }

    // Snap to 100% and show "Done" briefly before fading out
    if (progressBarFill) progressBarFill.style.width = '100%';
    if (statusEl) statusEl.textContent = "Scan complete";
    showToast("Scan completed successfully", "success");
  } catch (err) {
    showToast(err.message, "error");
  } finally {
    window.isScanning = false;
    if (btn) btn.disabled = false;
    await refreshMediaAndStats();
    // Let the 100% bar sit for a moment, then fade the whole widget out
    await new Promise(r => setTimeout(r, 900));
    if (progressBarContainer) progressBarContainer.classList.remove('scan-visible');
    // After the fade transition finishes, reset internal state silently
    setTimeout(() => {
      if (progressBarFill) progressBarFill.style.width = '0%';
      if (statusEl) statusEl.textContent = '';
    }, 400);
  }
};

window.toggleExpand = function(id) {
  if (window.expandedIds.has(id)) {
    window.expandedIds.delete(id);
  } else {
    window.expandedIds.add(id);
  }
  renderList();
};

window.renderList = function() {
  const container = document.getElementById('media-list');
  if (!container) return;

  const query = document.getElementById('search')?.value.toLowerCase() || "";
  const typeFilter = document.getElementById('filter-type')?.value || "";
  const statusFilter = document.getElementById('filter-status')?.value || "";

  const filtered = (window.mediaData || []).filter(m => {
    if (m.total_count === 0) return false;
    const name = m.name || m.Name || "";
    const type = m.type || m.Type || "";
    const status = m.status || m.Status || "";
    const matchesSearch = name.toLowerCase().includes(query);
    const matchesType = !typeFilter || type === typeFilter;
    const matchesStatus = !statusFilter ||
      status === statusFilter ||
      (statusFilter === 'missing' && status === 'partial');
    return matchesSearch && matchesType && matchesStatus;
  });

  if (filtered.length === 0) {
    container.innerHTML = '<div style="padding: 20px; color: var(--muted); text-align: center;">No media items found.</div>';
    return;
  }

  container.innerHTML = filtered.map(m => {
    const id = m.id || m.Id;
    const isExpanded = window.expandedIds.has(id);
    const type = m.type || m.Type;
    const label = type === 'series' ? 'Fetch All' : 'Fetch';
    return `
    <div class="media-card ${isExpanded ? 'expanded' : ''}">
      <div class="media-header" onclick="window.toggleExpand(${id})">
        <span class="badge badge-${type}">${type}</span>
        <span class="media-name">${m.name || m.Name}</span>
        <span class="coverage">${m.subtitles_count}/${m.total_count}</span>
        <div class="dot dot-${getStatusColor(m.status)}"></div>
        <button class="fetch-btn" id="fetch-media-${id}" onclick="window.fetchMedia(${id}, event)">${label}</button>
        <span class="chevron">${isExpanded ? '▾' : '▸'}</span>
      </div>
      ${isExpanded ? renderMediaBody(m) : ''}
    </div>
  `}).join('');
};

function renderMediaBody(m) {
  const type = m.type || m.Type;
  const id = m.id || m.Id;
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
                ${!hasSub ? `<button class="fetch-btn" id="fetch-file-${f.id}" onclick="window.fetchFile(${f.id}, event)">Fetch</button>` : ''}
              </div>
              ${hasSub && subName ? `
                <div class="subtitle-row">
                  <span class="subtitle-filename">${subName}</span>
                  <button class="subtitle-del-btn" onclick="window.deleteSubtitle(${f.id}, event)">Delete</button>
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
        const isSeasonExpanded = window.expandedSeasonIds.has(seasonKey);
        const withSub = s.files.filter(f => f.has_subtitle || f.HasSubtitle).length;
        const seasonLabel = s.number === 0 ? 'Specials' : s.number != null ? `Season ${s.number}` : 'Unknown';
        return `
          <div class="season-row" onclick="window.toggleSeasonExpand(${id}, '${s.key}', event)">
            <span class="chevron" style="margin-right:8px; width:12px; display:inline-block">${isSeasonExpanded ? '▾' : '▸'}</span>
            <span class="season-label">${seasonLabel}</span>
            <span class="coverage">${withSub}/${s.files.length}</span>
            ${s.number != null ? `<button class="fetch-btn" id="fetch-season-${id}-${s.key}" onclick="window.fetchSeason(${id}, ${s.number}, event)">Fetch Season</button>` : `<span style="font-size:11px;color:var(--muted)">Rescan to fix</span>`}
          </div>
          ${isSeasonExpanded ? renderEpisodes(id, s) : ''}
        `;
      }).join('')}
    </div>
  `;
}

window.toggleSeasonExpand = function(mediaId, seasonNum, event) {
  if (event) event.stopPropagation();
  const key = `${mediaId}-${seasonNum}`;
  if (window.expandedSeasonIds.has(key)) {
    window.expandedSeasonIds.delete(key);
  } else {
    window.expandedSeasonIds.add(key);
  }
  renderList();
};

function renderEpisodes(mediaId, season) {
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
            ${!hasSub ? `<button class="fetch-btn" id="fetch-file-${f.id}" onclick="window.fetchFile(${f.id}, event)">Fetch</button>` : ''}
          </div>
          ${hasSub && subName ? `
            <div class="subtitle-row">
              <span class="subtitle-filename">${subName}</span>
              <button class="subtitle-del-btn" onclick="window.deleteSubtitle(${f.id}, event)">Delete</button>
            </div>
          ` : ''}
        `;
      }).join('')}
    </div>
  `;
}

function getStatusColor(status) {
  switch (status) {
    case 'complete': return 'green';
    case 'partial': return 'yellow';
    case 'missing': return 'red';
    default: return 'gray';
  }
}

window.fetchMedia = async function(id, event) {
  if (event) event.stopPropagation();
  const btn = document.getElementById(`fetch-media-${id}`);
  const name = mediaNameById(id);
  setFetching(btn, true);
  try {
    const res = await fetch(`/api/fetch/media/${id}`, { method: 'POST' });
    const data = await res.json();
    const prefix = name ? `"${name}" — ` : '';
    const type = data.downloaded > 0 ? 'success' : 'error';
    showToast(`${prefix}${data.downloaded} downloaded, ${data.failed} failed`, type);
    await refreshMediaAndStats();
  } catch (err) {
    showToast(`${name ? `"${name}" — ` : ''}fetch failed`, "error");
  } finally {
    setFetching(btn, false);
  }
};

window.fetchSeason = async function(id, season, event) {
  if (event) event.stopPropagation();
  const btn = document.getElementById(`fetch-season-${id}-${season}`);
  const name = mediaNameById(id);
  setFetching(btn, true);
  try {
    const res = await fetch(`/api/fetch/season/${id}/${season}`, { method: 'POST' });
    const data = await res.json();
    const prefix = name ? `"${name}" S${String(season).padStart(2,'0')} — ` : `Season ${season} — `;
    const type = data.downloaded > 0 ? 'success' : 'error';
    showToast(`${prefix}${data.downloaded} downloaded, ${data.failed} failed`, type);
    await refreshMediaAndStats();
  } catch (err) {
    showToast(`${name ? `"${name}" S${season}` : `Season ${season}`} — fetch failed`, "error");
  } finally {
    setFetching(btn, false);
  }
};

window.fetchFile = async function(id, event) {
  if (event) event.stopPropagation();
  const btn = document.getElementById(`fetch-file-${id}`);
  const info = fileInfoById(id);
  const label = info
    ? `"${info.media.name || info.media.Name}"${info.file.episode != null ? ` E${String(info.file.episode).padStart(2,'0')}` : ''}`
    : 'file';
  setFetching(btn, true);
  try {
    const res = await fetch(`/api/fetch/file/${id}`, { method: 'POST' });
    const data = await res.json();
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
};

window.deleteSubtitle = async function(id, event) {
  if (event) event.stopPropagation();
  const btn = event.currentTarget;
  const info = fileInfoById(id);
  const label = info
    ? `"${info.media.name || info.media.Name}"${info.file.episode != null ? ` E${String(info.file.episode).padStart(2,'0')}` : ''}`
    : 'file';
  btn.disabled = true;
  try {
    const res = await fetch(`/api/subtitle/${id}`, { method: 'DELETE' });
    const data = await res.json();
    if (!res.ok) {
      showToast(`${label} — delete failed: ${data.error}`, 'error');
    } else {
      showToast(`${label} — subtitle deleted`, 'info');
      await refreshMediaAndStats();
    }
  } catch (err) {
    showToast(`${label} — delete failed`, 'error');
  } finally {
    btn.disabled = false;
  }
};

function updateStats(stats) {
  const el = document.getElementById('stats');
  if (!el || !stats) return;
  const movies = (window.mediaData || []).filter(m => (m.type || m.Type) === 'movie').length;
  const series = (window.mediaData || []).filter(m => (m.type || m.Type) === 'series').length;
  el.innerHTML = `
    <div class="stat"><div class="stat-label">Total Files</div><div class="stat-value">${stats.total_files}</div></div>
    <div class="stat"><div class="stat-label">Movies</div><div class="stat-value">${movies}</div></div>
    <div class="stat"><div class="stat-label">Series</div><div class="stat-value">${series}</div></div>
    <div class="stat"><div class="stat-label">Coverage</div><div class="stat-value">${stats.coverage}%</div></div>
    <div class="stat"><div class="stat-label">Missing</div><div class="stat-value" style="color:var(--red)">${stats.missing}</div></div>
  `;
}

function setFetching(btn, on) {
  if (!btn) return;
  btn.disabled = on;
  btn.classList.toggle('fetching', on);
}

function showToast(msg, type = "success") {
  const container = document.getElementById('toast-container');
  if (!container) return;
  const t = document.createElement('div');
  t.className = `toast ${type}`;
  t.textContent = msg;
  container.appendChild(t);
  requestAnimationFrame(() => requestAnimationFrame(() => t.classList.add('show')));
  setTimeout(() => {
    t.classList.remove('show');
    setTimeout(() => t.remove(), 250);
  }, 3500);
}

function mediaNameById(id) {
  const m = (window.mediaData || []).find(m => (m.id || m.Id) === id);
  return m ? (m.name || m.Name || null) : null;
}

function fileInfoById(fileId) {
  for (const m of (window.mediaData || [])) {
    const f = (m.files || m.Files || []).find(f => (f.id || f.Id) === fileId);
    if (f) return { media: m, file: f };
  }
  return null;
}

/**
 * Renders the provider configuration cards in the settings tab
 */
function renderSettings() {
  const container = document.getElementById('provider-list');
  if (!container) return;

  const lastIdx = window.providerOrder.length - 1;
  container.innerHTML = window.providerOrder.map((name, index) => {
    const isEnabled = window.providerEnabled[name];
    if (!window.providerFields[name]) window.providerFields[name] = {};
    const fields = window.providerFields[name];

    let fieldsHtml = '';
    if (name === 'opensubtitles') {
      fieldsHtml = `
        <div class="provider-field-row">
          <label>Username</label>
          <input type="text" value="${fields.username || ''}" oninput="window.providerFields['${name}'].username = this.value">
        </div>
        <div class="provider-field-row">
          <label>Password</label>
          <input type="password" placeholder="••••••••" oninput="window.providerFields['${name}'].password = this.value">
        </div>
        <div class="provider-field-row">
          <label>API Key</label>
          <input type="text" value="${fields.api_key || fields.openSubtitles_api_key || ''}" oninput="window.providerFields['${name}'].openSubtitles_api_key = this.value">
        </div>
      `;
    } else if (name === 'subdl') {
      fieldsHtml = `
        <div class="provider-field-row">
          <label>API Key</label>
          <input type="text" value="${fields.api_key || ''}" oninput="window.providerFields['${name}'].api_key = this.value">
        </div>
      `;
    } else if (name === 'wyzie') {
      fieldsHtml = `
        <div class="provider-field-row">
          <label>API Key</label>
          <input type="text" value="${fields.api_key || ''}" oninput="window.providerFields['${name}'].api_key = this.value">
        </div>
        <div class="provider-field-row">
          <label style="font-size:10px">Get a free key at <a href="https://sub.wyzie.io/redeem" target="_blank" style="color:var(--blue)">sub.wyzie.io/redeem</a></label>
        </div>
      `;
    }

    return `
      <div class="provider-card">
        <div class="provider-header">
          <span class="provider-rank">${index + 1}</span>
          <span class="provider-name">${name}</span>
          <div class="provider-actions">
            <button class="btn-sm" onclick="window.moveProvider('${name}', -1)" ${index === 0 ? 'disabled' : ''} title="Move up">↑</button>
            <button class="btn-sm" onclick="window.moveProvider('${name}',  1)" ${index === lastIdx ? 'disabled' : ''} title="Move down">↓</button>
            <button class="btn-sm btn-test" onclick="window.testProvider('${name}')">Test</button>
            <label class="provider-toggle-label">
              <input type="checkbox" ${isEnabled ? 'checked' : ''} onchange="window.providerEnabled['${name}'] = this.checked; window.saveAllProviders();">
            </label>
          </div>
        </div>
        <div class="provider-body">
          ${fieldsHtml}
          <div class="test-result hidden" id="test-result-${name}"></div>
        </div>
      </div>
    `;
  }).join('');
}

window.moveProvider = function(name, dir) {
  const i = window.providerOrder.indexOf(name);
  const j = i + dir;
  if (j < 0 || j >= window.providerOrder.length) return;
  [window.providerOrder[i], window.providerOrder[j]] = [window.providerOrder[j], window.providerOrder[i]];
  renderSettings();
};

/**
 * Persists all provider settings to the backend
 */
window.saveAllProviders = async function() {
  const payload = { provider_order: window.providerOrder };
  window.providerOrder.forEach(name => {
    payload[name] = { 
      ...window.providerFields[name], 
      enabled: window.providerEnabled[name] 
    };
  });

  try {
    const res = await fetch('/api/settings', { 
      method: 'POST', 
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload) 
    });
    if (res.ok) showToast("Settings saved successfully");
  } catch (err) {
    showToast("Failed to save settings", "error");
  }
}

window.testProvider = async function(name) {
  const resultEl = document.getElementById(`test-result-${name}`);
  const btn = document.querySelector(`button[onclick="window.testProvider('${name}')"]`);
  if (btn) { btn.disabled = true; btn.textContent = '...'; }
  if (resultEl) { resultEl.className = 'test-result'; resultEl.textContent = 'Testing…'; }

  try {
    const res = await fetch(`/api/health/provider-test?provider=${name}`, { method: 'POST' });
    const data = await res.json();

    if (!resultEl) return;
    if (res.ok && !data.error) {
      let msg = '';
      if (name === 'opensubtitles' && data.downloads_remaining !== undefined) {
        msg = `✓ Connected — ${data.downloads_remaining} downloads remaining today`;
      } else if (data.results !== undefined) {
        msg = `✓ Connected — ${data.results} result(s) returned`;
      } else {
        msg = `✓ Connected`;
      }
      resultEl.className = 'test-result ok';
      resultEl.textContent = msg;
    } else {
      resultEl.className = 'test-result error';
      resultEl.textContent = `✗ ${data.error || 'Connection failed'}`;
    }
    resultEl.classList.remove('hidden');
  } catch (err) {
    if (resultEl) {
      resultEl.className = 'test-result error';
      resultEl.textContent = `✗ ${err.message}`;
      resultEl.classList.remove('hidden');
    }
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = 'Test'; }
  }
};