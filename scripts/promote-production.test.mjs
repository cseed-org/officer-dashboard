// npm run test:scripts (node --test "scripts/*.test.mjs")
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { parse } from 'yaml';

const workflow = parse(readFileSync(new URL('../.github/workflows/promote-production.yml', import.meta.url), 'utf8'));
const precheck = workflow.jobs.promote.steps.find((step) => step.name === 'Require main ancestry and a successful CI publish').run;

// The environment precheck must fail closed: protected branches pass directly,
// otherwise custom branch policies must be enabled and the complete paginated
// policy list must be exactly one branch-type policy named main.
assert.match(precheck, /--paginate /, 'enumerate every deployment branch policy page');
const programs = [...precheck.matchAll(/jq -s?e '([^']+)'/g)].map((match) => match[1]);
const protectedRule = programs.find((program) => program.includes('.deployment_branch_policy.protected_branches'));
const customRule = programs.find((program) => program.includes('.deployment_branch_policy.custom_branch_policies'));
const exactPolicy = programs.find((program) => program.includes('[{name: "main", type: "branch"}]'));
assert.ok(protectedRule && customRule && exactPolicy, 'precheck keeps the protected/custom/exact-policy jq predicates');
const pageProjection = [...precheck.matchAll(/--jq '([^']+)'/g)]
  .map((match) => match[1])
  .find((program) => program.includes('branch_policies[]'));
assert.ok(pageProjection, 'the gh api step projects each policy to its name and type');

const jq = spawnSync('jq', ['--version'], { encoding: 'utf8' });
const missing = jq.error ? `jq not on PATH (${jq.error.message}); CI runners provide it` : false;

test('branch-policy precheck accepts only protected branches or exactly one branch-type main policy', { skip: missing }, () => {
  const evaluate = (program, input, slurp = false) => {
    const result = spawnSync('jq', [...(slurp ? ['-s'] : []), program], { input, encoding: 'utf8' });
    assert.equal(result.status, 0, result.stderr || `jq rejected ${program}`);
    return result.stdout.trim() === 'true';
  };
  // Mirrors the workflow: protected branches pass directly; otherwise custom
  // branch policies must be enabled and every paginated policy projected by the
  // gh --jq filter must slurp to exactly one branch-type main policy.
  const gateAllows = (env, pages) => {
    if (evaluate(protectedRule, JSON.stringify(env))) return true;
    if (!evaluate(customRule, JSON.stringify(env))) return false;
    const projected = pages.map((page) => {
      const result = spawnSync('jq', ['-c', pageProjection], { input: JSON.stringify(page), encoding: 'utf8' });
      assert.equal(result.status, 0, result.stderr);
      return result.stdout;
    }).join('');
    return evaluate(exactPolicy, projected, true);
  };

  const custom = { deployment_branch_policy: { protected_branches: false, custom_branch_policies: true } };
  const page = (policies) => ({ total_count: policies.length, branch_policies: policies });
  const main = { name: 'main', type: 'branch' };

  assert.equal(gateAllows({ deployment_branch_policy: { protected_branches: true, custom_branch_policies: false } }, []), true, 'protected branches');
  assert.equal(gateAllows(custom, [page([main])]), true, 'a single branch-type main policy');
  assert.equal(gateAllows(custom, [page([main, { name: '*', type: 'branch' }])]), false, '[main, *] permits unrestricted deployments');
  assert.equal(gateAllows(custom, [page([main, { name: 'main', type: 'tag' }])]), false, 'main branch plus a tag policy');
  assert.equal(gateAllows(custom, [page([])]), false, 'custom policies enabled but none configured');
  assert.equal(gateAllows(custom, []), false, 'custom policies enabled with an empty policy list');
  assert.equal(gateAllows(custom, [page([{ name: 'main', type: 'tag' }])]), false, 'a tag-type main policy');
  assert.equal(gateAllows(custom, [page([{ name: 'develop', type: 'branch' }])]), false, 'a non-main branch policy');
  assert.equal(gateAllows(custom, [page([main]), page([main])]), false, 'main repeated across paginated results');
  assert.equal(gateAllows({ deployment_branch_policy: { protected_branches: false, custom_branch_policies: false } }, [page([main])]), false, 'unrestricted branch policy');
  assert.equal(gateAllows({ deployment_branch_policy: null }, [page([main])]), false, 'no deployment branch policy at all');
});
