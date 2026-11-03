// A test run's head: the way back, its name and status, what can be done
// to it (refresh, hand the automated cases to an agent, complete, abort),
// its progress as a stacked bar, and the notices about it.
import React from 'react';
import { Link } from 'react-router-dom';
import type { AgentDef, TestRun } from '../api/client';
import { Chip, buttonStyle } from '../components/ui';
import { StackedBar, cardStyle, countResults, resultSegments, resultSummary } from './VVDashboardParts';
import type { ResultRow } from './TestRunViewCells';

interface Props {
  projectId: string;
  run: TestRun | null;
  rows: ResultRow[];
  isPhone: boolean;
  loading: boolean;
  onRefresh: () => void;
  agents: AgentDef[];
  agentSlug: string;
  onAgentSlug: (slug: string) => void;
  launching: boolean;
  agentRunnable: number;
  onLaunch: () => void;
  onRunStatus: (status: 'completed' | 'aborted') => void;
  /** On a phone: open the recorder. */
  onRecord?: () => void;
  recordLabel?: string;
}

const notice = (bg: string, border: string, fg: string): React.CSSProperties => ({
  background: bg,
  border: `1px solid ${border}`,
  color: fg,
  borderRadius: 6,
  padding: '8px 12px',
  fontSize: 13,
});

export const RunHeader: React.FC<Props> = (p) => {
  const { run, rows, isPhone } = p;
  const counts = countResults(rows);
  const tall = isPhone ? { minHeight: 44 } : {};
  return (
    <>
      <div style={{ display: 'flex', alignItems: 'center', gap: isPhone ? 8 : 12, flexWrap: 'wrap' }}>
        <Link
          to={`/projects/${p.projectId}/vv`}
          style={{ color: 'var(--accent-text)', textDecoration: 'none', fontSize: 13, display: 'inline-block', padding: '9px 0' }}
        >
          ← Back to V&amp;V
        </Link>
        <h2 style={{ color: 'var(--text)', margin: 0, fontSize: 20 }}>{run?.name || 'Test run'}</h2>
        {run && <Chip size="md">{run.status.replace('-', ' ')}</Chip>}
        <div style={{ flex: 1 }} />
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button type="button" style={{ ...buttonStyle('secondary', { disabled: p.loading }), ...tall }} onClick={p.onRefresh} disabled={p.loading}>
            {p.loading ? 'Refreshing…' : 'Refresh'}
          </button>
          {run?.status === 'in-progress' && (
            <>
              <select
                value={p.agentSlug}
                onChange={(e) => p.onAgentSlug(e.target.value)}
                disabled={p.agents.length === 0 || p.launching}
                aria-label="Agent"
                style={{ height: isPhone ? 44 : 32, padding: '0 8px', fontSize: 13, width: 'auto', minWidth: 150 }}
                title="Agent that will execute the automated test cases"
              >
                {p.agents.length === 0 && <option value="">No agents available</option>}
                {p.agents.map((a) => (
                  <option key={a.slug} value={a.slug}>
                    {a.name}
                  </option>
                ))}
              </select>
              <button
                type="button"
                style={{ ...buttonStyle('agent', { disabled: p.launching || !p.agentSlug || p.agentRunnable === 0 }), ...tall }}
                onClick={p.onLaunch}
                disabled={p.launching || !p.agentSlug || p.agentRunnable === 0}
                title={
                  p.agentRunnable === 0
                    ? 'Every test case in this run is flagged for human or physical verification'
                    : `Run the ${p.agentRunnable} automated test case(s) with an agent`
                }
              >
                {p.launching ? 'Starting…' : `Run ${p.agentRunnable} with agent`}
              </button>
              <button type="button" style={{ ...buttonStyle(isPhone ? 'secondary' : 'primary'), ...tall }} onClick={() => p.onRunStatus('completed')}>
                Complete run
              </button>
              <button type="button" style={{ ...buttonStyle('danger'), ...tall }} onClick={() => p.onRunStatus('aborted')}>
                Abort run
              </button>
            </>
          )}
        </div>
      </div>

      {run && (
        <section
          aria-label="Run progress"
          style={{
            ...cardStyle,
            borderColor: run.status === 'in-progress' ? 'var(--tint-blue-border)' : 'var(--border)',
            padding: 12,
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          {run.description && <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>{run.description}</div>}
          <StackedBar segments={resultSegments(counts)} total={rows.length} height={8} />
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap', fontSize: 12, color: 'var(--text-muted)' }}>
            <span>{resultSummary(counts, rows.length)}</span>
            <span>
              {counts.recorded} of {rows.length} recorded · started{' '}
              {run.started_at ? new Date(run.started_at).toLocaleString() : '—'}
            </span>
          </div>
          {p.onRecord && rows.length > 0 && (
            <button
              type="button"
              onClick={p.onRecord}
              style={{ ...buttonStyle('primary'), minHeight: 44, fontSize: 15, fontWeight: 700 }}
            >
              {p.recordLabel}
            </button>
          )}
        </section>
      )}
    </>
  );
};

export const RunNotices: React.FC<{
  run: TestRun | null;
  readOnly: boolean;
  launchNotice: React.ReactNode;
  humanOnly: number;
  total: number;
  error: string;
  loading: boolean;
}> = ({ run, readOnly, launchNotice, humanOnly, total, error, loading }) => (
  <>
    {readOnly && run && (
      <div style={notice('var(--tint-yellow)', 'var(--tint-yellow-border)', 'var(--warning-text)')}>
        This run is {run.status}; results are read-only.
      </div>
    )}
    {launchNotice && <div style={notice('var(--tint-green)', 'var(--tint-green-border)', 'var(--success-text)')}>{launchNotice}</div>}
    {!readOnly && humanOnly > 0 && (
      <div style={{ color: 'var(--text-muted)', fontSize: 12 }}>
        {humanOnly} of {total} test case{total === 1 ? '' : 's'} {humanOnly === 1 ? 'is' : 'are'} flagged for human or
        physical verification — agents skip {humanOnly === 1 ? 'it' : 'them'} and cannot record{' '}
        {humanOnly === 1 ? 'its' : 'their'} result. Set this per test case in its editor.
      </div>
    )}
    {error && <div style={{ color: 'var(--danger-strong)', fontSize: 13 }}>{error}</div>}
    {loading && <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>}
  </>
);
