import { readdir, stat, copyFile, mkdir, unlink } from 'node:fs/promises';
import { join } from 'node:path';
import { spawn } from 'node:child_process';

// Docker Desktop's Windows bind mounts resolve Sapling.ts to sapling.ts. Vite
// probes .ts before .tsx, so that masks Sapling.tsx. Mirror the bind-mounted
// source into a case-sensitive Linux volume before starting Astro.
const source = '/host-src';
const destination = '/app/web/src';
const known = new Map();
// Also remove files deleted on the host while the container was stopped.
async function rememberExisting(relative = '') {
  for (const entry of await readdir(join(destination, relative), { withFileTypes: true })) {
    const name = join(relative, entry.name);
    if (entry.isDirectory()) await rememberExisting(name);
    else if (entry.isFile()) known.set(name, '');
  }
}
async function sync() {
  const seen = new Set();
  async function visit(relative = '') {
    await mkdir(join(destination, relative), { recursive: true });
    for (const entry of await readdir(join(source, relative), { withFileTypes: true })) {
      const name = join(relative, entry.name);
      if (entry.isDirectory()) await visit(name);
      else if (entry.isFile()) {
        seen.add(name);
        const info = await stat(join(source, name));
        const stamp = `${info.mtimeMs}:${info.size}`;
        if (known.get(name) !== stamp) {
          await copyFile(join(source, name), join(destination, name));
          known.set(name, stamp);
        }
      }
    }
  }
  await visit();
  for (const name of known.keys()) if (!seen.has(name)) {
    await unlink(join(destination, name)).catch(error => { if (error.code !== 'ENOENT') throw error; });
    known.delete(name);
  }
}
await mkdir(destination, { recursive: true });
await rememberExisting();
await sync();
const child = spawn('npm', ['run', 'dev', '--', '--host', '0.0.0.0'], { stdio: 'inherit' });
let stopping = false;
for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => { stopping = true; child.kill(signal); });
child.on('exit', code => { stopping = true; process.exit(code ?? 1); });
while (!stopping) {
  await new Promise(resolve => setTimeout(resolve, 500));
  try { await sync(); } catch (error) { console.error('Source sync:', error); }
}
