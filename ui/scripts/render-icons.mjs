// Renders the home-screen icons in ui/www/icons from the brand mark.
//
// The mark is a coin (the balance) with a heartbeat (the monitoring) cut through it,
// on the 24-unit icon grid. Keep COIN_R and PULSE in sync with the i-logo symbol and the
// favicon in ui/index.html.
//
// Needs a local Chrome and puppeteer-core, which is deliberately not a dependency:
//   npm --prefix ui install --no-save puppeteer-core
//   node ui/scripts/render-icons.mjs
// Set CHROME_PATH if Chrome is not in the default macOS location.

import { mkdir } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export const COIN_R = 8.8;
export const PULSE = 'M1.5 12.2H8.55L10.45 7.6L13.55 16.9L15.45 12.2H22.5';
const CUT_WIDTH = 2.3;

const OUT = resolve(dirname(fileURLToPath(import.meta.url)), '../www/icons');
const CHROME = process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

// rounded: transparent corners (install icons); full-bleed otherwise (iOS and Android
// apply their own mask). glyph: share of the tile the 24-unit glyph box occupies.
const variants = [
  { file: 'apple-touch-icon.png', size: 180, rounded: false, glyph: 0.68 },
  { file: 'icon-192.png', size: 192, rounded: true, glyph: 0.68 },
  { file: 'icon-512.png', size: 512, rounded: true, glyph: 0.68 },
  // Android's maskable safe zone is the central 80% circle.
  { file: 'icon-maskable-512.png', size: 512, rounded: false, glyph: 0.58 },
];

export function appIconSVG({ size, rounded, glyph }) {
  const box = size * glyph;
  const offset = (size - box) / 2;
  const radius = rounded ? size * 0.2237 : 0;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" width="${size}" height="${size}">
  <defs>
    <linearGradient id="tile" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#1f232b"/><stop offset="1" stop-color="#0d0f12"/></linearGradient>
    <radialGradient id="glow" cx="0.5" cy="0.5" r="0.46"><stop offset="0" stop-color="#7b98f7" stop-opacity="0.3"/><stop offset="1" stop-color="#7b98f7" stop-opacity="0"/></radialGradient>
    <linearGradient id="coin" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#a3b8fc"/><stop offset="1" stop-color="#5878ef"/></linearGradient>
    <mask id="cut"><circle cx="12" cy="12" r="${COIN_R}" fill="#fff"/><path d="${PULSE}" fill="none" stroke="#000" stroke-width="${CUT_WIDTH}" stroke-linecap="round" stroke-linejoin="round"/></mask>
  </defs>
  <rect width="${size}" height="${size}" rx="${radius}" fill="url(#tile)"/>
  <rect width="${size}" height="${size}" rx="${radius}" fill="url(#glow)"/>
  <g transform="translate(${offset} ${offset}) scale(${box / 24})"><circle cx="12" cy="12" r="${COIN_R}" fill="url(#coin)" mask="url(#cut)"/></g>
</svg>`;
}

async function main() {
  let puppeteer;
  try {
    ({ default: puppeteer } = await import('puppeteer-core'));
  } catch {
    console.error('puppeteer-core is missing: npm --prefix ui install --no-save puppeteer-core');
    process.exit(1);
  }
  await mkdir(OUT, { recursive: true });
  const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
  try {
    const page = await browser.newPage();
    for (const v of variants) {
      await page.setViewport({ width: v.size, height: v.size, deviceScaleFactor: 1 });
      await page.setContent(`<!doctype html><body style="margin:0">${appIconSVG(v)}</body>`);
      await page.screenshot({ path: join(OUT, v.file), omitBackground: true, clip: { x: 0, y: 0, width: v.size, height: v.size } });
      console.log(`rendered ${v.file}`);
    }
  } finally {
    await browser.close();
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  await main();
}
