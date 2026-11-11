// The Gantt chart: the outline of tasks on the left and the timeline on the
// right in one scroller, so the header and the outline stay put while the
// bars scroll. Above the rows, a lane holds the milestones as diamonds, and
// between one milestone and the next a band fills with the later one's
// progress against its criteria. A line marks today.
import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { PlanMilestone, PlanTask, ProjectPlan } from '../../api/client';
import { ellipsis, eyebrowStyle, mutedStyle } from '../ui';
import { DependencyLines, LinkBand, OnLink, ROW_HEIGHT, useLinkDrag } from './GanttLinks';
import { OpenIcon, OutlineCell, TaskBar } from './GanttRows';
import { ScheduleTrack } from './GanttSchedule';
import { Scale, Tick, Zoom, buildScale, buildTicks, openingScroll, shortDate, todayScroll } from './ganttScale';
import { milestoneColor, visibleTasks } from './planModel';

const HEAD_ROW = 24;
const LANE_HEIGHT = 38;

const corner = (width: number, height: number): React.CSSProperties => ({
  position: 'sticky',
  left: 0,
  zIndex: 4,
  width,
  height,
  flexShrink: 0,
  boxSizing: 'border-box',
  display: 'flex',
  alignItems: 'center',
  padding: '0 12px',
  background: 'var(--surface-alt)',
  borderRight: '1px solid var(--border)',
  borderBottom: '1px solid var(--border)',
});

const TickRow: React.FC<{ ticks: Tick[]; width: number; strong?: boolean }> = ({ ticks, width, strong }) => (
  <div style={{ position: 'relative', width, height: HEAD_ROW }}>
    {ticks.map((t) => (
      <span
        key={t.key}
        style={{
          ...ellipsis,
          position: 'absolute',
          left: t.left,
          width: t.width,
          height: HEAD_ROW,
          boxSizing: 'border-box',
          padding: t.width > 22 ? '0 6px' : 0,
          textAlign: t.width > 22 ? 'left' : 'center',
          lineHeight: `${HEAD_ROW}px`,
          fontSize: strong ? 12 : 11,
          fontWeight: strong ? 600 : 400,
          color: strong ? 'var(--text-secondary)' : 'var(--text-muted)',
          borderLeft: '1px solid var(--border-soft)',
          background: t.weekend ? 'var(--surface-inset)' : undefined,
        }}
      >
        {t.width >= 14 ? t.label : ''}
      </span>
    ))}
  </div>
);

/** The milestones on the timeline: a diamond each, and the progress band that leads up to it. */
const MilestoneLane: React.FC<{
  milestones: PlanMilestone[];
  scale: Scale;
  today: string;
  selectedKey: string;
  /** Where the plan's first day falls, in pixels. */
  planStart: number;
  onSelect: (m: PlanMilestone) => void;
}> = ({ milestones, scale, today, selectedKey, planStart, onSelect }) => {
  const dated = milestones.filter((m) => m.date);
  const at = (m: PlanMilestone) => scale.x(m.date!) + scale.dayWidth / 2;
  return (
    <div style={{ position: 'relative', width: scale.width, height: LANE_HEIGHT, borderBottom: '1px solid var(--border)', background: 'var(--surface)' }}>
      {dated.map((m, i) => {
        const x = at(m);
        // The first band starts where the plan does, not at the chart's edge.
        const from = i === 0 ? Math.min(Math.max(planStart, 0), x) : at(dated[i - 1]);
        const next = i + 1 < dated.length ? at(dated[i + 1]) : scale.width;
        const color = milestoneColor(m);
        return (
          <React.Fragment key={m.key}>
            {x - from > 2 && (
              <span
                role="img"
                aria-label={`${m.name}: ${m.progress}% of its criteria met`}
                style={{ position: 'absolute', left: from, width: x - from, bottom: 3, height: 4, background: 'var(--neutral-soft)', overflow: 'hidden' }}
              >
                <span style={{ display: 'block', height: '100%', width: `${m.progress}%`, background: color }} />
              </span>
            )}
            <button
              type="button"
              onClick={() => onSelect(m)}
              aria-current={selectedKey === m.key ? 'true' : undefined}
              aria-label={`${m.kind === 'release' ? 'Release milestone' : 'Milestone'} ${m.name}, ${shortDate(m.date, today)}, ${m.progress}%${m.met ? ', met' : m.late ? ', late' : ''}`}
              data-milestone={m.key}
              style={{
                position: 'absolute',
                left: x - 9,
                top: 3,
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                height: 24,
                maxWidth: Math.max(next - x - 4, 24),
                overflow: 'hidden',
                padding: '0 6px 0 0',
                border: 'none',
                borderRadius: 4,
                background: selectedKey === m.key ? 'var(--tint-blue)' : 'transparent',
                font: 'inherit',
                cursor: 'pointer',
              }}
            >
              <span
                aria-hidden
                style={{
                  width: 12,
                  height: 12,
                  margin: '0 3px',
                  flexShrink: 0,
                  transform: 'rotate(45deg)',
                  borderRadius: 2,
                  background: m.kind === 'release' || m.met ? color : 'var(--surface)',
                  border: `2px solid ${color}`,
                  boxSizing: 'border-box',
                }}
              />
              <span style={{ ...ellipsis, fontSize: 12, fontWeight: 600, color: 'var(--text)' }}>{m.name}</span>
              <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{m.progress}%</span>
              <OpenIcon />
            </button>
          </React.Fragment>
        );
      })}
    </div>
  );
};

/** The vertical guides behind the bars: days off and period lines, each milestone, and today. */
const Guides: React.FC<{ scale: Scale; ticks: Tick[]; milestones: PlanMilestone[]; today: string; height: number }> = ({
  scale,
  ticks,
  milestones,
  today,
  height,
}) => (
  <div aria-hidden style={{ position: 'absolute', left: 0, top: 0, width: scale.width, height, pointerEvents: 'none' }}>
    {ticks.map((t) => (
      <span
        key={t.key}
        style={{ position: 'absolute', left: t.left, width: t.width, top: 0, bottom: 0, boxSizing: 'border-box', borderLeft: '1px solid var(--border-soft)', background: t.weekend ? 'var(--surface-inset)' : undefined, opacity: t.weekend ? 0.7 : 1 }}
      />
    ))}
    {milestones
      .filter((m) => m.date)
      .map((m) => (
        <span key={m.key} style={{ position: 'absolute', left: scale.x(m.date!) + scale.dayWidth / 2, top: 0, bottom: 0, borderLeft: `1px dashed ${milestoneColor(m)}`, opacity: 0.55 }} />
      ))}
    <span style={{ position: 'absolute', left: scale.x(today) + scale.dayWidth / 2, top: 0, bottom: 0, borderLeft: '2px solid var(--danger)', opacity: 0.7 }} />
  </div>
);

export interface GanttChartProps {
  plan: ProjectPlan;
  zoom: Zoom;
  /** The width of the outline column. */
  outlineWidth: number;
  selectedTaskId: string;
  selectedMilestoneKey: string;
  onSelectTask: (t: PlanTask) => void;
  onSelectMilestone: (m: PlanMilestone) => void;
  /** Set where the reader may edit with a pointer: bars drag, and an unscheduled row is clicked or dragged across to schedule it. */
  onReschedule?: (t: PlanTask, start: string, end: string) => void;
  /** Set where links can be drawn: a drag from the "+" at an end of a bar to another bar relates the two. */
  onLink?: OnLink;
  /** Marks the tasks with no float, and the arrows between them. */
  showCritical: boolean;
  /** Counts presses of Today: each one scrolls today back to a third of the way in. */
  todaySignal: number;
}

export const GanttChart: React.FC<GanttChartProps> = ({
  plan,
  zoom,
  outlineWidth,
  selectedTaskId,
  selectedMilestoneKey,
  onSelectTask,
  onSelectMilestone,
  onReschedule,
  onLink,
  showCritical,
  todaySignal,
}) => {
  const scroller = useRef<HTMLDivElement>(null);
  const [folded, setFolded] = useState<ReadonlySet<string>>(new Set());
  const [viewWidth, setViewWidth] = useState(0);
  const [pointed, setPointed] = useState('');
  const body = useRef<HTMLDivElement>(null);
  const { link, begin: beginLink } = useLinkDrag(body, onLink);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const measure = () => setViewWidth(el.clientWidth);
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const scale = useMemo(
    () => buildScale(zoom, plan.start, plan.end, plan.today, Math.max(viewWidth - outlineWidth, 0)),
    [zoom, plan.start, plan.end, plan.today, viewWidth, outlineWidth],
  );
  const ticks = useMemo(() => buildTicks(scale, plan.working_days), [scale, plan.working_days]);
  const rows = useMemo(() => visibleTasks(plan.tasks, folded), [plan.tasks, folded]);

  const timelineWidth = Math.max(viewWidth - outlineWidth, 0);

  // Where the timeline opens, and again whenever the zoom changes: on the
  // whole plan when it fits, else with today a third of the way in.
  useEffect(() => {
    const el = scroller.current;
    if (el && timelineWidth > 0) el.scrollLeft = openingScroll(scale, plan.start, plan.end, plan.today, timelineWidth);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [zoom, plan.project_id, timelineWidth > 0]);

  // An edit that moves the plan's first day moves the scale's: keep the
  // days the reader is looking at where they are.
  const lastScale = useRef(scale);
  useLayoutEffect(() => {
    const prev = lastScale.current;
    lastScale.current = scale;
    const el = scroller.current;
    if (el && prev.zoom === scale.zoom && prev.first !== scale.first) el.scrollLeft += (prev.first - scale.first) * scale.dayWidth;
  }, [scale]);

  useEffect(() => {
    const el = scroller.current;
    if (el && todaySignal > 0) el.scrollTo({ left: todayScroll(scale, plan.today, timelineWidth), behavior: 'smooth' });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [todaySignal]);

  // Dragging the empty timeline pans it, as a wheel or a trackpad scrolls it.
  const pan = useRef<{ x: number; y: number; left: number; top: number } | null>(null);
  const [panning, setPanning] = useState(false);
  const startPan = (e: React.PointerEvent) => {
    const el = scroller.current;
    if (!el || e.button !== 0 || e.pointerType !== 'mouse') return;
    if ((e.target as HTMLElement).closest('button, a, input, select, [role="rowheader"], [role="button"], [data-link-handle]')) return;
    pan.current = { x: e.clientX, y: e.clientY, left: el.scrollLeft, top: el.scrollTop };
    el.setPointerCapture(e.pointerId);
    setPanning(true);
  };
  const movePan = (e: React.PointerEvent) => {
    const el = scroller.current;
    if (!el || !pan.current) return;
    el.scrollLeft = pan.current.left - (e.clientX - pan.current.x);
    el.scrollTop = pan.current.top - (e.clientY - pan.current.y);
  };
  const endPan = () => {
    pan.current = null;
    setPanning(false);
  };

  const toggle = (id: string) =>
    setFolded((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const bodyHeight = rows.length * ROW_HEIGHT;
  return (
    <div
      ref={scroller}
      role="table"
      aria-label="Plan timeline"
      data-help-key="gantt.chart"
      tabIndex={0}
      onPointerDown={startPan}
      onPointerMove={movePan}
      onPointerUp={endPan}
      onPointerCancel={endPan}
      style={{
        position: 'relative',
        overflow: 'auto',
        maxHeight: 'calc(100dvh - 210px)',
        minHeight: 180,
        overscrollBehaviorX: 'contain',
        cursor: link ? 'crosshair' : panning ? 'grabbing' : 'grab',
        userSelect: panning || link ? 'none' : undefined,
      }}
    >
      <div style={{ width: outlineWidth + scale.width, minWidth: '100%' }}>
        <div role="row" style={{ position: 'sticky', top: 0, zIndex: 3, display: 'flex', background: 'var(--surface-alt)' }}>
          <div style={corner(outlineWidth, HEAD_ROW * 2)}>
            <span style={eyebrowStyle}>Task</span>
          </div>
          <div style={{ borderBottom: '1px solid var(--border)' }}>
            <TickRow ticks={ticks.upper} width={scale.width} strong />
            <TickRow ticks={ticks.lower} width={scale.width} />
          </div>
        </div>
        <div role="row" style={{ position: 'sticky', top: HEAD_ROW * 2 + 1, zIndex: 3, display: 'flex' }}>
          <div style={{ ...corner(outlineWidth, LANE_HEIGHT), background: 'var(--surface)' }}>
            <span style={eyebrowStyle}>Milestones</span>
          </div>
          <MilestoneLane
            milestones={plan.milestones}
            scale={scale}
            today={plan.today}
            selectedKey={selectedMilestoneKey}
            planStart={scale.x(plan.start || plan.today)}
            onSelect={onSelectMilestone}
          />
        </div>

        <div ref={body} style={{ position: 'relative' }}>
          <div style={{ position: 'absolute', left: outlineWidth, top: 0, width: scale.width, height: bodyHeight }}>
            <Guides scale={scale} ticks={ticks.lower} milestones={plan.milestones} today={plan.today} height={bodyHeight} />
          </div>
          {rows.map((t) => {
            // A folded task stands for its sub-tasks: it carries their critical mark.
            const critical = showCritical && (t.critical || (folded.has(t.id) && t.critical_subtasks));
            return (
              <div role="row" key={t.id} style={{ display: 'flex', height: ROW_HEIGHT }}>
                <OutlineCell
                  task={t}
                  width={outlineWidth}
                  selected={t.id === selectedTaskId}
                  folded={folded.has(t.id)}
                  onToggle={() => toggle(t.id)}
                  onSelect={() => onSelectTask(t)}
                  critical={critical}
                />
                <div
                  role="cell"
                  onPointerEnter={() => setPointed(t.id)}
                  onPointerLeave={() => setPointed((id) => (id === t.id ? '' : id))}
                  style={{ position: 'relative', width: scale.width, height: ROW_HEIGHT, boxSizing: 'border-box', borderBottom: '1px solid var(--border-soft)', background: t.id === selectedTaskId ? 'color-mix(in srgb, var(--tint-blue) 45%, transparent)' : undefined }}
                >
                  {t.span_start ? (
                    <TaskBar
                      task={t}
                      scale={scale}
                      today={plan.today}
                      selected={t.id === selectedTaskId}
                      critical={critical}
                      onSelect={() => onSelectTask(t)}
                      onReschedule={onReschedule ? (start, end) => onReschedule(t, start, end) : undefined}
                      onLinkStart={onLink && !link && (pointed === t.id || t.id === selectedTaskId) ? (end, e) => beginLink(t.id, end, e) : undefined}
                    />
                  ) : (
                    <ScheduleTrack
                      task={t}
                      scale={scale}
                      today={plan.today}
                      onSchedule={onReschedule ? (start, end) => onReschedule(t, start, end) : undefined}
                      onSelect={() => onSelectTask(t)}
                    />
                  )}
                </div>
              </div>
            );
          })}
          <div style={{ position: 'absolute', left: outlineWidth, top: 0, width: scale.width, height: bodyHeight, pointerEvents: 'none', zIndex: 1 }}>
            <DependencyLines tasks={plan.tasks} rows={rows} folded={folded} scale={scale} showCritical={showCritical} />
          </div>
          {link && <LinkBand link={link} />}
          {rows.length === 0 && (
            <p style={{ ...mutedStyle, margin: 0, padding: 16, position: 'sticky', left: 0, maxWidth: 520 }}>
              No tasks yet. Add one here, or on the Board: every card on the board is a task of the plan.
            </p>
          )}
        </div>
      </div>
    </div>
  );
};
