#!/usr/bin/env node
// scaffold: the first files of a frontend change the area guides describe,
// in the shape the code beside them has (refactor plan step N3; the Go
// scaffolds are internal/tools/scaffold).
//
//   node frontend/scripts/scaffold.mjs [-n] api-module <area>
//
// api-module writes src/api/<area>.ts, an <area>API object whose one stub
// method calls `client` from http.ts, and src/api/types/<area>.ts, its
// types, then appends `export type * from './types/<area>';` and
// `export * from './<area>';` to client.ts, each after the last line of its
// kind, as frontend/src/api/README.md ("Add an area module") asks. The stub
// calls GET /api/v1/projects/{id}/<area>, the route
// `go run ./internal/tools/scaffold api-area <area>` registers. Refactor
// step X17 adds `page`.
//
// <area> is lower-case words of letters and digits joined by '-' or '_', as
// the Go scaffold takes, and both spellings give the same result: lower
// camel case for the module and its object (work-items: workItems.ts,
// workItemsAPI), upper camel case for its type (WorkItemsItem), kebab case
// for the URL segment (/work-items).
//
// It refuses to overwrite a file or to reuse a name a module of src/api
// already exports, which the barrel would then export twice; -n prints what
// it would write and writes nothing. Having written, it prints what is left
// to do by hand: a docs/areas.json glob when no area claims a new file (as
// `go run ./internal/tools/areas which` answers), the server route, the
// barrel surface snapshot's regenerate command, and a release note.
// --root <frontend dir> defaults to the directory above this script. Node
// built-ins only.
//
// Exit status: 0 written (or previewed), 1 refused, 2 a usage error.

import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const FRONTEND_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
// The barrel surface snapshot's regenerate command, as
// frontend/src/api/README.md names it (the test holds them equal).
export const REGENERATE_SURFACE = 'cd frontend && npx vitest run src/arch -u';

const USAGE = 'usage: node frontend/scripts/scaffold.mjs [-n] [--root <frontend dir>] api-module <area>';
const NAME = /^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$/;

export class Refusal extends Error {}
class UsageError extends Error {}

/** A scaffold name in the spellings the files use. */
export function parseName(s) {
  if (!NAME.test(s)) {
    throw new UsageError(`name "${s}": want lower-case words of letters and digits joined by '-' or '_', starting with a letter (work-items)`);
  }
  const words = s.split(/[-_]/);
  const cap = (w) => w[0].toUpperCase() + w.slice(1);
  return {
    kebab: words.join('-'),
    camel: words[0] + words.slice(1).map(cap).join(''),
    pascal: words.map(cap).join(''),
    text: words.join(' '),
  };
}

/** Every name a module under src/api exports, to the file that exports it. */
function exportedNames(apiDir) {
  const names = new Map();
  const walk = (dir) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (/\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) {
        const text = fs.readFileSync(p, 'utf8');
        const rel = path.relative(apiDir, p).split(path.sep).join('/');
        const decl = /^export\s+(?:declare\s+)?(?:const|let|var|async\s+function\*?|function\*?|abstract\s+class|class|interface|type|enum)\s+([A-Za-z_$][\w$]*)/gm;
        for (const m of text.matchAll(decl)) names.set(m[1], rel);
        for (const m of text.matchAll(/^export\s+(?:type\s+)?\{([^}]*)\}/gm)) {
          for (const spec of m[1].split(',')) {
            const id = spec.trim().split(/\s+as\s+/).pop();
            if (id) names.set(id, rel);
          }
        }
      }
    }
  };
  walk(apiDir);
  return names;
}

const lastIndex = (lines, re) => lines.reduce((at, line, i) => (re.test(line) ? i : at), -1);

/** What api-module writes: files to create, and client.ts's new text. */
export function planApiModule(root, area) {
  const n = parseName(area);
  const mod = `src/api/${n.camel}.ts`;
  const types = `src/api/types/${n.camel}.ts`;
  const barrel = 'src/api/client.ts';
  for (const f of [mod, types]) {
    if (fs.existsSync(path.join(root, f))) throw new Refusal(`frontend/${f} already exists; scaffold never overwrites a file`);
  }
  const object = `${n.camel}API`;
  const type = `${n.pascal}Item`;
  const taken = exportedNames(path.join(root, 'src/api'));
  for (const id of [object, type]) {
    if (taken.has(id)) throw new Refusal(`${id} is already exported by frontend/src/api/${taken.get(id)}; pick another name`);
  }

  const old = fs.readFileSync(path.join(root, barrel), 'utf8');
  const lines = old.split('\n');
  const typeAt = lastIndex(lines, /^export type \* from '\.\/types\//);
  const modAt = lastIndex(lines, /^export \* from '\.\//);
  if (typeAt < 0 || modAt < 0) {
    throw new Refusal(`frontend/${barrel} has no export type * from './types/…' or export * from './…' line to append after; add the two lines by hand`);
  }
  const typeLine = `export type * from './types/${n.camel}';`;
  const modLine = `export * from './${n.camel}';`;
  // The later insertion first, so the earlier index still holds.
  for (const [at, line] of [[typeAt, typeLine], [modAt, modLine]].sort((a, b) => b[0] - a[0])) lines.splice(at + 1, 0, line);

  const typesText = [
    `// The types api/${n.camel}.ts sends and receives.`,
    `// TODO: say what the ${n.text} area covers.`,
    '// Code outside src/api imports these from api/client, which re-exports',
    '// this module (refactor plan F1, K12).',
    '',
    `/** TODO: one ${n.text} record, as the server answers it. */`,
    `export interface ${type} {`,
    '  id: string;',
    '}',
    '',
  ].join('\n');
  const modText = [
    `// TODO: say what the ${n.text} area covers.`,
    '// Code outside src/api imports these from api/client, which re-exports',
    '// this module (refactor plan F1, K12).',
    '',
    "import { client } from './http';",
    `import type { ${type} } from './types/${n.camel}';`,
    '',
    `export const ${object} = {`,
    '  /** TODO: one method per endpoint, its path exactly as the server registers it. */',
    `  list: (projectId: string) => client.get<${type}[]>(\`/api/v1/projects/\${projectId}/${n.kebab}\`),`,
    '};',
    '',
  ].join('\n');

  return {
    creates: [
      [types, typesText],
      [mod, modText],
    ],
    edit: { path: barrel, old, text: lines.join('\n'), added: [typeLine, modLine] },
    note:
      `${typeLine} and ${modLine} are the last lines of their kinds in client.ts. client.ts loads http.ts before ` +
      'any area module, so the instance and its interceptors exist before a module uses them.',
    steps: [
      `The server route: frontend/src/arch/clientRoutes.test.ts fails until GET /api/v1/projects/{id}/${n.kebab} is a route of ` +
        `internal/api/testdata/routes.txt; go run ./internal/tools/scaffold api-area ${n.kebab} registers it (internal/api/README.md).`,
      `The barrel's surface gains ${object} and ${type}: regenerate the S12 snapshots: ${REGENERATE_SURFACE}`,
      'Release note: the endpoint the module calls changes behaviour, so its pull request adds a bullet under "## Unreleased" ' +
        'in RELEASE_NOTES.md, in its group (CONTRIBUTING.md); one under "### New features" also registers a feature key in ' +
        'internal/domain/release/features.go and gates on it (docs/release-policy.md).',
    ],
  };
}

/** Writes the plan: new files first, each refused if it exists by then. */
function apply(root, plan) {
  for (const [rel, text] of plan.creates) {
    fs.mkdirSync(path.dirname(path.join(root, rel)), { recursive: true });
    fs.writeFileSync(path.join(root, rel), text, { flag: 'wx' });
  }
  fs.writeFileSync(path.join(root, plan.edit.path), plan.edit.text);
}

/** Says whether an area of docs/areas.json claims a frontend file, asking
 * `go run ./internal/tools/areas which`, the index's one reader. */
export function areaStep(root, rel) {
  const repo = path.dirname(root);
  const file = `${path.basename(root)}/${rel}`;
  const r = spawnSync('go', ['run', './internal/tools/areas', 'which', file], { cwd: repo, encoding: 'utf8' });
  if (r.status === 0) return `Area: ${file} belongs to ${r.stdout.trim()}; docs/areas.json needs no glob.`;
  if (r.status === 1 && (r.stderr || '').startsWith('areas: ')) return `Area: ${r.stderr.split('\n')[0].slice('areas: '.length)} (K15).`;
  return `Area: check that one area claims ${file}: go run ./internal/tools/areas which ${file}`;
}

const indent = (text) => `    ${text.replace(/\n$/, '').split('\n').join('\n    ')}`;

export function main(argv = process.argv.slice(2), { log = console, areaOf = areaStep } = {}) {
  let root = FRONTEND_DIR;
  let dryRun = false;
  const pos = [];
  try {
    for (let i = 0; i < argv.length; i++) {
      if (argv[i] === '-n') dryRun = true;
      else if (argv[i] === '--root' && i + 1 < argv.length) root = path.resolve(argv[++i]);
      else if (argv[i].startsWith('-')) throw new UsageError(`unknown flag ${argv[i]}`);
      else pos.push(argv[i]);
    }
    if (pos.length !== 2) throw new UsageError('want a kind and a name');
    if (pos[0] === 'page') throw new UsageError('scaffold page arrives with refactor step X17 (the page registry)');
    if (pos[0] !== 'api-module') throw new UsageError(`unknown kind "${pos[0]}"`);
    const plan = planApiModule(root, pos[1]);
    if (!dryRun) apply(root, plan);
    const out = [];
    for (const [rel, text] of plan.creates) out.push(dryRun ? `would create frontend/${rel}:\n${indent(text)}` : `created frontend/${rel}`);
    out.push(dryRun ? `would add to frontend/${plan.edit.path}:\n${indent(plan.edit.added.join('\n'))}` : `edited frontend/${plan.edit.path}`);
    out.push('', plan.note, '', 'Left to do:');
    const steps = [...plan.creates.map(([rel]) => areaOf(root, rel)), ...plan.steps];
    steps.forEach((s, i) => out.push(`  ${i + 1}. ${s}`));
    log.log(out.join('\n'));
    return 0;
  } catch (err) {
    if (err instanceof UsageError) {
      log.error(`scaffold: ${err.message}\n${USAGE}`);
      return 2;
    }
    if (err instanceof Refusal || err.code === 'EEXIST') {
      log.error(`scaffold: ${err.message}`);
      return 1;
    }
    throw err;
  }
}

if (import.meta.url === pathToFileURL(process.argv[1] || '').href) {
  process.exit(main());
}
