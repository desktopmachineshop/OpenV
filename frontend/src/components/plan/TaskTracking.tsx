// What has happened on a task, in its panel (TaskPanel.tsx): the days work
// really started and finished, a percentage complete typed by hand in place
// of the counted one, what it has cost, and how far it sits from the
// baseline.
import React, { useEffect, useState } from 'react';
import type { PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { eyebrowStyle, mutedStyle } from '../ui';
import { shortDate, spanLabel } from './ganttScale';
import { money } from './planModel';

const fieldStyle: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', minWidth: 0 };
const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

/** A variance in working days, worded. */
const variance = (n: number) => (n === 0 ? 'on the baseline' : `${Math.abs(n)} working ${Math.abs(n) === 1 ? 'day' : 'days'} ${n > 0 ? 'later' : 'earlier'}`);

/** A number that is empty when unset; it is saved when the field is left, and clearing it sends -1. */
const OptionalNumber: React.FC<{ value: number | null; label: string; max: number; placeholder: string; disabled: boolean; onCommit: (n: number) => void }> = ({ value, label, max, placeholder, disabled, onCommit }) => {
  const shown = value === null ? '' : String(value);
  const [text, setText] = useState(shown);
  useEffect(() => setText(shown), [shown]);
  const commit = () => {
    if (text.trim() === shown) return;
    const n = text.trim() === '' ? -1 : Number(text);
    if (!(n >= -1 && n <= max)) setText(shown);
    else onCommit(n);
  };
  return (
    <input
      type="number"
      min={0}
      max={max}
      value={text}
      disabled={disabled}
      aria-label={label}
      placeholder={placeholder}
      onChange={(e) => setText(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          commit();
        }
      }}
      style={inputStyle}
    />
  );
};

export const TaskTracking: React.FC<{
  plan: ProjectPlan;
  task: PlanTask;
  editable: boolean;
  busy: boolean;
  onSchedule: (task: PlanTask, change: PlanScheduleInput) => void;
}> = ({ plan, task, editable, busy, onSchedule }) => {
  const off = !editable || busy;
  const base = task.baseline;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderBottom: '1px solid var(--border-soft)' }} data-help-key="gantt.tracking">
      <h3 style={eyebrowStyle}>Tracking</h3>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <label style={fieldStyle}>
          Actual start
          <input type="date" value={task.actual_start || ''} disabled={off} aria-label="Actual start" onChange={(e) => onSchedule(task, { actual_start: e.target.value })} style={inputStyle} />
        </label>
        <label style={fieldStyle}>
          Actual finish
          <input type="date" value={task.actual_finish || ''} disabled={off} aria-label="Actual finish" onChange={(e) => onSchedule(task, { actual_finish: e.target.value })} style={inputStyle} />
        </label>
        <label style={fieldStyle}>
          % complete
          <OptionalNumber value={task.percent_complete} label="Percent complete" max={100} placeholder={`${task.progress} (counted)`} disabled={off} onCommit={(n) => onSchedule(task, { percent_complete: Math.round(n) })} />
        </label>
        <label style={fieldStyle}>
          Actual cost
          <OptionalNumber value={task.actual_cost} label="Actual cost" max={1e12} placeholder="As earned" disabled={off} onCommit={(n) => onSchedule(task, { actual_cost: n })} />
        </label>
      </div>
      {task.behind_schedule && <span style={{ ...mutedStyle, color: 'var(--danger-strong)' }}>Behind: less is done than {shortDate(plan.status_day, plan.today)} asks.</span>}
      {base && base.start && base.end ? (
        <dl data-task-baseline style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '2px 10px', margin: 0, fontSize: 12 }}>
          <dt style={mutedStyle}>Baseline</dt>
          <dd style={{ margin: 0 }}>{spanLabel(base.start, base.end, plan.today)}</dd>
          {task.start_variance_days !== null && (
            <>
              <dt style={mutedStyle}>Start</dt>
              <dd style={{ margin: 0, color: task.start_variance_days > 0 ? 'var(--danger-strong)' : undefined }}>{variance(task.start_variance_days)}</dd>
            </>
          )}
          {task.finish_variance_days !== null && (
            <>
              <dt style={mutedStyle}>Finish</dt>
              <dd style={{ margin: 0, color: task.finish_variance_days > 0 ? 'var(--danger-strong)' : undefined }}>{variance(task.finish_variance_days)}</dd>
            </>
          )}
          {base.cost > 0 && (
            <>
              <dt style={mutedStyle}>Cost</dt>
              <dd style={{ margin: 0, color: task.cost > base.cost ? 'var(--danger-strong)' : undefined }}>
                {money(task.cost, plan.currency)} against {money(base.cost, plan.currency)}
              </dd>
            </>
          )}
        </dl>
      ) : (
        <span style={mutedStyle}>{plan.baseline_id ? 'This task is not in the baseline.' : 'Save a baseline, under Tracking above the chart, to see how far the task has moved.'}</span>
      )}
    </div>
  );
};
