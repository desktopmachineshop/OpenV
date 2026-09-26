// Ratchet, no snapshot (the file snapshots beside it regenerate with:
//   npx vitest run src/arch -u).
//
// Inline `err.response?.data?.error` reads in production code. They render a
// plain-text error body differently from api/errors.ts apiErrorMessage
// (quirk Q20), so none may be added: new code calls apiErrorMessage. The
// count may only fall. When a change removes some, lower CEILING to the new
// count in the same change; never raise it.
import { describe, expect, it, vi } from 'vitest';
import ts from 'typescript';
import { parseFile, productionSources, srcRel, walk } from './repo';

const CEILING = 53;

/** `X.response.data.error`, with or without optional chaining. */
function isInlineChain(node: ts.Node): boolean {
  if (!ts.isPropertyAccessExpression(node) || node.name.text !== 'error') return false;
  const data = node.expression;
  if (!ts.isPropertyAccessExpression(data) || data.name.text !== 'data') return false;
  const response = data.expression;
  return ts.isPropertyAccessExpression(response) && response.name.text === 'response';
}

function countByFile(): Map<string, number> {
  const counts = new Map<string, number>();
  for (const file of productionSources()) {
    const rel = srcRel(file);
    if (rel === 'api/errors.ts') continue;
    walk(parseFile(file), (node) => {
      if (isInlineChain(node)) counts.set(rel, (counts.get(rel) || 0) + 1);
    });
  }
  return counts;
}

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('inline response.data.error chains', () => {
  it(`stay at or below ${CEILING}`, () => {
    const counts = countByFile();
    const total = [...counts.values()].reduce((a, b) => a + b, 0);
    const detail = [...counts.entries()]
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .map(([f, n]) => `${f}: ${n}`)
      .join('\n');
    expect(total, `use apiErrorMessage from api/errors instead; current reads by file:\n${detail}`).toBeLessThanOrEqual(CEILING);
  });

  it('are still recognised (the scanner has not gone blind)', () => {
    expect([...countByFile().values()].reduce((a, b) => a + b, 0)).toBeGreaterThan(0);
  });
});
