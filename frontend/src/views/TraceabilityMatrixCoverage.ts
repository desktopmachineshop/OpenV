import type { MatrixRow } from '../api/client';

// The phone matrix reads each requirement as four squares, one per link
// column, and sorts the rows that need attention first. This module decides
// what each square and row says; TraceabilityMatrixPhone draws it.

export type CoverageColumn = 'need' | 'design' | 'test' | 'hazard';

/**
 * A square's state. linked: something is linked and there is nothing to
 * report; pass / fail / blocked: the worst latest result of the test cases;
 * suspect: a link to one of them is suspect; missing: nothing linked where
 * a requirement should have something; none: nothing linked, and nothing
 * expected (hazards).
 */
export type CellState = 'linked' | 'pass' | 'fail' | 'blocked' | 'suspect' | 'missing' | 'none';

export interface CoverageCell {
  column: CoverageColumn;
  ids: string[];
  state: CellState;
}

/** A row's worst problem, in the order "Needs attention" sorts them. */
export type RowKind = 'fail' | 'suspect' | 'gap' | 'ok';

export interface CoverageIssue {
  text: string;
  tone: 'danger' | 'warning' | 'neutral';
}

export interface CoverageRow {
  row: MatrixRow;
  cells: CoverageCell[];
  kind: RowKind;
  issues: CoverageIssue[];
}

export interface CoverageSummary {
  total: number;
  fullyTraced: number;
  failing: number;
  suspect: number;
  attention: number;
}

export const COLUMN_LABELS: Record<CoverageColumn, string> = {
  need: 'Need',
  design: 'Design',
  test: 'Test',
  hazard: 'Hazard',
};

const KIND_RANK: Record<RowKind, number> = { fail: 0, suspect: 1, gap: 2, ok: 3 };

export function coverageRow(
  row: MatrixRow,
  isSuspect: (otherId: string) => boolean,
  refOf: (id: string) => string
): CoverageRow {
  const issues: CoverageIssue[] = [];
  const results = row.latest_results || {};
  const anySuspect = (ids: string[]) => ids.filter(isSuspect);

  const linkCell = (column: CoverageColumn, ids: string[], expected: boolean, missingText: string): CoverageCell => {
    const suspect = anySuspect(ids);
    if (suspect.length) {
      suspect.forEach((id) => issues.push({ text: `Suspect link to ${refOf(id)}`, tone: 'warning' }));
      return { column, ids, state: 'suspect' };
    }
    if (ids.length) return { column, ids, state: 'linked' };
    if (expected) issues.push({ text: missingText, tone: 'warning' });
    return { column, ids, state: expected ? 'missing' : 'none' };
  };

  const need = linkCell('need', row.user_need_ids || [], true, 'No user need');
  const design = linkCell('design', row.design_ids || [], true, 'No design output');

  const testIds = row.test_case_ids || [];
  let test: CoverageCell;
  const failing = testIds.filter((id) => results[id] === 'fail');
  const blocked = testIds.filter((id) => results[id] === 'blocked');
  const notRun = testIds.filter((id) => !results[id] || results[id] === 'not-run');
  failing.forEach((id) => issues.unshift({ text: `${refOf(id)} fails`, tone: 'danger' }));
  blocked.forEach((id) => issues.push({ text: `${refOf(id)} blocked`, tone: 'warning' }));
  notRun.forEach((id) => issues.push({ text: `${refOf(id)} not run`, tone: 'neutral' }));
  const suspectTests = anySuspect(testIds);
  suspectTests.forEach((id) => issues.push({ text: `Suspect link to ${refOf(id)}`, tone: 'warning' }));
  if (!testIds.length) {
    issues.push({ text: 'No test case', tone: 'warning' });
    test = { column: 'test', ids: testIds, state: 'missing' };
  } else if (failing.length) {
    test = { column: 'test', ids: testIds, state: 'fail' };
  } else if (suspectTests.length) {
    test = { column: 'test', ids: testIds, state: 'suspect' };
  } else if (blocked.length) {
    test = { column: 'test', ids: testIds, state: 'blocked' };
  } else if (notRun.length) {
    test = { column: 'test', ids: testIds, state: 'linked' };
  } else {
    test = { column: 'test', ids: testIds, state: 'pass' };
  }

  const hazard = linkCell('hazard', row.hazard_ids || [], false, '');
  const cells = [need, design, test, hazard];

  let kind: RowKind = 'ok';
  if (failing.length) kind = 'fail';
  else if (cells.some((c) => c.state === 'suspect')) kind = 'suspect';
  else if (issues.length) kind = 'gap';

  return { row, cells, kind, issues };
}

export function summarise(rows: CoverageRow[]): CoverageSummary {
  return {
    total: rows.length,
    fullyTraced: rows.filter((r) => r.kind === 'ok').length,
    failing: rows.filter((r) => r.kind === 'fail').length,
    suspect: rows.filter((r) => r.kind === 'suspect').length,
    attention: rows.filter((r) => r.kind !== 'ok').length,
  };
}

export type CoverageFilter = 'attention' | 'fail' | 'suspect' | 'all';

/** The rows a filter shows: "Needs attention" worst first, the others in matrix order. */
export function filterRows(rows: CoverageRow[], filter: CoverageFilter): CoverageRow[] {
  switch (filter) {
    case 'attention':
      return rows
        .map((r, i) => ({ r, i }))
        .filter(({ r }) => r.kind !== 'ok')
        .sort((a, b) => KIND_RANK[a.r.kind] - KIND_RANK[b.r.kind] || a.i - b.i)
        .map(({ r }) => r);
    case 'fail':
      return rows.filter((r) => r.kind === 'fail');
    case 'suspect':
      return rows.filter((r) => r.cells.some((c) => c.state === 'suspect'));
    default:
      return rows;
  }
}

/** A square's accessible name, which also serves as its tooltip. */
export function cellLabel(cell: CoverageCell): string {
  const col = COLUMN_LABELS[cell.column].toLowerCase();
  const n = cell.ids.length;
  const linked = `${n} ${col}${n === 1 ? '' : 's'}`;
  switch (cell.state) {
    case 'missing':
      return `No ${col} linked`;
    case 'none':
      return `No ${col}s`;
    case 'pass':
      return `${linked}, all passing`;
    case 'fail':
      return `${linked}, failing`;
    case 'blocked':
      return `${linked}, blocked`;
    case 'suspect':
      return `${linked}, suspect link`;
    default:
      return cell.column === 'test' ? `${linked}, not all run` : linked;
  }
}
