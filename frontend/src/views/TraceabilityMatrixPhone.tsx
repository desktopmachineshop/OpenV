import React, { useMemo, useState } from 'react';
import type { Artifact, MatrixRow } from '../api/client';
import {
  CellState,
  COLUMN_LABELS,
  CoverageCell,
  CoverageFilter,
  CoverageRow,
  cellLabel,
  coverageRow,
  filterRows,
  summarise,
} from './TraceabilityMatrixCoverage';
import { TraceabilityMatrixPhoneSheet } from './TraceabilityMatrixPhoneSheet';

// The matrix on a phone: a coverage summary, filters that put what needs
// attention first, and one row per requirement with a square per link
// column. A row opens a sheet with the linked artifacts by name.

interface Props {
  rows: MatrixRow[];
  /** The page's quick filter: matches the requirement's and its links' titles. */
  query: string;
  projectId: string;
  artifactMap: Record<string, Artifact>;
  isSuspectPair: (requirementId: string | undefined, otherId: string) => boolean;
}

const CELL_STYLE: Record<CellState, React.CSSProperties> = {
  linked: { background: 'var(--tint-blue)', border: '1px solid var(--tint-blue-border)', color: 'var(--accent-text)' },
  pass: { background: 'var(--tint-green)', border: '1.5px solid var(--success)', color: 'var(--success-text)' },
  fail: { background: 'var(--tint-red)', border: '1.5px solid var(--danger)', color: 'var(--danger-strong)' },
  blocked: { background: 'var(--tint-yellow)', border: '1.5px solid var(--warning)', color: 'var(--warning-text)' },
  suspect: { background: 'var(--tint-yellow)', border: '1.5px solid var(--warning)', color: 'var(--warning-text)' },
  missing: { background: 'transparent', border: '1.5px dashed var(--neutral-mid)', color: 'var(--text-muted)' },
  none: { background: 'transparent', border: '1px solid transparent', color: 'var(--text-muted)' },
};

const GRID = 'minmax(0, 1fr) repeat(4, 44px)';

const Square: React.FC<{ cell: CoverageCell }> = ({ cell }) => {
  const label = cellLabel(cell);
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      style={{
        ...CELL_STYLE[cell.state],
        justifySelf: 'center',
        width: 30,
        height: 30,
        boxSizing: 'border-box',
        borderRadius: 6,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize: 12,
        fontWeight: 700,
      }}
    >
      {cell.state === 'none' ? '–' : cell.ids.length || ''}
    </span>
  );
};

export const TraceabilityMatrixPhone: React.FC<Props> = ({ rows, query, projectId, artifactMap, isSuspectPair }) => {
  const [filter, setFilter] = useState<CoverageFilter>('all');
  const [openId, setOpenId] = useState<string | null>(null);

  const refOf = (id: string) => artifactMap[id]?.ref || artifactMap[id]?.title || id;
  const coverage: CoverageRow[] = useMemo(() => {
    const q = query.trim().toLowerCase();
    const titles = (ids: string[] | undefined) => (ids || []).map((id) => artifactMap[id]?.title || id).join(' ');
    const matches = (r: MatrixRow) =>
      !q ||
      [r.title, titles(r.user_need_ids), titles(r.design_ids), titles(r.test_case_ids), titles(r.hazard_ids)]
        .join(' ')
        .toLowerCase()
        .includes(q);
    return rows.filter(matches).map((r) => coverageRow(r, (other) => isSuspectPair(r.requirement_id, other), refOf));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, query, isSuspectPair, artifactMap]);
  const summary = summarise(coverage);
  const shown = filterRows(coverage, filter);
  const open = coverage.find((c) => c.row.requirement_id === openId) || null;

  const pct = (n: number) => (summary.total ? `${(n / summary.total) * 100}%` : '0%');
  const gaps = summary.attention - summary.failing - summary.suspect;
  const filters: { id: CoverageFilter; label: string; n: number }[] = [
    { id: 'attention', label: 'Needs attention', n: summary.attention },
    { id: 'fail', label: 'Failing', n: summary.failing },
    { id: 'suspect', label: 'Suspect', n: coverage.filter((c) => c.cells.some((x) => x.state === 'suspect')).length },
    { id: 'all', label: 'All', n: summary.total },
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }} data-help-key="matrix">
      <section
        aria-label="Coverage"
        style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 6, padding: 12, display: 'flex', flexDirection: 'column', gap: 8 }}
      >
        <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text)' }}>
          {summary.fullyTraced} of {summary.total} fully traced
        </div>
        <div aria-hidden="true" style={{ display: 'flex', height: 8, borderRadius: 999, overflow: 'hidden', background: 'var(--neutral-soft)' }}>
          <span style={{ width: pct(summary.fullyTraced), background: 'var(--success)' }} />
          <span style={{ width: pct(summary.failing), background: 'var(--danger)' }} />
          <span style={{ width: pct(summary.suspect), background: 'var(--warning)' }} />
        </div>
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {summary.failing} failing · {summary.suspect} suspect · {gaps} with a gap. Fully traced means a user need, a design
          output and passing tests.
        </div>
      </section>

      <div role="group" aria-label="Show" style={{ display: 'flex', gap: 8, overflowX: 'auto', paddingBottom: 2 }}>
        {filters.map((f) => {
          const on = f.id === filter;
          return (
            <button
              key={f.id}
              type="button"
              aria-pressed={on}
              onClick={() => setFilter(f.id)}
              style={{
                minHeight: 36,
                padding: '0 12px',
                flexShrink: 0,
                width: 'auto',
                borderRadius: 999,
                fontSize: 13,
                fontWeight: 500,
                whiteSpace: 'nowrap',
                cursor: 'pointer',
                background: on ? 'var(--tint-blue)' : 'var(--surface)',
                color: on ? 'var(--accent-text)' : 'var(--text)',
                border: `1px solid ${on ? 'var(--accent)' : 'var(--neutral-mid)'}`,
              }}
            >
              {f.label} {f.n}
            </button>
          );
        })}
      </div>

      <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden' }}>
        <div
          aria-hidden="true"
          style={{ display: 'grid', gridTemplateColumns: GRID, alignItems: 'center', height: 36, padding: '0 6px 0 12px', background: 'var(--surface-alt)', borderBottom: '1px solid var(--border)', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}
        >
          <span>Requirement</span>
          {(['need', 'design', 'test', 'hazard'] as const).map((c) => (
            <span key={c} style={{ textAlign: 'center' }}>{COLUMN_LABELS[c]}</span>
          ))}
        </div>
        {shown.length === 0 && (
          <div style={{ padding: 16, fontSize: 13, color: 'var(--text-muted)' }}>Nothing to show under this filter.</div>
        )}
        {shown.map((c) => {
          const req = artifactMap[c.row.requirement_id];
          return (
            <button
              key={c.row.requirement_id}
              type="button"
              onClick={() => setOpenId(c.row.requirement_id)}
              aria-label={`${req?.ref ? req.ref + ' ' : ''}${c.row.title}: ${c.cells.map(cellLabel).join(', ')}`}
              style={{
                display: 'grid',
                gridTemplateColumns: GRID,
                alignItems: 'center',
                width: '100%',
                minHeight: 52,
                padding: '6px 6px 6px 12px',
                border: 'none',
                borderBottom: '1px solid var(--border)',
                borderRadius: 0,
                background: 'var(--surface)',
                color: 'var(--text)',
                textAlign: 'left',
                cursor: 'pointer',
                font: 'inherit',
              }}
            >
              <span style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
                {req?.ref && (
                  <span style={{ fontFamily: 'source-code-pro, Menlo, Monaco, Consolas, monospace', fontSize: 12, color: 'var(--accent-text)' }}>{req.ref}</span>
                )}
                <span style={{ fontSize: 13, lineHeight: '18px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{c.row.title}</span>
              </span>
              {c.cells.map((cell) => (
                <Square key={cell.column} cell={cell} />
              ))}
            </button>
          );
        })}
      </div>

      {open && (
        <TraceabilityMatrixPhoneSheet
          coverage={open}
          projectId={projectId}
          artifactMap={artifactMap}
          onClose={() => setOpenId(null)}
        />
      )}
    </div>
  );
};
