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
const backupTemplate = read("server/deploy/backup.env.example");
const allTemplates = template + "\n" + backupTemplate;
const hasEntry = (source, key) => new RegExp(`(?:^|\\n)#?${key}(?:_FILE)?=`).test(source);

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
    "BACKUP_SINK_URL",
    "BACKUP_BEARER_SECRET",
    "BACKUP_ACCESS_CLIENT_ID",
    "BACKUP_ACCESS_CLIENT_SECRET",
    "BACKUP_AGE_RECIPIENT",
    "BACKUP_MEMBERSHIP_ACCOUNT_ID",
    "BACKUP_MEMBERSHIP_DATABASE_ID",
    "BACKUP_MEMBERSHIP_API_TOKEN",
    "KEEP",
    "BACKUP_REMOTE",
  ];
  assert.deepEqual(expected.filter((key) => !hasEntry(allTemplates, key)), []);
});

test("backup client environment keys are documented and templated", () => {
  const source = read('server/deploy/backup.py');
  const inventory = [...source.matchAll(/os\.environ(?:\[|\.get\()'([A-Z_]+)'/g)].map(m => m[1]);
  assert.ok(inventory.length >= 8);
  assert.deepEqual(inventory.filter(key => !configuration.includes(`\`${key}\``)), []);
  assert.deepEqual(inventory.filter(key => key !== 'DB_PATH' && !hasEntry(allTemplates, key)), []);
  assert.ok(!source.includes('LITESTREAM_'));
});

test("static container backup settings are documented without an interpreter runtime", () => {
  const source = read('server/cmd/dashboard-backup/main.go');
  const inventory = [...source.matchAll(/setting\("([A-Z_]+)"\)/g)].map(m => m[1]);
  assert.ok(inventory.length >= 8);
  assert.deepEqual(inventory.filter(key => !configuration.includes(`\`${key}\``)), []);
  assert.deepEqual(inventory.filter(key => key !== 'DB_PATH' && !hasEntry(allTemplates, key)), []);
  assert.match(read('server/Dockerfile'), /^FROM busybox:stable-musl@sha256:[0-9a-f]{64} AS runtime$/m);
  assert.ok(!read('server/deploy/entrypoint.sh').includes('python3'));
});


test("self-build production recipe publishes and pins before startup", () => {
  const docs = read('docs/deploy/other-setups.md');
  const recipe = docs.slice(docs.indexOf('## Build the image yourself'), docs.indexOf("## What's in the image"));
  assert.ok(recipe.includes('--push'));
  assert.ok(recipe.includes('imagetools inspect'));
  assert.ok(recipe.includes('server/deploy/prod-image.env'));
  const productionCommands = recipe.split('\n').filter(line => line.startsWith('docker compose') && line.includes('compose.cloudflare.yml'));
  assert.equal(productionCommands.length, 2);
  for (const command of productionCommands) assert.ok(command.includes('--env-file .env --env-file prod-image.env'));
  assert.ok(!read('server/deploy/compose.build.yml').includes('officer-dashboard:local'));
});

test("every env key `dashboard setup host` writes is documented and templated", () => {
  const host = read("server/internal/setup/host.go");
  const layout = read("server/internal/setup/hostfiles/layout.go");
  const templates = {
    ".env": read("server/deploy/.env.example"),
    "dashboard.env": template,
    "backup.env": backupTemplate,
  };
  const written = [...host.matchAll(/Key: "([A-Z][A-Z0-9_]+)"/g)].map((m) => m[1]);
  assert.ok(written.length >= 8, "the keys setup writes should be extracted from host.go");
  const all = Object.values(templates).join("\n");
  assert.deepEqual(written.filter((key) => !configuration.includes(`\`${key}\``)), [], "document in docs/configuration.md");
  assert.deepEqual(written.filter((key) => !new RegExp(`(?:^|\n)#?${key}=`).test(all)), [], "add to an env template");
  // Each secret's *_FILE key must be in the template of the file setup writes it to.
  const secrets = [...layout.matchAll(/EnvFile: "([^"]+)", EnvKey: "([A-Z0-9_]+)"/g)];
  assert.ok(secrets.length >= 7);
  for (const [, file, key] of secrets) {
    assert.ok(templates[file], `unknown env file ${file}`);
    assert.match(templates[file], new RegExp(`(?:^|\n)#?${key}=`), `${key} belongs in ${file}.example`);
    assert.ok(configuration.includes(`\`${key}\``), `${key} undocumented in docs/configuration.md`);
  }
});
