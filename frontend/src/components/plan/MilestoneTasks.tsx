// The tasks of a milestone, in its panel (MilestonePanel.tsx): the tasks
// assigned to it, each with its progress, and a searchable picker that
// assigns another. Finishing them is part of reaching the milestone: the
// share of them done is one of its criteria.
import React, { useMemo } from 'react';
import type { PlanMilestone, PlanTask, ProjectPlan } from '../../api/client';
import { Picker, buttonStyle, eyebrowStyle, mutedStyle, type PickerOption } from '../ui';
import { spanLabel } from './ganttScale';

export interface MilestoneTasksProps {
  plan: ProjectPlan;
  milestone: PlanMilestone;
  editable: boolean;
  busy: boolean;
  /** Assigns a task to a milestone by its key; an empty key takes the task off its milestone. */
  onAssign: (taskId: string, milestoneKey: string) => void;
  onSelectTask: (task: PlanTask) => void;
}

export const MilestoneTasks: React.FC<MilestoneTasksProps> = ({ plan, milestone: m, editable, busy, onAssign, onSelectTask }) => {
  const byId = useMemo(() => new Map(plan.tasks.map((t) => [t.id, t])), [plan.tasks]);
  const tasks = m.tasks.map((id) => byId.get(id)).filter((t): t is PlanTask => Boolean(t));
  const done = tasks.filter((t) => t.done).length;
  const candidates = useMemo<PickerOption[]>(
    () =>
      plan.tasks
        .filter((t) => t.milestone !== m.key)
        .map((t) => ({ value: t.id, label: `${t.wbs} ${t.title}`, meta: t.span_start ? spanLabel(t.span_start, t.span_end, plan.today) : 'Not scheduled', depth: t.depth })),
    [plan.tasks, plan.today, m.key],
  );
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderBottom: '1px solid var(--border-soft)' }} data-help-key="gantt.milestone-tasks">
      <h3 style={eyebrowStyle}>
        Tasks · {done}/{tasks.length} done
      </h3>
      {tasks.length === 0 && <span style={mutedStyle}>None assigned. Assign the tasks that must be finished to reach this milestone, and it counts them.</span>}
      {tasks.map((t) => {
        const parent = t.milestone_inherited && t.parent_id ? byId.get(t.parent_id) : undefined;
        return (
          <div key={t.id} data-milestone-task={t.id} style={{ display: 'flex', alignItems: 'center', gap: 6, minHeight: 28 }}>
            <button
              type="button"
              onClick={() => onSelectTask(t)}
              style={{ flex: 1, minWidth: 0, padding: 0, border: 'none', background: 'none', font: 'inherit', color: 'var(--accent-text)', textAlign: 'left', cursor: 'pointer', overflowWrap: 'anywhere' }}
            >
              {t.wbs} {t.title}
            </button>
            <span style={{ ...mutedStyle, flexShrink: 0, color: t.done ? 'var(--success-text)' : t.late ? 'var(--danger-strong)' : 'var(--text-muted)' }}>{t.done ? 'Done' : `${t.progress}%`}</span>
            {editable &&
              (t.milestone_inherited ? (
                <button
                  type="button"
                  disabled={!parent}
                  onClick={() => parent && onSelectTask(parent)}
                  title={`It is here because ${parent ? parent.title : 'a task above it'} is assigned to this milestone. Open that task to change it.`}
                  style={{ ...mutedStyle, flexShrink: 0, padding: 0, border: 'none', background: 'none', textDecoration: 'underline', cursor: 'pointer' }}
                >
                  via {parent ? parent.wbs : 'parent'}
                </button>
              ) : (
                <button type="button" disabled={busy} aria-label={`Take ${t.title} off this milestone`} onClick={() => onAssign(t.id, '')} style={buttonStyle('ghost', { compact: true, disabled: busy })}>
                  ✕
                </button>
              ))}
          </div>
        );
      })}
      {editable && candidates.length > 0 && (
        <>
          <label htmlFor="gantt-milestone-task-add" style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
            Assign a task
          </label>
          <Picker
            id="gantt-milestone-task-add"
            value=""
            options={candidates}
            searchable
            disabled={busy}
            placeholder="Search for a task…"
            noneLabel="Choose a task…"
            onChange={(id) => id && onAssign(id, m.key)}
          />
          <span style={mutedStyle}>A task with sub-tasks brings them with it. A task belongs to one milestone at a time.</span>
        </>
      )}
    </div>
  );
};
