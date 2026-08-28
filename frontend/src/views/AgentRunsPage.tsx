import React, { useCallback, useEffect, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { AgentRun, WorkerStatus, agentRunsAPI, workerStatusAPI } from '../api/client';
import { useAppStore } from '../state/store';
import { RunDetailPanel, runStatusColor } from '../components/agents/RunDetailPanel';
import { ProposalReviewPanel } from '../components/agents/ProposalReviewPanel';
import { RunnerConnectPrompt } from '../components/RunnerConnectPrompt';

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

export const AgentRunsPage: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const [searchParams, setSearchParams] = useSearchParams();

  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [workerStatus, setWorkerStatus] = useState<WorkerStatus | null>(null);
  const [statusFilter, setStatusFilter] = useState('all');
  const [pendingCount, setPendingCount] = useState(0);
  const [showProposals, setShowProposals] = useState(true);
  const [showConnect, setShowConnect] = useState(false);
  const [error, setError] = useState('');

  const selectedRunId = searchParams.get('run');

  const load = useCallback(() => {
    if (!projectId) return;
    const query: { project_id: string; status?: string } = { project_id: projectId };
    if (statusFilter !== 'all') query.status = statusFilter;
    agentRunsAPI
      .list(query)
      .then((res) => {
        setRuns(res.data || []);
        setError('');
      })
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load runs')
      );
    if (activeOrgId) {
      workerStatusAPI
        .get(activeOrgId)
        .then((res) => setWorkerStatus(res.data))
        .catch(() => {
          // Banner data is best-effort; keep the last known status on error.
        });
    }
  }, [projectId, statusFilter, activeOrgId]);

  useEffect(() => {
    load();
    const timer = window.setInterval(load, 5000);
    return () => window.clearInterval(timer);
  }, [load]);

  // Auto-prompt the Agent Connector the first time runs are queued with no
  // runner online (covers every launch path — they all land on this page).
  const autoPromptedRef = React.useRef(false);
  useEffect(() => {
    if (
      !autoPromptedRef.current &&
      workerStatus &&
      workerStatus.queue.queued > 0 &&
      !workerStatus.workers.some((w) => w.online)
    ) {
      autoPromptedRef.current = true;
      setShowConnect(true);
    }
  }, [workerStatus]);

  const selectRun = (runId: string | null) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (runId) next.set('run', runId);
      else next.delete('run');
      return next;
    });
  };

  return (
    <div style={{ padding: 20, height: '100%', display: 'flex', flexDirection: 'column' }}>
      <style>
        {`@keyframes ovPulseRun { 0% { opacity: 1; } 50% { opacity: 0.45; } 100% { opacity: 1; } }`}
      </style>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Agent Runs</h2>
        <button
          onClick={() => setShowProposals(!showProposals)}
          style={{
            background: pendingCount > 0 ? 'var(--warning)' : 'var(--neutral-soft)',
            color: pendingCount > 0 ? '#fff' : 'var(--text-muted)',
            border: 'none',
            padding: '6px 14px',
            borderRadius: 14,
            cursor: 'pointer',
            fontSize: 13,
            fontWeight: 600,
          }}
        >
          Pending approvals ({pendingCount})
        </button>
        <div style={{ flex: 1 }} />
        <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Status</label>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          style={{ width: 180, padding: '6px 10px' }}
        >
          {STATUS_FILTERS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>

      {error && <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 8 }}>{error}</div>}

      {workerStatus &&
        workerStatus.queue.queued > 0 &&
        !workerStatus.workers.some((w) => w.online) && (
          <div
            style={{
              background: 'var(--tint-yellow)',
              border: '1px solid var(--warning)',
              color: 'var(--warning-text)',
              padding: '10px 14px',
              borderRadius: 4,
              marginBottom: 10,
              fontSize: 13,
            }}
          >
            {workerStatus.queue.queued} run{workerStatus.queue.queued === 1 ? '' : 's'} queued but
            no runner is online.{' '}
            <button
              onClick={() => setShowConnect(true)}
              style={{
                background: 'var(--warning)',
                color: '#fff',
                border: 'none',
                padding: '4px 12px',
                borderRadius: 4,
                cursor: 'pointer',
                fontSize: 12.5,
                fontWeight: 600,
                marginRight: 8,
              }}
            >
              Open Agent Connector
            </button>
            <Link to="/org/settings" style={{ color: 'var(--warning-text)', fontWeight: 600 }}>
              Runner settings
            </Link>
          </div>
        )}

      {showConnect && activeOrgId && (
        <RunnerConnectPrompt
          orgId={activeOrgId}
          onClose={() => setShowConnect(false)}
          reason={`${workerStatus?.queue.queued || 0} queued run${
            (workerStatus?.queue.queued || 0) === 1 ? ' is' : 's are'
          } waiting for a runner.`}
        />
      )}

      {workerStatus &&
        workerStatus.queue.queued_repo_access > 0 &&
        workerStatus.workers.some((w) => w.online) &&
        workerStatus.workers.filter((w) => w.online).every((w) => w.hosted) && (
          <div
            style={{
              background: 'var(--tint-blue)',
              border: '1px solid var(--accent)',
              color: 'var(--accent-text)',
              padding: '10px 14px',
              borderRadius: 4,
              marginBottom: 10,
              fontSize: 13,
            }}
          >
            Some queued runs need repository access — they wait for a personal or workspace runner.
          </div>
        )}

      {showProposals && projectId && (
        <ProposalReviewPanel projectId={projectId} onCountChange={setPendingCount} />
      )}

      <div style={{ display: 'flex', gap: 16, flex: 1, minHeight: 0 }}>
        <div style={{ flex: 1, overflowY: 'auto', minWidth: 0 }}>
          <div className="table-container">
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead>
                <tr>
                  {['Agent', 'Status', 'Started', 'Duration', 'Tokens', 'Cost'].map((h) => (
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
                    onClick={() => selectRun(run.id)}
                    title={run.worker_id ? `executed by ${run.worker_id}` : undefined}
                    style={{
                      cursor: 'pointer',
                      background: selectedRunId === run.id ? 'var(--tint-blue)' : 'var(--surface)',
                    }}
                  >
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)' }}>
                      🤖 {run.agent_name || run.agent_id}
                      {run.team_id && (
                        <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 6 }}>(crew)</span>
                      )}
                    </td>
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)' }}>
                      <span
                        style={{
                          display: 'inline-block',
                          padding: '2px 10px',
                          borderRadius: 12,
                          background: runStatusColor(run.status),
                          color: '#fff',
                          fontSize: 11.5,
                          fontWeight: 600,
                          animation:
                            run.status === 'running'
                              ? 'ovPulseRun 1.4s ease-in-out infinite'
                              : undefined,
                        }}
                      >
                        {run.status}
                      </span>
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
                              fontSize: 10.5,
                              fontWeight: 600,
                            }}
                          >
                            reserved for launcher's runner
                          </span>
                        )}
                    </td>
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                      {run.started_at ? new Date(run.started_at).toLocaleString() : '—'}
                    </td>
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                      {formatDuration(run)}
                    </td>
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                      {run.tokens_in + run.tokens_out > 0
                        ? (run.tokens_in + run.tokens_out).toLocaleString()
                        : '—'}
                    </td>
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                      {run.cost_usd != null ? `$${run.cost_usd.toFixed(4)}` : '—'}
                    </td>
                  </tr>
                ))}
                {runs.length === 0 && (
                  <tr>
                    <td colSpan={6} style={{ padding: 16, color: 'var(--text-muted)', background: 'var(--surface)' }}>
                      No runs yet. Launch an agent from the Agents page or the board.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>

        {selectedRunId && (
          <div style={{ width: 460, flexShrink: 0, minHeight: 0 }}>
            <RunDetailPanel
              runId={selectedRunId}
              onSelectRun={(id) => selectRun(id)}
              onClose={() => selectRun(null)}
            />
          </div>
        )}
      </div>
    </div>
  );
};
