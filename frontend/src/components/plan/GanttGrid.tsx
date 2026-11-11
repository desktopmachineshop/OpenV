// The columns beside the task names on the Gantt (GanttChart.tsx): their
// headings, a task's cells, which are edited in place where the plan does
// not work the value out, and the heading row of a group of tasks.
import React, { useState } from 'react';
import type { PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { ellipsis } from '../ui';
import { ROW_HEIGHT } from './GanttLinks';
import { shortDate } from './ganttScale';
import { money, personName } from './planModel';
import type { ColumnSpec, PlanColumn } from './planView';

const cellStyle = (width: number): React.CSSProperties => ({ ...ellipsis, width, flexShrink: 0, boxSizing: 'border-box', padding: '0 4px', fontSize: 12, textAlign: 'right' });

/** The headings of the chosen columns, at the right of the chart's corner. */
export const GridHead: React.FC<{ columns: ColumnSpec[] }> = ({ columns }) => (
  <>
    {columns.map((c) => (
      <span key={c.key} role="columnheader" style={{ ...cellStyle(c.width), fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
        {c.label}
      </span>
    ))}
  </>
);

/** What a cell shows, and the text its editor starts from. */
const valueOf = (plan: ProjectPlan, t: PlanTask, key: PlanColumn): { shown: string; raw: string } => {
  switch (key) {
    case 'start': {
      const d = t.start || t.span_start;
      return { shown: d ? shortDate(d, plan.today) : '', raw: d || '' };
    }
    case 'end': {
      const d = t.end || t.span_end;
      return { shown: d ? shortDate(d, plan.today) : '', raw: d || '' };
    }
    case 'duration':
      return { shown: t.duration_days === null ? '' : `${t.duration_days}${t.elapsed ? 'e' : ''}d`, raw: t.duration_days === null ? '' : String(t.duration_days) };
    case 'effort':
      return { shown: t.effort_days === null ? '' : `${t.effort_days}d`, raw: t.effort_days === null ? '' : String(t.effort_days) };
    case 'percent':
      return { shown: `${t.progress}%`, raw: t.percent_complete === null ? '' : String(t.percent_complete) };
    case 'float':
      return { shown: t.float_days === null ? '' : `${t.float_days}d`, raw: '' };
    case 'cost':
      return { shown: t.cost ? money(t.cost, '') : '', raw: '' };
    default:
      return { shown: t.assignments.map((a) => personName(plan.people, a)).join(', '), raw: '' };
  }
};

/** The schedule change a cell's new text asks for; null when the text is not a value the column takes. */
const changeOf = (key: PlanColumn, text: string): PlanScheduleInput | null => {
  const n = Number(text);
  switch (key) {
    case 'start':
      return { start_date: text };
    case 'end':
      return { due_date: text };
    case 'duration':
      return Number.isInteger(n) && n >= 0 ? { duration_days: n } : null;
    case 'effort':
      return n >= 0 ? { effort_days: n } : null;
    case 'percent':
      return text === '' ? { percent_complete: -1 } : Number.isInteger(n) && n >= 0 && n <= 100 ? { percent_complete: n } : null;
    default:
      return null;
  }
};

/** A task's cells under the chosen columns. A cell the reader may edit opens a field when clicked; Enter or leaving it saves, Escape gives up. */
export const GridCells: React.FC<{
  plan: ProjectPlan;
  task: PlanTask;
  columns: ColumnSpec[];
  onEdit?: (t: PlanTask, change: PlanScheduleInput) => void;
}> = ({ plan, task, columns, onEdit }) => {
  const [editing, setEditing] = useState<{ key: PlanColumn; text: string } | null>(null);
  const commit = () => {
    if (!editing) return;
    const was = valueOf(plan, task, editing.key).raw;
    const change = editing.text.trim() === was ? null : changeOf(editing.key, editing.text.trim());
    setEditing(null);
    if (change && onEdit) onEdit(task, change);
  };
  return (
    <>
      {columns.map((c) => {
        const v = valueOf(plan, task, c.key);
        const editable = Boolean(onEdit && c.edit && task.subtasks_total === 0);
        if (editing && editing.key === c.key) {
          return (
            <input
              key={c.key}
              type={c.edit === 'date' ? 'date' : 'number'}
              autoFocus
              value={editing.text}
              aria-label={`${c.label} of ${task.title}`}
              onChange={(e) => setEditing({ key: c.key, text: e.target.value })}
              onBlur={commit}
              onKeyDown={(e) => {
                if (e.key === 'Enter') commit();
                if (e.key === 'Escape') setEditing(null);
              }}
              style={{ width: c.width, flexShrink: 0, boxSizing: 'border-box', minHeight: 24, height: 24, padding: '0 2px', fontSize: 11 }}
            />
          );
        }
        return editable ? (
          <button
            key={c.key}
            type="button"
            data-grid-cell={`${task.id}:${c.key}`}
            aria-label={`${c.label} of ${task.title}: ${v.shown || 'not set'}. Edit`}
            title={v.shown ? `${v.shown}: click to edit` : 'Click to set'}
            onClick={() => setEditing({ key: c.key, text: v.raw })}
            style={{ ...cellStyle(c.width), height: ROW_HEIGHT - 8, border: '1px solid transparent', borderRadius: 3, background: 'none', color: 'var(--text)', font: 'inherit', fontSize: 12, cursor: 'text' }}
          >
            {v.shown || <span style={{ color: 'var(--text-muted)' }}>–</span>}
          </button>
        ) : (
          <span key={c.key} role="cell" title={v.shown} style={{ ...cellStyle(c.width), color: 'var(--text-secondary)' }}>
            {v.shown}
          </span>
        );
      })}
    </>
  );
};

/** The heading of a group of tasks: its name and how many it holds, across the task column, over an empty stretch of timeline. */
export const GroupRow: React.FC<{ label: string; count: number; width: number; timeline: number }> = ({ label, count, width, timeline }) => (
  <div role="row" data-group-row={label} style={{ display: 'flex', height: ROW_HEIGHT }}>
    <div
      role="rowheader"
      style={{ ...ellipsis, position: 'sticky', left: 0, zIndex: 2, width, flexShrink: 0, boxSizing: 'border-box', height: ROW_HEIGHT, lineHeight: `${ROW_HEIGHT}px`, padding: '0 12px', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', background: 'var(--surface-alt)', borderRight: '1px solid var(--border)', borderBottom: '1px solid var(--border-soft)' }}
    >
      {label} <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>· {count}</span>
    </div>
    <div role="cell" style={{ width: timeline, height: ROW_HEIGHT, boxSizing: 'border-box', background: 'color-mix(in srgb, var(--surface-alt) 60%, transparent)', borderBottom: '1px solid var(--border-soft)' }} />
  </div>
);
