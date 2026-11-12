// The timeline of a task with no dates yet (GanttChart.tsx): a click
// schedules it for the period of the zoom that holds the day clicked (a day,
// a week, a month, a quarter), and a drag schedules the range dragged across,
// drawn as it grows.
import React, { useRef, useState } from 'react';
import type { PlanTask } from '../../api/client';
import { ROW_HEIGHT } from './GanttLinks';
import { Scale, spanLabel, unitSpan } from './ganttScale';

interface Drag {
  from: string;
  to: string;
  x: number;
  moved: boolean;
}

const hint: React.CSSProperties = {
  position: 'absolute',
  top: 7,
  height: ROW_HEIGHT - 14,
  boxSizing: 'border-box',
  display: 'flex',
  alignItems: 'center',
  padding: '0 8px',
  borderRadius: 4,
  fontSize: 12,
  whiteSpace: 'nowrap',
  pointerEvents: 'none',
};

export const ScheduleTrack: React.FC<{
  task: PlanTask;
  scale: Scale;
  today: string;
  /** Absent where the reader may not schedule with a pointer: the row only opens the task. */
  onSchedule?: (start: string, end: string) => void;
  onSelect: () => void;
}> = ({ task, scale, today, onSchedule, onSelect }) => {
  const [hover, setHover] = useState<string | null>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const live = useRef<Drag | null>(null);

  // A task with sub-tasks takes its dates from them: there is nothing to place.
  if (task.subtasks_total > 0 || !onSchedule) {
    const folder = task.subtasks_total > 0;
    return (
      <button
        type="button"
        onClick={onSelect}
        aria-label={`${task.title}: ${folder ? 'its dates come from its sub-tasks, none of which has any yet' : 'not scheduled'}`}
        style={{ ...hint, left: scale.x(today), pointerEvents: 'auto', border: '1px dashed var(--neutral-mid)', background: 'var(--surface)', color: 'var(--text-muted)', cursor: 'pointer' }}
      >
        {folder ? 'Schedule its sub-tasks' : 'Not scheduled'}
      </button>
    );
  }

  const dayAt = (e: React.PointerEvent) => scale.dayAt(e.clientX - (e.currentTarget as HTMLElement).getBoundingClientRect().left);
  const span = drag?.moved
    ? drag.from <= drag.to
      ? { start: drag.from, end: drag.to }
      : { start: drag.to, end: drag.from }
    : hover
      ? unitSpan(scale.zoom, hover)
      : null;

  const down = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    // The press is a schedule, not a pan of the timeline.
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
    const day = dayAt(e);
    live.current = { from: day, to: day, x: e.clientX, moved: false };
    setDrag(live.current);
  };
  const move = (e: React.PointerEvent) => {
    const day = dayAt(e);
    const d = live.current;
    if (!d) {
      if (day !== hover) setHover(day);
      return;
    }
    if (day !== d.to || (!d.moved && Math.abs(e.clientX - d.x) > 3)) {
      live.current = { ...d, to: day, moved: true };
      setDrag(live.current);
    }
  };
  const up = () => {
    const d = live.current;
    if (!d) return;
    live.current = null;
    setDrag(null);
    setHover(null);
    const placed = d.moved ? (d.from <= d.to ? { start: d.from, end: d.to } : { start: d.to, end: d.from }) : unitSpan(scale.zoom, d.from);
    onSchedule(placed.start, placed.end);
  };
  const cancel = () => {
    live.current = null;
    setDrag(null);
  };

  const unit = scale.zoom.slice(0, -1);
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`Schedule ${task.title}: click a date for one ${unit}, or drag across a range`}
      data-schedule-track={task.id}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={cancel}
      onPointerLeave={() => !live.current && setHover(null)}
      onKeyDown={(e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        e.preventDefault();
        const placed = unitSpan(scale.zoom, today);
        onSchedule(placed.start, placed.end);
      }}
      style={{ position: 'absolute', inset: 0, cursor: 'cell', touchAction: 'none', userSelect: 'none' }}
    >
      {span ? (
        <>
          <span
            aria-hidden
            data-schedule-ghost
            style={{
              ...hint,
              left: scale.x(span.start),
              width: Math.max(scale.x(span.end) + scale.dayWidth - scale.x(span.start), 6),
              padding: 0,
              border: `1px dashed var(--accent)`,
              background: drag ? 'var(--tint-blue)' : 'color-mix(in srgb, var(--tint-blue) 55%, transparent)',
            }}
          />
          <span aria-hidden style={{ ...hint, left: scale.x(span.end) + scale.dayWidth + 6, padding: 0, color: 'var(--text-secondary)' }}>
            {spanLabel(span.start, span.end, today)}
          </span>
        </>
      ) : (
        <span aria-hidden style={{ ...hint, left: scale.x(today), border: '1px dashed var(--neutral-mid)', background: 'var(--surface)', color: 'var(--text-muted)' }}>
          Click a date, or drag a range
        </span>
      )}
    </div>
  );
};
