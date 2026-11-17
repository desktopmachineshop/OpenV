// The Tests page's pieces: a run card, the test case table and the runs table.
import React from 'react';
import { Link } from 'react-router-dom';
import {
  Chip,
  GridHead,
  ResultBar,
  ellipsis,
  gridRowStyle,
  mutedStyle,
  panelStyle,
  plainLink,
  refStyle,
  type ChipTone,
} from '../components/ui';
import { whenLabel } from './ProjectHomeData';
import { RUN_STATE, type CaseRow, type RunSummary } from './TestsPageData';
import type { ExecutionMethod } from '../api/client';

export const METHOD_TONE: Record<ExecutionMethod, ChipTone> = { automated: 'neutral', manual: 'agent', physical: 'agent' };

export const runLine = (s: RunSummary) => `${s.pass} pass · ${s.fail} fail · ${s.blocked} blocked · ${s.toGo} to go`;

export const RunCard: React.FC<{ s: RunSummary; projectId: string }> = ({ s, projectId }) => {
  const active = s.run.status === 'in-progress';
  return (
    <Link
      to={`/projects/${projectId}/vv/runs/${s.run.id}`}
      style={{
        ...panelStyle,
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        padding: '10px 12px',
        borderColor: active ? 'var(--tint-blue-border)' : 'var(--border)',
        color: 'var(--text)',
        textDecoration: 'none',
      }}
    >
      <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{ ...ellipsis, flex: 1, fontWeight: 600 }}>{s.run.name}</span>
        <Chip>{RUN_STATE[s.run.status]}</Chip>
      </span>
      <ResultBar pass={s.pass} fail={s.fail} blocked={s.blocked} total={s.total} />
      <span style={{ display: 'flex', justifyContent: 'space-between', gap: 8, ...mutedStyle }}>
        <span>{runLine(s)}</span>
        <span style={{ flex: 'none' }}>{whenLabel(s.run.started_at)}</span>
      </span>
    </Link>
  );
};

export const CASE_COLUMNS = '84px minmax(0, 1fr) 96px 110px 100px minmax(0, 140px) 56px';

export const CaseTable: React.FC<{ rows: CaseRow[]; projectId: string }> = ({ rows, projectId }) => (
  <div style={{ overflowX: 'auto' }}>
    <div role="table" aria-label="Test cases" style={{ minWidth: 900 }}>
      <GridHead
        columns={CASE_COLUMNS}
        names={['Ref', 'Title', 'Method', 'Verifies', 'Last result', 'Run', <span style={{ display: 'block', textAlign: 'right' }}>Steps</span>]}
      />
      {rows.map((c) => (
        <div role="row" key={c.testCase.id} style={gridRowStyle(CASE_COLUMNS)}>
          <span role="cell" style={refStyle}>
            {c.testCase.ref || '—'}
          </span>
          <Link role="cell" to={`/projects/${projectId}/requirements?artifact=${c.testCase.id}`} style={{ ...plainLink, ...ellipsis, display: 'block', lineHeight: '24px', color: 'var(--text)' }}>
            {c.testCase.title}
          </Link>
          <span role="cell">
            <Chip tone={METHOD_TONE[c.method]}>{c.method}</Chip>
          </span>
          <span role="cell" style={{ ...ellipsis, ...refStyle }}>
            {c.verifies.length === 0
              ? '—'
              : c.verifies.map((r, i) => (
                  <React.Fragment key={r.id}>
                    {i > 0 && ', '}
                    <Link to={`/projects/${projectId}/requirements?artifact=${r.id}`} title={r.title} style={plainLink}>
                      {r.ref || r.title}
                    </Link>
                  </React.Fragment>
                ))}
          </span>
          <span role="cell">
            <Chip>{c.result}</Chip>
          </span>
          <span role="cell" style={{ ...mutedStyle, ...ellipsis }} title={c.run?.name}>
            {c.run ? c.run.name : '—'}
          </span>
          <span role="cell" style={{ fontSize: 12, color: 'var(--text-secondary)', textAlign: 'right' }}>
            {c.steps || '—'}
          </span>
        </div>
      ))}
      {rows.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No test case matches.</p>}
    </div>
  </div>
);

const RUN_COLUMNS = 'minmax(0, 1fr) 110px 120px minmax(160px, 1fr) 220px';

export const RunTable: React.FC<{ runs: RunSummary[]; projectId: string }> = ({ runs, projectId }) => (
  <div style={{ overflowX: 'auto' }}>
    <div role="table" aria-label="Test runs" style={{ minWidth: 760 }}>
      <GridHead columns={RUN_COLUMNS} names={['Run', 'State', 'Started', 'Results', '']} />
      {runs.map((s) => (
        <div role="row" key={s.run.id} style={gridRowStyle(RUN_COLUMNS)}>
          <Link role="cell" to={`/projects/${projectId}/vv/runs/${s.run.id}`} style={{ ...plainLink, ...ellipsis, fontWeight: 600 }}>
            {s.run.name}
          </Link>
          <span role="cell">
            <Chip>{RUN_STATE[s.run.status]}</Chip>
          </span>
          <span role="cell" style={mutedStyle}>
            {whenLabel(s.run.started_at)}
          </span>
          <span role="cell">
            <ResultBar pass={s.pass} fail={s.fail} blocked={s.blocked} total={s.total} />
          </span>
          <span role="cell" style={{ ...mutedStyle, ...ellipsis }}>
            {runLine(s)}
          </span>
        </div>
      ))}
    </div>
  </div>
);
