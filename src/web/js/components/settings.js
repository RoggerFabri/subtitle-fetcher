import { state } from '../state.js';
import * as api from '../api.js';
import { showToast } from '../utils.js';

export function loadSettings(s) {
  if (!s) return;

  if (s.provider_order && Array.isArray(s.provider_order)) {
    state.providerOrder = s.provider_order;
  }

  if (typeof s.workers === 'number' && s.workers >= 1) {
    state.workers = s.workers;
  }

  for (const name of state.providerOrder) {
    if (s[name]) {
      if (s[name].enabled !== undefined) {
        state.providerEnabled[name] = Boolean(s[name].enabled);
      }
      state.providerFields[name] = { ...(state.providerFields[name] || {}), ...s[name] };
    }
  }
  renderSettings();
}

export function renderSettings() {
  const container = document.getElementById('provider-list');
  if (!container) return;

  const generalHtml = `
    <div class="settings-section">
      <div class="settings-section-title">General</div>
      <div class="provider-field-row">
        <label>Parallel workers</label>
        <input type="number" min="1" max="50" value="${state.workers}"
          style="width:70px"
          onchange="window.app.state.workers = Math.max(1, Math.min(50, parseInt(this.value)||1)); this.value = window.app.state.workers; window.app.saveAllProviders();">
      </div>
    </div>
  `;

  const lastIdx = state.providerOrder.length - 1;
  const providersHtml = state.providerOrder.map((name, index) => {
    const isEnabled = state.providerEnabled[name];
    if (!state.providerFields[name]) state.providerFields[name] = {};
    const fields = state.providerFields[name];

    let fieldsHtml = '';
    if (name === 'opensubtitles') {
      fieldsHtml = `
        <div class="provider-field-row">
          <label>Username</label>
          <input type="text" value="${fields.username || ''}" oninput="window.app.state.providerFields['${name}'].username = this.value">
        </div>
        <div class="provider-field-row">
          <label>Password</label>
          <input type="password" placeholder="••••••••" oninput="window.app.state.providerFields['${name}'].password = this.value">
        </div>
        <div class="provider-field-row">
          <label>API Key</label>
          <input type="text" value="${fields.api_key || fields.openSubtitles_api_key || ''}" oninput="window.app.state.providerFields['${name}'].openSubtitles_api_key = this.value">
        </div>
      `;
    } else if (name === 'subdl') {
      fieldsHtml = `
        <div class="provider-field-row">
          <label>API Key</label>
          <input type="text" value="${fields.api_key || ''}" oninput="window.app.state.providerFields['${name}'].api_key = this.value">
        </div>
      `;
    } else if (name === 'wyzie') {
      fieldsHtml = `
        <div class="provider-field-row">
          <label>API Key</label>
          <input type="text" value="${fields.api_key || ''}" oninput="window.app.state.providerFields['${name}'].api_key = this.value">
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
          <span class="provider-name" style="cursor: pointer; display: flex; align-items: center;" onclick="this.closest('.provider-card').classList.toggle('expanded')">
            <span class="chevron" style="display:inline-block; width: 14px;">▸</span>
            ${name}
          </span>
          <div class="provider-actions">
            <button class="btn-sm" onclick="window.app.moveProvider('${name}', -1)" ${index === 0 ? 'disabled' : ''} title="Move up">↑</button>
            <button class="btn-sm" onclick="window.app.moveProvider('${name}',  1)" ${index === lastIdx ? 'disabled' : ''} title="Move down">↓</button>
            <button class="btn-sm btn-test" onclick="window.app.testProvider('${name}')">Test</button>
            <label class="provider-toggle-label">
              <input type="checkbox" ${isEnabled ? 'checked' : ''} onchange="window.app.state.providerEnabled['${name}'] = this.checked; window.app.saveAllProviders();">
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

  container.innerHTML = generalHtml + providersHtml;
}

export function moveProvider(name, dir) {
  const i = state.providerOrder.indexOf(name);
  const j = i + dir;
  if (j < 0 || j >= state.providerOrder.length) return;
  [state.providerOrder[i], state.providerOrder[j]] = [state.providerOrder[j], state.providerOrder[i]];
  renderSettings();
}

export async function saveAllProviders() {
  const payload = { provider_order: state.providerOrder, workers: state.workers };
  state.providerOrder.forEach(name => {
    payload[name] = { 
      ...state.providerFields[name], 
      enabled: state.providerEnabled[name] 
    };
  });

  try {
    await api.apiSaveSettings(payload);
    showToast("Settings saved successfully");
  } catch (err) {
    showToast("Failed to save settings", "error");
  }
}

export async function exportSettings() {
  const btn = document.getElementById('btn-export');
  btn.disabled = true;
  try {
    const blob = await api.apiExport();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'subtitle-fetcher-export.json';
    a.click();
    URL.revokeObjectURL(url);
  } catch (err) {
    showToast('Export failed', 'error');
  } finally {
    btn.disabled = false;
  }
}

export async function importSettings(file) {
  const resultEl = document.getElementById('import-result');
  resultEl.textContent = 'Importing…';
  try {
    const json = await file.text();
    const data = await api.apiImport(json);
    resultEl.textContent =
      `✓ Imported — ${data.settings_applied} settings, ${data.imdb_applied}/${data.imdb_total} IMDB IDs matched`;
    showToast('Import successful', 'success');
  } catch (err) {
    resultEl.textContent = `✗ ${err.message}`;
    showToast('Import failed', 'error');
  }
}

export async function testProvider(name) {
  const resultEl = document.getElementById(`test-result-${name}`);
  const btn = document.querySelector(`button[onclick="window.app.testProvider('${name}')"]`);

  if (resultEl) {
    const card = resultEl.closest('.provider-card');
    if (card && !card.classList.contains('expanded')) {
      card.classList.add('expanded');
    }
  }

  if (btn) { btn.disabled = true; btn.textContent = '...'; }
  if (resultEl) { resultEl.className = 'test-result'; resultEl.textContent = 'Testing…'; }

  try {
    const data = await api.apiTestProvider(name);

    if (!resultEl) return;
    if (!data.error) {
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
}
