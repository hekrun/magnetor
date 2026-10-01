const profileField = (selector) => document.querySelector(selector);

async function loadProfile() {
  const response = await fetch('/api/profile');
  const profile = await response.json();
  if (!response.ok) throw new Error(profile.error || 'Could not load profile');
  profileField('#profile-username').value = profile.username || '';
}

profileField('#profile-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const message = profileField('#profile-message');
  message.textContent = 'Saving profile...';
  try {
    const response = await fetch('/api/profile', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username: profileField('#profile-username').value.trim(),
        currentPassword: profileField('#profile-current-password').value,
      }),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || 'Could not save profile');
    message.textContent = 'Profile updated.';
    profileField('#profile-current-password').value = '';
    const username = document.querySelector('#auth-user');
    if (username) username.textContent = data.username;
  } catch (error) {
    message.textContent = error.message;
  }
});

profileField('#password-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const message = profileField('#password-message');
  message.textContent = 'Updating password...';
  try {
    const response = await fetch('/api/auth/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        currentPassword: profileField('#password-current').value,
        newPassword: profileField('#password-new').value,
      }),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || 'Could not change password');
    message.textContent = 'Password changed. Please sign in again.';
    window.location.replace('/login.html');
  } catch (error) {
    message.textContent = error.message;
  }
});

loadProfile().catch((error) => { profileField('#profile-message').textContent = error.message; });
