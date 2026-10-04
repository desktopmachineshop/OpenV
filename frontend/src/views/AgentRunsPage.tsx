import React, { useEffect, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { AgentRun, WorkerStatus, agentRunsAPI, workerStatusAPI } from '../api/client';
import { useAppStore } from '../state/store';
import { RunDetailBeside, RunStatusFilter, RunTable, pollGuard } from '../components/agents/RunTable';
import { ProposalReviewPanel } from '../components/agents/ProposalReviewPanel';
import { RunnerConnectPrompt } from '../components/RunnerConnectPrompt';
import { ErrorBanner } from '../components/ui';
import { useViewport } from '../hooks/useViewport';

// Cap the 5s poll: this page re-fetches every run in the project on a timer,
// which is unbounded as run history grows. 200 covers the visible table; the
// server also clamps the limit. UI follow-up: paginate / infinite-scroll the
// run list and drop this hard cap.
const RUNS_POLL_LIMIT = 200;

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
  // A phone shows the three columns that identify a run; the rest is in the
  // detail. A compact viewport opens that detail as a sheet over the list.
  const { isPhone, isCompact } = useViewport();

  useEffect(() => {
    if (!projectId) return;
    const query: { project_id: string; status?: string; limit: number } = {
      project_id: projectId,
      limit: RUNS_POLL_LIMIT,
    };
    if (statusFilter !== 'all') query.status = statusFilter;
    // A switch of project or workspace, or another filter, starts a new
    // poll: what the old one still has on its way is dropped (pollGuard).
    const runsGuard = pollGuard();
    const statusGuard = pollGuard();
    const load = () => {
      const runsCurrent = runsGuard.next();
      agentRunsAPI
        .list(query)
        .then((res) => {
          if (!runsCurrent()) return;
          setRuns(res.data || []);
          setError('');
        })
        .catch((err: any) => {
          if (runsCurrent()) setError(err.response?.data?.error || err.message || 'Failed to load runs');
        });
      if (activeOrgId) {
        const statusCurrent = statusGuard.next();
        workerStatusAPI
          .get(activeOrgId)
          .then((res) => {
            if (statusCurrent()) setWorkerStatus(res.data);
          })
          .catch(() => {
            // Banner data is best-effort; keep the last known status on error.
          });
      }
    };
    load();
    const timer = window.setInterval(load, 5000);
    return () => {
      runsGuard.close();
      statusGuard.close();
      window.clearInterval(timer);
    };
  }, [projectId, statusFilter, activeOrgId]);

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
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Runs</h2>
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
        <RunStatusFilter value={statusFilter} onChange={setStatusFilter} />
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 8 }} />

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
            <Link to="/org/settings?tab=worker-keys" style={{ color: 'var(--warning-text)', fontWeight: 600 }}>
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
          <RunTable
            runs={runs}
            selectedRunId={selectedRunId}
            onSelect={selectRun}
            isPhone={isPhone}
            emptyText="No runs yet. Launch an agent from the Agents page or the board."
          />
        </div>

        {selectedRunId && (
          <RunDetailBeside
            runId={selectedRunId}
            isCompact={isCompact}
            onSelectRun={(id) => selectRun(id)}
            onClose={() => selectRun(null)}
          />
        )}
      </div>
    </div>
  );
};
