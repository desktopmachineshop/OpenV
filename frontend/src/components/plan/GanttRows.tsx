// The rows of the Gantt (GanttChart.tsx): a task's cell in the outline, its
// bar on the timeline with the drag that moves or resizes it, and the arrows
// between a task and what it depends on.
import React, { useRef, useState } from 'react';
import type { PlanTask } from '../../api/client';
import { MONO, ellipsis } from '../ui';
import { BAR_COLORS, barTone } from './planModel';
import { Scale, addDays, spanLabel } from './ganttScale';

export const ROW_HEIGHT = 34;

const caret: React.CSSProperties = {
  width: 20,
  height: 20,
  padding: 0,
  border: 'none',
  background: 'none',
  color: 'var(--text-muted)',
  cursor: 'pointer',
  fontSize: 10,
  flexShrink: 0,
};

/** The small "open" mark on a row and a milestone: an arrow leaving a box. */
export const OpenIcon: React.FC = () => (
  <svg aria-hidden width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" style={{ flexShrink: 0 }}>
    <path d="M5 2.5H2.5v7h7V7M7 2.5h2.5V5M9.3 2.7L5.5 6.5" />
  </svg>
);

/** A task in the outline: its fold control, outline number, title, progress and the button that opens it. */
export const OutlineCell: React.FC<{
  task: PlanTask;
  width: number;
  selected: boolean;
  folded: boolean;
  /** Drawn as on the critical path. */
  critical?: boolean;
  onToggle: () => void;
  onSelect: () => void;
}> = ({ task, width, selected, folded, critical, onToggle, onSelect }) => {
  return (
    <div
      role="rowheader"
      style={{
        position: 'sticky',
        left: 0,
        zIndex: 2,
        width,
        flexShrink: 0,
        boxSizing: 'border-box',
        display: 'flex',
        alignItems: 'center',
        gap: 4,
        height: ROW_HEIGHT,
        padding: `0 8px 0 ${8 + task.depth * 14}px`,
        borderRight: '1px solid var(--border)',
        borderBottom: '1px solid var(--border-soft)',
        background: selected ? 'var(--tint-blue)' : 'var(--surface)',
        boxShadow: selected ? 'inset 3px 0 0 var(--accent)' : undefined,
      }}
    >
      {task.subtasks_total > 0 ? (
        <button type="button" onClick={onToggle} aria-expanded={!folded} aria-label={`${folded ? 'Unfold' : 'Fold'} ${task.title}`} style={caret}>
          {folded ? '▶' : '▼'}
        </button>
      ) : (
        <span style={{ width: 20, flexShrink: 0 }} />
      )}
      <span style={{ fontFamily: MONO, fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>{task.wbs}</span>
      <button
        type="button"
        onClick={onSelect}
        aria-current={selected ? 'true' : undefined}
        title={task.title}
        style={{
          ...ellipsis,
          flex: 1,
          padding: 0,
          border: 'none',
          background: 'none',
          font: 'inherit',
          fontWeight: task.subtasks_total > 0 ? 600 : 400,
          color: 'var(--text)',
          textAlign: 'left',
          cursor: 'pointer',
        }}
      >
        {task.title}
      </button>
      {critical && (
        <span title="On the critical path: any slip moves the plan's end" aria-label="Critical" role="img" style={{ width: 7, height: 7, borderRadius: '50%', background: 'var(--warning)', flexShrink: 0 }} />
      )}
      <span
        title={task.late ? 'Late' : task.conflicts.length > 0 ? 'Its dates break a relationship' : undefined}
        style={{ fontSize: 11, flexShrink: 0, color: task.late || task.conflicts.length > 0 ? 'var(--danger-strong)' : task.done ? 'var(--success-text)' : 'var(--text-muted)' }}
      >
        {task.conflicts.length > 0 && !task.late ? '⚠ ' : ''}
        {task.progress}%
      </span>
      <button
        type="button"
        onClick={onSelect}
        aria-label={`Open ${task.title}`}
        title="Open task"
        data-open-task={task.id}
        style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 22, height: 22, padding: 0, flexShrink: 0, border: '1px solid transparent', borderRadius: 4, background: 'none', color: 'var(--text-muted)', cursor: 'pointer' }}
      >
        <OpenIcon />
      </button>
    </div>
  );
};

type DragMode = 'move' | 'start' | 'end';

interface Drag {
  mode: DragMode;
  x: number;
  days: number;
  moved: boolean;
}

const handle = (side: 'left' | 'right'): React.CSSProperties => ({
  position: 'absolute',
  top: 0,
  bottom: 0,
  [side]: -3,
  width: 8,
  cursor: 'ew-resize',
  touchAction: 'none',
});

/**
 * A task's bar: the track of its span with its progress filled in. With
 * onReschedule, dragging the bar moves it and dragging an end resizes it, in
 * whole days; a press without a drag selects the task.
 */
export const TaskBar: React.FC<{
  task: PlanTask;
  scale: Scale;
  today: string;
  selected: boolean;
  /** Drawn as on the critical path: an amber ring. */
  critical?: boolean;
  onSelect: () => void;
  onReschedule?: (start: string, end: string) => void;
}> = ({ task, scale, today, selected, critical, onSelect, onReschedule }) => {
  const [drag, setDrag] = useState<Drag | null>(null);
  const live = useRef<Drag | null>(null);
  // A drag ends in a click on the bar, which must not also select the task.
  const dragged = useRef(false);
  if (!task.span_start || !task.span_end) return null;
  const summary = task.subtasks_total > 0;
  const draggable = Boolean(onReschedule) && !summary;

  let start = task.span_start;
  let end = task.span_end;
  if (drag) {
    if (drag.mode !== 'end') start = addDays(start, drag.days);
    if (drag.mode !== 'start') end = addDays(end, drag.days);
    if (start > end) [start, end] = drag.mode === 'start' ? [end, end] : [start, start];
  }
  const left = scale.x(start);
  const width = Math.max(scale.x(end) + scale.dayWidth - left, 6);
  const colors = BAR_COLORS[barTone(task)];

  const begin = (mode: DragMode) => (e: React.PointerEvent) => {
    if (!draggable || e.button !== 0) return;
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    dragged.current = false;
    live.current = { mode, x: e.clientX, days: 0, moved: false };
    setDrag(live.current);
  };
  const move = (e: React.PointerEvent) => {
    const d = live.current;
    if (!d) return;
    const days = Math.round((e.clientX - d.x) / scale.dayWidth);
    if (days !== d.days || (!d.moved && Math.abs(e.clientX - d.x) > 3)) {
      live.current = { ...d, days, moved: true };
      setDrag(live.current);
    }
  };
  const finish = () => {
    const d = live.current;
    // An end's pointerup bubbles to the bar: the second call has nothing to finish.
    if (!d) return;
    live.current = null;
    setDrag(null);
    dragged.current = d.moved;
    if (d.moved && d.days !== 0 && onReschedule) onReschedule(start, end);
  };
  const cancel = () => {
    live.current = null;
    setDrag(null);
  };
  const pointer = draggable ? { onPointerMove: move, onPointerUp: finish, onPointerCancel: cancel } : {};

  const label = `${task.title}: ${spanLabel(start, end, today)}, ${task.progress}% complete${task.late ? ', late' : ''}${task.critical ? ', critical' : ''}`;
  return (
    <>
      <button
        type="button"
        aria-label={label}
        title={label}
        data-task-bar={task.id}
        onClick={() => {
          if (!dragged.current) onSelect();
          dragged.current = false;
        }}
        onPointerDown={begin('move')}
        {...pointer}
        style={{
          position: 'absolute',
          left,
          width,
          top: summary ? 12 : 8,
          height: summary ? 10 : ROW_HEIGHT - 16,
          padding: 0,
          borderRadius: summary ? 2 : 4,
          border: `1px solid ${summary ? 'var(--text-secondary)' : colors.border}`,
          background: summary ? 'var(--neutral-soft)' : colors.track,
          boxShadow: critical ? '0 0 0 2px var(--warning)' : selected ? '0 0 0 2px var(--tint-blue-border)' : undefined,
          cursor: draggable ? (drag ? 'grabbing' : 'grab') : 'pointer',
          overflow: 'visible',
          touchAction: draggable ? 'none' : undefined,
        }}
      >
        <span
          style={{
            display: 'block',
            height: '100%',
            width: `${task.progress}%`,
            borderRadius: 'inherit',
            background: summary ? 'var(--text-secondary)' : colors.fill,
            opacity: summary ? 0.85 : 1,
          }}
        />
        {draggable && (
          <>
            <span aria-hidden onPointerDown={begin('start')} {...pointer} style={handle('left')} />
            <span aria-hidden onPointerDown={begin('end')} {...pointer} style={handle('right')} />
          </>
        )}
      </button>
      <span
        aria-hidden
        style={{ position: 'absolute', left: left + width + 6, top: 0, lineHeight: `${ROW_HEIGHT}px`, fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap', pointerEvents: 'none' }}
      >
        {drag ? spanLabel(start, end, today) : ''}
      </span>
    </>
  );
};

/**
 * The arrows between related tasks, drawn over the rows, each from the end
 * of the driving task its type names to the end it drives: finish to start
 * leaves the driver's right edge for the task's left edge, start to start
 * joins the two left edges, finish to finish the two right edges. A broken
 * relationship is dashed red.
 */
export const DependencyLines: React.FC<{ tasks: PlanTask[]; scale: Scale; showCritical?: boolean }> = ({ tasks, scale, showCritical }) => {
  const row = new Map(tasks.map((t, i) => [t.id, i]));
  const byId = new Map(tasks.map((t) => [t.id, t]));
  const paths: { key: string; d: string; conflict: boolean; critical: boolean }[] = [];
  for (const t of tasks) {
    for (const rel of t.relationships) {
      const on = byId.get(rel.task_id);
      if (!on || !on.span_start || !on.span_end || !t.span_start || !t.span_end) continue;
      const y1 = row.get(on.id)! * ROW_HEIGHT + ROW_HEIGHT / 2;
      const y2 = row.get(t.id)! * ROW_HEIGHT + ROW_HEIGHT / 2;
      const left = (task: PlanTask) => scale.x(task.span_start!);
      const right = (task: PlanTask) => Math.max(scale.x(task.span_end!) + scale.dayWidth, left(task) + 6);
      let d: string;
      if (rel.type === 'SS') {
        // Out of the driver's start, down a gutter left of both, into the task's start.
        const x1 = left(on);
        const x2 = left(t);
        const gutter = Math.min(x1, x2) - 8;
        d = `M${x1} ${y1} H${gutter} V${y2} H${x2 - 1}`;
      } else if (rel.type === 'FF') {
        // Out of the driver's end, down a gutter right of both, back into the task's end.
        const x1 = right(on);
        const x2 = right(t);
        const gutter = Math.max(x1, x2) + 8;
        d = `M${x1} ${y1} H${gutter} V${y2} H${x2 + 1}`;
      } else {
        const x1 = right(on);
        const x2 = left(t);
        d =
          x2 >= x1 + 14
            ? `M${x1} ${y1} H${x1 + 7} V${y2} H${x2 - 1}`
            : `M${x1} ${y1} H${x1 + 7} V${y1 + (y2 > y1 ? 1 : -1) * (ROW_HEIGHT / 2)} H${x2 - 9} V${y2} H${x2 - 1}`;
      }
      // The critical path runs along the arrows that join two critical tasks.
      paths.push({ key: `${rel.task_id}>${t.id}`, d, conflict: rel.conflict, critical: Boolean(showCritical && t.critical && on.critical) });
    }
  }
  if (paths.length === 0) return null;
  return (
    <svg
      aria-hidden
      width={scale.width}
      height={tasks.length * ROW_HEIGHT}
      style={{ position: 'absolute', left: 0, top: 0, pointerEvents: 'none', overflow: 'visible' }}
    >
      <defs>
        {(['ok', 'conflict', 'critical'] as const).map((k) => (
          <marker key={k} id={`gantt-arrow-${k}`} viewBox="0 0 6 6" refX="5" refY="3" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 0 L6 3 L0 6 z" fill={k === 'ok' ? 'var(--neutral)' : k === 'conflict' ? 'var(--danger)' : 'var(--warning)'} />
          </marker>
        ))}
      </defs>
      {paths.map((p) => (
        <path
          key={p.key}
          d={p.d}
          fill="none"
          stroke={p.conflict ? 'var(--danger)' : p.critical ? 'var(--warning)' : 'var(--neutral)'}
          strokeWidth={p.critical && !p.conflict ? 2 : 1.25}
          strokeDasharray={p.conflict ? '4 3' : undefined}
          markerEnd={`url(#gantt-arrow-${p.conflict ? 'conflict' : p.critical ? 'critical' : 'ok'})`}
        />
      ))}
    </svg>
  );
};
