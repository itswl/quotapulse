// Renders the screenshots in docs/images from the built dashboard (ui/dist) and the demo
// data in ./docs/fixtures.ts, which follows the server's own rules on a pinned clock.
//
// Needs Node 23.6+ (it imports the TypeScript fixtures directly), a local Chrome, and
// puppeteer-core, which is deliberately not a dependency:
//   npm --prefix ui run build
//   npm --prefix ui install --no-save puppeteer-core
//   node ui/scripts/docs-screenshots.mjs                 # all shots
//   node ui/scripts/docs-screenshots.mjs mobile email-scanning
//   node ui/scripts/docs-screenshots.mjs --serve         # just the demo, on :8123
// Set CHROME_PATH if Chrome is not in the default macOS location.
//
// The run fails on any page error or any API call the demo doesn't answer, so a
// dashboard change that needs new data shows up here instead of in a stale picture.

import http from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { dirname, extname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import * as demo from './docs/fixtures.ts';

const UI = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const DIST = join(UI, 'dist');
const OUT = resolve(UI, '../docs/images');
const PORT = Number(process.env.PORT || 8123);
const BASE = `http://127.0.0.1:${PORT}`;
const CHROME = process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

// ==================== Demo server ====================

// normal | empty (a fresh install after its first check) | down (the server is unreachable)
let mode = 'normal';
const unanswered = new Set();

const TYPES = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'text/javascript', '.woff2': 'font/woff2',
  '.png': 'image/png', '.svg': 'image/svg+xml', '.txt': 'text/plain', '.webmanifest': 'application/manifest+json',
};

function json(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(body));
}

const list = (data) => ({ status: 'success', count: data.length, data });

function api(path, empty) {
  if (path === '/api/features') return demo.features();
  if (path === '/api/credits') return demo.credits(empty);
  if (path === '/api/subscriptions') return demo.subscriptions(empty);
  if (path === '/api/config/subscriptions') return { status: 'success', subscriptions: demo.subscriptionsConfig(empty) };
  if (path === '/api/config/projects') return { status: 'success', projects: demo.projectsConfig(empty) };
  if (path === '/api/config/emails') return { status: 'success', emails: demo.mailboxes(empty) };
  if (path === '/api/providers') return demo.providers();
  if (path === '/api/email/scan') return demo.emailScan(empty);
  if (path === '/api/history/email-alerts') return list(demo.emailHistory(empty));
  if (path === '/api/email/suppressions') return list(demo.suppressions(empty));
  if (path === '/api/jobs') return demo.jobs();
  if (path === '/health') return demo.health('demo');
  if (path.startsWith('/api/history/trend/')) return demo.trend(decodeURIComponent(path.slice('/api/history/trend/'.length)));
  return undefined;
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, BASE);
  if (url.pathname === '/__mode') {
    mode = url.searchParams.get('m') || 'normal';
    return json(res, 200, { mode });
  }
  if (url.pathname.startsWith('/api/') || url.pathname === '/health') {
    if (mode === 'down') return req.socket.destroy(); // what a browser sees when nothing answers
    const body = api(url.pathname, mode === 'empty');
    if (body === undefined) {
      unanswered.add(`${req.method} ${url.pathname}`);
      return json(res, 404, { status: 'error', message: `The demo has no data for ${url.pathname}` });
    }
    if (body === null) return json(res, 404, { status: 'error', message: 'No balance history for this project' });
    return json(res, 200, body);
  }
  let file = join(DIST, url.pathname === '/' ? 'index.html' : url.pathname);
  if (!file.startsWith(DIST)) return json(res, 403, { status: 'error' });
  try {
    await stat(file);
  } catch {
    file = join(DIST, 'index.html');
  }
  res.writeHead(200, { 'Content-Type': TYPES[extname(file)] || 'application/octet-stream' });
  res.end(await readFile(file));
});

// ==================== Shots ====================

// Name, theme, and how to get there. Themes alternate the way the overview reads.
const SHOTS = {
  'dashboard-light': { theme: 'light', full: true },
  'dashboard-dark': { theme: 'dark', full: true },
  'project-list': { theme: 'light', full: true, list: true },
  'dashboard-zh-CN': { theme: 'light', full: true, locale: 'zh-CN' },
  subscriptions: { theme: 'dark', full: true, view: '#view-subscriptions-btn' },
  'email-scanning': { theme: 'light', full: true, view: '#view-email-btn' },
  'trend-modal': { theme: 'dark', run: openTrend },
  // Opened from the keyboard, so the focus ring shows where the dialog puts focus: Cancel.
  'delete-confirm': { theme: 'light', run: async (page) => { await page.focus('.js-delete-project'); await page.keyboard.press('Enter'); } },
  'empty-state': { theme: 'light', full: true, mode: 'empty' },
  'load-error': { theme: 'light', mode: 'down', waitFor: '.empty-state.error' },
  mobile: { theme: 'dark', full: true, phone: true },
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function openTrend(page) {
  await page.click('.js-show-trend[data-project="deepseek-prod"]');
  await page.waitForSelector('#trend-modal.active');
  await sleep(600);
  // Hover a point a few days back so the tooltip shows, as it would for a reader.
  const box = await (await page.$('#trend-chart')).boundingBox();
  await page.mouse.move(box.x + box.width * 0.72, box.y + box.height * 0.45);
}

async function shoot(browser, name, spec) {
  const page = await browser.newPage();
  const problems = [];
  page.on('pageerror', (e) => problems.push(`page error: ${e.message}`));
  page.on('console', (m) => m.type() === 'error' && spec.mode !== 'down' && problems.push(`console: ${m.text()}`));
  await page.setBypassServiceWorker(true);
  await page.emulateTimezone(demo.TIMEZONE);
  const viewport = spec.phone
    ? { width: 390, height: 844, deviceScaleFactor: 1, isMobile: true, hasTouch: true }
    : { width: 1440, height: 960, deviceScaleFactor: 1 };
  await page.setViewport(viewport);
  await page.evaluateOnNewDocument(
    (now, theme, list, locale) => {
      // Pin the clock without freezing it: relative times and animations keep working.
      const offset = now - Date.now();
      const RealDate = Date;
      class PinnedDate extends RealDate {
        constructor(...args) {
          if (args.length === 0) super(RealDate.now() + offset);
          else super(...args);
        }
        static now() {
          return RealDate.now() + offset;
        }
      }
      globalThis.Date = PinnedDate;
      localStorage.setItem('apiKey', 'demo');
      localStorage.setItem('theme', theme);
      localStorage.setItem('projectViewStyle', list ? 'list' : 'grid');
      // Pin the language too: headless Chrome inherits the machine's, which would turn
      // the English shots Chinese on a Chinese system.
      localStorage.setItem('locale', locale);
    },
    demo.NOW,
    spec.theme,
    Boolean(spec.list),
    spec.locale || 'en',
  );

  await fetch(`${BASE}/__mode?m=${spec.mode || 'normal'}`);
  await page.goto(BASE, { waitUntil: 'networkidle0' });
  await page.waitForSelector(spec.waitFor || (spec.mode ? '.empty-state' : '.project-card'));
  if (spec.view) {
    await page.click(spec.view);
    await sleep(500);
  }
  await sleep(700); // fonts, the card cascade, the band
  if (spec.run) {
    await spec.run(page);
    await sleep(450);
  }

  // Grow the viewport to the page instead of capturing beyond it: fixed layers such as the
  // grain overlay only cover the viewport, which would leave a seam.
  if (spec.full) {
    for (let i = 0; i < 2; i += 1) {
      const height = await page.evaluate(() => document.documentElement.scrollHeight);
      await page.setViewport({ ...viewport, height: Math.max(viewport.height, height) });
      await sleep(250);
    }
  }
  await page.screenshot({ path: join(OUT, `${name}.png`) });
  await page.close();
  return problems;
}

// ==================== Main ====================

await new Promise((ok) => server.listen(PORT, '127.0.0.1', ok));
if (process.argv.includes('--serve')) {
  console.log(`demo on ${BASE}  (switch with ${BASE}/__mode?m=normal|empty|down)`);
} else {
  let puppeteer;
  try {
    ({ default: puppeteer } = await import('puppeteer-core'));
  } catch {
    console.error('puppeteer-core is missing: npm --prefix ui install --no-save puppeteer-core');
    process.exit(1);
  }
  const wanted = process.argv.slice(2).filter((a) => !a.startsWith('--'));
  const names = wanted.length ? wanted : Object.keys(SHOTS);
  const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--hide-scrollbars'] });
  let failed = false;
  try {
    for (const name of names) {
      if (!SHOTS[name]) throw new Error(`unknown shot ${name}; choose from ${Object.keys(SHOTS).join(', ')}`);
      const problems = await shoot(browser, name, SHOTS[name]);
      console.log(problems.length ? `✗ ${name}` : `✓ ${name}`);
      for (const p of problems) console.log(`    ${p}`);
      failed ||= problems.length > 0;
    }
  } finally {
    await browser.close();
    server.close();
  }
  for (const call of unanswered) console.log(`✗ the demo does not answer ${call}`);
  if (failed || unanswered.size) process.exit(1);
}
