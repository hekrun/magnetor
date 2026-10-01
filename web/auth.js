const logoutButton = document.querySelector('#logout');
if (logoutButton) {
  logoutButton.addEventListener('click', async () => {
    logoutButton.disabled = true;
    try {
      await fetch('/api/auth/logout', { method: 'POST' });
    } finally {
      window.location.replace('/login.html');
    }
  });
}

fetch('/api/auth/status').then(async (response) => {
  if (!response.ok) return;
  const state = await response.json();
  const username = document.querySelector('#auth-user');
  if (username && state.username) username.textContent = state.username;
}).catch(() => {});
