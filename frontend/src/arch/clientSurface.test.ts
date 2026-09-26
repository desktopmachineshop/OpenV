// Regenerate the snapshot (only in a PR that means to change the client):
//   npx vitest run src/arch -u
//
// Pins the export surface of src/api/client.ts (invariant I23): every name
// it exports, what kind of thing each is, and the members of every exported
// object (the xxxAPI wrappers). Read through the type checker's view of the
// module, so a client.ts that becomes a barrel re-exporting the same names
// from area modules (F1) produces the same snapshot.
import path from 'node:path';
import ts from 'typescript';
import { describe, expect, it, vi } from 'vitest';
import { REGENERATE, SRC, tsProgram } from './repo';

const CLIENT = path.join(SRC, 'api/client.ts');

function kindOf(symbol: ts.Symbol): string {
  const f = symbol.flags;
  if (f & ts.SymbolFlags.Interface) return 'interface';
  if (f & ts.SymbolFlags.TypeAlias) return 'type';
  if (f & ts.SymbolFlags.Class) return 'class';
  if (f & ts.SymbolFlags.Enum) return 'enum';
  if (f & ts.SymbolFlags.Function) return 'function';
  if (f & ts.SymbolFlags.Variable) {
    const decl = symbol.valueDeclaration;
    return decl && ts.getCombinedNodeFlags(decl) & ts.NodeFlags.Const ? 'const' : 'let';
  }
  return `flags:${f}`;
}

function surface(): { lines: string[]; exports: number; members: number } {
  const program = tsProgram([CLIENT]);
  const checker = program.getTypeChecker();
  const moduleSymbol = checker.getSymbolAtLocation(program.getSourceFile(CLIENT)!)!;
  const rows: string[] = [];
  let members = 0;
  const exports = checker.getExportsOfModule(moduleSymbol).sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  for (const exp of exports) {
    const target = exp.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(exp) : exp;
    const kind = kindOf(target);
    rows.push(`${kind.padEnd(9)} ${exp.name}`);
    const decl = target.valueDeclaration;
    if (decl && ts.isVariableDeclaration(decl) && decl.initializer && ts.isObjectLiteralExpression(decl.initializer)) {
      const props = checker
        .getPropertiesOfType(checker.getTypeAtLocation(decl))
        .map((p) => p.name)
        .sort();
      members += props.length;
      rows.push(...props.map((p) => `            .${p}`));
    }
  }
  return { lines: rows, exports: exports.length, members };
}

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('api/client export surface', () => {
  it('matches the pinned surface', async () => {
    const s = surface();
    const text = [
      '# Exports of src/api/client.ts: kind and name, with the members of each exported object (invariant I23).',
      `# Regenerate: ${REGENERATE}`,
      '',
      `exports: ${s.exports}; members of exported objects: ${s.members}`,
      '',
      ...s.lines,
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/clientSurface.txt');
  });
});
