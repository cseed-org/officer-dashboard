#!/usr/bin/env node
// Reports whether any conventional-commit subject on stdin can cut a release:
// only feat, fix, perf and deps qualify (optional scope, optional breaking
// "!"). Hidden types never qualify, even with a breaking marker or a BREAKING
// CHANGE footer, because release-please keeps breaking notes for hidden types.
// release-please.yml skips its release-pr step when this prints "false".
// Callers pass subjects only (`git log --format=%s`), one per line.
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

const RELEASE_WORTHY = /^(feat|fix|perf|deps)(\([^()]+\))?!?:[ \t].+/;

export function hasReleaseWorthyCommit(subjects) {
  return subjects.split(/\r?\n/).some((line) => RELEASE_WORTHY.test(line.trim()));
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const eligible = hasReleaseWorthyCommit(readFileSync(0, 'utf8'));
  process.stdout.write(eligible ? 'true\n' : 'false\n');
}
