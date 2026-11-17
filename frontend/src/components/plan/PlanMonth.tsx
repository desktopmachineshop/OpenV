// The plan on a calendar: each day lists the tasks that run on it and the
// milestones due on it; the days the plan does not work are shaded. The
// time scale chooses how much of the calendar is shown: a week, four weeks,
// a month or a quarter.
import React, { useEffect, useState } from 'react';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { buttonStyle, ellipsis } from '../ui';
import { addDays, isoWeek, isoWeekday, type Zoom } from './ganttScale';
import { BAR_COLORS, WEEKDAYS, barTone, worksOn } from './planModel';

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const SHORT = MONTHS.map((m) => m.slice(0, 3));

/** What a time scale shows: how tall a day is and how many tasks it lists before "+n more". */
const SCALE: Record<Zoom, { unit: string; minHeight: number; perDay: number }> = {
  days: { unit: 'week', minHeight: 260, perDay: Infinity },
  weeks: { unit: 'four weeks', minHeight: 110, perDay: 4 },
  months: { unit: 'month', minHeight: 96, perDay: 4 },
  quarters: { unit: 'quarter', minHeight: 60, perDay: 2 },
};

const monday = (day: string) => addDays(day, 1 - isoWeekday(day));
const monthStart = (day: string) => `${day.slice(0, 7)}-01`;
const addMonths = (day: string, n: number) => {
  const d = new Date(`${monthStart(day)}T00:00:00Z`);
  d.setUTCMonth(d.getUTCMonth() + n);
  return d.toISOString().slice(0, 10);
};
const quarterStart = (day: string) => addMonths(day, -((Number(day.slice(5, 7)) - 1) % 3));
const dayLabel = (day: string) => `${Number(day.slice(8))} ${SHORT[Number(day.slice(5, 7)) - 1]}`;

/** One block of whole weeks, with the month its days belong to when it shows a month. */
interface Block {
  key: string;
  title?: string;
  days: string[];
  month?: string;
}

/** The weeks of a month: from the Monday on or before its first day, five rows or six. */
const monthBlock = (first: string, title?: string): Block => {
  const start = monday(first);
  const days = Array.from({ length: 42 }, (_, i) => addDays(start, i));
  const weeks = days[35].slice(0, 7) === first.slice(0, 7) ? 6 : 5;
  return { key: first, title, days: days.slice(0, weeks * 7), month: first.slice(0, 7) };
};

/** What the calendar shows for a time scale around an anchor day: its heading, its blocks, and the anchor one step either way. */
export const calendarRange = (zoom: Zoom, anchor: string): { heading: string; blocks: Block[]; step: (n: number) => string } => {
  if (zoom === 'days' || zoom === 'weeks') {
    const length = zoom === 'days' ? 7 : 28;
    const start = monday(anchor);
    const days = Array.from({ length }, (_, i) => addDays(start, i));
    const end = days[length - 1];
    return { heading: `${dayLabel(start)} – ${dayLabel(end)} ${end.slice(0, 4)}`, blocks: [{ key: start, days }], step: (n) => addDays(start, n * length) };
  }
  if (zoom === 'months') {
    const first = monthStart(anchor);
    return { heading: `${MONTHS[Number(first.slice(5, 7)) - 1]} ${first.slice(0, 4)}`, blocks: [monthBlock(first)], step: (n) => addMonths(first, n) };
  }
  const first = quarterStart(anchor);
  const months = [0, 1, 2].map((i) => addMonths(first, i));
  return {
    heading: `Q${(Number(first.slice(5, 7)) - 1) / 3 + 1} ${first.slice(0, 4)}`,
    blocks: months.map((m) => monthBlock(m, MONTHS[Number(m.slice(5, 7)) - 1])),
    step: (n) => addMonths(first, n * 3),
  };
};

export const PlanMonth: React.FC<{
  plan: ProjectPlan;
  selectedTaskId: string;
  onSelectTask: (t: PlanTask) => void;
  /** The time scale: a week, four weeks, a month or a quarter of the calendar. */
  zoom?: Zoom;
  /** Mark the tasks on the critical path and list them first in a day. */
  showCritical?: boolean;
  /** Changes when Today is pressed: the calendar returns to today. */
  todaySignal?: number;
}> = ({ plan, selectedTaskId, onSelectTask, zoom = 'months', showCritical = true, todaySignal = 0 }) => {
  // The day the calendar is opened around: today to begin with.
  const [anchor, setAnchor] = useState(plan.today);
  useEffect(() => {
    if (todaySignal) setAnchor(plan.today);
  }, [todaySignal, plan.today]);
  const range = calendarRange(zoom, anchor);
  const scale = SCALE[zoom];
  const holidays = plan.holidays.map((h) => h.date);
  const leaves = plan.tasks.filter((t) => t.subtasks_total === 0 && t.span_start && t.span_end);
  return (
    <div data-plan-view="calendar" data-calendar-scale={zoom} data-help-key="gantt.views" style={{ flex: 1, minHeight: 180, display: 'flex', flexDirection: 'column', overflow: 'auto' }}>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', padding: '6px 12px' }}>
        <button type="button" aria-label={`Previous ${scale.unit}`} onClick={() => setAnchor(range.step(-1))} style={buttonStyle('secondary', { compact: true })}>
          ‹
        </button>
        <b style={{ minWidth: 150, textAlign: 'center', fontSize: 13 }}>{range.heading}</b>
        <button type="button" aria-label={`Next ${scale.unit}`} onClick={() => setAnchor(range.step(1))} style={buttonStyle('secondary', { compact: true })}>
          ›
        </button>
        <button type="button" onClick={() => setAnchor(plan.today)} style={buttonStyle('secondary', { compact: true })}>
          This {scale.unit}
        </button>
      </div>
      {range.blocks.map((block) => (
        <div key={block.key} role="grid" aria-label={block.title || range.heading} style={{ display: 'grid', gridTemplateColumns: 'repeat(7, minmax(110px, 1fr))', borderTop: '1px solid var(--border-soft)', minWidth: 770 }}>
          {block.title && <div style={{ gridColumn: '1 / -1', padding: '6px 8px 2px', fontSize: 13, fontWeight: 600 }}>{block.title}</div>}
          {WEEKDAYS.map((w) => (
            <div key={w.day} role="columnheader" style={{ padding: '4px 6px', fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', borderBottom: '1px solid var(--border-soft)' }}>
              {w.name}
            </div>
          ))}
          {block.days.map((day) => {
            const running = leaves.filter((t) => t.span_start! <= day && day <= t.span_end!);
            // The critical path first, so "+n more" never hides it.
            const on = showCritical ? [...running.filter((t) => t.critical), ...running.filter((t) => !t.critical)] : running;
            const due = plan.milestones.filter((m) => m.date === day);
            const off = !worksOn(plan.working_days, day, holidays);
            const outside = block.month !== undefined && day.slice(0, 7) !== block.month;
            return (
              <div
                key={day}
                role="gridcell"
                data-day={day}
                style={{ minHeight: scale.minHeight, padding: 4, boxSizing: 'border-box', borderRight: '1px solid var(--border-soft)', borderBottom: '1px solid var(--border-soft)', background: off ? 'var(--surface-inset)' : undefined, opacity: outside ? 0.55 : 1, display: 'flex', flexDirection: 'column', gap: 2 }}
              >
                <span style={{ fontSize: 12, fontWeight: day === plan.today ? 700 : 400, color: day === plan.today ? 'var(--danger-strong)' : 'var(--text-muted)' }}>
                  {block.month === undefined && Number(day.slice(8)) === 1 ? dayLabel(day) : Number(day.slice(8))}
                  {/* A week's Monday carries its week number, where weeks are what is shown. */}
                  {block.month === undefined && isoWeekday(day) === 1 && <span data-week={isoWeek(day)} style={{ fontWeight: 400, color: 'var(--text-muted)' }}> (W{isoWeek(day)})</span>}
                </span>
                {due.map((m) => (
                  <span key={m.key} title={m.name} style={{ ...ellipsis, fontSize: 12, color: 'var(--text-secondary)' }}>
                    ◆ {m.name}
                  </span>
                ))}
                {on.slice(0, scale.perDay).map((t) => {
                  const c = BAR_COLORS[barTone(t)];
                  const critical = showCritical && t.critical;
                  return (
                    <button
                      key={t.id}
                      type="button"
                      data-critical={critical || undefined}
                      onClick={() => onSelectTask(t)}
                      title={critical ? `${t.title}, on the critical path` : t.title}
                      style={{ ...ellipsis, padding: '1px 4px', fontSize: 12, textAlign: 'left', cursor: 'pointer', borderRadius: 3, border: critical ? '2px solid var(--warning)' : `1px solid ${c.border}`, background: c.track, color: 'var(--text)', boxShadow: t.id === selectedTaskId ? '0 0 0 2px var(--tint-blue-border)' : undefined }}
                    >
                      {t.title}
                    </button>
                  );
                })}
                {on.length > scale.perDay && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>+{on.length - scale.perDay} more</span>}
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
};
