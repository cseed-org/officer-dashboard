import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import { gzipSync } from 'node:zlib';

const root = resolve(import.meta.dirname, '..');

test('route assets include the static closure and exclude dynamic imports', () => {
  const work = mkdtempSync(join(process.env.PANE_SCRATCH_DIR ?? tmpdir(), 'route-assets-test-'));
  try {
    const dist = join(work, 'dist');
    mkdirSync(join(dist, '_astro'), { recursive: true });
    const html = '<html><head><link rel="stylesheet" href="/_astro/page.css"></head><body><script type="module" src="/_astro/page.js"></script></body></html>';
    const page = 'import "./dep.js"; import("./lazy.js");';
    const dep = 'export const answer = 42;';
    const lazy = 'export const later = true;';
    const css = 'body { color: black; }';
    writeFileSync(join(dist, 'index.html'), html);
    writeFileSync(join(dist, '_astro/page.js'), page);
    writeFileSync(join(dist, '_astro/dep.js'), dep);
    writeFileSync(join(dist, '_astro/lazy.js'), lazy);
    writeFileSync(join(dist, '_astro/page.css'), css);
    const out = join(work, 'routes.json');
    execFileSync(process.execPath, [join(root, 'scripts/route-assets.mjs'), dist, out]);
    const result = JSON.parse(readFileSync(out, 'utf8'));
    assert.deepEqual(result.routes['/'].assets.sort(), ['_astro/dep.js', '_astro/page.css', '_astro/page.js']);
    assert.deepEqual(result.routes['/'].lazy, ['_astro/lazy.js']);
    assert.equal(result.routes['/'].js.raw, Buffer.byteLength(page) + Buffer.byteLength(dep));
    assert.equal(result.routes['/'].js.gzip, gzipSync(page, { level: 9 }).length + gzipSync(dep, { level: 9 }).length);
    assert.equal(result.routes['/'].css.raw, Buffer.byteLength(css));
    assert.equal(result.routes['/'].htmlBytes, Buffer.byteLength(html));
  } finally {
    rmSync(work, { recursive: true, force: true });
  }
});
