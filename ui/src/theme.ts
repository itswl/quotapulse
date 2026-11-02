/**
 * Theme and locale bootstrap, loaded synchronously in <head> before the
 * stylesheet-heavy body renders. It sets data-theme and lang on <html> so a
 * dark-mode or Chinese visitor never sees a light/English flash on open.
 *
 * This must stay its own entry point (not part of app.js): app.js is deferred,
 * and the attributes have to exist before first paint. Keeping it out of
 * index.html also lets the server send a strict CSP with script-src 'self'
 * and no 'unsafe-inline'.
 */
(function boot(): void {
  try {
    let theme = localStorage.getItem('theme');
    if (theme !== 'dark' && theme !== 'light') {
      theme = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    document.documentElement.setAttribute('data-theme', theme);
    let locale = localStorage.getItem('locale');
    if (locale !== 'en' && locale !== 'zh-CN') {
      locale = /^zh(?:[-_]|$)/i.test(navigator.language || '') ? 'zh-CN' : 'en';
    }
    document.documentElement.lang = locale;
  } catch {
    /* storage unavailable: fall back to the stylesheet default */
  }
})();
