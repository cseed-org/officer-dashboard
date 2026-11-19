// npm run test:scripts (node --test "scripts/*.test.mjs")
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("compose Playwright image matches @playwright/test", () => {
  const compose = read("compose.dev.yaml");
  const webPackage = JSON.parse(read("web/package.json"));
  const image = compose.match(/^\s*image:\s*mcr\.microsoft\.com\/playwright:([^\s]+)\s*$/m);

  assert.ok(image, "compose.dev.yaml should define the Playwright e2e image");
  assert.equal(
    image[1],
    `v${webPackage.devDependencies["@playwright/test"]}-noble`,
    "keep the compose Playwright image in sync with web/package.json",
  );
});
