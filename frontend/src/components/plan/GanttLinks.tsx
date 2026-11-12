// The links of the Gantt (GanttChart.tsx): the arrows between related
// tasks, drawn over the rows, and the drag that makes a new one from the
// "+" at an end of a bar to an end of another bar.
import React, { useEffect, useRef, useState } from 'react';
import type { PlanMilestone, PlanTask } from '../../api/client';
import type { Scale } from './ganttScale';
import { BarEnd, RelationshipView, linkBetween, relationshipLabel, shownAs } from './planModel';

export const ROW_HEIGHT = 34;

const leftOf = (t: PlanTask, scale: Scale) => scale.x(t.span_start!);
const rightOf = (t: PlanTask, scale: Scale) => Math.max(scale.x(t.span_end!) + scale.dayWidth, leftOf(t, scale) + 6);

/**
 * Where a line leaves the driving task's finish: the end of its bar, or,
 * when its float is drawn after the bar, the tick that ends the float line,
 * so the two never run over each other.
 */
const outOf = (t: PlanTask, scale: Scale, float: boolean) =>
  float && !t.done && t.float_until && t.float_until > t.span_end! ? Math.max(scale.x(t.float_until) + scale.dayWidth, rightOf(t, scale)) : rightOf(t, scale);

/** The arrow between two rows that both show their own task. */
const direct = (type: string, on: PlanTask, t: PlanTask, y1: number, y2: number, scale: Scale, float: boolean): string => {
  if (type === 'SS') {
    // Out of the driver's start, down a gutter left of both, into the task's start.
    const x1 = leftOf(on, scale);
    const x2 = leftOf(t, scale);
    return `M${x1} ${y1} H${Math.min(x1, x2) - 8} V${y2} H${x2 - 1}`;
  }
  if (type === 'SF') {
    // Out of the driver's start, round to the task's end.
    const x1 = leftOf(on, scale);
    const x2 = rightOf(t, scale);
    return x2 <= x1 - 14
      ? `M${x1} ${y1} H${x1 - 7} V${y2} H${x2 + 1}`
      : `M${x1} ${y1} H${x1 - 7} V${y1 + (y2 > y1 ? 1 : -1) * (ROW_HEIGHT / 2)} H${x2 + 9} V${y2} H${x2 + 1}`;
  }
  if (type === 'FF') {
    // Out of the driver's end, down a gutter right of both, back into the task's end.
    const x1 = outOf(on, scale, float);
    const x2 = rightOf(t, scale);
    return `M${x1} ${y1} H${Math.max(x1, x2) + 8} V${y2} H${x2 + 1}`;
  }
  const x1 = outOf(on, scale, float);
  const x2 = leftOf(t, scale);
  return x2 >= x1 + 14
    ? `M${x1} ${y1} H${x1 + 7} V${y2} H${x2 - 1}`
    : `M${x1} ${y1} H${x1 + 7} V${y1 + (y2 > y1 ? 1 : -1) * (ROW_HEIGHT / 2)} H${x2 - 9} V${y2} H${x2 - 1}`;
};

/**
 * The arrow when an end is a sub-task hidden in a folded task: it leaves or
 * lands on the folded task's bar at the point along it where the sub-task's
 * own start or finish falls, from above or below.
 */
const viaFolded = (type: string, x1: number, y1: number, hidden1: boolean, x2: number, y2: number, hidden2: boolean): string => {
  const dir = y2 > y1 ? 1 : -1;
  const out = type === 'SS' || type === 'SF' ? -1 : 1;
  const into = type === 'FF' || type === 'SF' ? 1 : -1;
  const mid = y1 + (dir * ROW_HEIGHT) / 2;
  const leave = hidden1 ? `M${x1} ${y1 + dir * 6} V${mid}` : `M${x1} ${y1} H${x1 + out * 7} V${mid}`;
  const land = hidden2 ? ` H${x2} V${y2 - dir * 6}` : ` H${x2 + into * 9} V${y2} H${x2 + into}`;
  return leave + land;
};

/**
 * The arrows between related tasks, each from the end of the driving task
 * its type names to the end it drives: finish to start leaves the driver's
 * right edge for the task's left edge, start to start joins the two left
 * edges, finish to finish the two right edges, start to finish the driver's
 * left edge and the task's right edge. A link of a sub-task inside a
 * folded task is drawn to the folded task's bar; one between two sub-tasks
 * of the same folded task is not drawn. A broken relationship is dashed red.
 */
export const DependencyLines: React.FC<{
  /** Every task of the plan, and the rows the outline shows. */
  tasks: PlanTask[];
  rows: { id: string }[];
  folded: ReadonlySet<string>;
  scale: Scale;
  showCritical?: boolean;
  /** The milestones, to draw a line from each task one waits for to its date. */
  milestones?: PlanMilestone[];
}> = ({ tasks, rows, folded, scale, showCritical, milestones = [] }) => {
  const row = new Map(rows.map((t, i) => [t.id, i]));
  const byId = new Map(tasks.map((t) => [t.id, t]));
  const shown = shownAs(tasks, folded);
  const paths: { key: string; d: string; conflict: boolean; critical: boolean }[] = [];
  const dots: { key: string; x: number; y: number }[] = [];
  for (const t of tasks) {
    for (const rel of t.relationships) {
      const on = byId.get(rel.task_id);
      if (!on || !on.span_start || !on.span_end || !t.span_start || !t.span_end) continue;
      const r1 = row.get(shown.get(on.id) ?? '');
      const r2 = row.get(shown.get(t.id) ?? '');
      if (r1 === undefined || r2 === undefined || r1 === r2) continue;
      const y1 = r1 * ROW_HEIGHT + ROW_HEIGHT / 2;
      const y2 = r2 * ROW_HEIGHT + ROW_HEIGHT / 2;
      const hidden1 = shown.get(on.id) !== on.id;
      const hidden2 = shown.get(t.id) !== t.id;
      let d: string;
      if (!hidden1 && !hidden2) {
        d = direct(rel.type, on, t, y1, y2, scale, Boolean(showCritical));
      } else {
        const x1 = rel.type === 'SS' || rel.type === 'SF' ? leftOf(on, scale) : rightOf(on, scale);
        const x2 = rel.type === 'FF' || rel.type === 'SF' ? rightOf(t, scale) : leftOf(t, scale);
        d = viaFolded(rel.type, x1, y1, hidden1, x2, y2, hidden2);
        if (hidden1) dots.push({ key: `${on.id}>${t.id}:from`, x: x1, y: y1 });
        if (hidden2) dots.push({ key: `${on.id}>${t.id}:to`, x: x2, y: y2 });
      }
      // The critical path runs along the arrows that join two critical tasks.
      paths.push({ key: `${rel.task_id}>${t.id}`, d, conflict: rel.conflict, critical: Boolean(showCritical && t.critical && on.critical) });
    }
  }
  // A task a milestone waits for: a line along its row from its finish to
  // the milestone's guide, red and dashed where it finishes after it.
  for (const m of milestones) {
    if (!m.date) continue;
    const xm = scale.x(m.date) + scale.dayWidth / 2;
    for (const id of m.waits_for || []) {
      const t = byId.get(id);
      const r = row.get(shown.get(id) ?? '');
      if (!t || !t.span_end || r === undefined) continue;
      const y = r * ROW_HEIGHT + ROW_HEIGHT / 2;
      const x1 = shown.get(id) === id ? outOf(t, scale, Boolean(showCritical)) : scale.x(t.span_end) + scale.dayWidth;
      const late = t.span_end > m.date;
      if (Math.abs(xm - x1) < 4) continue;
      paths.push({ key: `${id}>milestone:${m.key}`, d: `M${x1} ${y} H${xm + (xm > x1 ? -1 : 1)}`, conflict: late, critical: false });
    }
  }
  if (paths.length === 0) return null;
  return (
    <svg aria-hidden width={scale.width} height={rows.length * ROW_HEIGHT} style={{ position: 'absolute', left: 0, top: 0, pointerEvents: 'none', overflow: 'visible' }}>
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
          data-link={p.key}
          d={p.d}
          fill="none"
          stroke={p.conflict ? 'var(--danger)' : p.critical ? 'var(--warning)' : 'var(--neutral)'}
          strokeWidth={p.critical && !p.conflict ? 2 : 1.25}
          strokeDasharray={p.conflict ? '4 3' : undefined}
          markerEnd={`url(#gantt-arrow-${p.conflict ? 'conflict' : p.critical ? 'critical' : 'ok'})`}
        />
      ))}
      {dots.map((c) => (
        <circle key={c.key} cx={c.x} cy={c.y} r={2.5} fill="var(--surface)" stroke="var(--neutral)" strokeWidth={1.25} />
      ))}
    </svg>
  );
};

/** A link being dragged out of a bar's end, in the chart body's coordinates. */
export interface LinkDrag {
  fromId: string;
  fromEnd: BarEnd;
  x0: number;
  y0: number;
  x: number;
  y: number;
  /** The bar end under the pointer, when it is another task's; or a milestone's diamond. */
  over: { id: string; end: BarEnd; milestone?: boolean } | null;
}

export type OnLink = (fromId: string, toId: string, view: RelationshipView) => void;
/** Makes a milestone wait for a task: a link dropped on its diamond. */
export type OnLinkMilestone = (taskId: string, milestoneKey: string) => void;

/**
 * The drag that makes a relationship on the chart: begin it from a bar's
 * "+", and releasing over another bar relates the two, by the ends joined.
 * Escape, or a release anywhere else, abandons it.
 */
export const useLinkDrag = (body: React.RefObject<HTMLElement | null>, onLink?: OnLink, onLinkMilestone?: OnLinkMilestone) => {
  const [link, setLink] = useState<LinkDrag | null>(null);
  const live = useRef<LinkDrag | null>(null);
  const active = link !== null;

  const at = (clientX: number, clientY: number) => {
    const box = body.current?.getBoundingClientRect();
    return { x: clientX - (box?.left ?? 0), y: clientY - (box?.top ?? 0) };
  };

  const begin = (fromId: string, fromEnd: BarEnd, e: React.PointerEvent) => {
    if (!onLink || e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();
    const box = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const from = at(box.left + box.width / 2, box.top + box.height / 2);
    live.current = { fromId, fromEnd, x0: from.x, y0: from.y, ...at(e.clientX, e.clientY), over: null };
    setLink(live.current);
  };

  useEffect(() => {
    if (!active) return;
    const move = (e: PointerEvent) => {
      const d = live.current;
      if (!d) return;
      const bar = (document.elementFromPoint?.(e.clientX, e.clientY) as HTMLElement | null)?.closest<HTMLElement>('[data-task-bar]');
      const id = bar?.dataset.taskBar;
      let over: LinkDrag['over'] = null;
      // A task with sub-tasks takes no link: its sub-tasks do.
      if (bar && id && id !== d.fromId && bar.dataset.taskSummary === undefined) {
        const box = bar.getBoundingClientRect();
        over = { id, end: e.clientX < box.left + box.width / 2 ? 'start' : 'end' };
      }
      // A milestone's diamond takes a link too: the milestone waits for the task.
      const diamond = onLinkMilestone ? (document.elementFromPoint?.(e.clientX, e.clientY) as HTMLElement | null)?.closest<HTMLElement>('[data-milestone]') : null;
      if (diamond?.dataset.milestone) over = { id: diamond.dataset.milestone, end: 'start', milestone: true };
      live.current = { ...d, ...at(e.clientX, e.clientY), over };
      setLink(live.current);
    };
    const stop = () => {
      live.current = null;
      setLink(null);
    };
    const up = (e: PointerEvent) => {
      move(e);
      const d = live.current;
      stop();
      if (d?.over?.milestone) onLinkMilestone?.(d.fromId, d.over.id);
      else if (d?.over && onLink) onLink(d.fromId, d.over.id, linkBetween(d.fromEnd, d.over.end));
    };
    const key = (e: KeyboardEvent) => e.key === 'Escape' && stop();
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', stop);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', stop);
      window.removeEventListener('keydown', key);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active]);

  return { link, begin };
};

/** The link being dragged: a line from the bar's end to the pointer, and what releasing here would make. */
export const LinkBand: React.FC<{ link: LinkDrag }> = ({ link }) => {
  const view = link.over && !link.over.milestone ? linkBetween(link.fromEnd, link.over.end) : null;
  const onto = view || link.over?.milestone;
  return (
    <>
      <svg aria-hidden style={{ position: 'absolute', left: 0, top: 0, width: '100%', height: '100%', overflow: 'visible', pointerEvents: 'none', zIndex: 5 }}>
        <line x1={link.x0} y1={link.y0} x2={link.x} y2={link.y} stroke="var(--accent)" strokeWidth={1.75} strokeDasharray={onto ? undefined : '5 4'} />
        <circle cx={link.x} cy={link.y} r={4} fill={onto ? 'var(--accent)' : 'var(--surface)'} stroke="var(--accent)" strokeWidth={1.5} />
      </svg>
      <span
        role="status"
        style={{ position: 'absolute', left: link.x + 12, top: link.y - 28, zIndex: 5, padding: '2px 8px', borderRadius: 4, border: '1px solid var(--border)', background: 'var(--surface)', color: view ? 'var(--text)' : 'var(--text-muted)', fontSize: 12, whiteSpace: 'nowrap', pointerEvents: 'none' }}
      >
        {link.over?.milestone ? 'The milestone waits for this task' : view ? relationshipLabel(view.type) : 'Drop on the start or the end of another bar'}
      </span>
    </>
  );
};
