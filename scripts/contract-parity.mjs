import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';

// Dev login endpoints exist in only one copy; Search's validation is reconciled by PR 5.
// Search method, path and auth still have to match.
const developmentOnly = new Set(['authDev', 'authDevInfo']);
const searchSchemas = new Set(['query', 'response']);

export function compareEndpoints(canonical, browser, toJSONSchema) {
  const differences = [];
  const shape = (value) => value && typeof value === 'object'
    ? toJSONSchema(value, { io: 'input', unrepresentable: 'any' }) : value;
  for (const name of new Set([...Object.keys(canonical), ...Object.keys(browser)])) {
    if ((!canonical[name] || !browser[name]) && developmentOnly.has(name)) continue;
    if (!canonical[name] || !browser[name]) { differences.push(`${name}: missing endpoint`); continue; }
    for (const key of new Set([...Object.keys(canonical[name]), ...Object.keys(browser[name])])) {
      if (name === 'search' && searchSchemas.has(key)) continue;
      try { assert.deepStrictEqual(shape(canonical[name][key]), shape(browser[name][key])); }
      catch { differences.push(`${name}.${key}`); }
    }
  }
  return differences;
}

export async function checkContractParity(root) {
  const work = mkdtempSync(join(process.env.PANE_SCRATCH_DIR ?? tmpdir(), 'contract-parity-'));
  try {
    // One zod instance for both maps, without needing web/node_modules installed in CI.
    const outfile = join(work, 'parity.mjs');
    await build({
      stdin: { contents: "export { ENDPOINTS as canonical } from './src/core/contracts.ts'; export { ENDPOINTS as browser } from './web/src/lib/contracts.ts'; export { z } from 'zod';", resolveDir: root, loader: 'ts' },
      alias: { zod: join(root, 'node_modules/zod/index.js') },
      bundle: true, format: 'esm', platform: 'node', outfile, logLevel: 'error',
    });
    const { canonical, browser, z } = await import(pathToFileURL(outfile).href);
    const differences = compareEndpoints(canonical, browser, z.toJSONSchema);
    if (differences.length) throw new Error(`Contract copies differ: ${differences.join(', ')}`);
    console.log('Canonical and browser endpoint contracts match (documented dev/Search exceptions).');
    return browser;
  } finally { rmSync(work, { recursive: true, force: true }); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await checkContractParity(resolve(dirname(fileURLToPath(import.meta.url)), '..'));
}
