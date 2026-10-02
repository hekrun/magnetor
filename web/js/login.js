const field = (selector) => document.querySelector(selector);
let setupRequired = false;

async function loadAuthState() {
  const response = await fetch('/api/auth/status');
  if (!response.ok) throw new Error('Could not check account status');
  const state = await response.json();
  if (state.authenticated) {
    window.location.replace('/');
    return;
  }
  setupRequired = state.setupRequired;
  if (setupRequired) {
    field('#auth-eyebrow').textContent = 'FIRST-RUN SETUP';
    field('#auth-title').textContent = 'Create your account';
    field('#auth-description').textContent = 'Create the only account for this instance. Registration closes after this account is created.';
    field('#auth-submit').innerHTML = 'Create account <span>-></span>';
    field('#password-hint').hidden = false;
    field('#password').autocomplete = 'new-password';
  }
}

field('#auth-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const button = field('#auth-submit');
  const message = field('#auth-message');
  button.disabled = true;
  message.textContent = setupRequired ? 'Creating account...' : 'Signing in...';
  try {
    const response = await fetch(setupRequired ? '/api/auth/register' : '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: field('#username').value.trim(), password: field('#password').value }),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || 'Authentication failed');
    window.location.replace('/');
  } catch (error) {
    message.textContent = error.message;
    button.disabled = false;
  }
});

loadAuthState().catch((error) => { field('#auth-message').textContent = error.message; });
