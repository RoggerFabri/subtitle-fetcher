import { state } from './state.js';
import * as actions from './components/actions.js';
import * as mediaList from './components/mediaList.js';
import * as settings from './components/settings.js';
import * as modals from './components/modals.js';

// Expose everything needed by inline HTML event handlers
window.app = {
  state,
  ...actions,
  ...mediaList,
  ...settings,
  ...modals
};

// Also expose showTab
window.app.showTab = function(name) {
  document.querySelectorAll('[id^="panel-"]').forEach(p => {
    p.classList.toggle('hidden', p.id !== `panel-${name}`);
  });
  document.querySelectorAll('.tab').forEach(t => {
    t.classList.toggle('active', t.id === `tab-${name}`);
  });
};

// Defensive: this app ships no service worker. Unregister any stale one left on
// this origin by a previous project (a common cause of `GET /sw.js 404` noise
// and of a stale SW intercepting/caching requests) and drop its caches.
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.getRegistrations()
    .then(regs => regs.forEach(r => r.unregister()))
    .catch(() => {});
  if (window.caches?.keys) {
    caches.keys().then(keys => keys.forEach(k => caches.delete(k))).catch(() => {});
  }
}

// Hot-reload. The server streams a per-process boot id, a 1s heartbeat, and an
// explicit "reload" on file changes. We reload when:
//   - the server asks ("reload"), or
//   - the boot id changes (server was restarted, e.g. `make serve`), or
//   - heartbeats stop for a few seconds (the socket died) — a watchdog forces a
//     reconnect, which then picks up the new boot id.
// The watchdog matters because a dropped SSE connection is not detected
// promptly by the browser, so onerror alone would leave the tab stale until a
// manual refresh.
(function () {
  let boot = null;
  let es = null;
  let watchdog = null;

  function armWatchdog() {
    clearTimeout(watchdog);
    watchdog = setTimeout(() => { try { es && es.close(); } catch {} connect(); }, 4000);
  }

  function connect() {
    es = new EventSource('/api/hot-reload');
    es.onmessage = (e) => {
      armWatchdog();
      if (e.data === 'reload') { location.reload(); return; }
      if (e.data === 'ping') return;
      // Anything else is a boot id.
      if (boot === null) boot = e.data;
      else if (e.data !== boot) location.reload();
    };
    es.onerror = () => { clearTimeout(watchdog); try { es.close(); } catch {} setTimeout(connect, 1000); };
  }
  connect();
})();

const FILTER_KEY = 'sf-filters';

function saveFilterState() {
  const filters = {};
  document.querySelectorAll('.pills[data-filter]').forEach(group => {
    filters[group.dataset.filter] = group.querySelector('.pill.active')?.dataset.value || '';
  });
  filters.search = document.getElementById('search')?.value || '';
  localStorage.setItem(FILTER_KEY, JSON.stringify(filters));
}

function restoreFilterState() {
  let saved;
  try { saved = JSON.parse(localStorage.getItem(FILTER_KEY)); } catch { return; }
  if (!saved) return;

  if (saved.search) {
    const searchEl = document.getElementById('search');
    const searchClear = document.getElementById('search-clear');
    if (searchEl) searchEl.value = saved.search;
    if (searchClear) searchClear.classList.toggle('hidden', !saved.search);
  }

  document.querySelectorAll('.pills[data-filter]').forEach(group => {
    const val = saved[group.dataset.filter];
    if (val == null) return;
    const target = group.querySelector(`.pill[data-value="${val}"]`);
    if (target) {
      group.querySelectorAll('.pill').forEach(p => p.classList.remove('active'));
      target.classList.add('active');
    }
  });
}

// Initialize event listeners
document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('btn-scan')?.addEventListener('click', () => typeof window.app.triggerScan === 'function' && window.app.triggerScan());
  document.getElementById('btn-auto-imdb')?.addEventListener('click', () => window.app.autoIMDB());
  document.getElementById('backfill-nfo-btn')?.addEventListener('click', () => window.app.backfillAllNfo());
  document.getElementById('btn-settings-gear')?.addEventListener('click', () => window.app.showTab('settings'));
  document.getElementById('tab-library')?.addEventListener('click', () => window.app.showTab('library'));
  document.getElementById('tab-settings')?.addEventListener('click', () => window.app.showTab('settings'));

  restoreFilterState();

  const searchEl = document.getElementById('search');
  const searchClear = document.getElementById('search-clear');
  searchEl?.addEventListener('input', () => {
    searchClear?.classList.toggle('hidden', !searchEl.value);
    saveFilterState();
    window.app.renderList();
  });
  searchEl?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); window.app.refreshData(); }
  });
  searchClear?.addEventListener('click', () => {
    searchEl.value = '';
    searchClear.classList.add('hidden');
    saveFilterState();
    window.app.renderList();
    searchEl.focus();
  });

  document.querySelectorAll('.pills').forEach(group => {
    group.addEventListener('click', e => {
      const pill = e.target.closest('.pill');
      if (!pill) return;
      group.querySelectorAll('.pill').forEach(p => p.classList.remove('active'));
      pill.classList.add('active');
      saveFilterState();
      window.app.renderList();
    });
  });
  document.querySelector('.save-all-btn')?.addEventListener('click', () => window.app.saveAllProviders());
  document.getElementById('btn-export')?.addEventListener('click', () => window.app.exportSettings());
  document.getElementById('import-file-input')?.addEventListener('change', e => {
    if (e.target.files[0]) window.app.importSettings(e.target.files[0]);
    e.target.value = '';
  });

  // Set initial view state
  window.app.showTab('library');

  // Initial data load
  window.app.refreshData();

  // Background monitor for poller-triggered scans
  window.app.startAutoScanMonitor();
});

document.getElementById('picker-modal')?.addEventListener('click', e => {
  if (e.target === e.currentTarget) window.app.closePicker();
});
document.getElementById('imdb-modal')?.addEventListener('click', e => {
  if (e.target === e.currentTarget) window.app.closeImdbPicker();
});
document.getElementById('preview-modal')?.addEventListener('click', e => {
  if (e.target === e.currentTarget) window.app.closeSubtitlePreview();
});
document.getElementById('nfo-modal')?.addEventListener('click', e => {
  if (e.target === e.currentTarget) window.app.closeNfo();
});

document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') {
    window.app.closePicker();
    window.app.closeImdbPicker();
    window.app.closeSubtitlePreview();
    window.app.closeNfo();
  }
  window.app.handlePickerKey(e);
  window.app.handleImdbKey(e);
  window.app.handleNfoKey(e);
  const modalOpen = !document.getElementById('picker-modal')?.classList.contains('hidden') ||
                    !document.getElementById('imdb-modal')?.classList.contains('hidden');
  if (e.key === '/' && !modalOpen && document.activeElement?.tagName !== 'INPUT' && document.activeElement?.tagName !== 'TEXTAREA') {
    e.preventDefault();
    document.getElementById('search')?.focus();
  }
});

// Since index.html has some window.something calls (e.g. window.onImdbSearchInput, window.closeImdbPicker)
// Let's bind them explicitly to window to prevent breaking existing HTML without changing it.
window.closeImdbPicker = window.app.closeImdbPicker;
window.onImdbSearchInput = window.app.onImdbSearchInput;
window.closePicker = window.app.closePicker;
window.onPickerSearchInput = window.app.onPickerSearchInput;
