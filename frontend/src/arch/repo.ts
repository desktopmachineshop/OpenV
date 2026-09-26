// Test-only helpers for the src/arch guards: where the repository is, and a
// deterministic list of the frontend's production source files. Nothing in
// the app imports this module; it exists for the *.test.ts files beside it.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';

/** frontend/ */
export const FRONTEND = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
/** frontend/src/ */
export const SRC = path.join(FRONTEND, 'src');
/** The repository root, for the Go files and testdata the guards read. */
export const REPO = path.resolve(FRONTEND, '..');

/** A path relative to frontend/src, with forward slashes. */
export function srcRel(abs: string): string {
  return path.relative(SRC, abs).split(path.sep).join('/');
}

export function readRepoFile(relToRepo: string): string {
  return fs.readFileSync(path.join(REPO, relToRepo), 'utf8');
}

const isTestFile = (name: string) => /\.(test|spec)\.tsx?$/.test(name);

/**
 * Every production .ts/.tsx file under dir (default frontend/src), sorted.
 * Tests, declaration files and src/arch itself are left out.
 */
export function productionSources(dir: string = SRC): string[] {
  const out: string[] = [];
  const walk = (d: string) => {
    for (const entry of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, entry.name);
      if (entry.isDirectory()) {
        if (p === path.join(SRC, 'arch')) continue;
        walk(p);
      } else if (/\.tsx?$/.test(entry.name) && !entry.name.endsWith('.d.ts') && !isTestFile(entry.name)) {
        out.push(p);
      }
    }
  };
  walk(dir);
  return out.sort();
}

/** The command every guard here names for regenerating its file snapshot. */
export const REGENERATE = 'npx vitest run src/arch -u';

/** A type-checked program over the given files, with the app's tsconfig. */
export function tsProgram(roots: string[]): ts.Program {
  const configPath = path.join(FRONTEND, 'tsconfig.json');
  const config = ts.readConfigFile(configPath, ts.sys.readFile);
  const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, FRONTEND);
  return ts.createProgram(roots, parsed.options);
}

/** 1-based line of a node, for failure messages (never for snapshots). */
export function lineOf(node: ts.Node): number {
  const sf = node.getSourceFile();
  return sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;
}

/** Parse one file without a program (syntax only, parents set). */
export function parseFile(abs: string): ts.SourceFile {
  const kind = abs.endsWith('.tsx') ? ts.ScriptKind.TSX : abs.endsWith('.js') ? ts.ScriptKind.JS : ts.ScriptKind.TS;
  return ts.createSourceFile(abs, fs.readFileSync(abs, 'utf8'), ts.ScriptTarget.Latest, true, kind);
}

/** Resolve a relative module specifier the way Vite and tsc do for this app. */
export function resolveModule(fromFile: string, specifier: string): string | null {
  if (!specifier.startsWith('.')) return null;
  const base = path.resolve(path.dirname(fromFile), specifier);
  for (const candidate of [base, `${base}.ts`, `${base}.tsx`, path.join(base, 'index.ts'), path.join(base, 'index.tsx')]) {
    if (/\.tsx?$/.test(candidate) && fs.existsSync(candidate) && fs.statSync(candidate).isFile()) return candidate;
  }
  return null;
}

/** Visit every node below root, depth first, in source order. */
export function walk(root: ts.Node, visit: (node: ts.Node) => void): void {
  const go = (node: ts.Node) => {
    visit(node);
    ts.forEachChild(node, go);
  };
  go(root);
}

/** The value of a string literal or a template with no substitutions. */
export function literalText(node: ts.Node | undefined): string | null {
  if (!node) return null;
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  return null;
}

/** Pad the first column of each row so the file snapshots read as tables. */
export function table(rows: string[][], indent = ''): string[] {
  const widths: number[] = [];
  for (const row of rows) row.forEach((cell, i) => (widths[i] = Math.max(widths[i] || 0, cell.length)));
  return rows.map((row) =>
    (indent + row.map((cell, i) => (i === row.length - 1 ? cell : cell.padEnd(widths[i]))).join('  ')).trimEnd()
  );
}
