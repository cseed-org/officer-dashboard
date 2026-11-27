import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';
import { parse } from 'yaml';

const load = (file) => parse(readFileSync(new URL(`../.github/workflows/${file}`, import.meta.url), 'utf8'));
const ci = load('ci.yml');
const scanner = load('gitleaks.yml');

// Evaluate the actual job condition, including Actions' implicit success() for needs.
// Deliberately supports only this workflow's push branch filters, so adding a new
// publication event requires extending these security regression cases.
function canPublish(workflow, github, results = {}) {
  assert.deepEqual(Object.keys(workflow.on).sort(), ['pull_request', 'push']);
  assert.deepEqual(Object.keys(workflow.on.push), ['branches']);
  const triggered = github.event_name === 'pull_request' || (
    github.event_name === 'push' && github.ref.startsWith('refs/heads/') &&
    workflow.on.push.branches.includes(github.ref.slice('refs/heads/'.length))
  );
  const job = workflow.jobs.publish;
  assert.equal(job['continue-on-error'], undefined);
  assert.doesNotMatch(job.if, /always\(|failure\(|cancelled\(|success\(/);
  return triggered && job.needs.every((name) => (results[name] ?? 'success') === 'success') &&
    runInNewContext(job.if, { github }, { timeout: 1000 });
}

const main = { event_name: 'push', ref: 'refs/heads/main' };
test('only a main push can reach registry login and publication', () => {
  assert.equal(canPublish(ci, main), true);
  for (const event of [
    { event_name: 'push', ref: 'refs/heads/off-main' },
    // Neither an off-main nor an old pre-guard revision can be selected by a tag event.
    { event_name: 'push', ref: 'refs/tags/v1.0.0-off-main' },
    { event_name: 'push', ref: 'refs/tags/v0.1.0-pre-guard' },
    { event_name: 'pull_request', ref: 'refs/pull/103/merge' },
  ]) assert.equal(canPublish(ci, event), false, JSON.stringify(event));
  const checkout = ci.jobs.publish.steps.find((step) => step.uses?.startsWith('actions/checkout@'));
  assert.equal(checkout.with.ref, undefined, 'publish must build the triggering main revision');
  assert.equal(checkout.with['persist-credentials'], false);
  const meta = ci.jobs.publish.steps.find((step) => step.id === 'meta');
  assert.doesNotMatch(meta.with.tags, /semver|latest/);
  assert.match(meta.with.tags, /type=sha,prefix=sha-/);
  assert.match(meta.with.tags, /type=raw,value=main/);
  for (const [name, job] of Object.entries(ci.jobs)) {
    if (job.permissions?.packages === 'write') assert.equal(name, 'publish');
  }
});

test('failed, cancelled or skipped secret scanning blocks every publish step', () => {
  assert.ok(ci.jobs.publish.needs.includes('gitleaks'));
  assert.equal(ci.jobs.gitleaks.uses, './.github/workflows/gitleaks.yml');
  assert.equal(ci.jobs.gitleaks.if, undefined);
  assert.equal(ci.jobs.gitleaks['continue-on-error'], undefined);
  assert.deepEqual(scanner.on, { workflow_call: null });
  const scan = scanner.jobs.gitleaks;
  assert.equal(scan.if, undefined);
  assert.equal(scan['continue-on-error'], undefined);
  assert.equal(scan.steps[0].with['fetch-depth'], 0);
  assert.equal(scan.steps[0].with.ref, undefined, 'scan the same source revision as publication');
  const command = scan.steps.find((step) => step.name === 'Scan full history for secrets');
  assert.equal(command['continue-on-error'], undefined);
  assert.match(command.run, /sha256sum --check --strict/);
  assert.match(command.run.trim().split('\n').at(-1), /^"\$RUNNER_TEMP\/gitleaks" git .*--redact$/);
  for (const result of ['failure', 'cancelled', 'skipped']) {
    assert.equal(canPublish(ci, main, { gitleaks: result }), false, result);
  }
});

test('publish records the pushed digest in a run-scoped artifact for release promotion', () => {
  const build = ci.jobs.publish.steps.find((step) => step.id === 'build');
  assert.match(build.uses, /^docker\/build-push-action@/);
  const record = ci.jobs.publish.steps.find((step) => step.name === 'Record the pushed digest for release promotion');
  assert.equal(record.env.DIGEST, '${{ steps.build.outputs.digest }}');
  assert.match(record.run, /\^sha256:\[a-f0-9\]\{64\}\$/);
  const uploads = ci.jobs.publish.steps.filter((step) => step.uses?.startsWith('actions/upload-artifact@'));
  assert.equal(uploads.length, 1);
  assert.equal(uploads[0].with.name, 'image-digest-${{ github.run_attempt }}', 'the artifact name pins the run attempt');
  assert.equal(uploads[0].with.path, 'image-digest.txt');
  assert.equal(uploads[0].with['if-no-files-found'], 'error');
  assert.equal(uploads[0].with['retention-days'], 90, 'promotion window documented in docs/deployment.md');
});
