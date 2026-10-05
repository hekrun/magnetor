const $ = (selector) => document.querySelector(selector);
const state = { torrents: [], filter: 'all', downloadLimit: 3 };

function formatBytes(value) {
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const power = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** power).toFixed(power ? 1 : 0)} ${units[power]}`;
}

function formatSearchSize(value) {
  const numeric = Number(value);
  return Number.isFinite(numeric) && numeric > 0 ? formatBytes(numeric) : (value || 'Size unknown');
}

function isVideoFile(path) {
  return /\.(mp4|m4v|mkv|avi|webm|flv|f4v|wmv|mov|mpeg|mpg|3gp|ogv)$/i.test(path);
}

function renderTorrentFiles(torrent) {
  const files = torrent.files || [];
  const zipLink = files.length > 1 && files.every((file) => file.complete)
    ? `<a class="zip-download" href="/download-zip/${encodeURIComponent(torrent.hash)}">Download all as ZIP</a>`
    : '';
  const fileRows = files.map((file) => `<div class="file-item"><span>${escapeHTML(file.path)}</span><small>${formatBytes(file.size)}</small>${file.complete ? `${isVideoFile(file.path) ? `<button class="media-button" data-play="/stream/${torrent.hash}/${encodeURIComponent(file.path)}" data-title="${escapeAttribute(file.path)}">Play</button><a class="media-button" href="/stream/${torrent.hash}/${encodeURIComponent(file.path)}" target="_blank" rel="noreferrer">Open</a>` : ''}<a class="download-button" href="/download/${torrent.hash}/${encodeURIComponent(file.path)}">Download</a>` : '<small class="file-pending">Downloading</small>'}</div>`).join('');
  return `${zipLink}${fileRows}`;
}

function loadFooterStorage() {
  fetch('/api/storage').then((response) => response.json()).then((storage) => {
    const label = storage.free ? `${formatBytes(storage.free)} free` : 'Storage unavailable';
    $('#footer-disk').textContent = label;
    $('#hero-storage').textContent = label;
  }).catch(() => { $('#footer-disk').textContent = 'Storage unavailable'; $('#hero-storage').textContent = 'Storage unavailable'; });
}

async function loadFooterProcessUsage() {
  try {
    const response = await fetch('/api/process');
    if (!response.ok) throw new Error('Process usage unavailable');
    const usage = await response.json();
    const cpu = `${Number(usage.cpuPercent || 0).toFixed(1)}%`;
    const memory = formatBytes(usage.memoryBytes);
    const network = usage.networkAvailable
      ? `↓${formatBytes(usage.receiveRate)}/s ↑${formatBytes(usage.transmitRate)}/s`
      : 'network unavailable';
    $('#footer-process-usage').textContent = `CPU ${cpu} / RAM ${memory} / NET ${network}`;
  } catch (_) {
    $('#footer-process-usage').textContent = 'Process usage unavailable';
  }
}

function renderTorrents() {
  const list = $('#torrent-list');
  const openFileLists = new Set([...list.querySelectorAll('.file-details[open]')].map((details) => details.dataset.hash));
  const queued = state.torrents.filter((torrent) => ['Queued', 'Fetching metadata'].includes(torrent.status));
  const downloading = state.torrents.filter((torrent) => torrent.status === 'Downloading');
  const complete = state.torrents.filter((torrent) => torrent.progress >= 100);
  const visible = state.filter === 'queued' ? queued
    : state.filter === 'downloading' ? downloading
      : state.filter === 'complete' ? complete : state.torrents;
  $('#library-count').textContent = state.torrents.length;
  $('#active-count-tab').textContent = downloading.length;
  $('#queue-count').textContent = queued.length;
  $('#complete-count').textContent = complete.length;
  const limitLabel = state.downloadLimit === 0 ? '∞' : state.downloadLimit;
  $('#active-summary').innerHTML = `${downloading.length} <small>/ ${limitLabel}</small>`;
  $('#active-meter').style.width = state.downloadLimit === 0 ? (downloading.length ? '100%' : '0%') : `${Math.min(100, downloading.length / state.downloadLimit * 100)}%`;
  $('#queue-summary').textContent = queued.length ? `${queued.length} waiting in queue` : 'Queue clear';
  if (!visible.length) {
    list.innerHTML = `<div class="empty-state"><div class="empty-icon">＋</div><h3>${state.torrents.length ? 'Nothing in this view' : 'No transfers yet'}</h3><p>${state.torrents.length ? 'There are no transfers in this filter.' : 'Add a magnet link or torrent file to begin.'}</p></div>`;
    return;
  }
  list.innerHTML = visible.map((torrent) => {
    const isQueued = ['Queued', 'Fetching metadata'].includes(torrent.status);
    const statusClass = torrent.status.toLowerCase().replace(/\s+/g, '-');
    const badge = torrent.progress >= 100 ? 'OK' : isQueued ? `Q${torrent.queuePosition || ''}` : 'DL';
    const queueControls = isQueued ? `<div class="queue-order"><button class="icon-button" data-action="queue-up" data-hash="${torrent.hash}" aria-label="Move up in queue" title="Move up" ${torrent.queuePosition <= 1 ? 'disabled' : ''}>↑</button><button class="icon-button" data-action="queue-down" data-hash="${torrent.hash}" aria-label="Move down in queue" title="Move down" ${torrent.queuePosition >= queued.length ? 'disabled' : ''}>↓</button></div>` : '';
    const taskControl = torrent.progress < 100 ? `<button class="control-button" data-action="${torrent.status === 'Paused' ? 'start' : 'stop'}" data-hash="${torrent.hash}">${torrent.status === 'Paused' ? 'Resume' : 'Pause'}</button>` : `<span class="seed-state">${torrent.status === 'Seeding' ? 'Seeding' : 'Complete'}</span>`;
    return `<article class="torrent-row status-${statusClass}">
    <div class="file-badge">${badge}</div>
    <div class="torrent-main"><div class="torrent-title">${escapeHTML(torrent.name)}</div><div class="torrent-meta">${formatBytes(torrent.downloaded)} of ${formatBytes(torrent.size)} <span class="meta-separator">/</span> ${torrent.peers} peers <span class="meta-separator">/</span> ↓ ${formatBytes(torrent.downloadSpeed || 0)}/s <span class="meta-separator">/</span> ↑ ${formatBytes(torrent.uploadSpeed || 0)}/s</div><div class="progress-track"><span style="width:${Math.min(torrent.progress, 100)}%"></span></div><details class="file-details" data-hash="${torrent.hash}" ${openFileLists.has(torrent.hash) ? 'open' : ''}><summary>View files (${(torrent.files || []).length})</summary><div class="file-list">${renderTorrentFiles(torrent)}</div></details></div>
    <div class="torrent-stat"><strong>${torrent.progress > 0 ? `${Math.round(torrent.progress)}%` : isQueued ? `#${torrent.queuePosition || '—'}` : '0%'}</strong><small class="task-status ${statusClass}">${escapeHTML(torrent.status)}</small>${isQueued && torrent.queueReason ? `<small class="queue-reason">${escapeHTML(torrent.queueReason)}</small>` : ''}</div>
    <div class="torrent-controls">${queueControls}${taskControl}<button class="row-action" data-delete="${torrent.hash}" title="Remove torrent and files" aria-label="Remove torrent and files">×</button></div>
  </article>`;
  }).join('');
}

function renderResults(results) {
  const target = $('#search-results');
  if (!results.length) { target.innerHTML = '<p class="search-empty">No results from the selected providers.</p>'; return; }
  target.innerHTML = results.map((result) => `<article class="result-row"><div class="result-source">${escapeHTML(result.source || 'Index')}</div><div class="result-title"><a href="${escapeAttribute(result.link || '#')}" target="_blank" rel="noreferrer">${escapeHTML(result.title)}</a></div><div class="result-meta">${escapeHTML(formatSearchSize(result.size))} ${result.seeds ? `<span class="seed-count">${result.seeds} seeds</span>` : ''}</div><a class="provider-link" href="${escapeAttribute(result.link || '#')}" target="_blank" rel="noreferrer" ${result.link ? '' : 'aria-disabled="true"'}>Open</a><button class="add-result" data-magnet="${escapeAttribute(result.magnet || '')}" ${result.magnet ? '' : 'disabled'}>Add</button></article>`).join('');
}

function escapeHTML(value) { return String(value || '').replace(/[&<>'"]/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[char])); }
function escapeAttribute(value) { return escapeHTML(value); }

function setupMenuSelects() {
  document.querySelectorAll('[data-menu-select]').forEach((menu) => {
    const valueField = document.getElementById(menu.dataset.menuSelect);
    const summary = menu.querySelector('summary');
    const label = menu.querySelector('[data-menu-label]');
    const choices = [...menu.querySelectorAll('[data-value]')];

    const syncState = () => summary.setAttribute('aria-expanded', String(menu.open));
    menu.addEventListener('toggle', syncState);
    menu.addEventListener('click', (event) => {
      const choice = event.target.closest('[data-value]');
      if (!choice) return;
      valueField.value = choice.dataset.value;
      label.textContent = choice.querySelector('strong').textContent;
      choices.forEach((item) => item.setAttribute('aria-pressed', String(item === choice)));
      menu.open = false;
      summary.focus();
    });
    menu.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') {
        menu.open = false;
        summary.focus();
        return;
      }
      if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
      const currentIndex = choices.indexOf(document.activeElement);
      const direction = event.key === 'ArrowDown' ? 1 : -1;
      const nextIndex = currentIndex < 0
        ? (direction > 0 ? 0 : choices.length - 1)
        : (currentIndex + direction + choices.length) % choices.length;
      event.preventDefault();
      choices[nextIndex].focus();
    });
    document.addEventListener('click', (event) => {
      if (!menu.contains(event.target)) menu.open = false;
    });
    choices.forEach((choice) => choice.setAttribute('aria-pressed', String(choice.dataset.value === valueField.value)));
    syncState();
  });
}

async function loadTorrents() {
  const response = await fetch('/api/torrents');
  if (!response.ok) throw new Error('Could not load library');
  state.torrents = await response.json();
  renderTorrents();
  $('#last-updated').textContent = `Updated ${new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`;
}

async function loadQueueSettings() {
  try {
    const response = await fetch('/api/settings');
    if (!response.ok) return;
    const data = await response.json();
    const limit = Number(data.settings?.maxDownloads);
    if (limit === 0 || (limit >= 2 && limit <= 5)) state.downloadLimit = limit;
    renderTorrents();
  } catch (_) {}
}

async function addMagnet(magnet) {
  const response = await fetch('/api/torrent', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ magnet, mode: $('#add-mode').value }) });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || 'Could not add torrent');
  $('#magnet').value = '';
  $('#add-message').textContent = $('#add-mode').value === 'direct' ? 'Added with direct-start priority.' : 'Torrent added to the queue.';
  await loadTorrents();
}

$('#add').addEventListener('click', async () => {
  const magnet = $('#magnet').value.trim();
  if (!magnet) return;
  $('#add-message').textContent = 'Adding torrent...';
  try { await addMagnet(magnet); } catch (error) { $('#add-message').textContent = error.message; }
});
$('#magnet').addEventListener('keydown', (event) => { if (event.key === 'Enter') $('#add').click(); });
$('#torrent-file').addEventListener('change', async (event) => {
  const file = event.target.files[0];
  if (!file) return;
  const form = new FormData();
  form.append('file', file);
  form.append('mode', $('#add-mode').value);
  $('#upload-message').textContent = 'Adding .torrent file...';
  try {
    const response = await fetch('/api/torrent-file', { method: 'POST', body: form });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || 'Could not add torrent file');
    $('#upload-message').textContent = 'Torrent file added.';
    await loadTorrents();
  } catch (error) { $('#upload-message').textContent = error.message; }
  event.target.value = '';
});
$('#refresh').addEventListener('click', () => loadTorrents().catch(() => {}));
document.querySelectorAll('.tab').forEach((tab) => tab.addEventListener('click', () => { document.querySelector('.tab.active').classList.remove('active'); tab.classList.add('active'); state.filter = tab.dataset.filter; renderTorrents(); }));
$('#torrent-list').addEventListener('click', async (event) => {
  const playButton = event.target.closest('[data-play]');
  const playURL = playButton?.dataset.play;
  if (playURL) {
    const player = $('#media-player');
    const video = $('#media-video');
    const fallback = $('#media-error');
    fallback.hidden = true;
    video.hidden = false;
    video.onerror = () => {
      video.hidden = true;
      fallback.hidden = false;
    };
    video.src = playURL;
    $('#media-title').textContent = playButton.dataset.title || 'Video';
    $('#media-download').href = playButton.dataset.download || playURL.replace('/stream/', '/download/');
    player.showModal();
    video.play().catch(() => {});
    return;
  }
  const action = event.target.dataset.action;
  const hash = event.target.dataset.hash;
  if (action && hash) {
    event.target.disabled = true;
    await fetch(`/api/torrent/${hash}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action }) });
    await loadTorrents();
    return;
  }
  const deleteHash = event.target.dataset.delete;
  if (!deleteHash || !confirm('Remove this torrent and delete its server files?')) return;
  await fetch(`/api/torrent/${deleteHash}`, { method: 'DELETE' });
  await loadTorrents();
});
$('#media-player').addEventListener('close', () => {
	clearPlayerMedia();
});

function clearPlayerMedia() {
  const video = $('#media-video');
  video.pause();
  video.onerror = null;
  video.removeAttribute('src');
  video.load();
  $('#media-error').hidden = true;
  video.hidden = false;
}

function resetMediaPlayer() {
  const player = $('#media-player');
  if (player.open) player.close();
  else player.removeAttribute('open');
  clearPlayerMedia();
}

$('#media-player').addEventListener('click', (event) => {
  if (event.target === $('#media-player')) $('#media-player').close();
});
$('#search-form').addEventListener('submit', async (event) => { event.preventDefault(); const query = $('#query').value.trim(); if (!query) return; $('#search-results').innerHTML = '<p class="search-empty">Searching providers...</p>'; try { const response = await fetch(`/api/search?q=${encodeURIComponent(query)}&provider=${$('#provider').value}`); const data = await response.json(); if (!response.ok) throw new Error(data.error); renderResults(data); } catch (error) { $('#search-results').innerHTML = `<p class="search-empty">${escapeHTML(error.message)}</p>`; } });
$('#search-results').addEventListener('click', async (event) => { const magnet = event.target.dataset.magnet; if (!magnet) return; event.target.textContent = 'Adding...'; try { await addMagnet(magnet); event.target.textContent = 'Added'; } catch (error) { event.target.textContent = 'Retry'; } });
setupMenuSelects();
loadQueueSettings();
loadTorrents().catch(() => { $('#last-updated').textContent = 'Engine unavailable'; });
loadFooterStorage();
loadFooterProcessUsage();
resetMediaPlayer();
window.addEventListener('pageshow', resetMediaPlayer);
setInterval(() => loadTorrents().catch(() => {}), 4000);
setInterval(loadFooterStorage, 15000);
setInterval(loadFooterProcessUsage, 4000);
