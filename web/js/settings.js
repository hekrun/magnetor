const field = (selector) => document.querySelector(selector);

async function loadSettings() {
  const response = await fetch('/api/settings');
  if (!response.ok) throw new Error('Could not load settings');
  const data = await response.json();
  const settings = data.settings || data;
  const storage = data.storage || {};
  field('#download-path').value = settings.downloadPath || '';
  field('#download-limit').value = String(settings.maxDownloads ?? 3);
  field('#upload-enabled').checked = Boolean(settings.upload);
  field('#seeding-enabled').checked = Boolean(settings.seeding);
  field('#disk-total').textContent = formatBytes(storage.total);
  field('#disk-used').textContent = formatBytes(storage.used);
  field('#disk-free').textContent = formatBytes(storage.free);
  const percent = storage.total ? Math.min(100, storage.used / storage.total * 100) : 0;
  field('#disk-bar').style.width = `${percent}%`;
}

async function loadProcessUsage() {
  try {
    const response = await fetch('/api/process');
    if (!response.ok) throw new Error('Process usage unavailable');
    const usage = await response.json();
    field('#process-cpu').textContent = `${Number(usage.cpuPercent || 0).toFixed(1)}%`;
    field('#process-memory').textContent = formatBytes(usage.memoryBytes);
    field('#process-rx').textContent = usage.networkAvailable ? `${formatBytes(usage.receiveRate)}/s` : 'Unavailable';
    field('#process-tx').textContent = usage.networkAvailable ? `${formatBytes(usage.transmitRate)}/s` : 'Unavailable';
    field('#process-updated').textContent = `Updated ${new Date(usage.updatedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}`;
  } catch (_) {
    field('#process-updated').textContent = 'Usage unavailable';
  }
}

function formatBytes(value) {
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const power = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** power).toFixed(power ? 1 : 0)} ${units[power]}`;
}

field('#settings-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const message = field('#settings-message');
  message.textContent = 'Saving settings...';
  try {
    const response = await fetch('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        downloadPath: field('#download-path').value.trim(),
        maxDownloads: Number(field('#download-limit').value),
        upload: field('#upload-enabled').checked,
        seeding: field('#seeding-enabled').checked,
      }),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || 'Could not save settings');
    message.textContent = data.restartRequired ? 'Saved. Restart the server to apply these engine settings.' : 'Settings saved.';
  } catch (error) {
    message.textContent = error.message;
  }
});

document.querySelectorAll('[data-settings-tab]').forEach((tab) => tab.addEventListener('click', () => {
  const panel = document.getElementById(tab.getAttribute('aria-controls'));
  document.querySelectorAll('[data-settings-tab]').forEach((item) => {
    const active = item === tab;
    item.classList.toggle('active', active);
    item.setAttribute('aria-selected', String(active));
  });
  document.querySelectorAll('[role="tabpanel"]').forEach((item) => { item.hidden = item !== panel; });
  if (panel.id === 'process-panel') loadProcessUsage();
}));

function updateLayoutSelection() {
  const layout = document.documentElement.getAttribute('data-layout') || 'orbit';
  document.querySelectorAll('[data-site-layout]').forEach((button) => {
    const selected = button.dataset.siteLayout === layout;
    button.classList.toggle('active', selected);
    button.setAttribute('aria-pressed', String(selected));
  });
}

document.querySelectorAll('[data-site-layout]').forEach((button) => button.addEventListener('click', () => {
  const layout = button.dataset.siteLayout;
  document.documentElement.setAttribute('data-layout', layout);
  localStorage.setItem('magnetor-layout', layout);
  updateLayoutSelection();
}));
window.addEventListener('storage', (event) => {
  if (event.key === 'magnetor-layout') updateLayoutSelection();
});

loadSettings().catch((error) => { field('#settings-message').textContent = error.message; });
updateLayoutSelection();
setInterval(() => {
  if (!field('#process-panel').hidden) loadProcessUsage();
}, 2500);
