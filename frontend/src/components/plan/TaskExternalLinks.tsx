// A task's relationships to tasks of other projects, in its panel
// (TaskPanel.tsx): the ones it has, and a form that makes it wait on a task
// of another project of the workspace.
import React, { useEffect, useState } from 'react';
import { planAPI, projectAPI, type PlanRelationshipType, type PlanTask, type Project, type ProjectPlan } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { Chip, buttonStyle, eyebrowStyle, mutedStyle } from '../ui';
import { spanLabel } from './ganttScale';
import { RELATIONSHIP_TYPES, lagText, relationshipLabel } from './planModel';

const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box', minWidth: 0 };

export const TaskExternalLinks: React.FC<{
  plan: ProjectPlan;
  task: PlanTask;
  editable: boolean;
  busy: boolean;
  /** Called after a link is made or removed, to read the plan again. */
  onChanged: () => void;
}> = ({ plan, task, editable, busy, onChanged }) => {
  const [adding, setAdding] = useState(false);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectId, setProjectId] = useState('');
  const [tasks, setTasks] = useState<PlanTask[]>([]);
  const [otherId, setOtherId] = useState('');
  const [type, setType] = useState<PlanRelationshipType>('FS');
  const [lag, setLag] = useState('0');
  const [error, setError] = useState('');

  useEffect(() => {
    if (!adding) return;
    projectAPI
      .list()
      .then((res) => setProjects(res.data.filter((p) => p.id !== plan.project_id)))
      .catch((err) => setError(apiErrorMessage(err, 'Failed to list the projects')));
  }, [adding, plan.project_id]);
  useEffect(() => {
    setTasks([]);
    setOtherId('');
    if (!projectId) return;
    planAPI
      .get(projectId)
      .then((res) => setTasks(res.data.tasks))
      .catch((err) => setError(apiErrorMessage(err, "Failed to read that project's plan")));
  }, [projectId]);

  const act = async (fn: () => Promise<unknown>, fallback: string) => {
    setError('');
    try {
      await fn();
      setAdding(false);
      onChanged();
    } catch (err) {
      setError(apiErrorMessage(err, fallback));
    }
  };
  const links = task.external_links;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderBottom: '1px solid var(--border-soft)' }} data-help-key="gantt.master">
      <h3 style={eyebrowStyle}>Other projects · {links.length}</h3>
      {links.length === 0 && !adding && <span style={mutedStyle}>No task of another project drives this one, or waits on it.</span>}
      {links.map((l) => (
        <div key={l.task_id} data-external-link={l.task_id} style={{ display: 'flex', gap: 6, alignItems: 'flex-start', fontSize: 13 }}>
          <span style={{ flex: 1, minWidth: 0 }}>
            {l.direction === 'driven_by' ? 'Waits on ' : 'Drives '}
            <b>{l.title}</b> <span style={mutedStyle}>in {l.project_name}</span>
            <span style={{ ...mutedStyle, display: 'block' }}>
              {relationshipLabel(l.type)}
              {l.lag_days ? `, ${lagText(l.lag_days, l.lag_unit)}` : ''} · {l.end ? spanLabel(l.start, l.end, plan.today) : 'not scheduled'}
            </span>
            {l.conflict && <Chip tone="danger">the dates break it</Chip>}
          </span>
          {editable && (
            <button type="button" disabled={busy} aria-label={`Remove the link to ${l.title}`} onClick={() => act(() => planAPI.unrelateExternal(task.id, l.task_id), 'Failed to remove the link')} style={buttonStyle('ghost', { compact: true, disabled: busy })}>
              ✕
            </button>
          )}
        </div>
      ))}
      {editable && !adding && (
        <button type="button" data-add-external onClick={() => setAdding(true)} style={{ ...buttonStyle('secondary', { compact: true }), alignSelf: 'flex-start' }}>
          Wait on a task of another project
        </button>
      )}
      {adding && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <select value={projectId} onChange={(e) => setProjectId(e.target.value)} aria-label="Project" style={inputStyle}>
            <option value="">Choose a project…</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <select value={otherId} disabled={!projectId} onChange={(e) => setOtherId(e.target.value)} aria-label="Task of that project" style={inputStyle}>
            <option value="">{projectId && tasks.length === 0 ? 'No tasks there' : 'Choose a task…'}</option>
            {tasks.map((t) => (
              <option key={t.id} value={t.id}>
                {t.wbs} {t.title}
              </option>
            ))}
          </select>
          <div style={{ display: 'flex', gap: 6 }}>
            <select value={type} onChange={(e) => setType(e.target.value as PlanRelationshipType)} aria-label="Relationship type" style={{ ...inputStyle, flex: 1 }}>
              {RELATIONSHIP_TYPES.map((r) => (
                <option key={r.value} value={r.value}>
                  {r.label}
                </option>
              ))}
            </select>
            <input type="number" value={lag} onChange={(e) => setLag(e.target.value)} aria-label="Offset in working days" title="Offset in working days" style={{ ...inputStyle, width: 70 }} />
          </div>
          <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
            <button type="button" onClick={() => setAdding(false)} style={buttonStyle('secondary', { compact: true })}>
              Cancel
            </button>
            <button
              type="button"
              data-save-external
              disabled={!otherId || busy}
              onClick={() => act(() => planAPI.relateExternal(task.id, otherId, type, Math.round(Number(lag)) || 0), 'Failed to link the tasks')}
              style={buttonStyle('primary', { compact: true, disabled: !otherId || busy })}
            >
              Link
            </button>
          </div>
          <span style={mutedStyle}>This task then follows that one when the plan is recalculated. The other project's task is never moved from here.</span>
        </div>
      )}
      {error && (
        <span role="alert" style={{ ...mutedStyle, color: 'var(--danger-strong)' }}>
          {error}
        </span>
      )}
    </div>
  );
};
