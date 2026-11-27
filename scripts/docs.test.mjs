// npm run test:scripts (node --test "scripts/*.test.mjs")
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { test } from "node:test";
import { extractEndpoints } from "../web/integrations/endpoints.mjs";

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const docsDir = new URL("../docs/", import.meta.url);
const guides = readdirSync(docsDir, { withFileTypes: true })
  .filter((entry) => entry.isFile() && entry.name.endsWith(".md"))
  .map((entry) => entry.name);
const docs = Object.fromEntries(guides.map((name) => [name, readFileSync(new URL(name, docsDir), "utf8")]));
const allDocs = Object.values(docs).join("\n");
const normalizeProse = (markdown) => markdown
  .replace(/\*\*|__/g, "")
  .replace(/\s+/g, " ")
  .trim();
const canonicalPath = (path) => path
  .replace(/\/:[A-Za-z][A-Za-z0-9_]*/g, "/:param")
  .replace(/\/\{[A-Za-z][A-Za-z0-9_]*\}/g, "/:param");
const documentedPath = (path) => {
  const normalized = canonicalPath(allDocs);
  if (normalized.includes(canonicalPath(path))) return true;
  // The Launchpad guide states the canonical prefix once, then uses short rows such
  // as `POST /cohorts/:id`. Accept that shorthand only when the prefix is present.
  const prefix = "/api/buildspace-launchpad";
  return path.startsWith(`${prefix}/`) && normalized.includes(prefix) && normalized.includes(canonicalPath(path.slice(prefix.length)));
};

test("every ENDPOINTS path is documented in a guide", () => {
  const endpoints = extractEndpoints(read("src/core/contracts.ts"));
  const missing = Object.entries(endpoints)
    .filter(([, endpoint]) => !documentedPath(endpoint.path))
    .map(([name, endpoint]) => `${name}: ${endpoint.method} ${endpoint.path}`);
  assert.deepEqual(missing, [], "document each endpoint in docs/api.md or a feature guide");
});

test("API guide references live contract enforcement and implementation files", () => {
  const api = docs["api.md"];
  for (const path of [
    "src/core/contracts.ts",
    "web/src/lib/format.ts",
    "server/internal/api/dto.go",
    "server/internal/purchases/lifecycle.go",
    "scripts/contract-check.mjs",
    "scripts/gen-mcp-tools.mjs",
  ]) {
    assert.ok(existsSync(new URL(`../${path}`, import.meta.url)), `${path} should exist`);
  }
  for (const removed of ["tests/core/contracts.test.ts", "src/core/money.ts", "src/core/metrics.ts", "src/core/purchase-lifecycle.ts", "requireRole(locals"]) {
    assert.ok(!api.includes(removed), `docs/api.md still references removed ${removed}`);
  }
});

test("architecture lists every internal package and current command set", () => {
  const architecture = docs["architecture.md"];
  const internalDir = new URL("../server/internal/", import.meta.url);
  const packages = readdirSync(internalDir, { withFileTypes: true }).filter((entry) => entry.isDirectory()).map((entry) => entry.name);
  assert.deepEqual(packages.filter((name) => !architecture.includes(`internal/${name}`)), []);
  const main = read("server/cmd/dashboard/main.go");
  const usage = /usage: dashboard \[([^\]]+)\]/.exec(main)?.[1];
  const documentedUsage = /cmd\/dashboard\s+main: ([^\n]+)/.exec(architecture)?.[1].replaceAll(" ", "").trim();
  assert.equal(documentedUsage, usage, "architecture command list should match main.go usage");
});

test("migration guidance uses the highest existing file without pinning the next number", () => {
  const contributing = normalizeProse(read("CONTRIBUTING.md"));
  assert.ok(contributing.includes("next zero-padded, numbered SQL file"));
  assert.ok(contributing.includes("after the highest existing file"));
  assert.ok(contributing.includes("illustrative example only"));
  assert.ok(docs["architecture.md"].includes("migrations 0000 onward"));
  assert.ok(!docs["architecture.md"].includes("0000–0010"));
});

test("documentation index links every top-level guide", () => {
  const index = docs["README.md"];
  const missing = guides.filter((name) => name !== "README.md" && !index.includes(`](${name})`));
  assert.deepEqual(missing, []);
});

test("README Astro badge uses the package source and MCP tool count matches generated sources", () => {
  const readme = read("README.md");
  const badge = /\[!\[Astro\]\((https:\/\/img\.shields\.io\/[^)]+)\)\]\(web\/package\.json\)/.exec(readme);
  assert.ok(badge, "README Astro badge should link to web/package.json");
  const source = new URL(badge[1]);
  assert.equal(source.pathname, "/github/package-json/dependency-version/cseed-org/officer-dashboard/astro");
  assert.equal(source.searchParams.get("filename"), "web/package.json");
  const count = JSON.parse(read("server/internal/api/mcp_tools.json")).tools.length;
  const claimed = /works through (\d+) tools/.exec(readme);
  if (claimed) assert.equal(Number(claimed[1]), count);
  assert.ok(docs["mcp.md"].includes(`${count} tools, one per API endpoint`));
});

test("frontend route table covers current pages and login errors", () => {
  const routes = docs["frontend.md"].split("## Routes (for the Go server)")[1].split("## Preferences")[0];
  for (const route of ["/members", "/contacts", "/design", "/onboarding", "/buildspace-launchpad", "/launchpad/", "/launchpad/join", "/launchpad/login", "/speakers", "/buildspace"]) {
    assert.ok(routes.includes(`\`${route}\``), `missing route ${route}`);
  }
  assert.ok(routes.includes("invite_unavailable"));
  const develop = normalizeProse(docs["frontend.md"].split("## Develop")[1].split("## Routes")[0]);
  assert.ok(develop.includes("open `/login`"));
  assert.ok(develop.includes("Developer mode panel"));
  assert.ok(develop.includes("[development](development.md#developer-mode-on-a-test-deployment)"));
  assert.ok(!develop.includes("dev: director / officer"));
});

test("pagination convention names offset and unpaginated exceptions", () => {
  const api = normalizeProse(docs["api.md"]);
  assert.ok(api.includes("Members instead uses `offset`/`limit`"));
  assert.ok(api.includes("Contacts and the Buildspace register"));
  assert.ok(!api.includes("Lists take `?cursor=&limit=`"));
});

test("calendar guides describe the current no-controls widget", () => {
  const calendar = normalizeProse(docs["calendar-widget.md"]);
  for (const view of ["Compact current week", "Expanded current week", "Six-week month grid"]) {
    assert.ok(calendar.includes(view), `missing automatic view ${view}`);
  }
  assert.ok(calendar.includes("no view, source, colour, weekend, subscribe or add-event controls"));
  assert.ok(calendar.includes("`GET /api/calendar/feed"));
  assert.ok(calendar.includes("keyed by range and time zone"));
  assert.ok(!calendar.includes("Options saves view"));
  assert.ok(!calendar.includes("keyed by sources and time zone"));
  assert.ok(docs["purchase-deadlines.md"].includes("Historical V0.6 controls"));
  assert.ok(normalizeProse(docs["buildspace.md"]).includes("projects marked Shipped"));
});
