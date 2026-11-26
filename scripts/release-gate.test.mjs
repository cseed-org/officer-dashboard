import assert from 'node:assert/strict';
import { test } from 'node:test';
import { hasReleaseWorthyCommit } from './release-gate.mjs';

const eligible = [
  'feat: add calendar export',
  'fix(api): handle expired sign-in',
  'perf: skip redundant renders',
  'deps: bump foo from 1 to 2',
  'deps(npm): bump foo from 1 to 2',
  'feat!: drop the legacy endpoint',
  'fix(api)!: require signed webhooks',
  'deps!: require Node 24',
];
const ineligible = [
  // Hidden types never cut a release, even with a breaking marker; their
  // BREAKING CHANGE footers never reach this script (subjects only), but the
  // subject alone must not qualify either.
  'ci!: replace the build environment',
  'chore!: drop support for old browsers',
  'chore(main): release 0.15.0',
  'chore: tidy scripts',
  'docs: clarify deployment',
  'ci: pin actions by SHA',
  'build: switch bundler',
  'test: add fixtures',
  'refactor: split parser',
  'style: format',
  'revert: undo feat',
  'deps-dev: bump eslint from 1 to 2',
  'deps-dev(npm): bump eslint',
  'Bump foo from 1 to 2',
  'release 0.15.0',
  'feat:',
  'feat: ',
  'feat : spaced type',
  'performance: not the perf type',
];

for (const subject of eligible) {
  test(`release-worthy: ${subject}`, () => {
    assert.equal(hasReleaseWorthyCommit(subject), true);
  });
}
for (const subject of ineligible) {
  test(`not release-worthy: ${JSON.stringify(subject)}`, () => {
    assert.equal(hasReleaseWorthyCommit(subject), false);
  });
}

test('any single release-worthy subject in history qualifies', () => {
  assert.equal(hasReleaseWorthyCommit('chore: tidy\ndocs: clarify\nfix: repair login\n'), true);
});
test('hidden-only history and empty history do not qualify', () => {
  assert.equal(hasReleaseWorthyCommit('chore: a\nci!: breaking build change\ndocs: b\n'), false);
  assert.equal(hasReleaseWorthyCommit(''), false);
  assert.equal(hasReleaseWorthyCommit('\n'), false);
});
