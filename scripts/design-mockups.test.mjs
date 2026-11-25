// npm run test:scripts (node --test "scripts/*.test.mjs")
// Static checks on the design fixtures in web/design: they are not built or served by CI, so these
// guard the problems a browser run would only show on someone's machine.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
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
    for (const [m] of text.matchAll(/[\w.%+-]+@(?:[\w-]+\.)*(?:gmail\.com|outlook\.com|hotmail\.com|yahoo\.com|uw\.edu|washington\.edu|ubc\.ca|cseed\.(?:ca|co))\b/gi))
      real.push(`${rel(file)}: ${m}`);
    for (const [m] of text.matchAll(/linkedin\.com\/in\/(?!example-)[\w-]+/gi)) real.push(`${rel(file)}: ${m}`);
  }
  assert.deepEqual(real, [], "fixture contacts that could belong to real people");
});

test("design Python builders name the file encoding (Windows defaults to cp1252)", () => {
  const implicit = [];
  for (const file of files(design, (n) => n.endsWith(".py"))) {
    read(file).split("\n").forEach((line, i) => {
      if (/\.(read|write)_text\(|\bopen\(/.test(line) && !/encoding=/.test(line)) implicit.push(`${rel(file)}:${i + 1}`);
    });
  }
  assert.deepEqual(implicit, [], "read_text/write_text/open without encoding=");
});

test("design browser scripts resolve paths portably and honour CHROMIUM_PATH", () => {
  const bad = [];
  for (const file of files(design, (n) => n.endsWith(".mjs"))) {
    read(file).split("\n").forEach((line, i) => {
      // URL.pathname keeps the leading /C: on Windows; fileURLToPath does not.
      if (/import\.meta\.url\)\.pathname/.test(line)) bad.push(`${rel(file)}:${i + 1} uses URL.pathname`);
      if (/chromium\.launch\(/.test(line) && !/CHROMIUM_PATH/.test(line)) bad.push(`${rel(file)}:${i + 1} ignores CHROMIUM_PATH`);
    });
  }
  assert.deepEqual(bad, [], "non-portable design scripts");
});

/** Expand shell-style braces: "a-{1,2}.png" → ["a-1.png", "a-2.png"]. */
function expandBraces(s) {
  const m = s.match(/\{([^{}]*)\}/);
  if (!m) return [s];
  return m[1].split(",").flatMap((alt) => expandBraces(s.slice(0, m.index) + alt + s.slice(m.index + m[0].length)));
}

test("tracked design shots are exactly the ones the repo references (M-108)", () => {
  const git = (...args) => execFileSync("git", args, { cwd: root, encoding: "utf8" }).split("\n").filter(Boolean);
  const shotsDir = "web/design/shots/";
  const tracked = new Set(git("ls-files", "--", shotsDir));
  // Literal paths containing shots/ (relative to the file that names them, or from the repo root) in
  // docs, mockups and scripts. Template literals such as `shots/${name}.png` are write targets for
  // regenerated captures, not references, and do not match because `$` stops the path.
  const sources = git("ls-files", "--", "*.md", "*.html", "*.js", "*.mjs", "*.cjs", "*.ts", "*.css", "*.py", "*.json", "*.yml", "*.yaml")
    .filter((f) => !f.startsWith(shotsDir) && existsSync(path.join(root, f)));
  const referenced = new Map();
  for (const f of sources) {
    for (const [token] of read(path.join(root, f)).matchAll(/[\w./-]*shots\/[\w./{},-]*\.(?:png|json)\b/g)) {
      for (const p of expandBraces(token)) {
        const candidates = [path.posix.normalize(path.posix.join(path.posix.dirname(f), p)), path.posix.normalize(p)];
        const hit = candidates.find((c) => c.startsWith(shotsDir));
        if (hit && !referenced.has(hit)) referenced.set(hit, f);
      }
    }
  }
  const missing = [...referenced].filter(([p]) => !tracked.has(p)).map(([p, by]) => `${p} (named in ${by})`);
  // The validation*.json summaries are small check reports that M-108 keeps alongside the captures.
  const unreferenced = [...tracked].filter((p) => !referenced.has(p) && !/\/validation[\w-]*\.json$/.test(p));
  assert.deepEqual(missing, [], "shots that docs or scripts name but git does not track");
  assert.deepEqual(unreferenced, [], "tracked shots nothing references (regenerate locally; they are git-ignored)");
});
