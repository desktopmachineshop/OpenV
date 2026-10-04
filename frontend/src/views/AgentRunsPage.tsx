import React, { useEffect, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { AgentRun, WorkerStatus, agentRunsAPI, workerStatusAPI } from '../api/client';
import { useAppStore } from '../state/store';
import {
  RunDetailBeside,
  RunStatusFilter,
  RunTable,
  pollGuard,
  useCloseRunOnSwitch,
} from '../components/agents/RunTable';
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

  // The last answer and the project it is of: the list shows it while that
  // project is on screen, and says it is loading until then, so a switch of
  // project empties it until the new project's answer (#379 bug 177), and
  // useCloseRunOnSwitch closes the old project's run. Another status filter
  // keeps it while its answer loads. A workspace switch that keeps the
  // project, as ProjectLayout makes to follow a link into another
  // workspace's project, is no switch here.
  const [listed, setListed] = useState<{ projectId: string; runs: AgentRun[] } | null>(null);
  const runs = listed?.projectId === projectId ? listed.runs : null;
  // The runner status and the workspace it was read for: the banners show
  // it while that workspace is the active one, so a workspace switch, as
  // ProjectLayout makes to follow a link into another workspace's project,
  // hides the old workspace's until the new one's arrives (#379 bug 181).
  const [readStatus, setReadStatus] = useState<{ orgId: string; status: WorkerStatus } | null>(null);
  const workerStatus = readStatus?.orgId === activeOrgId ? readStatus.status : null;
  const [statusFilter, setStatusFilter] = useState('all');
  const [pendingCount, setPendingCount] = useState(0);
  const [showProposals, setShowProposals] = useState(true);
  const [showConnect, setShowConnect] = useState(false);
  // The last error and the project it was raised for: it shows while that
  // project is on screen, so a switch of project hides the old project's
  // error (#379 bug 180).
  const [raised, setRaised] = useState<{ projectId: string; message: string } | null>(null);
  const error = raised?.projectId === projectId ? raised.message : '';

  const selectedRunId = searchParams.get('run');
  useCloseRunOnSwitch(projectId);
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
          setListed({ projectId, runs: res.data || [] });
          setRaised(null);
        })
        .catch((err: any) => {
          if (!runsCurrent()) return;
          setRaised({ projectId, message: err.response?.data?.error || err.message || 'Failed to load runs' });
        });
      if (activeOrgId) {
        const statusCurrent = statusGuard.next();
        workerStatusAPI
          .get(activeOrgId)
          .then((res) => {
            if (statusCurrent()) setReadStatus({ orgId: activeOrgId, status: res.data });
          })
          .catch(() => {
            // Banner data is best-effort; keep the workspace's last known
            // status on error.
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

      <ErrorBanner message={error} onDismiss={() => setRaised(null)} style={{ marginBottom: 8 }} />

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
          // Until the workspace's runner status arrives, as after a switch
          // that hides the old one's (#379 bug 181), the prompt gives its own.
          reason={
            workerStatus
              ? `${workerStatus.queue.queued} queued run${workerStatus.queue.queued === 1 ? ' is' : 's are'} waiting for a runner.`
              : undefined
          }
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
