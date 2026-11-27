import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { highestUpdateType, parseTrailer } from './dependabot-update-type.mjs';

const dep = (name, type) =>
  `  - dependency-name: ${name}\n    dependency-type: direct:production\n    update-type: ${type}`;
const block = (...deps) => `---\nupdated-dependencies:\n${deps.join('\n')}`;

const recognized = [
  ['update-minor', [`Bumps foo.\n\n${block(dep('foo', 'version-update:semver-minor'))}`], 'version-update:semver-minor'],
  ['update-patch', [`Bumps x from 1 to 2.\n\n${block(dep('x', 'version-update:semver-patch'))}`], 'version-update:semver-patch'],
  ['grouped takes the maximum', [block(dep('foo', 'version-update:semver-patch'), dep('bar', 'version-update:semver-major'))], 'version-update:semver-major'],
  ['multi-commit takes the maximum', [block(dep('a', 'version-update:semver-patch')), block(dep('b', 'version-update:semver-minor'))], 'version-update:semver-minor'],
  ['unindented list items parse', ['---\nupdated-dependencies:\n- dependency-name: foo\n  update-type: version-update:semver-patch'], 'version-update:semver-patch'],
  ['quoted values parse', [`---\nupdated-dependencies:\n- dependency-name: "@scope/pkg"\n  update-type: 'version-update:semver-minor'`], 'version-update:semver-minor'],
  ['CRLF bodies parse', ['Bumps foo.\r\n\r\n---\r\nupdated-dependencies:\r\n- dependency-name: foo\r\n  update-type: version-update:semver-patch'], 'version-update:semver-patch'],
  ['text before the trailer is ignored', [`Bumps foo from 1 to 2.\n\nRelease notes: a --- b.\n\n${block(dep('foo', 'version-update:semver-patch'))}`], 'version-update:semver-patch'],
];
for (const [name, bodies, expected] of recognized) {
  test(`recognized: ${name}`, () => assert.equal(highestUpdateType(bodies), expected));
}

const rejected = [
  ['empty bodies', []],
  ['no trailer', ['A hand-written commit with no metadata.']],
  ['one commit lacks metadata', [block(dep('a', 'version-update:semver-patch')), 'A commit with no metadata trailer.']],
  ['unknown update-type', [block(dep('foo', 'version-update:semver-bogus'))]],
  ['entry missing update-type', [block('  - dependency-name: foo')]],
  ['mixed known and unknown', [block(`${dep('foo', 'version-update:semver-patch')}\n  - dependency-name: bar`)]],
  ['empty list', ['---\nupdated-dependencies: []']],
  ['key with no entries', ['---\nupdated-dependencies:']],
  ['malformed list item', ['---\nupdated-dependencies:\n  - [unclosed']],
  ['scalar entry', ['---\nupdated-dependencies:\n  - justastring']],
  ['scalar trailer', ['---\nnot a mapping']],
  ['nested mapping value', ['---\nupdated-dependencies:\n  - dependency-name: foo\n    update-type:\n      nested: value']],
  ['non-string body', [42]],
  ['non-array input', 'not an array'],
];
for (const [name, bodies] of rejected) {
  test(`fail closed: ${name}`, () => assert.equal(highestUpdateType(bodies), 'none'));
}

test('parseTrailer returns null without a --- marker', () => {
  assert.equal(parseTrailer('no marker here'), null);
});

test('CLI prints the highest type for a JSON array on stdin', () => {
  const script = fileURLToPath(new URL('./dependabot-update-type.mjs', import.meta.url));
  const run = (input) => execFileSync(process.execPath, [script], { input, encoding: 'utf8' });
  assert.equal(run(JSON.stringify([block(dep('foo', 'version-update:semver-major'))])), 'version-update:semver-major\n');
  assert.equal(run('not json', 'utf8'), 'none\n', 'invalid JSON reports none instead of failing');
  assert.equal(run('[]'), 'none\n');
});
