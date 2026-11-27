#!/usr/bin/env node
// Reads a JSON array of commit message bodies on stdin (the commits of a
// Dependabot PR) and prints the highest semver update-type across every
// commit's structured metadata trailer: the YAML `updated-dependencies`
// block after the final "---" of each body.
//
// Fails closed by printing "none" unless every commit carries a well-formed
// trailer whose updated-dependencies list is nonempty and every entry has a
// recognized semver update-type. Any missing, partial, malformed or
// unrecognized metadata leaves the PR to a person instead of authorizing a
// merge. Invalid JSON input also prints "none" rather than failing.
//
// Deliberately uses no third-party YAML library: the trailer subset
// Dependabot emits (one top-level key whose value is a list of flat
// string-valued maps) is parsed directly, and anything outside that subset
// is rejected, which also fails closed.
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

const RECOGNIZED = {
  'version-update:semver-major': 3,
  'version-update:semver-minor': 2,
  'version-update:semver-patch': 1,
};

const scalar = (raw) => {
  const value = raw.trim();
  if (value === '') return null;
  const match = /^("([^"]*)"|'([^']*)')$/.exec(value);
  return match ? (match[2] ?? match[3]) : value;
};

// Parses the restricted trailer shape into {key: [entry, ...]} or null.
// Accepted: top-level "key:" (list follows) or "key: value" lines, list
// items "- key: value" at any indent, and their more-indented
// "key: value" continuations. Everything else is rejected.
function parseSimpleYaml(lines) {
  const result = {};
  let listKey = null;
  let entry = null;
  for (const line of lines) {
    if (line.trim() === '') continue;
    let match = /^[ \t]*-[ \t]+([A-Za-z0-9_-]+):[ \t]*(.*)$/.exec(line);
    if (match) {
      if (listKey === null) return null;
      entry = {};
      result[listKey].push(entry);
      entry[match[1]] = scalar(match[2]);
      continue;
    }
    match = /^([A-Za-z0-9_-]+):[ \t]*(.*)$/.exec(line);
    if (match) {
      listKey = null;
      entry = null;
      if (match[2].trim() === '') {
        result[match[1]] = [];
        listKey = match[1];
      } else if (match[2].trim() === '[]') {
        result[match[1]] = [];
      } else {
        result[match[1]] = scalar(match[2]);
      }
      continue;
    }
    match = /^[ \t]+([A-Za-z0-9_-]+):[ \t]*(.*)$/.exec(line);
    if (match && entry !== null) {
      entry[match[1]] = scalar(match[2]);
      continue;
    }
    return null;
  }
  return result;
}

export function parseTrailer(body) {
  if (typeof body !== 'string') return null;
  const lines = body.split(/\r?\n/);
  for (let i = lines.length - 1; i >= 0; i--) {
    if (lines[i].trim() === '---') return parseSimpleYaml(lines.slice(i + 1));
  }
  return null;
}

export function highestUpdateType(bodies) {
  if (!Array.isArray(bodies) || bodies.length === 0) return 'none';
  let best = 'none';
  let rank = 0;
  for (const body of bodies) {
    const data = parseTrailer(body);
    if (data === null) return 'none';
    const deps = data['updated-dependencies'];
    if (!Array.isArray(deps) || deps.length === 0) return 'none';
    for (const entry of deps) {
      if (entry === null || typeof entry !== 'object' || Array.isArray(entry)) return 'none';
      const update = entry['update-type'];
      if (typeof update !== 'string' || !(update in RECOGNIZED)) return 'none';
      if (RECOGNIZED[update] > rank) {
        rank = RECOGNIZED[update];
        best = update;
      }
    }
  }
  return best;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  let bodies = [];
  try {
    bodies = JSON.parse(readFileSync(0, 'utf8'));
  } catch {
    bodies = [];
  }
  process.stdout.write(`${highestUpdateType(bodies)}\n`);
}
