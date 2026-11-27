import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import { parse } from 'yaml';

// D-14 fail-closed layer: the Dependabot odd-major ignore lists in
// .github/dependabot.yml are finite, so they cannot by themselves guarantee
// that a future non-LTS (odd) Node major is never proposed or merged. This
// test is the enforcement: CI goes red if any pinned Node major in the
// repository is odd, or if the ignore ranges stop covering odd majors up to
// the highest pinned major + 10 (the signal to extend the lists).

const read = (rel) => readFileSync(new URL(`../${rel}`, import.meta.url), 'utf8');

// Every place a Node major is pinned: { source, major }.
function pinnedNodeMajors() {
  const pins = [];
  for (const file of ['server/Dockerfile', 'docker/dev.Dockerfile']) {
    const text = read(file);
    for (const match of text.matchAll(/^FROM\s+(?:--platform=\S+\s+)*node:(\d+)/gm)) {
      pins.push({ source: `${file} FROM node:${match[1]}`, major: Number(match[1]) });
    }
  }
  const workflows = readdirSync(new URL('../.github/workflows/', import.meta.url))
    .filter((name) => name.endsWith('.yml') || name.endsWith('.yaml'));
  for (const name of workflows) {
    const text = read(`.github/workflows/${name}`);
    for (const match of text.matchAll(/node-version:\s*['"]?(\d+)/g)) {
      pins.push({ source: `.github/workflows/${name} node-version ${match[1]}`, major: Number(match[1]) });
    }
  }
  for (const file of ['package.json', 'web/package.json']) {
    const manifest = JSON.parse(read(file));
    const typesNode = manifest.dependencies?.['@types/node'] ?? manifest.devDependencies?.['@types/node'];
    if (typesNode) {
      const major = Number(typesNode.match(/\d+/)[0]);
      pins.push({ source: `${file} @types/node ${typesNode}`, major });
    }
    const engines = manifest.engines?.node;
    if (engines) {
      const lowerBound = engines.match(/(\d+)/);
      assert.ok(lowerBound, `${file} engines.node "${engines}" has no numeric lower bound`);
      pins.push({ source: `${file} engines.node ${engines}`, major: Number(lowerBound[1]) });
    }
  }
  assert.ok(pins.length > 0, 'expected to find at least one pinned Node major');
  return pins;
}

// Odd majors ignored per Dependabot update entry, parsed from the actual
// dependabot.yml syntax (Bundler-style ranges for docker, X-ranges for npm).
function ignoredOddMajors(update) {
  const covered = new Set();
  for (const rule of update.ignore ?? []) {
    for (const version of rule.versions ?? []) {
      const range = version.match(/^>=\s*(\d+)\s*,\s*<\s*(\d+)$/);
      if (range) {
        for (let m = Number(range[1]); m < Number(range[2]); m += 1) covered.add(m);
        continue;
      }
      const xRange = version.match(/^(\d+)\.x$/);
      assert.ok(xRange, `unrecognized ignore version pattern "${version}"`);
      covered.add(Number(xRange[1]));
    }
  }
  return covered;
}

test('every pinned Node major is even (LTS)', () => {
  const pins = pinnedNodeMajors();
  for (const { source, major } of pins) {
    assert.equal(major % 2, 0, `${source} pins odd (non-LTS) Node major ${major}; D-14 keeps the repo on even LTS majors`);
  }
});

test('dependabot odd-major ignores stay ahead of the pinned majors', () => {
  const highest = Math.max(...pinnedNodeMajors().map(({ major }) => major));
  const dependabot = parse(read('.github/dependabot.yml'));
  const nodeEntries = dependabot.updates.filter((update) =>
    (update.ignore ?? []).some((rule) => rule['dependency-name'] === 'node' || rule['dependency-name'] === '@types/node'));
  assert.ok(nodeEntries.length >= 3, 'expected docker /server, docker /docker and npm /web ignore entries');
  for (const update of nodeEntries) {
    const covered = ignoredOddMajors(update);
    for (const major of covered) {
      assert.equal(major % 2, 1, `${update['package-ecosystem']} ${update.directory} ignores even major ${major}, blocking LTS proposals`);
    }
    const first = Math.min(...covered);
    for (let m = first; m <= highest + 10; m += 2) {
      assert.ok(covered.has(m),
        `${update['package-ecosystem']} ${update.directory} does not ignore odd major ${m} ` +
        `(highest pinned major is ${highest}); extend the ignore list in .github/dependabot.yml`);
    }
  }
});
