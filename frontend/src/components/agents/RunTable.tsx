import React, { useCallback, useEffect, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import { AgentRun } from '../../api/client';
import { AgentGlyph, Sheet, chipStyle } from '../ui';
import { RunDetailPanel, runStatusTone, ErrorClassChip } from './RunDetailPanel';

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

/**
 * Closes the run open beside a Runs page's list (?run=) when what the page
 * lists switches under it, to another project or workspace (scope): the run
 * is the old one's (#379 bug 177). Not a run the switch came with, as a link
 * to another project's run brings its own, nor one the page follows to
 * where it belongs: the page calls the function returned with the run and
 * the scope it is about to switch to. Another status filter is no switch.
 */
export const useCloseRunOnSwitch = (scope: string): ((runId: string, scope: string) => void) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const runId = searchParams.get('run');
  // The open run and the scope it belongs to, as far as the page knows.
  const open = useRef({ runId, scope });
  useEffect(() => {
    const was = open.current;
    open.current = { runId, scope };
    if (!runId || runId !== was.runId || scope === was.scope) return;
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('run');
        return next;
      },
      { replace: true }
    );
  }, [runId, scope, setSearchParams]);
  return useCallback((followed: string, to: string) => {
    open.current = { runId: followed, scope: to };
  }, []);
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

const statusLabel = (status: string): string =>
  status === 'all' ? 'All runs' : (status.charAt(0).toUpperCase() + status.slice(1)).replace(/_/g, ' ');

/**
 * The status a Runs page lists ('all' for every one): a labelled select, or
 * with chips (the refreshed interface) one chip a status in a row that
 * scrolls sideways on a narrow screen.
 */
export const RunStatusFilter: React.FC<{ value: string; onChange: (status: string) => void; chips?: boolean }> = ({
  value,
  onChange,
  chips = false,
}) =>
  chips ? (
    <div role="group" aria-label="Status" className="tab-strip" style={{ display: 'flex', gap: 6, overflowX: 'auto', maxWidth: '100%' }}>
      {STATUS_FILTERS.map((s) => {
        const on = value === s;
        return (
          <button
            key={s}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(s)}
            style={{
              flex: 'none',
              minHeight: 32,
              padding: '0 12px',
              border: on ? '1px solid var(--sidebar-bg)' : '1px solid var(--border)',
              borderRadius: 16,
              background: on ? 'var(--sidebar-bg)' : 'var(--surface)',
              color: on ? 'var(--sidebar-text)' : 'var(--text)',
              fontSize: 13,
              fontWeight: on ? 600 : 400,
              cursor: 'pointer',
              whiteSpace: 'nowrap',
            }}
          >
            {statusLabel(s)}
          </button>
        );
      })}
    </div>
  ) : (
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
  /** null while the page waits for its first answer, or the first after a switch. */
  runs: AgentRun[] | null;
  selectedRunId: string | null;
  onSelect: (runId: string) => void;
  /** A phone shows the three columns that identify a run; the rest is in the detail. */
  isPhone: boolean;
  /** What the table says when there is no run to list. */
  emptyText: string;
  /**
   * A phone in the refreshed interface lists the runs as rows of two lines,
   * the agent and its status, then when it ran and what it cost, in place of
   * a table squeezed to three columns.
   */
  rows?: boolean;
}

const PULSE = '@keyframes ovPulseRun { 0% { opacity: 1; } 50% { opacity: 0.45; } 100% { opacity: 1; } }';

/** A run as a row of two lines: who and how it went, then when, how long and the cost. */
const RunRow: React.FC<{ run: AgentRun; selected: boolean; onSelect: (runId: string) => void }> = ({ run, selected, onSelect }) => {
  const facts = [
    run.started_at ? new Date(run.started_at).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : 'Not started',
    run.started_at ? formatDuration(run) : '',
    run.cost_usd != null && run.cost_usd > 0 ? '$' + run.cost_usd.toFixed(2) : '',
  ].filter(Boolean);
  return (
    <button
      type="button"
      onClick={() => onSelect(run.id)}
      aria-current={selected ? 'true' : undefined}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        width: '100%',
        minHeight: 56,
        padding: '8px 12px',
        border: 'none',
        borderBottom: '1px solid var(--neutral-soft)',
        background: selected ? 'var(--tint-blue)' : 'var(--surface)',
        color: 'var(--text)',
        font: 'inherit',
        textAlign: 'left',
        cursor: 'pointer',
      }}
    >
      <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
        <span style={{ fontSize: 14, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {run.agent_name || run.agent_id}
        </span>
        <span style={{ fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {facts.join(' · ')}
        </span>
      </span>
      <span style={{ flex: 'none', display: 'flex', alignItems: 'center' }}>
        <span style={{ ...chipStyle(runStatusTone(run.status)), animation: run.status === 'running' ? 'ovPulseRun 1.4s ease-in-out infinite' : undefined }}>
          {run.status}
        </span>
        <ErrorClassChip errorClass={run.error_class} />
      </span>
    </button>
  );
};

/** The runs of a Runs page, a row each; a row's click selects its run. */
export const RunTable: React.FC<RunTableProps> = ({ runs, selectedRunId, onSelect, isPhone, emptyText, rows = false }) => {
  if (rows) {
    return (
      <div className="table-container" role="list" aria-label="Runs">
        <style>{PULSE}</style>
        {runs?.map((run) => (
          <div key={run.id} role="listitem">
            <RunRow run={run} selected={selectedRunId === run.id} onSelect={onSelect} />
          </div>
        ))}
        {!runs?.length && (
          <p style={{ margin: 0, padding: 16, color: 'var(--text-muted)', background: 'var(--surface)' }}>{runs ? emptyText : 'Loading runs…'}</p>
        )}
      </div>
    );
  }
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
          {runs?.map((run) => (
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
                <AgentGlyph /> {run.agent_name || run.agent_id}
              </td>
              <td style={cell}>
                <span
                  style={{
                    ...chipStyle(runStatusTone(run.status)),
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
          {!runs?.length && (
            <tr>
              <td colSpan={columns.length} style={{ padding: 16, color: 'var(--text-muted)', background: 'var(--surface)' }}>
                {runs ? emptyText : 'Loading runs…'}
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
