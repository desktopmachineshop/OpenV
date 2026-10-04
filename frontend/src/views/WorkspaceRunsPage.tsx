import React, { useEffect, useState } from 'react';
import { Navigate, useSearchParams } from 'react-router-dom';
import { AgentRun, agentRunsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { Navbar } from '../components/Navbar';
import { ErrorBanner } from '../components/ui';
import { RunDetailBeside, RunStatusFilter, RunTable, pollGuard } from '../components/agents/RunTable';
import { WORKSPACE_RUNS_FEATURE } from '../components/agents/workspaceRuns';

// As on a project's Runs page, the 5s poll lists at most this many runs.
const RUNS_POLL_LIMIT = 200;

/**
 * The workspace Runs page (#379 bug 168): the active workspace's agent runs
 * that belong to no project, newest first, with the run detail beside them.
 * A whole-workspace automation's scheduled and event runs, the products an
 * agent invents from the projects list and the runs a project's delete
 * left behind have no project, so no project's Runs page lists them.
 *
 * Gated on workspace-runs: it waits for the gates, and without the feature
 * sends a member to the projects list, where /org/runs led before (the
 * catch-all route), so the link an email or a web push carries for such a
 * run lands where it did. So it does with no workspace to have gates, and
 * when the gates fail to load, which counts as the feature off (#379 bug
 * 174): a load still on its way keeps it waiting.
 */
export const WorkspaceRunsPage: React.FC = () => {
  const gatesSettled = useAppStore((s) => s.features !== null || s.featuresFailed);
  const noWorkspace = useAppStore((s) => s.orgsLoaded && !s.activeOrgId);
  const on = useFeature(WORKSPACE_RUNS_FEATURE);
  if ((gatesSettled && !on) || noWorkspace) return <Navigate to="/projects" replace />;
  return (
    <div className="app-shell" style={{ background: 'var(--bg-app)', display: 'flex', flexDirection: 'column' }}>
      <Navbar title="Workspace runs" showWorkspaceControls />
      {on ? (
        <WorkspaceRuns />
      ) : (
        <p style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</p>
      )}
    </div>
  );
};

/**
 * The list and the detail. ?run= names the run open beside the list, as on
 * a project's Runs page, so a reload keeps it. The server lists what the
 * member may open (GET /agent-runs?project=none): every such run to a
 * workspace admin, to anyone else the ones they launched.
 */
const WorkspaceRuns: React.FC = () => {
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const orgs = useAppStore((s) => s.orgs);
  const setActiveOrgId = useAppStore((s) => s.setActiveOrgId);
  const isAdmin = useAppStore((s) =>
    Boolean(s.currentUser?.is_admin || s.orgs.find((o) => o.id === s.activeOrgId)?.role === 'admin')
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [statusFilter, setStatusFilter] = useState('all');
  const [error, setError] = useState('');
  const selectedRunId = searchParams.get('run');
  const { isPhone, isCompact } = useViewport();

  useEffect(() => {
    if (!activeOrgId) return;
    const query: { project: 'none'; status?: string; limit: number } = { project: 'none', limit: RUNS_POLL_LIMIT };
    if (statusFilter !== 'all') query.status = statusFilter;
    // A workspace switch, or another filter, starts a new poll: what the
    // old one still has on its way is dropped (pollGuard).
    const guard = pollGuard();
    const load = () => {
      const current = guard.next();
      agentRunsAPI
        .list(query)
        .then((res) => {
          if (!current()) return;
          setRuns(res.data || []);
          setError('');
        })
        .catch((err) => {
          if (current()) setError(apiErrorMessage(err, 'Failed to load runs'));
        });
    };
    load();
    const timer = window.setInterval(load, 5000);
    return () => {
      guard.close();
      window.clearInterval(timer);
    };
  }, [activeOrgId, statusFilter]);

  // A notification lists a member's runs of every workspace, so its link
  // may name a run of another one: follow the run there, as a project's
  // pages follow their project, so that it is listed where it is shown.
  useEffect(() => {
    if (!selectedRunId || orgs.length === 0) return;
    let cancelled = false;
    agentRunsAPI
      .get(selectedRunId)
      .then((res) => {
        const org = res.data.org_id;
        if (!cancelled && org && org !== activeOrgId && orgs.some((o) => o.id === org)) {
          setActiveOrgId(org, { clearProjects: false });
        }
      })
      .catch(() => {
        // The detail panel says what is wrong with the run.
      });
    return () => {
      cancelled = true;
    };
  }, [selectedRunId, orgs, activeOrgId, setActiveOrgId]);

  const selectRun = (runId: string | null) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (runId) next.set('run', runId);
      else next.delete('run');
      return next;
    });
  };

  return (
    <div style={{ padding: isPhone ? '0 12px 12px' : '0 20px 20px', flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 8, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Workspace runs</h2>
        <div style={{ flex: 1 }} />
        <RunStatusFilter value={statusFilter} onChange={setStatusFilter} />
      </div>
      <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
        The runs that belong to no project: a whole-workspace automation&apos;s, the products an agent invents
        from the projects list, and those of a deleted project. A project&apos;s own runs are on its Runs page.{' '}
        {isAdmin
          ? 'As a workspace admin you see everyone’s.'
          : 'You see the ones you launched; workspace admins see everyone’s.'}
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 8 }} />

      <div style={{ display: 'flex', gap: 16, flex: 1, minHeight: 0 }}>
        <div style={{ flex: 1, overflowY: 'auto', minWidth: 0 }}>
          <RunTable
            runs={runs}
            selectedRunId={selectedRunId}
            onSelect={selectRun}
            isPhone={isPhone}
            emptyText="No runs without a project yet."
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
