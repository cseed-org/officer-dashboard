import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import { CONTRACTS, endpointsModule, writeEndpoints } from '../web/integrations/endpoints.mjs';

const root = resolve(import.meta.dirname, '..');
test('M-013: MCP freshness accepts a CRLF copy of the generated output', () => {
  const file = join(root, 'server/internal/api/mcp_tools.json');
  const original = readFileSync(file);
  try {
    writeFileSync(file, original.toString().replace(/\r?\n/g, '\r\n'));
    assert.match(execFileSync(process.execPath, ['scripts/gen-mcp-tools.mjs', '--check'], { cwd: root, encoding: 'utf8' }), /up to date/);
  } finally { writeFileSync(file, original); }
});

test('M-013: endpoint generation leaves an equivalent CRLF file untouched', () => {
  const work = mkdtempSync(join(process.env.PANE_SCRATCH_DIR ?? tmpdir(), 'endpoints-test-'));
  try {
    const file = join(work, 'endpoints.generated.ts');
    const crlf = endpointsModule(readFileSync(CONTRACTS, 'utf8')).replace(/\n/g, '\r\n');
    writeFileSync(file, crlf);
    assert.equal(writeEndpoints(file), false);
    assert.equal(readFileSync(file, 'utf8'), crlf);
  } finally { rmSync(work, { recursive: true, force: true }); }
});
