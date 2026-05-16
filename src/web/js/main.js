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

// Initialize event listeners
document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('btn-scan')?.addEventListener('click', () => typeof window.app.triggerScan === 'function' && window.app.triggerScan());
  document.getElementById('btn-settings-gear')?.addEventListener('click', () => window.app.showTab('settings'));
  document.getElementById('tab-library')?.addEventListener('click', () => window.app.showTab('library'));
  document.getElementById('tab-settings')?.addEventListener('click', () => window.app.showTab('settings'));
  
  document.getElementById('search')?.addEventListener('input', window.app.renderList);
  document.getElementById('search')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      window.app.refreshData();
    }
  });
  
  document.getElementById('filter-type')?.addEventListener('change', window.app.renderList);
  document.getElementById('filter-status')?.addEventListener('change', window.app.renderList);
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

document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') {
    window.app.closePicker();
    window.app.closeImdbPicker();
  }
});

// Since index.html has some window.something calls (e.g. window.onImdbSearchInput, window.closeImdbPicker)
// Let's bind them explicitly to window to prevent breaking existing HTML without changing it.
window.closeImdbPicker = window.app.closeImdbPicker;
window.onImdbSearchInput = window.app.onImdbSearchInput;
window.closePicker = window.app.closePicker;
window.onPickerSearchInput = window.app.onPickerSearchInput;
