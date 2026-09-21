// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.

import { createHash } from 'node:crypto';
import { copyFile, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, context } from 'esbuild';

const root = dirname(fileURLToPath(import.meta.url));
const outdir = resolve(root, 'dist');
const staticDir = join(outdir, 'static');
const testOutdir = resolve(root, 'dist-test');

const watch = process.argv.includes('--watch');
const testOnly = process.argv.includes('--test');

/** @type {import('esbuild').BuildOptions} */
const shared = {
  bundle: true,
  format: 'iife',
  target: ['es2022', 'chrome109', 'firefox115', 'safari16'],
  charset: 'utf8',
  logLevel: 'info',
  absWorkingDir: root,
};

/**
 * Self-hosted web fonts. The CSS references the woff2 files inside the `geist` npm package;
 * esbuild copies them next to app.css so the dashboard never loads fonts from a third-party CDN.
 */
const fontAssets = {
  loader: { '.woff2': 'file' },
  assetNames: 'fonts/[name]',
  publicPath: '/static',
};

/* The Geist fonts are OFL-1.1 licensed; redistribution must ship the license text alongside them. */
async function copyFontLicense() {
  const fontsDir = join(staticDir, 'fonts');
  await mkdir(fontsDir, { recursive: true });
  await copyFile(resolve(root, 'node_modules/geist/LICENSE.txt'), join(fontsDir, 'OFL.txt'));
}

/* Implementation note. */
async function emitHtml() {
  const [js, css] = await Promise.all([
    readFile(join(staticDir, 'app.js')),
    readFile(join(staticDir, 'app.css')),
  ]);
  const buildId = createHash('sha256').update(js).update(css).digest('hex').slice(0, 12);
  const html = await readFile(join(root, 'index.html'), 'utf8');
  await writeFile(join(outdir, 'index.html'), html.replaceAll('__BUILD_ID__', buildId), 'utf8');
  return buildId;
}

async function reportSizes(buildId) {
  const files = ['index.html', 'static/app.js', 'static/app.css', 'static/fonts/Geist-Variable.woff2', 'static/fonts/GeistMono-Variable.woff2'];
  const { gzipSync, brotliCompressSync } = await import('node:zlib');
  console.log(`\nBuild ID ${buildId}`);
  for (const file of files) {
    const buf = await readFile(join(outdir, file));
    const gz = gzipSync(buf, { level: 9 }).length;
    const br = brotliCompressSync(buf).length;
    console.log(
      `  ${file.padEnd(38)} ${String(buf.length).padStart(7)} B  gzip ${String(gz).padStart(6)} B  br ${String(br).padStart(6)} B`,
    );
  }
}

async function buildApp() {
  await rm(outdir, { recursive: true, force: true });
  await mkdir(staticDir, { recursive: true });

  await build({
    ...shared,
    ...fontAssets,
    entryPoints: [
      { in: resolve(root, 'src/main.ts'), out: 'app' },
      { in: resolve(root, 'src/styles/index.css'), out: 'app' },
    ],
    outdir: staticDir,
    minify: true,
    sourcemap: false,
  });
  await copyFontLicense();

  const buildId = await emitHtml();
  await reportSizes(buildId);
}

async function watchApp() {
  await mkdir(staticDir, { recursive: true });
  await copyFontLicense();
  const ctx = await context({
    ...shared,
    ...fontAssets,
    entryPoints: [
      { in: resolve(root, 'src/main.ts'), out: 'app' },
      { in: resolve(root, 'src/styles/index.css'), out: 'app' },
    ],
    outdir: staticDir,
    minify: false,
    sourcemap: 'inline',
    plugins: [
      {
        name: 'emit-html',
        setup(b) {
          b.onEnd(async (result) => {
            if (result.errors.length === 0) {
              const id = await emitHtml();
              console.log(`index.html updated (${id})`);
            }
          });
        },
      },
    ],
  });
  await ctx.watch();
  console.log('Watching for changes');
}

/* Implementation note. */
async function buildTests() {
  await rm(testOutdir, { recursive: true, force: true });
  await mkdir(testOutdir, { recursive: true });

  const entries = (await readdir(resolve(root, 'test')))
    .filter((f) => f.endsWith('.test.ts'))
    .map((f) => resolve(root, 'test', f));
  if (entries.length === 0) throw new Error('No test/*.test.ts files found');

  await build({
    ...shared,
    entryPoints: entries,
    outdir: testOutdir,
    outExtension: { '.js': '.mjs' },
    format: 'esm',
    platform: 'node',
    minify: false,
    sourcemap: 'inline',
    // Implementation note.
    external: ['node:*'],
  });

  const built = await readdir(testOutdir);
  console.log(`Built ${built.length} test files`);
}

if (testOnly) {
  await buildTests();
} else if (watch) {
  await watchApp();
} else {
  await buildApp();
  // Implementation note.
  const listing = await readdir(outdir, { recursive: true });
  const files = [];
  for (const entry of listing) {
    const full = join(outdir, entry);
    if ((await stat(full)).isFile()) files.push(entry);
  }
  console.log(`\ndist/ contains ${files.length} files: ${files.sort().join(', ')}`);
}
