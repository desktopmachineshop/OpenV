// Coverage by section: one row per document section with a stacked bar of
// its requirements' rollups, opening onto the requirements themselves with
// their verification method, rollup chip and child-project refinements.
import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import type { Artifact, CoverageEntry } from '../api/client';
import { Chip, buttonStyle } from '../components/ui';
import {
  FILL,
  Legend,
  StackedBar,
  cardHeadStyle,
  cardStyle,
  cardTitleStyle,
  refStyle,
  tableHeadStyle,
} from './VVDashboardParts';
import { RollupCounts, Section, percent, tracedCount } from './VVDashboardData';

const COLUMNS = 'minmax(0, 1.2fr) 56px 64px minmax(0, 2fr) 44px 44px 52px 56px';

const rollupSegments = (c: RollupCounts) => [
  { value: c.pass, color: FILL.pass, label: 'pass' },
  { value: c.fail, color: FILL.fail, label: 'fail' },
  { value: c.blocked, color: FILL.blocked, label: 'blocked' },
  { value: c.unrun, color: FILL.notRun, label: 'not-run' },
];

const num: React.CSSProperties = { textAlign: 'right', color: 'var(--text-secondary)' };

interface Props {
  projectId: string;
  sections: Section[];
  totals: RollupCounts;
  artifacts: Record<string, Artifact>;
  isPhone: boolean;
  loading: boolean;
}

const RequirementRows: React.FC<{
  projectId: string;
  entries: CoverageEntry[];
  artifacts: Record<string, Artifact>;
  isPhone: boolean;
}> = ({ projectId, entries, artifacts, isPhone }) => (
  <div style={{ background: 'var(--surface-alt)', borderTop: '1px solid var(--border-soft)' }}>
    {entries.map((entry) => {
      const art = artifacts[entry.requirement_id];
      return (
        <div
          key={entry.requirement_id}
          style={{
            display: 'grid',
            gridTemplateColumns: isPhone ? 'minmax(0, 1fr) auto' : '80px minmax(0, 1fr) 140px 130px',
            gap: isPhone ? '2px 8px' : 8,
            alignItems: 'center',
            padding: isPhone ? '8px 12px 8px 20px' : '6px 12px 6px 24px',
            borderBottom: '1px solid var(--border-soft)',
            fontSize: 13,
          }}
        >
          {!isPhone && <span style={refStyle}>{art?.ref || ''}</span>}
          <span style={{ minWidth: 0 }}>
            <Link
              to={`/projects/${projectId}/requirements?artifact=${entry.requirement_id}`}
              style={{ color: 'var(--text)', textDecoration: 'none' }}
            >
              {isPhone && art?.ref && <span style={{ ...refStyle, marginRight: 6 }}>{art.ref}</span>}
              {entry.title || art?.title || entry.requirement_id}
            </Link>
            {(entry.refinements || []).length > 0 && (
              <span style={{ display: 'block', marginTop: 2, fontSize: 12, color: 'var(--text-muted)' }}>
                {entry.via_refinements ? 'From child projects: ' : 'Refined in child projects: '}
                {(entry.refinements || []).map((r, i) => (
                  <span key={r.requirement_id}>
                    {i > 0 ? ', ' : ''}
                    {r.project_name} / {r.ref || r.title} <Chip>{r.rollup}</Chip>
                  </span>
                ))}
              </span>
            )}
          </span>
          <span style={{ color: 'var(--text-body)', fontSize: 12, gridRow: isPhone ? 2 : undefined }}>
            {entry.verification_method || <span style={{ color: 'var(--danger-strong)' }}>method missing</span>}
          </span>
          <span style={{ gridRow: isPhone ? '1 / span 2' : undefined, gridColumn: isPhone ? 2 : undefined }}>
            <Chip>{entry.rollup}</Chip>
          </span>
        </div>
      );
    })}
  </div>
);

export const SectionCoverage: React.FC<Props> = ({ projectId, sections, totals, artifacts, isPhone, loading }) => {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const allOpen = sections.length > 0 && sections.every((s) => open[s.key]);
  const toggleAll = () => {
    const next: Record<string, boolean> = {};
    if (!allOpen) sections.forEach((s) => (next[s.key] = true));
    setOpen(next);
  };
  const legend = [
    { label: 'pass', color: FILL.pass, count: totals.pass },
    { label: 'fail', color: FILL.fail, count: totals.fail },
    { label: 'blocked', color: FILL.blocked, count: totals.blocked },
    { label: 'not-run', color: FILL.notRun, count: totals.unrun },
    { label: 'no test', color: FILL.track, count: totals.uncovered, hollow: true },
    ...(totals.methodMissing > 0 ? [{ label: 'no method', color: FILL.track, count: totals.methodMissing, hollow: true }] : []),
  ];

  return (
    <section
      aria-label="Coverage by section"
      data-help-key="vv.coverage"
      style={{ ...cardStyle, flex: '999 1 560px', minWidth: 0 }}
    >
      <div style={cardHeadStyle}>
        <h3 style={cardTitleStyle}>{isPhone ? 'By section' : 'Coverage by section'}</h3>
        <span style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <Legend items={isPhone ? legend.slice(0, 3) : legend} />
          {sections.length > 0 && (
            <button type="button" onClick={toggleAll} style={{ ...buttonStyle('ghost', { compact: true }), minHeight: 32 }}>
              {allOpen ? 'Hide requirements' : 'Show all requirements'}
            </button>
          )}
        </span>
      </div>
      {sections.length === 0 && !loading && (
        <div style={{ padding: 16, color: 'var(--text-muted)', fontSize: 13 }}>
          No requirements yet. Requirements you write appear here with their verification status.
        </div>
      )}
      {isPhone ? (
        sections.map((s, i) => (
          <div key={s.key || 'none'} style={{ borderTop: i ? '1px solid var(--border-soft)' : undefined }}>
            <button
              type="button"
              aria-expanded={!!open[s.key]}
              onClick={() => setOpen((o) => ({ ...o, [s.key]: !o[s.key] }))}
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 5,
                width: '100%',
                minHeight: 48,
                padding: '9px 12px',
                border: 'none',
                background: 'transparent',
                color: 'var(--text)',
                textAlign: 'left',
                cursor: 'pointer',
              }}
            >
              <span style={{ display: 'flex', gap: 8, width: '100%' }}>
                <span style={{ flex: 1, fontWeight: 500, fontSize: 14 }}>{s.name}</span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {s.counts.total} reqs · {s.counts.uncovered + s.counts.methodMissing} gaps
                </span>
              </span>
              <span style={{ display: 'block', width: '100%' }}>
                <StackedBar segments={rollupSegments(s.counts)} total={s.counts.total} height={8} />
              </span>
            </button>
            {open[s.key] && (
              <RequirementRows projectId={projectId} entries={s.entries} artifacts={artifacts} isPhone />
            )}
          </div>
        ))
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <div style={{ minWidth: 700 }}>
            <div
              style={{
                ...tableHeadStyle,
                display: 'grid',
                gridTemplateColumns: COLUMNS,
                alignItems: 'center',
                gap: 8,
                height: 28,
                padding: '0 12px',
              }}
            >
              <span>Section</span>
              <span style={{ textAlign: 'right' }}>Reqs</span>
              <span style={{ textAlign: 'right' }}>Traced</span>
              <span>Verification</span>
              <span style={{ textAlign: 'right' }}>Pass</span>
              <span style={{ textAlign: 'right' }}>Fail</span>
              <span style={{ textAlign: 'right' }}>Block</span>
              <span style={{ textAlign: 'right' }}>Gaps</span>
            </div>
            {sections.map((s) => (
              <div key={s.key || 'none'}>
                <button
                  type="button"
                  className="ov-row"
                  aria-expanded={!!open[s.key]}
                  title={open[s.key] ? 'Hide its requirements' : 'Show its requirements'}
                  onClick={() => setOpen((o) => ({ ...o, [s.key]: !o[s.key] }))}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: COLUMNS,
                    alignItems: 'center',
                    gap: 8,
                    width: '100%',
                    height: 36,
                    padding: '0 12px',
                    border: 'none',
                    borderTop: '1px solid var(--border-soft)',
                    background: 'var(--surface)',
                    color: 'var(--text)',
                    fontSize: 13,
                    textAlign: 'left',
                    cursor: 'pointer',
                  }}
                >
                  <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    <span aria-hidden="true" style={{ display: 'inline-block', width: 14, color: 'var(--text-muted)' }}>
                      {open[s.key] ? '▾' : '▸'}
                    </span>
                    {s.name}
                  </span>
                  <span style={num}>{s.counts.total}</span>
                  <span style={num}>{percent(tracedCount(s.counts), s.counts.total)}%</span>
                  <StackedBar segments={rollupSegments(s.counts)} total={s.counts.total} />
                  <span style={{ ...num, color: 'var(--success-text)', fontWeight: 600 }}>{s.counts.pass}</span>
                  <span style={{ ...num, color: 'var(--danger-strong)', fontWeight: 600 }}>{s.counts.fail}</span>
                  <span style={{ ...num, color: 'var(--warning-text)', fontWeight: 600 }}>{s.counts.blocked}</span>
                  <span style={num}>{s.counts.uncovered + s.counts.methodMissing}</span>
                </button>
                {open[s.key] && (
                  <RequirementRows projectId={projectId} entries={s.entries} artifacts={artifacts} isPhone={false} />
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
};
