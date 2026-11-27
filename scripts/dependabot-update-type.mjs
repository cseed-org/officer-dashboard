#!/usr/bin/env node
// Reads a JSON array of commit message bodies on stdin (the commits of a
// Dependabot PR) and prints the highest semver update-type across every
// commit's structured metadata trailer: the YAML `updated-dependencies`
// block after the final "---" of each body, up to its "..." document-end
// marker when Dependabot's envelope includes one.
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
// Grammar: top-level "key:" (list follows), "key: []" or "key: value"
// lines at column 0; list items "- key: value" all at one consistent
// indent; and each entry's direct "key: value" continuations all at the
// column of the entry's first key, which is fixed by the key's position
// after the "- " marker rather than inferred from a later line. A deeper
// or misaligned line (a nested mapping or list), an empty value (a
// nested-map introducer) and a duplicate key anywhere are rejected, so a
// value can only ever come from a direct property of its own list entry —
// never supplied or overwritten by a nested mapping, not even one nested
// beneath the item's first key.
function parseSimpleYaml(lines) {
  const result = Object.create(null);
  let listKey = null; // top-level key whose list is being collected
  let listIndent = null; // indent shared by every item of that list
  let entry = null; // current list entry
  let entryIndent = null; // indent shared by the entry's direct keys
  for (const line of lines) {
    if (line.trim() === '') continue;
    let match = /^([ \t]*)-([ \t]+)([A-Za-z0-9_-]+):[ \t]*(.*)$/.exec(line);
    if (match) {
      if (listKey === null) return null;
      if (listIndent === null) listIndent = match[1].length;
      if (match[1].length !== listIndent) return null;
      const value = scalar(match[4]);
      if (value === null) return null; // empty value opens a nested mapping
      entry = Object.create(null);
      // The entry's direct keys sit at the column of this first key, fixed
      // here from the "- " marker — not inferred from a continuation line.
      entryIndent = match[1].length + 1 + match[2].length;
      result[listKey].push(entry);
      entry[match[3]] = value;
      continue;
    }
    match = /^([A-Za-z0-9_-]+):[ \t]*(.*)$/.exec(line);
    if (match) {
      listKey = null;
      listIndent = null;
      entry = null;
      entryIndent = null;
      if (match[1] in result) return null; // duplicate top-level key
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
    match = /^([ \t]+)([A-Za-z0-9_-]+):[ \t]*(.*)$/.exec(line);
    if (match && entry !== null && match[1].length > listIndent) {
      if (match[1].length !== entryIndent) return null; // nested mapping or misaligned keys
      if (match[2] in entry) return null; // duplicate key: ambiguous, reject
      const value = scalar(match[3]);
      if (value === null) return null; // empty value opens a nested mapping
      entry[match[2]] = value;
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
    if (lines[i].trim() !== '---') continue;
    // The metadata document runs from the marker to its YAML document-end
    // ("...") when Dependabot's envelope includes one; whatever follows the
    // terminator (the Git signoff) is not part of the document.
    let doc = lines.slice(i + 1);
    const end = doc.findIndex((line) => line.trim() === '...');
    if (end !== -1) doc = doc.slice(0, end);
    return parseSimpleYaml(doc);
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
