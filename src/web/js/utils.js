export function showToast(msg, type = "success") {
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

export function setFetching(btn, on) {
  if (!btn) return;
  btn.disabled = on;
  btn.classList.toggle('fetching', on);
}

export function getStatusColor(status) {
  switch (status) {
    case 'complete': return 'green';
    case 'partial': return 'yellow';
    case 'missing': return 'red';
    default: return 'gray';
  }
}
