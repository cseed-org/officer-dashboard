// npm run test:scripts (node --test "scripts/*.test.mjs")
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import {
  checkFiles,
  extractLinks,
  githubSlug,
  markdownAnchors,
} from "./check-doc-links.mjs";

function fixture(files) {
  const root = mkdtempSync(path.join(tmpdir(), "doc-links-"));
  for (const [name, content] of Object.entries(files)) {
    const full = path.join(root, name);
    mkdirSync(path.dirname(full), { recursive: true });
    writeFileSync(full, content);
  }
  return root;
}

test("githubSlug matches GitHub heading anchors", () => {
  assert.equal(githubSlug("Hello, World!"), "hello-world");
  assert.equal(githubSlug("What's new: v0.24.2 (offline)"), "whats-new-v0242-offline");
  assert.equal(githubSlug("Foo & Bar"), "foo--bar");
  assert.equal(githubSlug("under_score and-dash"), "under_score-and-dash");
  assert.equal(githubSlug("Привет мир"), "привет-мир");
  assert.equal(githubSlug("日本語 見出し"), "日本語-見出し");
});

test("markdownAnchors covers duplicates, HTML anchors and skips code blocks", () => {
  const anchors = markdownAnchors([
    '<a id="top"></a>',
    "# Setup",
    "## Setup",
    "## Setup",
    "```",
    "# Not a heading",
    "```",
    "## `make dev` resets **everything**",
    "## With <code>html</code> tags",
  ].join("\n"));
  assert.ok(anchors.has("top"));
  assert.ok(anchors.has("setup"));
  assert.ok(anchors.has("setup-1"));
  assert.ok(anchors.has("setup-2"));
  assert.ok(anchors.has("make-dev-resets-everything"));
  assert.ok(anchors.has("with-html-tags"));
  assert.ok(!anchors.has("not-a-heading"));
});

test("extractLinks reads inline, image, reference and HTML source links", () => {
  const links = extractLinks([
    "[inline](docs/guide.md \"a title\") and ![shot](img/one.png)",
    "[full][ref] [collapsed][] [shortcut]",
    "[ref]: docs/ref.md",
    "[collapsed]: docs/collapsed.md",
    "[shortcut]: docs/shortcut.md",
    '<img src="img/two.png" alt="x"> <source srcset="img/a.png 1x, img/b.png 2x">',
    "`[code](ignored.md)`",
    "```",
    "[fenced](ignored.md)",
    "```",
    "![nested [brackets]](img/three.png)",
    "[parens](docs/file(1).md) and [titled](<docs/my file.md>)",
  ].join("\n"));
  const targets = links.map((link) => link.target);
  assert.deepEqual(targets.filter((t) => t === "ignored.md"), []);
  for (const expected of [
    "docs/guide.md", "img/one.png", "docs/ref.md", "docs/collapsed.md",
    "docs/shortcut.md", "img/two.png", "img/a.png", "img/b.png",
    "img/three.png", "docs/file(1).md", "docs/my file.md",
  ]) {
    assert.ok(targets.includes(expected), `missing ${expected}`);
  }
  assert.equal(links[0].line, 1);
});

test("extractLinks keeps line numbers with front matter and code", () => {
  const links = extractLinks([
    "---",
    "title: x",
    "---",
    "",
    "```",
    "[nope](ignored.md)",
    "```",
    "",
    "[yes](ok.md)",
  ].join("\n"));
  assert.deepEqual(links, [{ target: "ok.md", line: 9 }]);
});

test("checkFiles accepts valid relative and anchor links", () => {
  const root = fixture({
    "README.md": [
      "# Title",
      "[guide](docs/guide.md#install) [self](#title) [dir](docs/) [line](src/app.go#L12)",
      "[encoded](docs/my%20file.md) [q](docs/guide.md?plain=1#install)",
    ].join("\n"),
    "docs/guide.md": "# Guide\n## Install\n",
    "docs/my file.md": "# Space\n",
    "src/app.go": "package main\n",
  });
  const { checked, failures } = checkFiles(root, ["README.md"]);
  assert.equal(checked, 6);
  assert.deepEqual(failures, []);
});

test("checkFiles reports file:line for missing files and anchors", () => {
  const root = fixture({
    "README.md": [
      "[missing](docs/nope.md)",
      "[anchor](docs/guide.md#absent)",
      "[self](#nowhere)",
      "[case](Docs/guide.md)",
    ].join("\n"),
    "docs/guide.md": "# Guide\n",
  });
  const { failures } = checkFiles(root, ["README.md"]);
  assert.deepEqual(
    failures.map(({ file, line, problem }) => `${file}:${line}: ${problem}`),
    [
      "README.md:1: broken link (no such file): docs/nope.md",
      "README.md:2: broken anchor (no such heading): docs/guide.md#absent",
      "README.md:3: broken anchor (no such heading): #nowhere",
      "README.md:4: broken link (no such file): Docs/guide.md",
    ],
  );
});

test("checkFiles skips external links and rejects escapes", () => {
  const root = fixture({
    "README.md": [
      "[web](https://example.com/x#y) [mail](mailto:a@b.c) [proto](//cdn.example.com/x)",
      "[text](https://example.com/t/:~:text=frag)",
      "[out](../outside.md)",
    ].join("\n"),
  });
  const { checked, failures } = checkFiles(root, ["README.md"]);
  assert.equal(checked, 1);
  assert.deepEqual(failures.map((f) => f.problem), ["link escapes the repository: ../outside.md"]);
});
test("checkFiles validates the target file of line and text fragments", () => {
  const root = fixture({
    "README.md": [
      "[line](docs/guide.md#L12) [range](docs/guide.md#L1-L2) [text](docs/guide.md#:~:text=Install)",
      "[missing](docs/nope.md#L12)",
      "[escape](../outside.md#L1)",
      "[missing-text](docs/nope.md#:~:text=hello)",
    ].join("\n"),
    "docs/guide.md": "# Guide\n## Install\n",
  });
  const { checked, failures } = checkFiles(root, ["README.md"]);
  assert.equal(checked, 6);
  assert.deepEqual(
    failures.map(({ file, line, problem }) => `${file}:${line}: ${problem}`),
    [
      "README.md:2: broken link (no such file): docs/nope.md#L12",
      "README.md:3: link escapes the repository: ../outside.md#L1",
      "README.md:4: broken link (no such file): docs/nope.md#:~:text=hello",
    ],
  );
});

test("checkFiles rejects links escaping through directory junctions", (t) => {
  const outside = fixture({ "guide.md": "# Outside\n" });
  const root = fixture({
    "README.md": "[out](linked/guide.md#outside) [in](alias/guide.md#inside)\n",
    "docs/guide.md": "# Guide\n## Inside\n",
  });
  const linkType = process.platform === "win32" ? "junction" : "dir";
  try {
    symlinkSync(outside, path.join(root, "linked"), linkType);
    symlinkSync(path.join(root, "docs"), path.join(root, "alias"), linkType);
  } catch (error) {
    t.skip(`cannot create directory links: ${error.message}`);
    return;
  }
  const { checked, failures } = checkFiles(root, ["README.md"]);
  assert.equal(checked, 2);
  // The junction to an outside directory is rejected before it is read; the
  // junction that stays inside the repository keeps working.
  assert.deepEqual(
    failures.map((f) => f.problem),
    ["link escapes the repository: linked/guide.md#outside"],
  );
});

test("extractLinks finds images inside link labels and HTML hrefs", () => {
  const links = extractLinks([
    "[![shot](img/badge.png)](https://example.org)",
    '<a href="docs/guide.md">go</a> <a href="https://example.org/x">web</a>',
  ].join("\n"));
  const targets = links.map((link) => link.target);
  for (const expected of [
    "img/badge.png", "https://example.org", "docs/guide.md", "https://example.org/x",
  ]) {
    assert.ok(targets.includes(expected), `missing ${expected}`);
  }
  assert.equal(links[0].line, 1);
  assert.equal(links[2].line, 2);
});

test("checkFiles validates nested-label images and relative HTML hrefs", () => {
  const root = fixture({
    "README.md": [
      "[![shot](missing.png)](https://example.org)",
      '<a href="missing.md">go</a>',
      "[ok](docs/guide.md)",
    ].join("\n"),
    "docs/guide.md": "# Guide\n",
  });
  const { checked, failures } = checkFiles(root, ["README.md"]);
  assert.equal(checked, 3);
  assert.deepEqual(
    failures.map(({ file, line, problem }) => `${file}:${line}: ${problem}`),
    [
      "README.md:1: broken link (no such file): missing.png",
      "README.md:2: broken link (no such file): missing.md",
    ],
  );
});

test("extractLinks ignores CRLF front matter and longer code fences", () => {
  const crlf = extractLinks("---\r\nexample: [example](missing.md)\r\n---\r\n[ok](ok.md)\r\n");
  assert.deepEqual(crlf, [{ target: "ok.md", line: 4 }]);
  const nested = extractLinks([
    "````",
    "```",
    "[inside](missing.md)",
    "```",
    "````",
    "[after](ok.md)",
  ].join("\n"));
  assert.deepEqual(nested, [{ target: "ok.md", line: 6 }]);
});

test("checkFiles accepts CRLF front matter and nested fence examples", () => {
  const root = fixture({
    "README.md": [
      "---",
      "title: [x](missing.md)",
      "---",
      "````",
      "```",
      "[y](missing2.md)",
      "```",
      "````",
      "[ok](docs/guide.md)",
      "",
    ].join("\r\n"),
    "docs/guide.md": "# Guide\n",
  });
  const { failures } = checkFiles(root, ["README.md"]);
  assert.deepEqual(failures, []);
});

test("markdownAnchors ignores anchors and headings in code and comments", () => {
  // Built from char codes so the fixture source carries no comment-end tag.
  const commentEnd = String.fromCharCode(45, 45, 62);
  const anchors = markdownAnchors([
    "```html",
    '<a id="fake"></a>',
    "```",
    "<!--",
    "# Ghost",
    '<a name="hidden"></a>',
    commentEnd,
    "# Real",
  ].join("\n"));
  assert.ok(!anchors.has("fake"));
  assert.ok(!anchors.has("hidden"));
  assert.ok(!anchors.has("ghost"));
  assert.ok(anchors.has("real"));
});

test("checkFiles rejects anchors that only exist in code or comments", () => {
  const root = fixture({
    "README.md": [
      "[go](#fake)",
      "```html",
      '<a id="fake"></a>',
      "```",
      `<!-- # Ghost ${String.fromCharCode(45, 45, 62)}`,
      "[ghost](#ghost)",
    ].join("\n"),
  });
  const { failures } = checkFiles(root, ["README.md"]);
  assert.deepEqual(
    failures.map((f) => f.problem),
    ["broken anchor (no such heading): #fake", "broken anchor (no such heading): #ghost"],
  );
});
