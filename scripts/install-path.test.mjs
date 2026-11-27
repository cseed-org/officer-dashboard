import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('../', import.meta.url));

test('tracked deployment files do not require a fixed install directory', () => {
  const files = execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
  const fixedPath = '/opt/' + 'officer-dashboard';
  const matches = files.filter(file => !file.startsWith('docs/') && readFileSync(new URL('../' + file, import.meta.url)).includes(fixedPath));
  assert.deepEqual(matches, [], 'use an install-directory placeholder in: ' + matches.join(', '));
});

test('systemd WorkingDirectory paths are unquoted because systemd preserves those quotes', () => {
  const deploy = new URL('../server/deploy/', import.meta.url);
  for (const file of readdirSync(deploy).filter(file => file.endsWith('.service'))) {
    for (const line of readFileSync(new URL(file, deploy), 'utf8').split('\n')) {
      const match = /^\s*WorkingDirectory\s*=\s*(.*)/.exec(line);
      if (match) assert.doesNotMatch(match[1], /^["']/, `${file}: WorkingDirectory treats quotes as part of the path`);
    }
  }
});
