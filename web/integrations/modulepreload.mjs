/**
 * Adds <link rel="modulepreload"> for every module a page's islands import statically.
 *
 * Without it the browser discovers island code one import level at a time (island loader →
 * component → its imports → theirs), and only after the stylesheets, because the island loader
 * is an inline script that waits for them. The links go after the stylesheet links so CSS is
 * still requested first. Links are not scripts, so the page CSP (#43) is unchanged.
 */
import { readdir, readFile, writeFile } from 'node:fs/promises';
import { join, posix } from 'node:path';
import { fileURLToPath } from 'node:url';

// Static imports and re-exports in Vite/Rolldown output: `import{a}from"./x.js"`, `import"./x.js"`.
const STATIC_IMPORT = /(?:^|[;}\s])(?:import|export)\s*(?:[\w$*{},\s]*?from\s*)?["'](\.{1,2}\/[^"']+\.js)["']/g;
const ISLAND_URL = /\s(?:component-url|renderer-url)="(\/[^"]+\.js)"/g;
const LAST_STYLESHEET = /(<link rel="stylesheet"[^>]*>)(?![\s\S]*<link rel="stylesheet")/;

/** The URLs `entry` needs before it can run (itself included), breadth first. */
export async function moduleGraph(dist, entries) {
  const seen = new Set();
  const queue = [...entries];
  while (queue.length) {
    const url = queue.shift();
    if (seen.has(url)) continue;
    seen.add(url);
    const code = await readFile(join(dist, url), 'utf8');
    for (const m of code.matchAll(STATIC_IMPORT)) queue.push(posix.join(posix.dirname(url), m[1]));
  }
  return [...seen];
}

/** Insert the preload links into one built page; returns the new HTML (unchanged without islands). */
export async function addPreloads(dist, html) {
  const entries = [...new Set([...html.matchAll(ISLAND_URL)].map((m) => m[1]))];
  if (!entries.length || html.includes('rel="modulepreload"')) return html;
  const links = (await moduleGraph(dist, entries)).map((u) => `<link rel="modulepreload" href="${u}">`).join('');
  return LAST_STYLESHEET.test(html) ? html.replace(LAST_STYLESHEET, `$1${links}`) : html.replace('</head>', `${links}</head>`);
}

async function* htmlFiles(dir) {
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) yield* htmlFiles(p);
    else if (e.name.endsWith('.html')) yield p;
  }
}

export default function modulepreload() {
  return {
    name: 'cseed-modulepreload',
    hooks: {
      'astro:build:done': async ({ dir, logger }) => {
        const dist = fileURLToPath(dir);
        let pages = 0;
        for await (const file of htmlFiles(dist)) {
          const html = await readFile(file, 'utf8');
          const next = await addPreloads(dist, html);
          if (next !== html) { await writeFile(file, next); pages++; }
        }
        logger.info(`modulepreload links added to ${pages} page(s)`);
      },
    },
  };
}
