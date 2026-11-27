import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { highestUpdateType, parseTrailer } from './dependabot-update-type.mjs';

const dep = (name, type) =>
  `  - dependency-name: ${name}\n    dependency-type: direct:production\n    update-type: ${type}`;
const block = (...deps) => `---\nupdated-dependencies:\n${deps.join('\n')}`;
// Dependabot's fetch-metadata envelope: the YAML document is terminated by
// "..." and followed by the Git signoff trailer.
const signoff = '\n...\n\nSigned-off-by: dependabot[bot] <support@github.com>';

const recognized = [
  ['update-minor', [`Bumps foo.\n\n${block(dep('foo', 'version-update:semver-minor'))}`], 'version-update:semver-minor'],
  ['update-patch', [`Bumps x from 1 to 2.\n\n${block(dep('x', 'version-update:semver-patch'))}`], 'version-update:semver-patch'],
  ['grouped takes the maximum', [block(dep('foo', 'version-update:semver-patch'), dep('bar', 'version-update:semver-major'))], 'version-update:semver-major'],
  ['multi-commit takes the maximum', [block(dep('a', 'version-update:semver-patch')), block(dep('b', 'version-update:semver-minor'))], 'version-update:semver-minor'],
  ['unindented list items parse', ['---\nupdated-dependencies:\n- dependency-name: foo\n  update-type: version-update:semver-patch'], 'version-update:semver-patch'],
  ['quoted values parse', [`---\nupdated-dependencies:\n- dependency-name: "@scope/pkg"\n  update-type: 'version-update:semver-minor'`], 'version-update:semver-minor'],
  ['CRLF bodies parse', ['Bumps foo.\r\n\r\n---\r\nupdated-dependencies:\r\n- dependency-name: foo\r\n  update-type: version-update:semver-patch'], 'version-update:semver-patch'],
  ['text before the trailer is ignored', [`Bumps foo from 1 to 2.\n\nRelease notes: a --- b.\n\n${block(dep('foo', 'version-update:semver-patch'))}`], 'version-update:semver-patch'],
  // N4: the "..." document-end marker and trailing signoff are accepted.
  ['document-end marker with signoff (minor)', [`Bumps foo.\n\n${block(dep('foo', 'version-update:semver-minor'))}${signoff}`], 'version-update:semver-minor'],
  ['document-end marker with signoff (major)', [`${block(dep('foo', 'version-update:semver-major'))}${signoff}`], 'version-update:semver-major'],
  ['document-end marker with signoff (grouped)', [`${block(dep('foo', 'version-update:semver-patch'), dep('bar', 'version-update:semver-major'))}${signoff}`], 'version-update:semver-major'],
  ['document-end marker with signoff (CRLF)', ['---\r\nupdated-dependencies:\r\n- dependency-name: foo\r\n  update-type: version-update:semver-patch\r\n...\r\n\r\nSigned-off-by: dependabot[bot] <support@github.com>'], 'version-update:semver-patch'],
  ['document-end marker without signoff', [`${block(dep('foo', 'version-update:semver-patch'))}\n...`], 'version-update:semver-patch'],
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
  // N4: a malformed document before its terminator stays fail-closed.
  ['malformed line before document-end marker', [`${block(dep('foo', 'version-update:semver-patch'))}\n  bogus line${signoff}`]],
  // N5: a nested mapping must neither supply nor overwrite a direct
  // update-type; ambiguous or duplicate keys fail closed.
  ['nested mapping cannot supply the update-type', ['---\nupdated-dependencies:\n- dependency-name: foo\n  unexpected:\n    update-type: version-update:semver-patch']],
  ['nested mapping cannot overwrite a direct major', ['---\nupdated-dependencies:\n- dependency-name: foo\n  update-type: version-update:semver-major\n  nested:\n    update-type: version-update:semver-patch']],
  ['duplicate update-type keys are ambiguous', ['---\nupdated-dependencies:\n- dependency-name: foo\n  update-type: version-update:semver-major\n  update-type: version-update:semver-patch']],
  ['misaligned continuation keys', ['---\nupdated-dependencies:\n- dependency-name: foo\n  update-type: version-update:semver-patch\n   dependency-type: direct:production']],
  ['duplicate top-level keys are ambiguous', [`${block(dep('foo', 'version-update:semver-patch'))}\nupdated-dependencies: []`]],
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
