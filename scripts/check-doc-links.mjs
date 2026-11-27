// Offline Markdown link check for tracked docs: every relative file link must
// resolve inside the repository (after resolving symlinks and junctions) and
// every #fragment must match a GitHub-style heading slug or explicit HTML
// anchor in the target file. Source-line (#L1, #L1-L2) and text (#:~:text=)
// fragments only require the target file to exist. External (scheme or
// protocol-relative) links are skipped by design.
//
// Markdown is parsed with a real CommonMark/GFM parser (micromark/mdast), not
// a hand-written tokenizer: code spans, fenced code, HTML comments, reference
// definitions, nested image/link labels and front matter are all recognized
// with their actual Markdown contexts, so markup inside code or comments can
// never be mistaken for a live link, heading or anchor. Heading anchors are
// derived from the rendered heading text with github-slugger semantics.
//
// Usage: node scripts/check-doc-links.mjs [file.md ...]
// With no arguments it checks every tracked **/*.md file (git ls-files).
// Exits 1 and prints file:line for each broken link.
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync, realpathSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import GithubSlugger, { slug as githubSlug } from "github-slugger";
import { fromMarkdown } from "mdast-util-from-markdown";
import { gfmFromMarkdown } from "mdast-util-gfm";
import { gfm } from "micromark-extension-gfm";

const SCHEME = /^[A-Za-z][A-Za-z0-9+.-]*:/;
const LINE_ANCHOR = /^L\d+(?:-L\d+)?$/;

export { githubSlug };

// Blank YAML front matter with spaces (keeping every newline) so its content
// is never parsed as Markdown and line numbers survive. A document whose
// opening delimiter is never closed stays fully blank, as before.
function blankFrontMatter(markdown) {
  const lines = markdown.split("\n");
  if (!/^---\s*$/.test(lines[0])) return markdown;
  const chars = [...markdown];
  let offset = 0;
  for (const line of lines) {
    for (let i = offset; i < offset + line.length; i++) {
      if (chars[i] !== "\n") chars[i] = " ";
    }
    if (offset !== 0 && /^(?:---|\.\.\.)\s*$/.test(line)) break;
    offset += line.length + 1;
  }
  return chars.join("");
}

function parse(markdown) {
  return fromMarkdown(blankFrontMatter(markdown), {
    extensions: [gfm()],
    mdastExtensions: [gfmFromMarkdown()],
  });
}

// Rendered text of a heading, matching what GitHub shows: code spans keep
// their contents, images degrade to their alt text, and raw HTML (tags and
// comments alike) contributes nothing because its inner text, when any, is a
// separate text sibling.
function renderedText(node) {
  let out = "";
  const visit = (current) => {
    if (current.type === "text" || current.type === "inlineCode") {
      out += current.value;
    } else if (current.type === "image" || current.type === "imageReference") {
      out += current.alt ?? "";
    } else if (current.type === "html") {
      return;
    } else if (current.type === "break") {
      out += " ";
    }
    for (const child of current.children ?? []) visit(child);
  };
  visit(node);
  return out;
}

// HTML comment markup is not rendered, so hrefs, srcs and anchors inside a
// comment never count; strip comments before scanning raw HTML.
const stripHtmlComments = (value) => value.replace(/<!--[\s\S]*?-->/g, "");

const lineInHtml = (node, index) => {
  let line = node.position.start.line;
  for (let i = 0; i < index; i++) if (node.value[i] === "\n") line++;
  return line;
};

// Anchor ids GitHub exposes for a rendered Markdown file: slugified headings
// (duplicates get -1, -2, ...) plus explicit <a id>/<a name> anchors. Headings
// and anchors that exist only in code, front matter or HTML comments are never
// part of the parse tree used here.
export function markdownAnchors(markdown) {
  const tree = parse(markdown);
  const slugger = new GithubSlugger();
  const anchors = new Set();
  const visit = (node) => {
    if (node.type === "heading") {
      anchors.add(slugger.slug(renderedText(node)));
    } else if (node.type === "html") {
      const html = stripHtmlComments(node.value);
      for (const match of html.matchAll(/<a\s+(?:id|name)="([^"]+)"/g)) {
        anchors.add(match[1]);
      }
    }
    for (const child of node.children ?? []) visit(child);
  };
  visit(tree);
  return anchors;
}

// Inline links, images, reference links (full/collapsed/shortcut), images
// nested inside link labels ([![alt](img.png)](page)), and HTML src/srcset/
// href file references. Returns [{target, line}] in document order, with the
// contents of a link label reported before the label's own destination. An
// image description is alt text, not navigation: link syntax inside it is not
// a rendered link and its children are never scanned.
export function extractLinks(markdown) {
  const tree = parse(markdown);
  const definitions = new Map();
  const collect = (node) => {
    if (node.type === "definition") definitions.set(node.identifier, node.url);
    for (const child of node.children ?? []) collect(child);
  };
  collect(tree);
  const links = [];
  const htmlRefs = (node) => {
    const html = stripHtmlComments(node.value);
    for (const match of html.matchAll(/<(?:img|source)\s[^>]*?(?:src|srcset)="([^"]+)"/g)) {
      for (const candidate of match[1].split(",")) {
        const url = candidate.trim().split(/\s+/)[0];
        if (url) links.push({ target: url, line: lineInHtml(node, match.index) });
      }
    }
    for (const match of html.matchAll(/<a\s[^>]*?href="([^"]+)"/g)) {
      const url = match[1].trim();
      if (url) links.push({ target: url, line: lineInHtml(node, match.index) });
    }
  };
  const visit = (node) => {
    if (node.type === "image") {
      links.push({ target: node.url, line: node.position.start.line });
      return;
    }
    if (node.type === "imageReference") {
      const target = definitions.get(node.identifier);
      if (target !== undefined) {
        links.push({ target, line: node.position.start.line });
      }
      return;
    }
    if (node.type === "link" || node.type === "linkReference") {
      for (const child of node.children ?? []) visit(child);
      const target = node.type === "link" ? node.url : definitions.get(node.identifier);
      if (target !== undefined) {
        links.push({ target, line: node.position.start.line });
      }
      return;
    }
    if (node.type === "html") {
      htmlRefs(node);
      return;
    }
    for (const child of node.children ?? []) visit(child);
  };
  visit(tree);
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
