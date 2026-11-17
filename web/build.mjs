// Builds the dashboard into dist/.
//
//   node build.mjs            production build: minified, no sourcemaps
//   node build.mjs --watch    rebuild on change, unminified, with sourcemaps
//   node build.mjs --test     bundle test/*.test.ts into .test-dist/ and run them
//
// QP_DIST and QP_TEST_DIST move the two output directories elsewhere, so
// several builds of one checkout can run side by side.
//
// Layout of dist/, which the server's caching depends on:
//   index.html               names the hashed files; never cached
//   assets/*                 every file name carries a content hash; cached forever
//   manifest.webmanifest, icons/*   copied from public/
import { createHash } from 'node:crypto';
import { cp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import * as esbuild from 'esbuild';

const root = path.dirname(fileURLToPath(import.meta.url));
const dist = process.env.QP_DIST ? path.resolve(process.env.QP_DIST) : path.join(root, 'dist');
const args = new Set(process.argv.slice(2));
const watch = args.has('--watch');

const shared = {
  absWorkingDir: root,
  bundle: true,
  target: ['es2022', 'chrome120', 'firefox121', 'safari17'],
  jsx: 'automatic',
  jsxImportSource: 'preact',
  logLevel: 'info',
};

if (args.has('--test')) {
  await buildTests();
} else {
  await buildApp();
}

async function buildTests() {
  const out = process.env.QP_TEST_DIST ? path.resolve(process.env.QP_TEST_DIST) : path.join(root, '.test-dist');
  await rm(out, { recursive: true, force: true });
  const tests = (await readdir(path.join(root, 'test'))).filter((name) => name.endsWith('.test.ts'));
  if (tests.length === 0) {
    console.log('no tests in test/');
    return;
  }
  await esbuild.build({
    ...shared,
    entryPoints: tests.map((name) => path.join('test', name)),
    outdir: out,
    outExtension: { '.js': '.mjs' },
    platform: 'node',
    // The tests run in Node, not in a browser. Without a Node target esbuild
    // rewrites `node:test` to `test`, which is not a module.
    target: 'node22',
    format: 'esm',
    logLevel: 'warning',
  });
  const { spawnSync } = await import('node:child_process');
  const bundles = tests.map((name) => path.join(out, name.replace(/\.ts$/, '.mjs')));
  const run = spawnSync(process.execPath, ['--test', ...bundles], { stdio: 'inherit' });
  process.exit(run.status ?? 1);
}

async function buildApp() {
  await clean();
  const options = {
    ...shared,
    outdir: dist,
    entryNames: 'assets/[name]-[hash]',
    assetNames: 'assets/[name]-[hash]',
    metafile: true,
    minify: !watch,
    sourcemap: watch ? 'linked' : false,
    legalComments: watch ? 'none' : 'linked',
    define: { 'process.env.NODE_ENV': watch ? '"development"' : '"production"' },
  };
  // Two builds because they are two kinds of script. The app is a module.
  // The theme script is a classic one: it has to run before the first paint,
  // and a module would be deferred.
  const app = { ...options, entryPoints: { app: 'src/main.tsx' }, format: 'esm', loader: { '.woff2': 'file' } };
  const theme = { ...options, entryPoints: { theme: 'src/theme-boot.ts' }, format: 'iife', legalComments: 'none' };

  if (!watch) {
    const [appResult, themeResult] = await Promise.all([esbuild.build(app), esbuild.build(theme)]);
    await finish([appResult.metafile, themeResult.metafile]);
    await report([appResult.metafile, themeResult.metafile]);
    return;
  }

  const metafiles = [null, null];
  const onEnd = (slot) => ({
    name: 'finish',
    setup(build) {
      build.onEnd(async (result) => {
        if (result.errors.length > 0 || !result.metafile) return;
        metafiles[slot] = result.metafile;
        if (metafiles.every(Boolean)) await finish(metafiles, { prune: true });
      });
    },
  });
  const contexts = await Promise.all([
    esbuild.context({ ...app, plugins: [onEnd(0)] }),
    esbuild.context({ ...theme, plugins: [onEnd(1)] }),
  ]);
  await Promise.all(contexts.map((context) => context.watch()));
}

// clean empties dist but keeps .gitkeep: the Go embed needs the directory.
async function clean() {
  await mkdir(dist, { recursive: true });
  for (const name of await readdir(dist)) {
    if (name !== '.gitkeep') await rm(path.join(dist, name), { recursive: true, force: true });
  }
}

// finish writes everything esbuild does not: the public files, the fonts'
// licence, and index.html with the hashed names filled in.
async function finish(metafiles, { prune = false } = {}) {
  const outputs = Object.assign({}, ...metafiles.map((meta) => meta.outputs));
  const url = (file) => '/' + path.relative(dist, path.join(root, file)).split(path.sep).join('/');
  const find = (test) => {
    const hit = Object.keys(outputs).find(test);
    if (!hit) throw new Error('build output is missing a file index.html needs');
    return url(hit);
  };
  const entry = (name) => find((file) => outputs[file].entryPoint === name);
  const names = {
    'app.js': entry('src/main.tsx'),
    'theme.js': entry('src/theme-boot.ts'),
    'app.css': find((file) => /\/app-[^/]+\.css$/.test(file)),
    'font.sans': find((file) => /\/Geist-Variable-[^/]+\.woff2$/.test(file)),
    'font.mono': find((file) => /\/GeistMono-Variable-[^/]+\.woff2$/.test(file)),
  };

  await cp(path.join(root, 'public'), dist, { recursive: true });

  // The fonts' licence travels next to them, hashed like everything in assets/.
  const licence = await readFile(path.join(root, 'src/fonts/OFL.txt'));
  const licenceName = `assets/Geist-OFL-${hash(licence)}.txt`;
  await writeFile(path.join(dist, licenceName), licence);

  const template = await readFile(path.join(root, 'index.html'), 'utf8');
  const html = template.replace(/\{\{([a-z.]+)\}\}/g, (_, key) => {
    if (!(key in names)) throw new Error(`index.html asks for {{${key}}}, which the build does not produce`);
    return names[key];
  });
  await writeFile(path.join(dist, 'index.html'), html);

  if (prune) {
    // A watch session leaves a hashed file behind for every rebuild.
    const keep = new Set([...Object.keys(outputs).map((file) => path.join(root, file)), path.join(dist, licenceName)]);
    const assets = path.join(dist, 'assets');
    for (const name of await readdir(assets)) {
      if (!keep.has(path.join(assets, name))) await rm(path.join(assets, name), { force: true });
    }
  }
}

function hash(bytes) {
  return createHash('sha256').update(bytes).digest('base64url').slice(0, 8).toUpperCase().replace(/[-_]/g, '0');
}

// report prints what a browser downloads, gzipped, so a size regression is
// seen at build time rather than in a review.
async function report(metafiles) {
  const { gzipSync } = await import('node:zlib');
  const rows = [];
  for (const meta of metafiles) {
    for (const file of Object.keys(meta.outputs)) {
      if (!/\.(js|css)$/.test(file)) continue;
      const bytes = await readFile(path.join(root, file));
      rows.push([path.relative(dist, path.join(root, file)), bytes.length, gzipSync(bytes, { level: 9 }).length]);
    }
  }
  const kb = (n) => (n / 1024).toFixed(1).padStart(7) + ' kB';
  console.log('\n  gzipped');
  for (const [name, raw, gz] of rows.sort((a, b) => b[2] - a[2])) {
    console.log(`  ${kb(gz)}  ${name}  (${kb(raw).trim()} raw)`);
  }
}
