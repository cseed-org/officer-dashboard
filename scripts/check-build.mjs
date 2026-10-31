// Fails a production build if the dev login bypass survived bundling.
// src/pages/auth/dev.ts logs "[dev-auth] signing in as ..." only inside its dev-only branch.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const MARKER = 'dev-auth] signing in as';
const root = fileURLToPath(new URL('../dist/', import.meta.url));
const hits = [];
const walk = (dir) => {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else if (/\.(m?js|cjs)$/.test(name) && readFileSync(p, 'utf8').includes(MARKER)) hits.push(p);
  }
};
walk(root);
if (hits.length) {
  console.error(`Dev auth bypass found in the production build:\n  ${hits.join('\n  ')}`);
  process.exit(1);
}
console.log('check-build: no dev auth bypass in dist/');
