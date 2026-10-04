import React from 'react';
import { AgentRun } from '../../api/client';
import { Sheet } from '../ui';
import { RunDetailPanel, runStatusColor, ErrorClassChip } from './RunDetailPanel';

// The parts of a Runs page: the status filter, the list of runs and the run
// detail beside it. A project's Runs page and the workspace's (the runs with
// no project) show runs the same way.

const STATUS_FILTERS = [
  'all',
  'queued',
  'running',
  'awaiting_approval',
  'succeeded',
  'failed',
  'timed_out',
  'cancelled',
];

/**
 * Guards a Runs page's poll (#379 bug 175). The page polls every 5 s, and
 * polls anew when what it lists changes: another workspace or project,
 * another status filter. An answer can land after that change, or after a
 * later request's answer, and would put back a list the page has moved
 * past. The poll's effect opens a guard and closes it on cleanup; each
 * request takes the next number, and its answer is current while the guard
 * is open and no later request has been answered, as SharedProductVotes
 * numbers its reads. An answer slower than the poll still counts, so a
 * server that takes longer than 5 s still updates the list.
 */
export const pollGuard = () => {
  let open = true;
  let sent = 0;
  let shown = 0;
  return {
    /** Numbers a request; the function returned says, as its answer lands, whether to show it. */
    next: () => {
      const seq = ++sent;
      return () => {
        if (!open || seq < shown) return false;
        shown = seq;
        return true;
      };
    },
    close: () => {
      open = false;
    },
  };
};

const formatDuration = (run: AgentRun): string => {
  if (!run.started_at) return '—';
  const start = new Date(run.started_at).getTime();
  const end = run.finished_at ? new Date(run.finished_at).getTime() : Date.now();
  const secs = Math.max(0, Math.round((end - start) / 1000));
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ${secs % 60}s`;
  return `${Math.floor(mins / 60)}h ${mins % 60}m`;
};

/** The status a Runs page lists ('all' for every one), as a labelled select. */
export const RunStatusFilter: React.FC<{ value: string; onChange: (status: string) => void }> = ({
  value,
  onChange,
}) => (
  <>
    <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Status</label>
    <select value={value} onChange={(e) => onChange(e.target.value)} style={{ width: 180, padding: '6px 10px' }}>
      {STATUS_FILTERS.map((s) => (
        <option key={s} value={s}>
          {s}
        </option>
      ))}
    </select>
  </>
);

const cell: React.CSSProperties = { padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)' };
const bodyCell: React.CSSProperties = { ...cell, color: 'var(--text-body)' };

interface RunTableProps {
  runs: AgentRun[];
  selectedRunId: string | null;
  onSelect: (runId: string) => void;
  /** A phone shows the three columns that identify a run; the rest is in the detail. */
  isPhone: boolean;
  /** What the table says when there is no run to list. */
  emptyText: string;
}

/** The runs of a Runs page, a row each; a row's click selects its run. */
export const RunTable: React.FC<RunTableProps> = ({ runs, selectedRunId, onSelect, isPhone, emptyText }) => {
  const columns = isPhone
    ? ['Agent', 'Status', 'Started']
    : ['Agent', 'Status', 'Started', 'Duration', 'Tokens', 'Cost'];
  return (
    <div className="table-container">
      <style>
        {`@keyframes ovPulseRun { 0% { opacity: 1; } 50% { opacity: 0.45; } 100% { opacity: 1; } }`}
      </style>
      <div className="table-scroll">
      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
        <thead>
          <tr>
            {columns.map((h) => (
              <th
                key={h}
                style={{
                  textAlign: 'left',
                  borderBottom: '2px solid var(--neutral-soft)',
                  padding: '10px 12px',
                  color: 'var(--text-muted)',
                  fontWeight: 600,
                  background: 'var(--surface)',
                }}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {runs.map((run) => (
            <tr
              key={run.id}
              onClick={() => onSelect(run.id)}
              title={run.worker_id ? `executed by ${run.worker_id}` : undefined}
              style={{
                cursor: 'pointer',
                background: selectedRunId === run.id ? 'var(--tint-blue)' : 'var(--surface)',
              }}
            >
              <td style={cell}>
                🤖 {run.agent_name || run.agent_id}
                {run.team_id && (
                  <span style={{ fontSize: 12, color: 'var(--text-muted)', marginLeft: 6 }}>(crew)</span>
                )}
              </td>
              <td style={cell}>
                <span
                  style={{
                    display: 'inline-block',
                    padding: '2px 10px',
                    borderRadius: 12,
                    background: runStatusColor(run.status),
                    color: '#fff',
                    fontSize: 12,
                    fontWeight: 600,
                    animation: run.status === 'running' ? 'ovPulseRun 1.4s ease-in-out infinite' : undefined,
                  }}
                >
                  {run.status}
                </span>
                <ErrorClassChip errorClass={run.error_class} />
                {run.status === 'queued' &&
                  run.preferred_user_id &&
                  run.hosted_after &&
                  new Date(run.hosted_after).getTime() > Date.now() && (
                    <span
                      title="This run waits briefly for the launcher's personal runner before hosted or workspace runners claim it."
                      style={{
                        display: 'inline-block',
                        marginLeft: 6,
                        padding: '2px 8px',
                        borderRadius: 12,
                        background: 'var(--tint-purple)',
                        color: 'var(--purple)',
                        fontSize: 12,
                        fontWeight: 600,
                      }}
                    >
                      reserved for launcher's runner
                    </span>
                  )}
              </td>
              <td style={bodyCell}>{run.started_at ? new Date(run.started_at).toLocaleString() : '—'}</td>
              {!isPhone && (
                <>
                  <td style={bodyCell}>{formatDuration(run)}</td>
                  <td style={bodyCell}>
                    {run.tokens_in + run.tokens_out > 0 ? (run.tokens_in + run.tokens_out).toLocaleString() : '—'}
                  </td>
                  <td style={bodyCell}>{run.cost_usd != null ? `$${run.cost_usd.toFixed(4)}` : '—'}</td>
                </>
              )}
            </tr>
          ))}
          {runs.length === 0 && (
            <tr>
              <td colSpan={columns.length} style={{ padding: 16, color: 'var(--text-muted)', background: 'var(--surface)' }}>
                {emptyText}
              </td>
            </tr>
          )}
        </tbody>
      </table>
      </div>
    </div>
  );
};

interface RunDetailBesideProps {
  runId: string;
  /** A compact viewport opens the detail as a sheet over the list. */
  isCompact: boolean;
  onSelectRun: (runId: string) => void;
  onClose: () => void;
}

/** The selected run's detail beside the list, or over it on a compact viewport. */
export const RunDetailBeside: React.FC<RunDetailBesideProps> = ({ runId, isCompact, onSelectRun, onClose }) => {
  const panel = <RunDetailPanel runId={runId} onSelectRun={onSelectRun} onClose={onClose} />;
  if (isCompact) {
    return (
      <Sheet label="Run detail" onClose={onClose}>
        <div style={{ flex: 1, minHeight: 0 }}>{panel}</div>
      </Sheet>
    );
  }
  return <div style={{ width: 460, flexShrink: 0, minHeight: 0 }}>{panel}</div>;
};
