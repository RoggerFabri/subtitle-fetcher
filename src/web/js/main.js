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

// Hot-reload: listen for server-sent file-change events and reload the page.
(function () {
  function connect() {
    const es = new EventSource('/api/hot-reload');
    es.onmessage = () => location.reload();
    es.onerror = () => { es.close(); setTimeout(connect, 2000); };
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
});

document.getElementById('picker-modal')?.addEventListener('click', e => {
  if (e.target === e.currentTarget) window.app.closePicker();
});
document.getElementById('imdb-modal')?.addEventListener('click', e => {
  if (e.target === e.currentTarget) window.app.closeImdbPicker();
});

document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') {
    window.app.closePicker();
    window.app.closeImdbPicker();
  }
  window.app.handlePickerKey(e);
  window.app.handleImdbKey(e);
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
