// npm run test:scripts (node --test "scripts/*.test.mjs")
// Static checks on the design fixtures in web/design: they are not built or served by CI, so these
// guard the problems a browser run would only show on someone's machine.
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const design = path.join(root, "web", "design");

/** Every file under `dir` (recursive) whose name passes `keep`, as absolute paths. */
function files(dir, keep) {
  return readdirSync(dir, { withFileTypes: true, recursive: true })
    .filter((d) => d.isFile() && keep(d.name))
    .map((d) => path.join(d.parentPath, d.name));
}
const rel = (f) => path.relative(root, f).split(path.sep).join("/");
const read = (f) => readFileSync(f, "utf8");
const mockups = files(path.join(design, "mockups"), (n) => n.endsWith(".html"));

test("every local href and src in the mockups points at a file that exists", () => {
  const broken = [];
  for (const file of mockups) {
    for (const [, url] of read(file).matchAll(/\b(?:href|src)="([^"]*)"/g)) {
      // Skip anchors, query-only links, app routes, other schemes and template placeholders.
      if (!url || /^(#|\?|\/|[a-z][a-z0-9+.-]*:)/i.test(url) || url.includes("${")) continue;
      const target = path.resolve(path.dirname(file), decodeURI(url.replace(/[?#].*$/, "")));
      if (!existsSync(target)) broken.push(`${rel(file)}: ${url}`);
    }
  }
  assert.deepEqual(broken, [], "broken local links");
});

const designText = files(design, (n) => /\.(html|py|m?js|css|md)$/.test(n));

test("fixture contact data uses reserved domains and placeholder LinkedIn profiles", () => {
  const real = [];
  for (const file of designText) {
    const text = read(file);
    // Real mail providers and universities; fixtures use example.com/.net/.edu or *.example.
    for (const [m] of text.matchAll(/[\w.%+-]+@(?:[\w-]+\.)*(?:gmail\.com|outlook\.com|hotmail\.com|yahoo\.com|uw\.edu|washington\.edu|ubc\.ca)\b/gi))
      real.push(`${rel(file)}: ${m}`);
    for (const [m] of text.matchAll(/linkedin\.com\/in\/(?!example-)[\w-]+/gi)) real.push(`${rel(file)}: ${m}`);
  }
  assert.deepEqual(real, [], "fixture contacts that could belong to real people");
});
