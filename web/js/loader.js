/* Splash/loading screen shown once per browser session. Runs inline (placed
   right after the loader markup) so repeat views are removed instantly with
   no flash before the rest of the page is even parsed. */
(function () {
  var SESSION_KEY = 'ctd-loaded';
  var loader = document.getElementById('app-loader');
  if (!loader) return;

  if (sessionStorage.getItem(SESSION_KEY)) {
    loader.remove();
    return;
  }

  var MIN_VISIBLE_MS = 650;
  var shownAt = Date.now();

  function hide() {
    var remaining = MIN_VISIBLE_MS - (Date.now() - shownAt);
    setTimeout(function () {
      loader.classList.add('is-hidden');
      sessionStorage.setItem(SESSION_KEY, '1');
      loader.addEventListener('transitionend', function () { loader.remove(); }, { once: true });
      setTimeout(function () { loader.remove(); }, 700);
    }, Math.max(0, remaining));
  }

  if (document.readyState === 'complete') hide();
  else window.addEventListener('load', hide);
})();
