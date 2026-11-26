// npm run test:scripts (node --test "scripts/*.test.mjs")
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
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
  ].join("\n"));
  assert.ok(anchors.has("top"));
  assert.ok(anchors.has("setup"));
  assert.ok(anchors.has("setup-1"));
  assert.ok(anchors.has("setup-2"));
  assert.ok(anchors.has("make-dev-resets-everything"));
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
