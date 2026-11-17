// The workspace Plan's "My tasks": the tasks the reader works on, across
// every project of the workspace they can read. It is the project's plan
// turned round: one person, every project. A task leads to its project's
// Gantt, where it is scheduled.
import React, { useEffect, useMemo, useState } from 'react';
import { Link, Navigate } from 'react-router-dom';
import { planAPI, type MyPlanTasks } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { useFeature } from '../hooks/useFeature';
import { GANTT_PLAN_FEATURE, WORKSPACE_PLAN_FEATURE } from '../features';
import { Navbar } from '../components/Navbar';
import { Chip, ErrorBanner, PanelHeader, mutedStyle, panelStyle } from '../components/ui';
import { spanLabel } from '../components/plan/ganttScale';
import { columnLabel } from '../components/plan/planModel';

type Task = MyPlanTasks['tasks'][number];

/** Open tasks first: the late ones, then by the day they end, the undated last; done ones after. */
export const myTaskOrder = (a: Task, b: Task): number =>
  Number(a.done) - Number(b.done) || Number(b.late) - Number(a.late) || (a.end || '9999').localeCompare(b.end || '9999') || a.title.localeCompare(b.title);

export const GanttWorkspaceTasks: React.FC = () => {
  const gatesSettled = useAppStore((s) => s.features !== null || s.featuresFailed);
  const noWorkspace = useAppStore((s) => s.orgsLoaded && !s.activeOrgId);
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const gantt = useFeature(GANTT_PLAN_FEATURE);
  const page = useFeature(WORKSPACE_PLAN_FEATURE);
  const on = gantt && page;
  const [mine, setMine] = useState<MyPlanTasks | null>(null);
  const [error, setError] = useState('');
  const [showDone, setShowDone] = useState(false);
  useEffect(() => {
    if (!on) return undefined;
    let live = true;
    setMine(null);
    planAPI
      .mine()
      .then((res) => live && setMine(res.data))
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to load your tasks')));
    return () => {
      live = false;
    };
  }, [on, activeOrgId]);
  const tasks = useMemo(() => (mine ? [...mine.tasks].sort(myTaskOrder) : []), [mine]);
  const open = tasks.filter((t) => !t.done);
  const shown = showDone ? tasks : open;
  if ((gatesSettled && !on) || noWorkspace) return <Navigate to="/projects" replace />;
  return (
    <div className="app-shell" style={{ background: 'var(--bg-app)', display: 'flex', flexDirection: 'column' }}>
      <Navbar title="My tasks" showWorkspaceControls />
      <div className="ov-gutter" style={{ padding: 16 }}>
        <h2 className="sr-only">My tasks</h2>
        <ErrorBanner message={error} onDismiss={() => setError('')} />
        <section aria-label="My tasks" data-help-key="gantt.master" style={{ ...panelStyle, overflow: 'hidden' }}>
          <PanelHeader
            title="My tasks"
            count={open.length}
            action={
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, margin: 0, fontSize: 13 }}>
                <input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} style={{ width: 'auto', margin: 0 }} />
                Show done {tasks.length - open.length}
              </label>
            }
          />
          {!mine && !error && <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Loading your tasks…</p>}
          {mine && shown.length === 0 && (
            <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>
              {tasks.length === 0 ? 'No task in this workspace is assigned to you. A task is yours when you are its assignee on the board, or one of its resources on the plan.' : 'Nothing open: every task of yours is done.'}
            </p>
          )}
          {shown.length > 0 && (
            <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {shown.map((t) => (
                <li key={t.id} data-my-task={t.id} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 12px', padding: '10px 16px', borderTop: '1px solid var(--border-soft)' }}>
                  <Link to={`/projects/${t.project_id}/gantt`} style={{ flex: '1 1 240px', minWidth: 0, fontWeight: 600, textDecoration: t.done ? 'line-through' : undefined }}>
                    {t.title}
                  </Link>
                  <span style={{ ...mutedStyle, flex: '1 1 160px', minWidth: 0 }}>{t.project_name}</span>
                  <span style={{ ...mutedStyle, flex: '0 0 auto' }}>
                    {spanLabel(t.start, t.end, mine!.today)} · {columnLabel(t.column)} · {t.progress}%
                  </span>
                  {t.done ? <Chip tone="success">Done</Chip> : t.late ? <Chip tone="danger">Late</Chip> : t.critical ? <Chip tone="warning">Critical</Chip> : null}
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </div>
  );
};
