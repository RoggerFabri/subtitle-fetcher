
// State management
window.mediaData = [];
window.providerOrder = [];
window.providerEnabled = {};
window.providerFields = {};
window.providerConfigured = {};
window.providerDefs = [];
window.isScanning = false;
window.expandedIds = new Set();

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
  document.querySelector('.save-all-btn')?.addEventListener('click', saveAllProviders);

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
    const [mediaRes, settingsRes] = await Promise.all([
      fetch('/api/report'),
      fetch('/api/settings')
    ]);
    
    if (mediaRes.ok) {
      const data = await mediaRes.json();
      // The server returns a top-level array, not {media: []}
      window.mediaData = Array.isArray(data) ? data : (data.media || []);
      
      // Enrich data with computed counts and status for the UI
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

      // Calculate global stats for the dashboard
      const total = window.mediaData.reduce((acc, m) => acc + m.total_count, 0);
      const subbed = window.mediaData.reduce((acc, m) => acc + m.subtitles_count, 0);
      const stats = {
        total_files: total,
        missing: total - subbed,
        coverage: total > 0 ? Math.round((subbed / total) * 100) : 0
      };
      updateStats(stats);
    }

    if (settingsRes.ok) {
      const settings = await settingsRes.json();
      loadSettings(settings);
    }
  } catch (err) {
    console.error(err);
    showToast("Failed to load data from server", "error");
  }
}

window.triggerScan = async function() {
  if (window.isScanning) return;
  
  const btn = document.getElementById('btn-scan');
  const statusEl = document.getElementById('scan-status');
  
  try {
    window.isScanning = true;
    if (btn) btn.disabled = true;
    if (statusEl) statusEl.textContent = "Initiating scan...";

    // Start polling for real-time progress
    let stopPolling = false;
    const pollStatus = async () => {
      while (!stopPolling) {
        try {
          const sRes = await fetch('/api/scan/status');
          if (sRes.ok) {
            const sData = await sRes.json();
            if (statusEl && sData.status) statusEl.textContent = sData.status;
            if (!sData.scanning) break;
          }
        } catch (e) {}
        await new Promise(r => setTimeout(r, 500));
      }
    };

    const pollPromise = pollStatus();
    const res = await fetch('/api/scan', { method: 'POST' });
    stopPolling = true;
    await pollPromise;

    if (!res.ok) throw new Error("Scan failed");
    
    showToast("Scan completed successfully", "success");
    await refreshData();
  } catch (err) {
    showToast(err.message, "error");
  } finally {
    window.isScanning = false;
    if (btn) btn.disabled = false;
    if (statusEl) statusEl.textContent = "";
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
    const name = m.name || m.Name || "";
    const type = m.type || m.Type || "";
    const status = m.status || m.Status || "";
    const matchesSearch = name.toLowerCase().includes(query);
    const matchesType = !typeFilter || type === typeFilter;
    const matchesStatus = !statusFilter || status === statusFilter;
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
    return `
    <div class="media-card ${isExpanded ? 'expanded' : ''}">
      <div class="media-header" onclick="window.toggleExpand(${id})">
        <span class="badge badge-${type}">${type}</span>
        <span class="media-name">${m.name || m.Name}</span>
        <span class="coverage">${m.subtitles_count}/${m.total_count}</span>
        <div class="dot dot-${getStatusColor(m.status)}"></div>
        <span class="chevron">${isExpanded ? '▾' : '▸'}</span>
      </div>
      ${isExpanded ? renderMediaBody(m) : ''}
    </div>
  `}).join('');
};

function renderMediaBody(m) {
  const type = m.type || m.Type;
  const id = m.id || m.Id;

  if (type === 'movie') {
    return `
      <div class="media-body">
        <div class="media-actions">
          <button class="btn-fetch" onclick="window.fetchMedia(${id}, event)">Fetch Subtitles</button>
        </div>
      </div>
    `;
  }

  const files = m.files || m.Files || [];
  const seasonsMap = {};
  files.forEach(f => {
    const sNum = f.season !== undefined ? f.season : (f.Season || 0);
    if (!seasonsMap[sNum]) seasonsMap[sNum] = { number: sNum, files: [] };
    seasonsMap[sNum].files.push(f);
  });
  const seasonsArr = Object.values(seasonsMap).sort((a, b) => a.number - b.number);

  return `
    <div class="media-body">
      ${seasonsArr.map(s => `
        <div class="season-row">
          <span class="season-label">Season ${s.number}</span>
          <span class="coverage">${s.files.filter(f => f.has_subtitle || f.HasSubtitle).length}/${s.files.length}</span>
          <button class="btn-fetch" onclick="window.fetchSeason(${id}, ${s.number}, event)">Fetch Season</button>
        </div>
      `).join('')}
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
  showToast("Fetching subtitles...", "info");
  try {
    const res = await fetch(`/api/fetch/media/${id}`, { method: 'POST' });
    const data = await res.json();
    showToast(`Done: ${data.downloaded} downloaded, ${data.failed} failed`);
    refreshData();
  } catch (err) {
    showToast("Fetch failed", "error");
  }
};

window.fetchSeason = async function(id, season, event) {
  if (event) event.stopPropagation();
  showToast("Fetching subtitles for season...", "info");
  try {
    const res = await fetch(`/api/fetch/season/${id}/${season}`, { method: 'POST' });
    const data = await res.json();
    showToast(`Done: ${data.downloaded} downloaded, ${data.failed} failed`);
    refreshData();
  } catch (err) {
    showToast("Fetch failed", "error");
  }
};

function updateStats(stats) {
  const el = document.getElementById('stats');
  if (!el || !stats) return;
  el.innerHTML = `
    <div class="stat"><div class="stat-label">Total Files</div><div class="stat-value">${stats.total_files}</div></div>
    <div class="stat"><div class="stat-label">Missing</div><div class="stat-value" style="color:var(--red)">${stats.missing}</div></div>
    <div class="stat"><div class="stat-label">Coverage</div><div class="stat-value">${stats.coverage}%</div></div>
  `;
}

function showToast(msg, type = "success") {
  const t = document.getElementById('toast');
  if (!t) return;
  t.textContent = msg;
  t.className = `toast show ${type}`;
  setTimeout(() => t.classList.remove('show'), 3000);
}

/**
 * Renders the provider configuration cards in the settings tab
 */
function renderSettings() {
  const container = document.getElementById('provider-list');
  if (!container) return;

  container.innerHTML = window.providerOrder.map((name, index) => {
    const isEnabled = window.providerEnabled[name];
    const fields = window.providerFields[name] || {};
    return `
      <div class="provider-card">
        <div class="provider-header">
          <span class="provider-rank">${index + 1}</span>
          <span class="provider-name">${name}</span>
          <div class="provider-actions">
            <button class="btn-sm btn-test" onclick="window.testProvider('${name}')">Test</button>
            <label class="provider-toggle-label">
              <input type="checkbox" ${isEnabled ? 'checked' : ''} onchange="window.providerEnabled['${name}'] = this.checked">
            </label>
          </div>
        </div>
        <div class="provider-body">
          <div class="provider-field-row">
            <label>Username</label>
            <input type="text" value="${fields.username || ''}" oninput="window.providerFields['${name}'].username = this.value">
          </div>
          <div class="provider-field-row">
            <label>Password</label>
            <input type="password" placeholder="••••••••" oninput="window.providerFields['${name}'].password = this.value">
          </div>
        </div>
      </div>
    `;
  }).join('');
}

/**
 * Persists all provider settings to the backend
 */
async function saveAllProviders() {
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
  showToast(`Testing ${name} connection...`, "info");
};