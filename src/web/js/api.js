export async function apiGetSettings() {
  const res = await fetch('/api/settings');
  if (!res.ok) throw new Error("Failed to load settings");
  return res.json();
}

export async function apiGetReport() {
  const res = await fetch('/api/report');
  if (!res.ok) throw new Error("Failed to load report");
  return res.json();
}

export async function apiPostScan() {
  const res = await fetch('/api/scan', { method: 'POST' });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || "Scan failed to start");
  }
  return res;
}

export async function apiGetScanStatus() {
  const res = await fetch('/api/scan/status');
  if (!res.ok) throw new Error("Failed to get scan status");
  return res.json();
}

export async function apiFetchMedia(id) {
  const res = await fetch(`/api/fetch/media/${id}`, { method: 'POST' });
  if (!res.ok) throw new Error("Failed to fetch media");
  return res.json();
}

export async function apiFetchSeason(id, season) {
  const res = await fetch(`/api/fetch/season/${id}/${season}`, { method: 'POST' });
  if (!res.ok) throw new Error("Failed to fetch season");
  return res.json();
}

export async function apiFetchFile(id) {
  const res = await fetch(`/api/fetch/file/${id}`, { method: 'POST' });
  if (!res.ok) throw new Error("Failed to fetch file");
  return res.json();
}

export async function apiDeleteSubtitle(id) {
  const res = await fetch(`/api/subtitle/${id}`, { method: 'DELETE' });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || "Delete failed");
  return data;
}

export async function apiSearchFile(fileId, query = "") {
  let url = `/api/search/file/${fileId}`;
  if (query) {
    url += `?q=${encodeURIComponent(query)}`;
  }
  const res = await fetch(url, { method: 'POST' });
  if (!res.ok) throw new Error("Search failed");
  return res.json();
}

export async function apiDownloadFile(fileId, token) {
  const res = await fetch(`/api/download/file/${fileId}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token })
  });
  return res.json();
}

export async function apiAutoIMDB() {
  const res = await fetch('/api/imdb/auto', { method: 'POST' });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || 'Auto IMDB failed');
  }
  return res.json();
}

export async function apiAutoIMDBStatus() {
  const res = await fetch('/api/imdb/auto/status');
  if (!res.ok) throw new Error('Failed to get status');
  return res.json();
}

export async function apiImdbSearch(val) {
  const res = await fetch(`/api/imdb/search?q=${encodeURIComponent(val)}`);
  if (!res.ok) throw new Error("Search failed");
  return res.json();
}

export async function apiSetImdbId(mediaId, imdbId) {
  const res = await fetch(`/api/media/${mediaId}/imdb`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ imdb_id: imdbId })
  });
  return res.json();
}

export async function apiSaveSettings(payload) {
  const res = await fetch('/api/settings', { 
    method: 'POST', 
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload) 
  });
  if (!res.ok) throw new Error("Failed to save settings");
}

export async function apiExport() {
  const res = await fetch('/api/export');
  if (!res.ok) throw new Error("Export failed");
  return res.blob();
}

export async function apiImport(json) {
  const res = await fetch('/api/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: json
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || "Import failed");
  return data;
}

export async function apiTestProvider(name) {
  const res = await fetch(`/api/health/provider-test?provider=${name}`, { method: 'POST' });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    return { error: data.error || 'Connection failed' };
  }
  return res.json();
}
