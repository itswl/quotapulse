// Runs before the first paint, from its own tiny file in <head>: it decides
// light or dark so the page never flashes the wrong one. Keep it free of
// imports — it is not part of the app bundle. src/theme.ts takes over once
// the app has loaded, and must agree with what is decided here.
(() => {
  let choice: string | null = null;
  try {
    choice = localStorage.getItem('qp.theme');
  } catch {
    // Storage can be blocked; the system preference still applies.
  }
  const dark = choice === 'dark' || (choice !== 'light' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? '#0e0e0d' : '#f5f4f0');
})();
