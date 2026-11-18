// usage: node scripts/route-assets.mjs <dist> <out.json>
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join, relative, posix, sep } from 'node:path';
import { gzipSync } from 'node:zlib';
const dist = process.argv[2];
const walk = (d) => readdirSync(d).flatMap((n) => { const p = join(d, n); return statSync(p).isDirectory() ? walk(p) : [p]; });
const files = walk(dist);
const rel = (p) => relative(dist, p).split(sep).join('/');
const size = new Map();
for (const f of files) if (/\.(js|css)$/.test(f)) { const b = readFileSync(f); size.set(rel(f), { raw: b.length, gzip: gzipSync(b, { level: 9 }).length }); }
const staticDeps = new Map(), dynDeps = new Map();
for (const [a] of size) if (a.endsWith('.js')) {
  const src = readFileSync(join(dist, a), 'utf8'); const base = posix.dirname(a);
  const res = (s) => s.startsWith('/') ? s.slice(1) : posix.normalize(posix.join(base, s));
  const st = new Set(), dy = new Set();
  for (const m of src.matchAll(/(?:import|export)\s*(?:[\w*{}\s,$]+from\s*)?["']([^"']+\.(?:js|css))["']/g)) st.add(res(m[1]));
  for (const m of src.matchAll(/import\(\s*["']([^"']+\.js)["']\s*\)/g)) dy.add(res(m[1]));
  // vite __vitePreload dep arrays: "_astro/x.js" strings
  for (const m of src.matchAll(/["'](_astro\/[^"']+\.(?:js|css))["']/g)) if (!st.has(m[1])) dy.add(m[1]);
  staticDeps.set(a, st); dynDeps.set(a, dy);
}
const closure = (roots) => { const seen = new Set(); const q = [...roots]; while (q.length) { const x = q.pop(); if (seen.has(x) || !size.has(x)) continue; seen.add(x); for (const d of staticDeps.get(x) ?? []) q.push(d); } return seen; };
const routes = {};
for (const f of files.filter((f) => f.endsWith('.html'))) {
  const html = readFileSync(f, 'utf8'); const r = '/' + rel(f).replace(/index\.html$/, '');
  const refs = new Set();
  for (const m of html.matchAll(/(?:src|href|component-url|renderer-url|before-hydration-url)="\/?(_astro\/[^"]+\.(?:js|css))"/g)) refs.add(m[1]);
  for (const m of html.matchAll(/import\s*["']\/?(_astro\/[^"']+\.js)["']/g)) refs.add(m[1]);
  const all = closure(refs);
  const dyn = new Set(); for (const a of all) for (const d of dynDeps.get(a) ?? []) if (!all.has(d)) dyn.add(d);
  const sum = (set, ext) => [...set].filter((a) => a.endsWith(ext)).reduce((s, a) => ({ raw: s.raw + size.get(a).raw, gzip: s.gzip + size.get(a).gzip }), { raw: 0, gzip: 0 });
  routes[r] = { js: sum(all, '.js'), css: sum(all, '.css'), htmlBytes: Buffer.byteLength(html), assets: [...all].sort((a, b) => size.get(b).raw - size.get(a).raw), lazy: [...dyn].filter((d) => size.has(d)).sort() };
}
const assets = Object.fromEntries([...size].sort((a, b) => b[1].raw - a[1].raw));
writeFileSync(process.argv[3], JSON.stringify({ assets, routes }, null, 2));
for (const [r, v] of Object.entries(routes).sort()) console.log(`${r.padEnd(28)} js ${v.js.raw}/${v.js.gzip}  css ${v.css.raw}/${v.css.gzip}  html ${v.htmlBytes}  n=${v.assets.length} lazy=${v.lazy.length}`);
