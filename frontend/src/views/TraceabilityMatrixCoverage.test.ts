import { describe, expect, it } from 'vitest';
import type { MatrixRow } from '../api/client';
import { cellLabel, coverageRow, filterRows, summarise } from './TraceabilityMatrixCoverage';

const row = (id: string, over: Partial<MatrixRow> = {}): MatrixRow => ({
  requirement_id: id,
  title: id,
  user_need_ids: ['N1'],
  design_ids: ['D1'],
  test_case_ids: ['T1'],
  latest_results: { T1: 'pass' },
  hazard_ids: [],
  ...over,
});

const ref = (id: string) => `REF-${id}`;
const never = () => false;

describe('coverageRow', () => {
  it('reads a fully traced row as ok, with no hazard expected', () => {
    const c = coverageRow(row('R1'), never, ref);
    expect(c.kind).toBe('ok');
    expect(c.cells.map((x) => x.state)).toEqual(['linked', 'linked', 'pass', 'none']);
    expect(c.issues).toEqual([]);
  });

  it('puts a failing test first and names it by ref', () => {
    const c = coverageRow(row('R2', { design_ids: [], latest_results: { T1: 'fail' } }), never, ref);
    expect(c.kind).toBe('fail');
    expect(c.cells[2].state).toBe('fail');
    expect(c.issues.map((i) => i.text)).toEqual(['REF-T1 fails', 'No design output']);
  });

  it('marks the column of a suspect link', () => {
    const c = coverageRow(row('R3'), (id) => id === 'D1', ref);
    expect(c.kind).toBe('suspect');
    expect(c.cells[1].state).toBe('suspect');
    expect(c.issues[0].text).toBe('Suspect link to REF-D1');
  });

  it('treats a missing link or an unrun test as a gap', () => {
    expect(coverageRow(row('R4', { test_case_ids: [] }), never, ref).kind).toBe('gap');
    const unrun = coverageRow(row('R5', { latest_results: {} }), never, ref);
    expect(unrun.kind).toBe('gap');
    expect(unrun.cells[2].state).toBe('linked');
    expect(unrun.issues).toEqual([{ text: 'REF-T1 not run', tone: 'neutral' }]);
  });
});

describe('summarise and filterRows', () => {
  const rows = [
    coverageRow(row('ok'), never, ref),
    coverageRow(row('gap', { user_need_ids: [] }), never, ref),
    coverageRow(row('sus'), (id) => id === 'N1', ref),
    coverageRow(row('fail', { latest_results: { T1: 'fail' } }), never, ref),
  ];

  it('counts each row once, by its worst problem', () => {
    expect(summarise(rows)).toEqual({ total: 4, fullyTraced: 1, failing: 1, suspect: 1, attention: 3 });
  });

  it('sorts "Needs attention" worst first and filters the rest', () => {
    expect(filterRows(rows, 'attention').map((r) => r.row.requirement_id)).toEqual(['fail', 'sus', 'gap']);
    expect(filterRows(rows, 'fail').map((r) => r.row.requirement_id)).toEqual(['fail']);
    expect(filterRows(rows, 'suspect').map((r) => r.row.requirement_id)).toEqual(['sus']);
    expect(filterRows(rows, 'all')).toHaveLength(4);
  });

  it('names each square in words', () => {
    expect(rows[0].cells.map(cellLabel)).toEqual(['1 need', '1 design', '1 test, all passing', 'No hazards']);
    expect(cellLabel(rows[1].cells[0])).toBe('No need linked');
  });
});
