// Resource usage: for each person and agent with work on the plan, the
// days of their time each day, week, month or quarter asks for (the time
// scale chooses which), against the days they can give.
import React, { useMemo } from 'react';
import type { ProjectPlan } from '../../api/client';
import { mutedStyle } from '../ui';
import { addDays, shortDate, unitSpan, type Zoom } from './ganttScale';
import { worksOn } from './planModel';

/** The most columns the table draws: a plan longer than that at this scale is cut off there. */
const MAX_COLUMNS = 180;

const UNIT: Record<Zoom, string> = { days: 'day', weeks: 'week', months: 'month', quarters: 'quarter' };

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/** A column's heading: its first day for a day or a week, "Nov 2026" for a month, "Q4 2026" for a quarter. */
const columnLabel = (zoom: Zoom, start: string, today: string): string => {
  if (zoom === 'days' || zoom === 'weeks') return shortDate(start, today);
  const month = Number(start.slice(5, 7)) - 1;
  return zoom === 'months' ? `${MONTHS[month]} ${start.slice(0, 4)}` : `Q${month / 3 + 1} ${start.slice(0, 4)}`;
};

/** The columns of the table at a time scale: each a day, a week, a month or a quarter of the plan. */
export const usageColumns = (zoom: Zoom, start: string, end: string): { start: string; end: string }[] => {
  const out: { start: string; end: string }[] = [];
  for (let span = unitSpan(zoom, start); span.start <= end && out.length < MAX_COLUMNS; span = unitSpan(zoom, addDays(span.end, 1))) out.push(span);
  return out;
};

export const PlanUsage: React.FC<{ plan: ProjectPlan; zoom?: Zoom; showCritical?: boolean }> = ({ plan, zoom = 'weeks', showCritical = false }) => {
  const table = useMemo(() => {
    if (!plan.start || !plan.end) return null;
    const holidays = plan.holidays.map((h) => h.date);
    const columns = usageColumns(zoom, plan.start, plan.end).map((c) => {
      let working = 0;
      for (let d = c.start; d <= c.end; d = addDays(d, 1)) if (worksOn(plan.working_days, d, holidays)) working++;
      return { ...c, working };
    });
    const index = new Map(columns.map((c, i) => [c.start, i]));
    // critical: the columns in which the person has work on the critical path.
    const rows = plan.resources.map((r) => ({ res: r, days: columns.map(() => 0), critical: columns.map(() => false) }));
    const byKey = new Map(rows.map((row) => [`${row.res.type}:${row.res.id}`, row]));
    for (const t of plan.tasks) {
      if (t.subtasks_total > 0 || !t.span_start || !t.span_end) continue;
      for (const a of t.assignments) {
        const row = byKey.get(`${a.type}:${a.id}`);
        if (!row) continue;
        for (let d = t.span_start; d <= t.span_end; d = addDays(d, 1)) {
          if (!t.elapsed && !worksOn(plan.working_days, d, holidays)) continue;
          const i = index.get(unitSpan(zoom, d).start);
          if (i === undefined) continue;
          row.days[i] += a.units / 100;
          if (t.critical) row.critical[i] = true;
        }
      }
    }
    return { weeks: columns, rows };
  }, [plan, zoom]);

  if (!table || table.rows.length === 0) {
    return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Nobody has scheduled work on the plan yet. Assign people to tasks with dates, and their weeks are listed here.</p>;
  }
  const cell: React.CSSProperties = { padding: '4px 6px', borderBottom: '1px solid var(--border-soft)', borderRight: '1px solid var(--border-soft)', fontSize: 12, textAlign: 'right', whiteSpace: 'nowrap' };
  const round = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));
  return (
    <div data-plan-view="usage" data-plan-scale={zoom} data-help-key="gantt.views" style={{ flex: 1, minHeight: 180, overflow: 'auto' }}>
      <table style={{ borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th style={{ ...cell, position: 'sticky', left: 0, zIndex: 1, textAlign: 'left', background: 'var(--surface-alt)', minWidth: 170 }}>Who · days asked each {UNIT[zoom]}</th>
            {table.weeks.map((w) => (
              <th key={w.start} style={{ ...cell, fontWeight: 600, color: 'var(--text-muted)', background: 'var(--surface-alt)' }} title={w.start === w.end ? w.start : `${w.start} to ${w.end}`}>
                {columnLabel(zoom, w.start, plan.today)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {table.rows.map(({ res, days, critical }) => (
            <tr key={`${res.type}:${res.id}`} data-usage-row={res.id}>
              <th style={{ ...cell, position: 'sticky', left: 0, zIndex: 1, textAlign: 'left', fontWeight: 400, background: 'var(--surface)' }}>
                {res.name || res.id}
                <span style={mutedStyle}> · {res.capacity}%</span>
              </th>
              {days.map((n, i) => {
                const can = (table.weeks[i].working * res.capacity) / 100;
                const over = n > can + 1e-9;
                const crit = showCritical && critical[i];
                return (
                  <td
                    key={table.weeks[i].start}
                    data-critical={crit || undefined}
                    title={n ? `${round(n)} of ${round(can)} days${crit ? ', with work on the critical path' : ''}` : undefined}
                    style={{ ...cell, color: over ? 'var(--danger-strong)' : n ? 'var(--text)' : 'var(--text-muted)', background: over ? 'var(--tint-red)' : n ? 'color-mix(in srgb, var(--tint-blue) 60%, transparent)' : undefined, fontWeight: over ? 600 : 400, boxShadow: crit ? 'inset 0 -3px 0 var(--warning)' : undefined }}
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
