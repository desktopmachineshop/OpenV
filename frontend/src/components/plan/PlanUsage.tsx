// Resource usage: for each person and agent with work on the plan, the
// days of their time each week asks for, against the days they can give.
import React, { useMemo } from 'react';
import type { ProjectPlan } from '../../api/client';
import { mutedStyle } from '../ui';
import { addDays, isoWeekday, shortDate } from './ganttScale';
import { worksOn } from './planModel';

const MAX_WEEKS = 78;

export const PlanUsage: React.FC<{ plan: ProjectPlan }> = ({ plan }) => {
  const table = useMemo(() => {
    if (!plan.start || !plan.end) return null;
    const holidays = plan.holidays.map((h) => h.date);
    const weeks: { start: string; working: number }[] = [];
    for (let w = addDays(plan.start, 1 - isoWeekday(plan.start)); w <= plan.end && weeks.length < MAX_WEEKS; w = addDays(w, 7)) {
      let working = 0;
      for (let i = 0; i < 7; i++) if (worksOn(plan.working_days, addDays(w, i), holidays)) working++;
      weeks.push({ start: w, working });
    }
    const index = new Map(weeks.map((w, i) => [w.start, i]));
    const rows = plan.resources.map((r) => ({ res: r, days: weeks.map(() => 0) }));
    const byKey = new Map(rows.map((row) => [`${row.res.type}:${row.res.id}`, row]));
    for (const t of plan.tasks) {
      if (t.subtasks_total > 0 || !t.span_start || !t.span_end) continue;
      for (const a of t.assignments) {
        const row = byKey.get(`${a.type}:${a.id}`);
        if (!row) continue;
        for (let d = t.span_start; d <= t.span_end; d = addDays(d, 1)) {
          if (!t.elapsed && !worksOn(plan.working_days, d, holidays)) continue;
          const i = index.get(addDays(d, 1 - isoWeekday(d)));
          if (i !== undefined) row.days[i] += a.units / 100;
        }
      }
    }
    return { weeks, rows };
  }, [plan]);

  if (!table || table.rows.length === 0) {
    return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Nobody has scheduled work on the plan yet. Assign people to tasks with dates, and their weeks are listed here.</p>;
  }
  const cell: React.CSSProperties = { padding: '4px 6px', borderBottom: '1px solid var(--border-soft)', borderRight: '1px solid var(--border-soft)', fontSize: 12, textAlign: 'right', whiteSpace: 'nowrap' };
  const round = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));
  return (
    <div data-plan-view="usage" data-help-key="gantt.views" style={{ flex: 1, minHeight: 180, overflow: 'auto' }}>
      <table style={{ borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th style={{ ...cell, position: 'sticky', left: 0, zIndex: 1, textAlign: 'left', background: 'var(--surface-alt)', minWidth: 170 }}>Who · days asked each week</th>
            {table.weeks.map((w) => (
              <th key={w.start} style={{ ...cell, fontWeight: 600, color: 'var(--text-muted)', background: 'var(--surface-alt)' }} title={`Week of ${w.start}`}>
                {shortDate(w.start, plan.today)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {table.rows.map(({ res, days }) => (
            <tr key={`${res.type}:${res.id}`} data-usage-row={res.id}>
              <th style={{ ...cell, position: 'sticky', left: 0, zIndex: 1, textAlign: 'left', fontWeight: 400, background: 'var(--surface)' }}>
                {res.name || res.id}
                <span style={mutedStyle}> · {res.capacity}%</span>
              </th>
              {days.map((n, i) => {
                const can = (table.weeks[i].working * res.capacity) / 100;
                const over = n > can + 1e-9;
                return (
                  <td
                    key={table.weeks[i].start}
                    title={n ? `${round(n)} of ${round(can)} days` : undefined}
                    style={{ ...cell, color: over ? 'var(--danger-strong)' : n ? 'var(--text)' : 'var(--text-muted)', background: over ? 'var(--tint-red)' : n ? 'color-mix(in srgb, var(--tint-blue) 60%, transparent)' : undefined, fontWeight: over ? 600 : 400 }}
                  >
                    {n ? round(n) : ''}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};
