// The plan on a month's calendar: each day lists the tasks that run on it
// and the milestones due on it; the days the plan does not work are shaded.
import React, { useState } from 'react';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { buttonStyle, ellipsis } from '../ui';
import { addDays, isoWeekday } from './ganttScale';
import { BAR_COLORS, WEEKDAYS, barTone, worksOn } from './planModel';

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const PER_DAY = 4;

export const PlanMonth: React.FC<{ plan: ProjectPlan; selectedTaskId: string; onSelectTask: (t: PlanTask) => void }> = ({ plan, selectedTaskId, onSelectTask }) => {
  // The month shown, as its first day: the one today is in to begin with.
  const [month, setMonth] = useState(`${plan.today.slice(0, 7)}-01`);
  const step = (n: number) => {
    const d = new Date(`${month}T00:00:00Z`);
    d.setUTCMonth(d.getUTCMonth() + n);
    setMonth(d.toISOString().slice(0, 10));
  };
  const first = addDays(month, 1 - isoWeekday(month));
  const days = Array.from({ length: 42 }, (_, i) => addDays(first, i));
  const weeks = days[35].slice(0, 7) === month.slice(0, 7) ? 6 : 5;
  const holidays = plan.holidays.map((h) => h.date);
  const leaves = plan.tasks.filter((t) => t.subtasks_total === 0 && t.span_start && t.span_end);
  return (
    <div data-plan-view="calendar" data-help-key="gantt.views" style={{ flex: 1, minHeight: 180, display: 'flex', flexDirection: 'column', overflow: 'auto' }}>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', padding: '6px 12px' }}>
        <button type="button" aria-label="Previous month" onClick={() => step(-1)} style={buttonStyle('secondary', { compact: true })}>
          ‹
        </button>
        <b style={{ minWidth: 130, textAlign: 'center', fontSize: 13 }}>
          {MONTHS[Number(month.slice(5, 7)) - 1]} {month.slice(0, 4)}
        </b>
        <button type="button" aria-label="Next month" onClick={() => step(1)} style={buttonStyle('secondary', { compact: true })}>
          ›
        </button>
        <button type="button" onClick={() => setMonth(`${plan.today.slice(0, 7)}-01`)} style={buttonStyle('secondary', { compact: true })}>
          This month
        </button>
      </div>
      <div role="grid" aria-label="Month" style={{ display: 'grid', gridTemplateColumns: 'repeat(7, minmax(110px, 1fr))', borderTop: '1px solid var(--border-soft)', minWidth: 770 }}>
        {WEEKDAYS.map((w) => (
          <div key={w.day} role="columnheader" style={{ padding: '4px 6px', fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', borderBottom: '1px solid var(--border-soft)' }}>
            {w.name}
          </div>
        ))}
        {days.slice(0, weeks * 7).map((day) => {
          const on = leaves.filter((t) => t.span_start! <= day && day <= t.span_end!);
          const due = plan.milestones.filter((m) => m.date === day);
          const off = !worksOn(plan.working_days, day, holidays);
          const outside = day.slice(0, 7) !== month.slice(0, 7);
          return (
            <div
              key={day}
              role="gridcell"
              data-day={day}
              style={{ minHeight: 96, padding: 4, boxSizing: 'border-box', borderRight: '1px solid var(--border-soft)', borderBottom: '1px solid var(--border-soft)', background: off ? 'var(--surface-inset)' : undefined, opacity: outside ? 0.55 : 1, display: 'flex', flexDirection: 'column', gap: 2 }}
            >
              <span style={{ fontSize: 12, fontWeight: day === plan.today ? 700 : 400, color: day === plan.today ? 'var(--danger-strong)' : 'var(--text-muted)' }}>{Number(day.slice(8))}</span>
              {due.map((m) => (
                <span key={m.key} title={m.name} style={{ ...ellipsis, fontSize: 12, color: 'var(--text-secondary)' }}>
                  ◆ {m.name}
                </span>
              ))}
              {on.slice(0, PER_DAY).map((t) => {
                const c = BAR_COLORS[barTone(t)];
                return (
                  <button
                    key={t.id}
                    type="button"
                    onClick={() => onSelectTask(t)}
                    title={t.title}
                    style={{ ...ellipsis, padding: '1px 4px', fontSize: 12, textAlign: 'left', cursor: 'pointer', borderRadius: 3, border: `1px solid ${t.critical ? 'var(--warning)' : c.border}`, background: c.track, color: 'var(--text)', boxShadow: t.id === selectedTaskId ? '0 0 0 2px var(--tint-blue-border)' : undefined }}
                  >
                    {t.title}
                  </button>
                );
              })}
              {on.length > PER_DAY && <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>+{on.length - PER_DAY} more</span>}
            </div>
          );
        })}
      </div>
    </div>
  );
};
