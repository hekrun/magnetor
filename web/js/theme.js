(function () {
  var MODE_KEY = 'ctd-theme';
  var LAYOUT_KEY = 'magnetor-layout';
  var LAYOUTS = ['orbit', 'atlas', 'dock'];
  var root = document.documentElement;

  function validMode(value) {
    return value === 'light' || value === 'dark' ? value : 'dark';
  }

  function validLayout(value) {
    return LAYOUTS.indexOf(value) >= 0 ? value : 'orbit';
  }

  function applyMode(mode, persist) {
    mode = validMode(mode);
    root.setAttribute('data-theme', mode);
    if (persist) localStorage.setItem(MODE_KEY, mode);
    document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
      button.setAttribute('aria-pressed', mode === 'dark' ? 'true' : 'false');
      button.setAttribute('title', mode === 'dark' ? 'Switch to light mode' : 'Switch to dark mode');
    });
    var themeColor = document.querySelector('meta[name="theme-color"]');
    if (themeColor) themeColor.content = getComputedStyle(root).getPropertyValue('--bg').trim();
  }

  function toggleTheme() {
    applyMode(root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark', true);
  }

  function applyLayout(layout, persist) {
    layout = validLayout(layout);
    root.setAttribute('data-layout', layout);
    if (persist) localStorage.setItem(LAYOUT_KEY, layout);
  }

  document.addEventListener('DOMContentLoaded', function () {
    applyMode(root.getAttribute('data-theme'), false);
    applyLayout(root.getAttribute('data-layout'), false);
    document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
      button.addEventListener('click', toggleTheme);
    });
  });

  window.addEventListener('storage', function (event) {
    if (event.key === MODE_KEY && event.newValue) applyMode(event.newValue, false);
    if (event.key === LAYOUT_KEY && event.newValue) applyLayout(event.newValue, false);
  });
})();
