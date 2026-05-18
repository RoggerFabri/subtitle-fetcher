export function showToast(msg, type = "success") {
  const container = document.getElementById('toast-container');
  if (!container) return;
  const t = document.createElement('div');
  t.className = `toast ${type}`;

  const text = document.createElement('span');
  text.textContent = msg;

  const close = document.createElement('button');
  close.className = 'toast-close';
  close.textContent = '✕';
  close.addEventListener('click', () => {
    t.classList.remove('show');
    setTimeout(() => t.remove(), 250);
  });

  t.appendChild(text);
  t.appendChild(close);
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
  if (on) {
    btn.dataset.label = btn.textContent;
    btn.textContent = '…';
  } else if (btn.dataset.label) {
    btn.textContent = btn.dataset.label;
    delete btn.dataset.label;
  }
}

export function getStatusColor(status) {
  switch (status) {
    case 'complete': return 'green';
    case 'partial': return 'yellow';
    case 'missing': return 'red';
    default: return 'gray';
  }
}
