// Offline Markdown link check for tracked docs: every relative file link must
// resolve inside the repository (after resolving symlinks and junctions) and
// every #fragment must match a GitHub-style heading slug or explicit HTML
// anchor in the target file. Source-line (#L1, #L1-L2) and text (#:~:text=)
// fragments only require the target file to exist. External (scheme or
// protocol-relative) links are skipped by design.
//
// Usage: node scripts/check-doc-links.mjs [file.md ...]
// With no arguments it checks every tracked **/*.md file (git ls-files).
// Exits 1 and prints file:line for each broken link.
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync, realpathSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SCHEME = /^[A-Za-z][A-Za-z0-9+.-]*:/;
const LINE_ANCHOR = /^L\d+(?:-L\d+)?$/;

// GitHub heading slugs (github-slugger semantics): lowercase, drop everything
// that is not a letter, mark, number, connector punctuation, hyphen or space,
// then turn spaces into hyphens without collapsing them.
export function githubSlug(text) {
  return text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{M}\p{N}\p{Pc}\- ]/gu, "")
    .replace(/ /g, "-");
}

// Remove HTML tags without a sanitizing regex: only "<" followed by a tag
// open (letter, "/" or "!") starts a tag, and plain text resumes after ">".
function stripHtmlTags(text) {
  let out = "";
  let i = 0;
  while (i < text.length) {
    if (text[i] === "<" && /[A-Za-z/!]/.test(text[i + 1] ?? "")) {
      const end = text.indexOf(">", i + 1);
      if (end === -1) break;
      i = end + 1;
      continue;
    }
    out += text[i];
    i++;
  }
  return out;
}

function headingText(raw) {
  const withoutMarkup = raw
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]*)\]\[[^\]]*\]/g, "$1")
    .replace(/(\*\*|__)(.*?)\1/g, "$2")
    .replace(/(\*|~~)(.*?)\1/g, "$2")
    .replace(/_([^_]+)_/g, "$1");
  return stripHtmlTags(withoutMarkup).trim();
}

// Replace HTML comments with spaces (keeping every newline): comment markup
// is not rendered, so it is neither a link, a heading nor an anchor.
const blankHtmlComments = (text) =>
  text.replace(/<!--[\s\S]*?-->/g, (m) => m.replace(/[^\n]/g, " "));

// Anchor ids GitHub exposes for a rendered Markdown file: slugified ATX
// headings (duplicates get -1, -2, ...) plus explicit <a id>/<a name> anchors.
// Fenced code, front matter and HTML comments are blanked first so example
// markup never counts as a live heading or anchor.
export function markdownAnchors(markdown) {
  const text = blankHtmlComments(blankBlocks(markdown));
  const anchors = new Set();
  const seen = new Map();
  for (const match of text.matchAll(/^ {0,3}#{1,6}[ \t]+(.+?)[ \t#]*$/gm)) {
    const base = githubSlug(headingText(match[1]));
    const count = seen.get(base) ?? 0;
    seen.set(base, count + 1);
    anchors.add(count === 0 ? base : `${base}-${count}`);
  }
  for (const match of text.matchAll(/<a\s+(?:id|name)="([^"]+)"/g)) {
    anchors.add(match[1]);
  }
  return anchors;
}

// Replace fenced code and front matter with spaces (keeping every newline) so
// example content is never parsed as links or headings and line numbers survive.
export function blankBlocks(markdown) {
  const chars = [...markdown];
  const blank = (from, to) => {
    for (let i = from; i < to; i++) if (chars[i] !== "\n") chars[i] = " ";
  };
  const lines = markdown.split("\n");
  let offset = 0;
  let fence = null;
  let inFrontMatter = /^---\s*$/.test(lines[0]);
  for (const line of lines) {
    const fenceMatch = /^ {0,3}(```+|~~~+)/.exec(line);
    if (inFrontMatter) {
      blank(offset, offset + line.length);
      if (offset !== 0 && /^(?:---|\.\.\.)\s*$/.test(line)) inFrontMatter = false;
    } else if (fence === null && fenceMatch) {
      // Keep the real opening fence run: a 4+ backtick/tilde fence is only
      // closed by a run at least as long, so shorter fences inside stay code.
      fence = fenceMatch[1];
      blank(offset, offset + line.length);
    } else if (fence !== null) {
      blank(offset, offset + line.length);
      const closer = fence[0];
      if (new RegExp(`^ {0,3}${closer}{${fence.length},}\\s*$`).test(line)) fence = null;
    }
    offset += line.length + 1;
  }
  return chars.join("");
}

// blankBlocks plus inline code spans and HTML comments, for link extraction.
export function blankCodeAndComments(markdown) {
  return blankHtmlComments(blankBlocks(markdown))
    .replace(/(`+)([^`]|[^`][\s\S]*?[^`])\1(?!`)/g, (m) => m.replace(/[^\n]/g, " "));
}

const normalizeLabel = (label) => label.trim().replace(/\s+/g, " ").toLowerCase();

function parseInlineDestination(text, start) {
  let k = start;
  while (k < text.length && /[ \t\n]/.test(text[k])) k++;
  let dest;
  if (text[k] === "<") {
    const end = text.indexOf(">", k + 1);
    if (end === -1) return null;
    dest = text.slice(k + 1, end);
    k = end + 1;
  } else {
    const begin = k;
    let depth = 0;
    for (; k < text.length; k++) {
      const c = text[k];
      if (c === "\\") { k++; continue; }
      if (c === "(") depth++;
      else if (c === ")") { if (depth === 0) break; depth--; }
      else if (/[ \t\n]/.test(c) && depth === 0) break;
    }
    dest = text.slice(begin, k);
  }
  let m = k;
  while (m < text.length && /[ \t\n]/.test(text[m])) m++;
  if (text[m] !== ")") {
    const quote = text[m];
    if (quote !== '"' && quote !== "'" && quote !== "(") return null;
    const closer = quote === "(" ? ")" : quote;
    let end = m + 1;
    for (; end < text.length; end++) {
      if (text[end] === "\\") { end++; continue; }
      if (text[end] === closer) break;
    }
    if (end >= text.length) return null;
    m = end + 1;
    while (m < text.length && /[ \t\n]/.test(text[m])) m++;
    if (text[m] !== ")") return null;
  }
  return { dest, end: m + 1 };
}

// Inline links, images, reference links (full/collapsed/shortcut), images and
// links nested inside link labels ([![alt](img.png)](page)), and HTML
// src/srcset/href file references. Returns [{target, line}].
export function extractLinks(markdown) {
  const text = blankCodeAndComments(markdown);
  const lineOf = (index) => {
    let line = 1;
    for (let i = 0; i < index; i++) if (text[i] === "\n") line++;
    return line;
  };
  const links = [];
  const definitions = new Map();
  for (const match of text.matchAll(/^ {0,3}\[([^\]\n]+)\]:[ \t]*(<[^>\n]*>|\S+).*$/gm)) {
    const raw = match[2];
    definitions.set(normalizeLabel(match[1]), raw.startsWith("<") ? raw.slice(1, -1) : raw);
  }
  // Scan one text segment; recurses into bracket labels so an image or link
  // nested inside a link label is still extracted, while the scanner skips
  // past the outer destination and never double-counts it.
  const scan = (segment, baseLine) => {
    const lineAt = (index) => {
      let line = baseLine;
      for (let i = 0; i < index; i++) if (segment[i] === "\n") line++;
      return line;
    };
    let i = 0;
    while (i < segment.length) {
      const open = segment[i] === "!" && segment[i + 1] === "[" ? i + 1 : segment[i] === "[" ? i : -1;
      if (open === -1) { i++; continue; }
      let depth = 0;
      let close = -1;
      for (let j = open; j < segment.length; j++) {
        const c = segment[j];
        if (c === "\\") { j++; continue; }
        if (c === "[") depth++;
        else if (c === "]") { depth--; if (depth === 0) { close = j; break; } }
      }
      if (close === -1) { i = open + 1; continue; }
      const labelText = segment.slice(open + 1, close);
      scan(labelText, lineAt(open));
      const after = segment[close + 1];
      if (after === "(") {
        const parsed = parseInlineDestination(segment, close + 2);
        if (parsed) {
          links.push({ target: parsed.dest, line: lineAt(open) });
          i = parsed.end;
          continue;
        }
        i = close + 1;
        continue;
      }
      if (after === "[") {
        const end = segment.indexOf("]", close + 2);
        if (end !== -1) {
          const ref = segment.slice(close + 2, end) || labelText;
          const target = definitions.get(normalizeLabel(ref));
          if (target !== undefined) links.push({ target, line: lineAt(open) });
          i = end + 1;
          continue;
        }
        i = close + 1;
        continue;
      }
      if (after !== ":" && labelText && definitions.has(normalizeLabel(labelText))) {
        links.push({ target: definitions.get(normalizeLabel(labelText)), line: lineAt(open) });
      }
      i = close + 1;
    }
  };
  scan(text, 1);
  for (const match of text.matchAll(/<(?:img|source)\s[^>]*?(?:src|srcset)="([^"]+)"/g)) {
    for (const candidate of match[1].split(",")) {
      const url = candidate.trim().split(/\s+/)[0];
      if (url) links.push({ target: url, line: lineOf(match.index) });
    }
  }
  for (const match of text.matchAll(/<a\s[^>]*?href="([^"]+)"/g)) {
    const url = match[1].trim();
    if (url) links.push({ target: url, line: lineOf(match.index) });
  }
  return links;
}

const decode = (value) => {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
};

// Existence check that respects case, unlike the Windows filesystem: every
// path segment must match a directory entry exactly, as on GitHub.
function caseSensitiveExists(root, relPosix) {
  let dir = root;
  for (const segment of relPosix.split("/")) {
    let entries;
    try {
      entries = readdirSync(dir);
    } catch {
      return false;
    }
    if (!entries.includes(segment)) return false;
    dir = path.join(dir, segment);
  }
  return true;
}

export function checkLink(root, realRoot, fromFile, target, anchorsCache) {
  if (!target || SCHEME.test(target) || target.startsWith("//")) return null;
  const hashIndex = target.indexOf("#");
  const rawPath = hashIndex === -1 ? target : target.slice(0, hashIndex);
  const rawFragment = hashIndex === -1 ? "" : target.slice(hashIndex + 1);
  const filePart = decode(rawPath.split("?")[0]);
  const fragment = decode(rawFragment);
  const targetRel = (
    filePart
      ? path.posix.normalize(path.posix.join(path.posix.dirname(fromFile), filePart))
      : fromFile
  ).replace(/\/+$/, "");
  if (targetRel === ".." || targetRel.startsWith("../") || path.posix.isAbsolute(targetRel)) {
    return `link escapes the repository: ${target}`;
  }
  // Containment is checked on real paths, not lexically: a symlink or junction
  // inside the checkout can point outside it, and such a target is neither
  // tracked content nor present on GitHub.
  let realTarget;
  try {
    realTarget = realpathSync(path.join(root, targetRel));
  } catch {
    return `broken link (no such file): ${target}`;
  }
  if (realTarget !== realRoot && !realTarget.startsWith(realRoot + path.sep)) {
    return `link escapes the repository: ${target}`;
  }
  if (!caseSensitiveExists(root, targetRel)) {
    return `broken link (no such file): ${target}`;
  }
  // Source-line (#L1, #L1-L2) and text (#:~:text=) fragments are resolved by
  // the forge, not the Markdown renderer: the target file must exist and stay
  // inside the repository, but there is no heading anchor to validate.
  if (!fragment || LINE_ANCHOR.test(rawFragment) || rawFragment.startsWith(":~:text=")) return null;
  if (!targetRel.toLowerCase().endsWith(".md")) return null;
  if (!anchorsCache.has(targetRel)) {
    anchorsCache.set(targetRel, markdownAnchors(readFileSync(realTarget, "utf8")));
  }
  if (!anchorsCache.get(targetRel).has(fragment)) {
    return `broken anchor (no such heading): ${target}`;
  }
  return null;
}

export function checkFiles(root, relFiles) {
  const realRoot = realpathSync(root);
  const anchorsCache = new Map();
  const failures = [];
  let checked = 0;
  for (const relFile of relFiles) {
    const markdown = readFileSync(path.join(root, relFile), "utf8");
    for (const link of extractLinks(markdown)) {
      if (!link.target || SCHEME.test(link.target) || link.target.startsWith("//")) continue;
      checked++;
      const problem = checkLink(root, realRoot, relFile, link.target, anchorsCache);
      if (problem) failures.push({ file: relFile, line: link.line, problem });
    }
  }
  return { checked, failures };
}

function repoMarkdownFiles(root) {
  const out = execFileSync("git", ["ls-files", "-z", "--", "*.md"], { cwd: root, encoding: "utf8" });
  return out.split("\0").filter(Boolean);
}

const invokedAsScript = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (invokedAsScript) {
  const root = execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
  const requested = process.argv.slice(2);
  const files = requested.length > 0 ? requested : repoMarkdownFiles(root);
  const { checked, failures } = checkFiles(root, files);
  for (const failure of failures) {
    console.error(`${failure.file}:${failure.line}: ${failure.problem}`);
  }
  if (failures.length > 0) {
    console.error(`${failures.length} broken link(s) in ${files.length} Markdown file(s)`);
    process.exit(1);
  }
  console.log(`ok: ${checked} relative links and anchors across ${files.length} Markdown files`);
}
