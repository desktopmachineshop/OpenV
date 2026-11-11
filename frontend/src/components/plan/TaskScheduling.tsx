// How a task is scheduled, in its panel (TaskPanel.tsx): its mode (by hand,
// or placed by the platform), its date constraint, and the dates the plan
// works out for it: the earliest and latest it can run, and its float.
import React, { useEffect, useState } from 'react';
import type { PlanConstraintType, PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { eyebrowStyle, mutedStyle } from '../ui';
import { shortDate } from './ganttScale';
import { CONSTRAINTS } from './planModel';

const fieldStyle: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', minWidth: 0 };
const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

const days = (n: number) => `${n} working ${Math.abs(n) === 1 ? 'day' : 'days'}`;

export const TaskScheduling: React.FC<{
  plan: ProjectPlan;
  task: PlanTask;
  editable: boolean;
  busy: boolean;
  onSchedule: (task: PlanTask, change: PlanScheduleInput) => void;
}> = ({ plan, task, editable, busy, onSchedule }) => {
  const [type, setType] = useState<PlanConstraintType>(task.constraint_type);
  const [date, setDate] = useState(task.constraint_date || '');
  useEffect(() => {
    setType(task.constraint_type);
    setDate(task.constraint_date || '');
  }, [task.id, task.constraint_type, task.constraint_date]);
  const dated = type !== 'asap' && type !== 'alap';
  // A constraint that needs a date is saved once it has one.
  const commit = (nextType: PlanConstraintType, nextDate: string) => {
    const needsDate = nextType !== 'asap' && nextType !== 'alap';
    if (needsDate && !nextDate) return;
    if (nextType === task.constraint_type && (nextDate || '') === (task.constraint_date || '')) return;
    onSchedule(task, { constraint_type: nextType, ...(needsDate ? { constraint_date: nextDate } : {}) });
  };
  const float = task.float_days;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderBottom: '1px solid var(--border-soft)' }} data-help-key="gantt.scheduling">
      <h3 style={eyebrowStyle}>Scheduling</h3>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <label style={{ ...fieldStyle, gridColumn: '1 / -1' }}>
          Mode
          <select
            value={task.mode_own}
            disabled={!editable || busy}
            aria-label="Scheduling mode"
            onChange={(e) => onSchedule(task, { mode: e.target.value as PlanScheduleInput['mode'] })}
            style={inputStyle}
          >
            <option value="">The plan's: {plan.scheduling_mode === 'auto' ? 'automatic' : 'manual'}</option>
            <option value="manual">Manual: it stays where it is put</option>
            <option value="auto">Automatic: placed from its relationships</option>
          </select>
        </label>
        <label style={fieldStyle}>
          Constraint
          <select
            value={type}
            disabled={!editable || busy}
            aria-label="Constraint"
            onChange={(e) => {
              const next = e.target.value as PlanConstraintType;
              setType(next);
              commit(next, date);
            }}
            style={inputStyle}
          >
            {CONSTRAINTS.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
        </label>
        <label style={fieldStyle}>
          Constraint date
          <input
            type="date"
            value={dated ? date : ''}
            disabled={!editable || busy || !dated}
            aria-label="Constraint date"
            onChange={(e) => {
              setDate(e.target.value);
              commit(type, e.target.value);
            }}
            style={inputStyle}
          />
        </label>
      </div>
      {task.constraint_violated && (
        <span style={{ ...mutedStyle, color: 'var(--danger-strong)' }}>Its dates do not keep to its constraint. Recalculate the plan, or move it, to put it right.</span>
      )}
      {task.early_start && task.late_finish && (
        <dl data-task-dates style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '2px 10px', margin: 0, fontSize: 12 }}>
          <dt style={mutedStyle}>Earliest</dt>
          <dd style={{ margin: 0 }}>
            {shortDate(task.early_start, plan.today)} – {shortDate(task.early_finish, plan.today)}
          </dd>
          <dt style={mutedStyle}>Latest</dt>
          <dd style={{ margin: 0 }}>
            {shortDate(task.late_start, plan.today)} – {shortDate(task.late_finish, plan.today)}
          </dd>
          {float !== null && task.free_float_days !== null && (
            <>
              <dt style={mutedStyle}>Float</dt>
              <dd style={{ margin: 0, color: float < 0 ? 'var(--danger-strong)' : undefined }}>
                {float < 0 ? `${days(-float)} behind` : days(float)}
                {float >= 0 && task.free_float_days !== float ? `, ${days(Math.max(task.free_float_days, 0))} before it moves the next task` : ''}
              </dd>
            </>
          )}
        </dl>
      )}
      <span style={mutedStyle}>
        {task.mode === 'auto'
          ? 'An automatic task is placed after every change: after the tasks that drive it, no earlier than the plan starts, and as its constraint says. Type a start and it is held to no earlier than that day.'
          : 'A manual task stays where you put it. Recalculate, above the chart, moves it to what its relationships and constraint ask.'}
      </span>
    </div>
  );
};
