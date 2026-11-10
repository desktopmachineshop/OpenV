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

/** A task in the outline: its fold control, outline number, title and progress. */
export const OutlineCell: React.FC<{
  task: PlanTask;
  width: number;
  selected: boolean;
  folded: boolean;
  onToggle: () => void;
  onSelect: () => void;
}> = ({ task, width, selected, folded, onToggle, onSelect }) => {
  const tone = barTone(task);
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
      <span
        title={task.late ? 'Late' : task.conflicts.length > 0 ? 'Starts before a task it depends on ends' : undefined}
        style={{ fontSize: 11, flexShrink: 0, color: task.late || task.conflicts.length > 0 ? 'var(--danger-strong)' : BAR_COLORS[tone].fill === 'var(--success)' ? 'var(--success-text)' : 'var(--text-muted)' }}
      >
        {task.conflicts.length > 0 && !task.late ? '⚠ ' : ''}
        {task.progress}%
      </span>
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
  onSelect: () => void;
  onReschedule?: (start: string, end: string) => void;
}> = ({ task, scale, today, selected, onSelect, onReschedule }) => {
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

  const label = `${task.title}: ${spanLabel(start, end, today)}, ${task.progress}% complete${task.late ? ', late' : ''}`;
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
          boxShadow: selected ? '0 0 0 2px var(--tint-blue-border)' : undefined,
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
        {drag ? spanLabel(start, end, today) : task.todos_total > 0 ? `${task.todos_done}/${task.todos_total} to-dos` : ''}
      </span>
    </>
  );
};

/** The arrows from each task to the tasks that wait on it, drawn over the rows. */
export const DependencyLines: React.FC<{ tasks: PlanTask[]; scale: Scale }> = ({ tasks, scale }) => {
  const row = new Map(tasks.map((t, i) => [t.id, i]));
  const byId = new Map(tasks.map((t) => [t.id, t]));
  const paths: { key: string; d: string; conflict: boolean }[] = [];
  for (const t of tasks) {
    for (const dep of t.depends_on) {
      const on = byId.get(dep);
      if (!on || !on.span_end || !t.span_start) continue;
      const x1 = scale.x(on.span_end) + scale.dayWidth;
      const y1 = row.get(on.id)! * ROW_HEIGHT + ROW_HEIGHT / 2;
      const x2 = scale.x(t.span_start);
      const y2 = row.get(t.id)! * ROW_HEIGHT + ROW_HEIGHT / 2;
      const d =
        x2 >= x1 + 14
          ? `M${x1} ${y1} H${x1 + 7} V${y2} H${x2 - 1}`
          : `M${x1} ${y1} H${x1 + 7} V${y1 + (y2 > y1 ? 1 : -1) * (ROW_HEIGHT / 2)} H${x2 - 9} V${y2} H${x2 - 1}`;
      paths.push({ key: `${dep}>${t.id}`, d, conflict: t.conflicts.includes(dep) });
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
        {(['ok', 'conflict'] as const).map((k) => (
          <marker key={k} id={`gantt-arrow-${k}`} viewBox="0 0 6 6" refX="5" refY="3" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 0 L6 3 L0 6 z" fill={k === 'ok' ? 'var(--neutral)' : 'var(--danger)'} />
          </marker>
        ))}
      </defs>
      {paths.map((p) => (
        <path
          key={p.key}
          d={p.d}
          fill="none"
          stroke={p.conflict ? 'var(--danger)' : 'var(--neutral)'}
          strokeWidth={1.25}
          strokeDasharray={p.conflict ? '4 3' : undefined}
          markerEnd={`url(#gantt-arrow-${p.conflict ? 'conflict' : 'ok'})`}
        />
      ))}
    </svg>
  );
};
