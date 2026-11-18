// npm run test:scripts (node --test "scripts/*.test.mjs")
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const main = read("server/cmd/dashboard/main.go");
const loadSettings = main.slice(main.indexOf("func loadSettings"), main.indexOf("func atoiOr"));
const cloudflare = read("server/internal/cloudflare/client.go");
const fromEnv = cloudflare.slice(cloudflare.indexOf("func FromEnv"), cloudflare.indexOf("type Client"));
const keys = new Set();
for (const [source, pattern] of [[loadSettings, /\benv\("([A-Z][A-Z0-9_]+)"/g], [fromEnv, /\bget\("([A-Z][A-Z0-9_]+)"/g]]) {
  for (const match of source.matchAll(pattern)) keys.add(match[1]);
}
assert.ok(keys.size >= 25, "the server environment inventory should be extracted from Go sources");

const configuration = read("docs/configuration.md");
const template = read("server/deploy/dashboard.env.example");
const hasEntry = (source, key) => new RegExp(`(?:^|\\n)#?${key}=`).test(source);

// ADDR and DB_PATH are set by the systemd unit/container. Dev auth belongs only on
// disposable test deployments. New deployments use DRIVE_ROOT_FOLDER_ID, not its alias.
const templateExclusions = new Set(["ADDR", "DB_PATH", "DEV_AUTH", "DEV_AUTH_KEY", "GOOGLE_DRIVE_FOLDER_ID"]);

test("every server environment key is documented", () => {
  const missing = [...keys].filter((key) => !configuration.includes(`\`${key}\``));
  assert.deepEqual(missing, [], "add server settings to docs/configuration.md");
});

test("deploy-appropriate server environment keys have template entries", () => {
  const missing = [...keys].filter((key) => !templateExclusions.has(key) && !hasEntry(template, key));
  assert.deepEqual(missing, [], "add active or commented entries to dashboard.env.example");
});

test("container and backup settings have template entries", () => {
  const expected = [
    "LITESTREAM_BUCKET",
    "LITESTREAM_ENDPOINT",
    "LITESTREAM_REGION",
    "LITESTREAM_PATH",
    "LITESTREAM_ACCESS_KEY_ID",
    "LITESTREAM_SECRET_ACCESS_KEY",
    "KEEP",
    "BACKUP_REMOTE",
  ];
  assert.deepEqual(expected.filter((key) => !hasEntry(template, key)), []);
});
