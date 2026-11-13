/**
 * Writes src/lib/endpoints.generated.ts: the method and path of every ENDPOINTS entry in
 * src/lib/contracts.ts, without the zod schemas.
 *
 * `api()` needs only the method and path to send a request. Importing ENDPOINTS for those would
 * put zod and all ~350 schemas (about half of Home's JavaScript) in front of the first fetch.
 * The schemas still validate every response: lib/api/client.ts loads them in the background.
 *
 * Regenerated at the start of every `astro build`/`astro dev`, so a new endpoint needs no extra
 * step; the file is committed so type checks and unit tests see it, and tests/home-speed.spec.ts
 * fails when the committed copy is stale.
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

export const CONTRACTS = fileURLToPath(new URL('../src/lib/contracts.ts', import.meta.url));
export const GENERATED = fileURLToPath(new URL('../src/lib/endpoints.generated.ts', import.meta.url));
const ENTRY = /^\s*(\w+):\s*\{\s*method:\s*'(\w+)',\s*path:\s*'([^']+)'/;

/** { name: { method, path } } for each line of the ENDPOINTS object. Throws on an unreadable entry. */
export function extractEndpoints(source) {
  const start = source.indexOf('export const ENDPOINTS = {');
  const end = source.indexOf('\n} as const', start);
  if (start < 0 || end < 0) throw new Error('endpoints: ENDPOINTS block not found in contracts.ts');
  const out = {};
  for (const line of source.slice(start, end).split('\n').slice(1)) {
    if (!/\bmethod\s*:/.test(line)) continue;
    const m = ENTRY.exec(line);
    if (!m) throw new Error(`endpoints: write this ENDPOINTS entry as \`name: { method: '…', path: '…', … }\` on one line:\n${line.trim()}`);
    out[m[1]] = { method: m[2], path: m[3] };
  }
  return out;
}

/** The generated module's source for a contracts.ts source. */
export function endpointsModule(contractsSource) {
  const rows = Object.entries(extractEndpoints(contractsSource)).map(([name, s]) => `  ${name}: { method: '${s.method}', path: '${s.path}' },`);
  return `// Generated from ENDPOINTS in contracts.ts by integrations/endpoints.mjs on every build. Do not edit.\nimport type { EndpointName } from './contracts';\n\nconst ENDPOINTS: Record<EndpointName, { method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'; path: string }> = {\n${rows.join('\n')}\n};\nexport default ENDPOINTS;\n`;
}

/** Rewrite the generated file when contracts.ts changed; returns whether it did. */
export function writeEndpoints() {
  const next = endpointsModule(readFileSync(CONTRACTS, 'utf8'));
  let current = '';
  try { current = readFileSync(GENERATED, 'utf8'); } catch { /* first run */ }
  if (current === next) return false;
  writeFileSync(GENERATED, next);
  return true;
}

export default function endpoints() {
  return {
    name: 'cseed-endpoints',
    hooks: {
      'astro:config:setup': ({ logger }) => { if (writeEndpoints()) logger.info('updated src/lib/endpoints.generated.ts'); },
    },
  };
}
