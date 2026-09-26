#!/usr/bin/env node
// tsmovecheck: prove a TypeScript pure move (refactor class A). It fails
// unless the only differences between a base ref and the head side are
// wiring (import declarations, re-exports, export lists) and declarations
// that moved between modules unchanged.
//
//   node frontend/scripts/tsmovecheck.mjs [--root dir] [--head <git-ref>] <base-ref> [path...]
//
// Paths default to <root>/src (root defaults to frontend/); the head side
// is the working tree, or --head's ref. Declarations are tsdeclhash's, and
// so is "unchanged": the same text and comments, and every name the
// declaration uses still bound to the same declaration (by content, so a
// moved dependency still matches) or package export. It prints each moved
// declaration as "moved name: from -> to", each module whose wiring alone
// changed, and each declaration that was added, removed or changed (a
// declaration whose text is the same but whose names now refer to other
// declarations says which); any of the last kind exits 1. Exit 2 is a
// usage or read error.

import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { FRONTEND_DIR, load, parseArgs } from './tsdeclhash.mjs';

const byText = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

/** Compares two analysed trees. */
export function moveCheck(base, head) {
  const entries = (mods) =>
    [...mods.values()].flatMap((m) => m.decls.map((d) => ({ module: m.path, name: d.name, own: d.own, hash: d.hash, bindings: d.bindings })));
  const take = (pool, key) => {
    const list = pool.get(key);
    return list && list.length ? list.shift() : null;
  };
  const group = (list, keyOf) => {
    const m = new Map();
    for (const e of [...list].sort((a, b) => byText(a.module, b.module))) {
      const k = keyOf(e);
      if (!m.has(k)) m.set(k, []);
      m.get(k).push(e);
    }
    return m;
  };
  const sortEntries = (list) => list.sort((a, b) => byText(a.module, b.module) || byText(a.name, b.name));
  const headEntries = sortEntries(entries(head));

  // 1. Same module, same declaration.
  const inPlace = group(entries(base), (e) => `${e.module}\t${e.name}\t${e.hash}`);
  let unchanged = 0;
  let rest = headEntries.filter((e) => (take(inPlace, `${e.module}\t${e.name}\t${e.hash}`) ? (unchanged++, false) : true));
  // 2. The same declaration in another module: a move.
  const pool = group([...inPlace.values()].flat(), (e) => `${e.name}\t${e.hash}`);
  const moved = [];
  rest = rest.filter((e) => {
    const from = take(pool, `${e.name}\t${e.hash}`);
    if (!from) return true;
    moved.push(`moved ${e.name}: ${from.module} -> ${e.module}`);
    return false;
  });
  // 3. What is left differs.
  const left = [...pool.values()].flat();
  const failures = [];
  const claim = (pred) => {
    const i = left.findIndex(pred);
    return i < 0 ? null : left.splice(i, 1)[0];
  };
  for (const e of rest) {
    const same = claim((b) => b.module === e.module && b.name === e.name);
    if (same) {
      failures.push(`changed ${e.module} ${e.name}${rebinding(same, e)}`);
      continue;
    }
    const movedText = claim((b) => b.name === e.name && b.own === e.own);
    if (movedText) failures.push(`changed ${e.module} ${e.name}: moved from ${movedText.module}${rebinding(movedText, e)}`);
    else failures.push(`added ${e.module} ${e.name}`);
  }
  for (const b of left) failures.push(`removed ${b.module} ${b.name}`);

  const wiring = (mods) => new Map([...mods.values()].map((m) => [m.path, m.wiring.join('\n')]));
  const bw = wiring(base);
  const hw = wiring(head);
  const rewired = [...new Set([...bw.keys(), ...hw.keys()])]
    .filter((p) => (bw.get(p) ?? '') !== (hw.get(p) ?? ''))
    .sort()
    .map((p) => `rewired ${p}`);
  return { unchanged, moved: moved.sort(), rewired, failures: failures.sort() };
}

/** Names the bindings that differ when a declaration's text did not. */
function rebinding(b, h) {
  if (b.own !== h.own) return '';
  const names = (list) => new Map(list.map((l) => [l.slice(0, l.indexOf('=')), l]));
  const bn = names(b.bindings);
  const hn = names(h.bindings);
  const differ = [...new Set([...bn.keys(), ...hn.keys()])].filter((k) => bn.get(k) !== hn.get(k)).sort();
  return differ.length ? ` (same text, but what ${differ.join(', ')} refers to changed)` : '';
}

function main(argv) {
  const usage = 'usage: tsmovecheck.mjs [--root dir] [--head <git-ref>] <base-ref> [path...]\n';
  let opts;
  try {
    opts = parseArgs(argv, { root: 'value', head: 'value', help: 'bool' });
  } catch (e) {
    process.stderr.write(`tsmovecheck: ${e.message}\n${usage}`);
    return 2;
  }
  if (opts.help || opts.rest.length === 0) {
    process.stderr.write(usage);
    return 2;
  }
  const [baseRef, ...rest] = opts.rest;
  const root = path.resolve(opts.root ?? FRONTEND_DIR);
  const paths = (rest.length ? rest : [path.join(root, 'src')]).map((p) => path.resolve(p));
  let result;
  try {
    result = moveCheck(load(root, baseRef, paths), load(root, opts.head, paths));
  } catch (e) {
    process.stderr.write(`tsmovecheck: ${e.stderr ? e.stderr.toString().trim() : e.message}\n`);
    return 2;
  }
  const { unchanged, moved, rewired, failures } = result;
  for (const l of [...moved, ...rewired, ...failures]) process.stdout.write(`${l}\n`);
  const head = opts.head ?? 'the working tree';
  const summary = `${moved.length} moved, ${unchanged} unchanged, ${rewired.length} modules rewired`;
  if (failures.length) {
    process.stdout.write(`tsmovecheck: not a pure move from ${baseRef} to ${head}: ${failures.length} declarations differ (${summary})\n`);
    return 1;
  }
  process.stdout.write(`tsmovecheck: a pure move from ${baseRef} to ${head} (${summary})\n`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(fs.realpathSync(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}
