// The rows of the Gantt (GanttChart.tsx): a task's cell in the outline, its
// bar on the timeline with the drag that moves or resizes it and the "+" at
// each end that a link is dragged from. The arrows are in GanttLinks.tsx.
import React, { useRef, useState } from 'react';
import type { PlanTask } from '../../api/client';
import { MONO, ellipsis } from '../ui';
import { ROW_HEIGHT } from './GanttLinks';
import { BAR_COLORS, BarEnd, barTone } from './planModel';
import { Scale, addDays, spanLabel } from './ganttScale';

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

/** The "+" beside an end of a bar: press it and drag to another bar to relate the two tasks. */
const linkHandle = (left: number): React.CSSProperties => ({
  position: 'absolute',
  left,
  top: (ROW_HEIGHT - 14) / 2,
  width: 14,
  height: 14,
  boxSizing: 'border-box',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  borderRadius: '50%',
  border: '1px solid var(--accent)',
  background: 'var(--surface)',
  color: 'var(--accent-text)',
  fontSize: 12,
  lineHeight: 1,
  cursor: 'crosshair',
  touchAction: 'none',
  userSelect: 'none',
  zIndex: 2,
});

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
  /** Set while the row is pointed at, where links can be drawn: the "+" at each end begins one. */
  onLinkStart?: (end: BarEnd, e: React.PointerEvent) => void;
}> = ({ task, scale, today, selected, critical, onSelect, onReschedule, onLinkStart }) => {
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

  const label = `${task.title}: ${spanLabel(start, end, today)}, ${task.progress}% complete${task.late ? ', late' : ''}${task.critical ? ', critical' : critical ? ', the critical path runs through its sub-tasks' : ''}`;
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
      {onLinkStart && !drag && (
        <>
          <span aria-hidden title="Drag to another bar to link from this task's start" data-link-handle={`${task.id}:start`} onPointerDown={(e) => onLinkStart('start', e)} style={linkHandle(left - 18)}>
            +
          </span>
          <span aria-hidden title="Drag to another bar to link from this task's finish" data-link-handle={`${task.id}:end`} onPointerDown={(e) => onLinkStart('end', e)} style={linkHandle(left + width + 4)}>
            +
          </span>
        </>
      )}
    </>
  );
};
