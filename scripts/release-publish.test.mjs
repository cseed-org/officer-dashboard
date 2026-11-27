import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { parse } from 'yaml';

const load = (file) => parse(readFileSync(new URL(`../.github/workflows/${file}`, import.meta.url), 'utf8'));
const publish = load('release-publish.yml');
const ci = load('ci.yml');
const steps = publish.jobs['release-image'].steps;
const gate = steps.find((step) => step.id === 'release');
const digest = steps.find((step) => step.id === 'digest');
const promote = steps.find((step) => step.id === 'image');

test('digest artifact comes from the run-level route, pinned to the validated attempt', () => {
  assert.match(gate.run, /attempts\/\$run_attempt\/jobs/, 'attempt-specific job validation stays');
  assert.match(digest.run, /actions\/runs\/\$RUN_ID\/artifacts/, 'supported run-level artifacts route');
  assert.doesNotMatch(digest.run, /attempts\/\$RUN_ATTEMPT\/artifacts/, 'no unsupported per-attempt artifacts route');
  assert.match(digest.run, /name="image-digest-\$RUN_ATTEMPT"/, 'attempt pinned in the artifact name');
  assert.match(digest.run, /\.expired \| not/, 'expired artifacts are excluded');
  assert.match(digest.run, /length.*!= 1/, 'exactly one matching artifact is required');
  assert.match(digest.run, /actions\/artifacts\/\$artifact_id\/zip/, 'immutable artifact-id download');
  const upload = ci.jobs.publish.steps.find((step) => step.uses?.startsWith('actions/upload-artifact@'));
  assert.equal(upload.with.name, 'image-digest-${{ github.run_attempt }}', 'CI names the artifact with its run attempt');
});

test('alias precedence compares published stable releases, never bare tags', () => {
  assert.match(promote.run, /repos\/\$GITHUB_REPOSITORY\/releases\?per_page=100/, 'candidates come from the Releases API');
  assert.match(promote.run, /--paginate/, 'releases are paginated');
  assert.match(promote.run, /\.draft \| not/, 'draft releases excluded');
  assert.match(promote.run, /\.prerelease \| not/, 'GitHub prereleases excluded');
  assert.match(promote.run, /\.tag_name/, 'release tags feed the comparison');
  assert.doesNotMatch(promote.run, /repos\/\$GITHUB_REPOSITORY\/tags/, 'bare tags never reserve alias precedence');
  assert.match(promote.run, /\^v\(0\|\[1-9\]\[0-9\]\*\)/, 'strict stable vX.Y.Z filter stays');
  assert.match(promote.run, /sort -V/, 'semver-aware comparison stays');
});

test('exact-version guard fails closed on unresolved registry errors', () => {
  assert.doesNotMatch(promote.run, /imagetools inspect[^\n]*\|\| true/, 'no blanket swallow of lookup failures');
  assert.match(promote.run, /for try in 1 2 3 4/, 'bounded retries with backoff');
  assert.match(promote.run, /not found\|manifest unknown/i, 'confirmed-absent detection');
  assert.match(promote.run, /refusing to promote without knowing whether the tag already exists/, 'unresolved errors fail closed');
});

test('dependabot auto-merge parses metadata with the tested repo script, not runner-incidental PyYAML', () => {
  const automerge = load('dependabot-automerge.yml');
  const text = readFileSync(new URL('../.github/workflows/dependabot-automerge.yml', import.meta.url), 'utf8');
  assert.doesNotMatch(text, /python3|PyYAML|import yaml/, 'no runner-incidental Python dependency');
  const update = automerge.jobs['dependabot-automerge'].steps.find((step) => step.id === 'update');
  assert.match(update.run, /node scripts\/dependabot-update-type\.mjs/, 'parses via the tested script');
  const checkout = automerge.jobs['dependabot-automerge'].steps.find((step) => step.uses?.startsWith('actions/checkout@'));
  assert.equal(checkout.with.ref, '${{ github.event.pull_request.base.sha }}', 'the parser comes from the trusted base branch');
  assert.equal(checkout.with['persist-credentials'], false);
  assert.ok(automerge.jobs['dependabot-automerge'].steps.some((step) => step.uses?.startsWith('actions/setup-node@')), 'pinned Node runtime');
});
