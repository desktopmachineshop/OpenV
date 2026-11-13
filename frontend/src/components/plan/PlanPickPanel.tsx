// What the Plan page shows beside the chart while more than one task is
// picked: which they are, and what can be done to all of them at once.
import React, { useState } from 'react';
import { planAPI, type PlanTask, type ProjectPlan } from '../../api/client';
import { addDays } from './ganttScale';
import { buttonStyle, ellipsis, mutedStyle } from '../ui';

export interface PlanPickPanelProps {
  plan: ProjectPlan;
  tasks: PlanTask[];
  busy?: boolean;
  /** Assigns every picked task to the milestone; an empty key takes them off theirs. */
  onAssign: (tasks: PlanTask[], milestone: string) => void;
  /** Moves every picked task with dates of its own by so many days. */
  onShift: (tasks: PlanTask[], days: number) => void;
  onOpenTask: (t: PlanTask) => void;
  onClose: () => void;
}

/** Assigns each task to the milestone, one after another: each save reads the plan the last one left. */
export const assignTasks = async (tasks: PlanTask[], milestone: string): Promise<void> => {
  for (const t of tasks) await planAPI.schedule(t.id, { milestone });
};

/** Moves each task's own dates by so many days. */
export const shiftTasks = async (tasks: PlanTask[], days: number): Promise<void> => {
  for (const t of tasks) if (t.start && t.end) await planAPI.schedule(t.id, { start_date: addDays(t.start, days), due_date: addDays(t.end, days) });
};

const field: React.CSSProperties = { display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' };

export const PlanPickPanel: React.FC<PlanPickPanelProps> = ({ plan, tasks, busy, onAssign, onShift, onOpenTask, onClose }) => {
  const [milestone, setMilestone] = useState('');
  const [days, setDays] = useState('');
  // A parent's dates are its sub-tasks': it moves when they do.
  const movable = tasks.filter((t) => t.subtasks_total === 0 && t.start && t.end);
  const by = Math.trunc(Number(days));
  return (
    <aside aria-label={`${tasks.length} tasks selected`} data-plan-picks className="card" style={{ margin: 0, padding: 12, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <strong style={{ flex: 1 }}>{tasks.length} tasks selected</strong>
        <button type="button" onClick={onClose} style={buttonStyle('ghost', { compact: true })}>
          Clear
        </button>
      </div>
      <ul aria-label="Selected tasks" style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 2, maxHeight: 220, overflowY: 'auto' }}>
        {tasks.map((t) => (
          <li key={t.id}>
            <button type="button" onClick={() => onOpenTask(t)} title={`Open ${t.title} on its own`} style={{ ...ellipsis, display: 'block', width: '100%', textAlign: 'left', background: 'none', border: 'none', padding: '2px 0', font: 'inherit', color: 'var(--text)', cursor: 'pointer' }}>
              <span style={{ color: 'var(--text-muted)', marginRight: 6 }}>{t.wbs}</span>
              {t.title}
            </button>
          </li>
        ))}
      </ul>
      <div style={field}>
        <select value={milestone} onChange={(e) => setMilestone(e.target.value)} aria-label="Milestone for the selected tasks" style={{ flex: 1, minWidth: 0, margin: 0 }}>
          <option value="">No milestone</option>
          {plan.milestones.map((m) => (
            <option key={m.key} value={m.key}>
              {m.name}
            </option>
          ))}
        </select>
        <button type="button" disabled={busy} onClick={() => onAssign(tasks, milestone)} style={buttonStyle('secondary', { compact: true, disabled: busy })}>
          Assign
        </button>
      </div>
      <div style={field}>
        <input type="number" step={1} value={days} onChange={(e) => setDays(e.target.value)} placeholder="Days" aria-label="Days to move the selected tasks by" style={{ width: 80, margin: 0 }} />
        <button type="button" disabled={busy || !by || movable.length === 0} onClick={() => onShift(movable, by)} style={buttonStyle('secondary', { compact: true, disabled: busy || !by || movable.length === 0 })}>
          Move dates
        </button>
      </div>
      <p style={{ ...mutedStyle, margin: 0, fontSize: 12 }}>
        A negative number moves them earlier. {movable.length < tasks.length ? `${tasks.length - movable.length} of them ${tasks.length - movable.length === 1 ? 'has' : 'have'} no dates of ${tasks.length - movable.length === 1 ? 'its' : 'their'} own to move. ` : ''}
        Ctrl-click adds or drops a task; Shift-click takes the run between.
      </p>
    </aside>
  );
};
