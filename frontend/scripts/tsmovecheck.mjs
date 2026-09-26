#!/usr/bin/env node
// tsmovecheck: prove a TypeScript pure move (refactor class A). It fails
// unless the only differences between a base ref and the head side are
// wiring (import declarations, re-exports, export lists) and declarations
// that moved between modules unchanged, and module evaluation still runs
// what it ran in the same order.
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
// usage or read error, or a path with no .ts/.tsx file on either side.
//
// Evaluation order is part of the proof, and so each of these fails too:
//   - "reordered": runtime statements (all but functions, interfaces, type
//     aliases and `declare`s; side-effect imports included) that share a
//     module on both sides and changed their relative order, and modules a
//     module imports on both sides that it now loads in another order (as
//     compiled: type-only imports do not count);
//   - "new import cycle": a runtime import cycle the base did not have, in
//     which a module can read a binding before it is initialised;
//   - "moved ... changes when it runs": a statement that may have side
//     effects (any call not known to be pure, `new`, assignment, and so on,
//     outside function bodies; a side-effect import of a script) moved to
//     another module, except a module's whole set of them moving in order
//     to a new module that the old one loads. The tool cannot prove other
//     such moves and refuses them. CSS cascade order across modules is
//     S12b's; getters are assumed pure.

import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { FRONTEND_DIR, load, parseArgs, requireSources, runtimeCycles, runtimeImports } from './tsdeclhash.mjs';

const byText = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

/** Compares two analysed trees. */
export function moveCheck(base, head) {
  const entries = (mods) =>
    [...mods.values()].flatMap((m) =>
      m.decls.map((d) => ({ module: m.path, name: d.name, own: d.own, hash: d.hash, bindings: d.bindings, pos: d.pos, runtime: d.runtime, effectful: d.effectful })),
    );
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
  const pairs = []; // [base, head] of every unchanged or moved declaration
  let rest = headEntries.filter((e) => {
    const b = take(inPlace, `${e.module}\t${e.name}\t${e.hash}`);
    if (!b) return true;
    unchanged++;
    pairs.push([b, e]);
    return false;
  });
  // 2. The same declaration in another module: a move.
  const pool = group([...inPlace.values()].flat(), (e) => `${e.name}\t${e.hash}`);
  const moved = [];
  rest = rest.filter((e) => {
    const from = take(pool, `${e.name}\t${e.hash}`);
    if (!from) return true;
    moved.push(`moved ${e.name}: ${from.module} -> ${e.module}`);
    pairs.push([from, e]);
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
  // 4. Evaluation order.
  failures.push(...reorders(pairs), ...effectMoves(base, head, pairs), ...importOrder(base, head));
  const baseCycles = new Set(runtimeCycles(base));
  for (const c of runtimeCycles(head)) if (!baseCycles.has(c)) failures.push(`new import cycle ${c}`);

  const wiring = (mods) => new Map([...mods.values()].map((m) => [m.path, m.wiring.join('\n')]));
  const bw = wiring(base);
  const hw = wiring(head);
  const rewired = [...new Set([...bw.keys(), ...hw.keys()])]
    .filter((p) => (bw.get(p) ?? '') !== (hw.get(p) ?? ''))
    .sort()
    .map((p) => `rewired ${p}`);
  return { unchanged, moved: moved.sort(), rewired, failures: failures.sort() };
}

/** A module runs its statements in source order: runtime statements that
 * share a module on both sides keep their relative order. */
function reorders(pairs) {
  const groups = new Map();
  for (const [b, h] of pairs) {
    if (!b.runtime) continue;
    const k = `${b.module}\t${h.module}`;
    if (!groups.has(k)) groups.set(k, []);
    groups.get(k).push([b, h]);
  }
  const out = [];
  for (const list of groups.values()) {
    list.sort(([b1, h1], [b2, h2]) => h1.pos - h2.pos || b1.pos - b2.pos);
    let latest = null;
    for (const [b, h] of list) {
      if (latest && b.pos < latest[0].pos) out.push(`reordered ${h.module} ${h.name}: now runs after ${latest[1].name}`);
      else latest = [b, h];
    }
  }
  return out;
}

/** A statement that may have a side effect runs when its module is
 * evaluated, so moving it to another module changes when it runs relative
 * to everything else. The one move proved safe is a module's whole set of
 * such statements moving, in order (reorders), to a module new on the head
 * side that holds nothing else of the kind and that the old module (if it
 * is still there) imports, directly or not: the statements then run
 * together as the old module is loaded, before its body. Any other move
 * of one fails: the tool refuses to certify what it cannot prove. */
function effectMoves(base, head, pairs) {
  const reach = (from, to) => {
    const seen = new Set([from]);
    const work = [from];
    while (work.length) {
      const m = head.get(work.pop());
      for (const k of m ? runtimeImports(head, m) : []) {
        if (k === to) return true;
        if (!seen.has(k)) seen.add(k), work.push(k);
      }
    }
    return false;
  };
  const effects = (mod) => (mod ? mod.decls.filter((d) => d.effectful).length : 0);
  const groups = new Map(); // "from\tto" -> the effectful pairs moved between them
  for (const [b, h] of pairs) {
    if (!b.effectful || b.module === h.module) continue;
    const k = `${b.module}\t${h.module}`;
    if (!groups.has(k)) groups.set(k, []);
    groups.get(k).push(h);
  }
  const out = [];
  for (const [k, list] of groups) {
    const [from, to] = k.split('\t');
    const whole = list.length === effects(base.get(from)) && list.length === effects(head.get(to));
    if (whole && !base.has(to) && (!head.has(from) || reach(from, to))) continue;
    for (const h of list) out.push(`moved ${h.name}: ${from} -> ${to} changes when it runs (it may have side effects; see tsmovecheck's header)`);
  }
  return out;
}

/** A module evaluates the modules it imports in import order: the ones it
 * imports on both sides keep their relative order. */
function importOrder(base, head) {
  const out = [];
  for (const [p, hm] of head) {
    const bm = base.get(p);
    if (!bm) continue;
    const bi = runtimeImports(base, bm);
    const hi = runtimeImports(head, hm);
    const common = (list, other) => list.filter((k) => other.includes(k));
    const bc = common(bi, hi);
    const hc = common(hi, bi);
    const i = bc.findIndex((k, j) => k !== hc[j]);
    if (i >= 0) out.push(`reordered imports of ${p}: ${hc[i]} now loads before ${bc[i]}`);
  }
  return out;
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
    const [b, h] = [baseRef, opts.head].map((ref) => load(root, ref, paths));
    requireSources(root, paths, b, h);
    result = moveCheck(b, h);
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
