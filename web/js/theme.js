/* Theme engine: applies the saved/preferred theme and wires up any
   [data-theme-toggle] buttons. The attribute itself is already set by the
   inline bootstrap snippet in <head> (before first paint) to avoid flashes;
   this file only keeps it in sync with user interaction and other tabs. */
(function () {
  var STORAGE_KEY = 'ctd-theme';
  var root = document.documentElement;

  function currentTheme() {
    return root.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
  }

  function applyTheme(theme) {
    root.setAttribute('data-theme', theme);
    document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
      button.setAttribute('aria-pressed', theme === 'dark' ? 'true' : 'false');
      button.setAttribute('title', theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode');
    });
  }

  function toggleTheme() {
    var next = currentTheme() === 'dark' ? 'light' : 'dark';
    localStorage.setItem(STORAGE_KEY, next);
    applyTheme(next);
  }

  document.addEventListener('DOMContentLoaded', function () {
    applyTheme(currentTheme());
    document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
      button.addEventListener('click', toggleTheme);
    });
  });

  window.addEventListener('storage', function (event) {
    if (event.key === STORAGE_KEY && event.newValue) applyTheme(event.newValue);
  });
})();
