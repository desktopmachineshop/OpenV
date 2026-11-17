// The views of a project's Gantt, over every project of the workspace at
// once: the outline with its bars, the network, the calendar and who is
// loaded when. They are the Gantt's own components, given every project's
// plan merged into one (mergePlans), so nothing is drawn twice. Here they
// only read: a task is changed on its own project's Gantt, which its open
// mark leads to.
import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { planAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { mutedStyle } from '../ui';
import { GanttChart } from './GanttChart';
import type { Zoom } from './ganttScale';
import { mergePlans, type PlanOfProject } from './mergePlans';
import { PlanMonth } from './PlanMonth';
import { PlanNetwork } from './PlanNetwork';
import { PlanUsage } from './PlanUsage';
import { DEFAULT_VIEW } from './planView';

export type WorkspaceView = 'lines' | 'gantt' | 'network' | 'calendar' | 'usage';

/** The views the workspace's Plan page offers; `lines` is the chart of a line per project. */
export const WORKSPACE_VIEWS: { value: WorkspaceView; label: string }[] = [
  { value: 'lines', label: 'Timeline' },
  { value: 'gantt', label: 'Gantt' },
  { value: 'network', label: 'Network' },
  { value: 'calendar', label: 'Calendar' },
  { value: 'usage', label: 'Resource usage' },
];

export const PlanWorkspaceViews: React.FC<{ view: Exclude<WorkspaceView, 'lines'>; zoom: Zoom; showCritical: boolean; todaySignal: number }> = ({ view, zoom, showCritical, todaySignal }) => {
  const navigate = useNavigate();
  const [plans, setPlans] = useState<PlanOfProject[] | null>(null);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState('');
  useEffect(() => {
    let live = true;
    (async () => {
      try {
        // The projects the reader can see, in the order the lines are drawn in.
        const { data: master } = await planAPI.master('1');
        const loaded = await Promise.all(master.projects.map(async (p) => ({ id: p.project_id, name: p.name, plan: (await planAPI.get(p.project_id)).data })));
        if (live) setPlans(loaded);
      } catch (err) {
        if (live) setError(apiErrorMessage(err, 'Failed to load the plans'));
      }
    })();
    return () => {
      live = false;
    };
  }, []);
  const merged = useMemo(() => (plans ? mergePlans(plans) : null), [plans]);
  if (error) return <p role="alert" style={{ padding: 16, margin: 0, color: 'var(--danger-strong)' }}>{error}</p>;
  if (!plans) return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Loading every project's plan…</p>;
  if (!merged) return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>No project of this workspace has a plan yet.</p>;
  const { plan, projectOf } = merged;
  const open = (id: string) => {
    const project = projectOf.get(id);
    if (project) navigate(`/projects/${project}/gantt`);
  };
  return (
    <div data-workspace-view={view} style={{ flex: 1, minHeight: 0, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
      {view === 'gantt' && (
        <GanttChart
          plan={plan}
          zoom={zoom}
          outlineWidth={320}
          view={DEFAULT_VIEW}
          selectedTaskId={selected}
          selectedMilestoneKey=""
          onSelectTask={(t) => setSelected(t.id)}
          onOpenTask={(t) => open(t.id)}
          onSelectMilestone={() => undefined}
          showCritical={showCritical}
          todaySignal={todaySignal}
        />
      )}
      {view === 'network' && <PlanNetwork plan={plan} selectedTaskId={selected} onSelectTask={(t) => setSelected(t.id)} showCritical={showCritical} />}
      {view === 'calendar' && <PlanMonth plan={plan} selectedTaskId={selected} onSelectTask={(t) => setSelected(t.id)} zoom={zoom} showCritical={showCritical} todaySignal={todaySignal} />}
      {view === 'usage' && <PlanUsage plan={plan} zoom={zoom} showCritical={showCritical} />}
    </div>
  );
};
