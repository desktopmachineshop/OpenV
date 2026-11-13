// Two charts on the Gantt's time scale: the timeline, a project's top-level
// tasks and milestones on one band; and the master plan, every project of
// the workspace on a line of its own with the links between their tasks.
// Both are as wide as the scale makes the plan and scroll sideways, so a
// long plan stays readable.
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { planAPI, type MasterLevels, type MasterPlan, type PlanTask, type Project, type ProjectPlan } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ellipsis, eyebrowStyle, mutedStyle } from '../ui';
import { buildScale, buildTicks, openingScroll, shortDate, spanLabel, toDay, todayScroll, type Scale, type Tick, type Zoom } from './ganttScale';
import { useViewport } from '../../hooks/useViewport';
import { projectRows } from '../ProjectListTree';
import { barTone, relationshipLabel } from './planModel';
import { PlanBar } from './PlanBar';

export interface Bar {
  id: string;
  title: string;
  start: string | null;
  end: string | null;
  progress: number;
  done: boolean;
  late: boolean;
  critical: boolean;
  /** How many tasks sit above this one, where a band draws more than the top level. */
  depth?: number;
  /** The bar spans sub-tasks the band also draws: it is drawn as the Gantt draws a parent. */
  summary?: boolean;
}

/** Bars packed onto as few lanes as will hold them without overlapping. */
const pack = (bars: Bar[]): Bar[][] => {
  const out: Bar[][] = [];
  for (const b of bars.filter((x) => x.start && x.end).sort((x, y) => x.start!.localeCompare(y.start!))) {
    const lane = out.find((l) => l[l.length - 1].end! < b.start!);
    if (lane) lane.push(b);
    else out.push([b]);
  }
  return out;
};

/**
 * The lanes of a band: the critical path first, on the top row (more rows
 * only where critical bars overlap), then everything else beneath it; and
 * where the band draws sub-tasks, each level beneath the one above.
 */
export const lanes = (bars: Bar[]): Bar[][] => {
  // A level at a time, so a parent's bar sits over its sub-tasks' rows.
  const depths = Array.from(new Set(bars.map((b) => b.depth ?? 0))).sort((x, y) => x - y);
  return depths.flatMap((d) => {
    const level = bars.filter((b) => (b.depth ?? 0) === d);
    return [...pack(level.filter((b) => b.critical)), ...pack(level.filter((b) => !b.critical))];
  });
};

const Band: React.FC<{
  bars: Bar[];
  milestones: { key: string; name: string; date: string | null; met: boolean; late: boolean }[];
  scale: Scale;
  today: string;
  /** Mark the critical bars (the Critical path button); they sit on the top row either way. */
  showCritical: boolean;
  onBar?: (id: string) => void;
}> = ({ bars, milestones, scale, today, showCritical, onBar }) => {
  const rows = lanes(bars);
  return (
    <div style={{ position: 'relative', width: scale.width, height: 26 + Math.max(rows.length, 1) * 24 }}>
      <span aria-hidden style={{ position: 'absolute', left: scale.x(today), top: 0, bottom: 0, borderLeft: '2px solid var(--danger)', opacity: 0.6 }} />
      {milestones
        .filter((m) => m.date)
        .map((m) => (
          <span key={m.key} title={`${m.name}, ${shortDate(m.date, today)}`} style={{ ...ellipsis, position: 'absolute', left: scale.x(m.date!), top: 0, maxWidth: 160, fontSize: 12, color: m.met ? 'var(--success-text)' : m.late ? 'var(--danger-strong)' : 'var(--text-secondary)', transform: 'translateX(-5px)' }}>
            ◆ {m.name}
          </span>
        ))}
      {rows.map((row, i) =>
        row.map((b) => (
          <PlanBar
            key={b.id}
            left={scale.x(b.start!)}
            width={Math.max((toDay(b.end!) - toDay(b.start!) + 1) * scale.dayWidth, 6)}
            top={24 + i * 24}
            height={20}
            tone={barTone({ done: b.done, late: b.late, progress: b.progress, column: '' })}
            progress={b.progress}
            summary={b.summary}
            critical={showCritical && b.critical}
            label={`${b.title}: ${spanLabel(b.start, b.end, today)}, ${b.progress}%${showCritical && b.critical ? ', on the critical path' : ''}`}
            text={b.title}
            cursor={onBar ? 'pointer' : 'default'}
            button={{ 'data-critical': (showCritical && b.critical) || undefined, 'data-summary': b.summary || undefined, disabled: !onBar, onClick: () => onBar?.(b.id) }}
          />
        )),
      )}
    </div>
  );
};

const tickRow: React.CSSProperties = { position: 'relative', height: 20, borderBottom: '1px solid var(--border-soft)' };

/** The two rows of dates over a band, as the Gantt's header draws them. */
const Ticks: React.FC<{ scale: Scale }> = ({ scale }) => {
  const { upper, lower } = buildTicks(scale);
  const row = (ticks: Tick[], strong: boolean) => (
    <div style={tickRow}>
      {ticks.map((t) => (
        <span key={t.key} style={{ ...ellipsis, position: 'absolute', left: t.left, width: t.width, boxSizing: 'border-box', padding: '0 4px', fontSize: 12, lineHeight: '20px', fontWeight: strong ? 600 : 400, color: strong ? 'var(--text)' : 'var(--text-muted)', borderLeft: '1px solid var(--border-soft)' }}>
          {t.label}
        </span>
      ))}
    </div>
  );
  return (
    <div data-plan-ticks style={{ width: scale.width, background: 'var(--surface-alt)' }}>
      {row(upper, true)}
      {row(lower, false)}
    </div>
  );
};

/**
 * A band's scroller: it opens on the whole span when that fits and around
 * today otherwise, as the Gantt does, and returns to today when Today is
 * pressed. `inset` is the width a sticky column takes from the view.
 */
const useScroller = (scale: Scale, start: string | null, end: string | null, today: string, todaySignal: number, inset = 0) => {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (el) el.scrollLeft = openingScroll(scale, start, end, today, el.clientWidth - inset);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scale.zoom, scale.first, start, end]);
  useEffect(() => {
    const el = ref.current;
    if (el && todaySignal > 0) el.scrollLeft = todayScroll(scale, today, el.clientWidth - inset);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [todaySignal]);
  return ref;
};

/** The room a band has, near enough: the scale reaches past the plan by it, so the band always fills the page. */
const viewWidth = () => (typeof window === 'undefined' ? 0 : window.innerWidth);

interface LinesProps {
  /** The time scale: how wide a day is drawn. The band scrolls sideways when the plan is wider than the page. */
  zoom?: Zoom;
  showCritical?: boolean;
  /** Changes when Today is pressed: the band scrolls to today. */
  todaySignal?: number;
}

/** The project's top-level tasks and milestones on one band. */
export const PlanTimeline: React.FC<{ plan: ProjectPlan; onSelectTask: (t: PlanTask) => void } & LinesProps> = ({ plan, onSelectTask, zoom = 'weeks', showCritical = true, todaySignal = 0 }) => {
  const scale = useMemo(() => buildScale(zoom, plan.start, plan.end, plan.today, viewWidth()), [zoom, plan.start, plan.end, plan.today]);
  const scroller = useScroller(scale, plan.start, plan.end, plan.today, todaySignal);
  const top = plan.tasks.filter((t) => t.depth === 0);
  const bars = top.map((t) => ({ id: t.id, title: t.title, start: t.span_start, end: t.span_end, progress: t.progress, done: t.done, late: t.late, critical: t.critical || t.critical_subtasks }));
  if (!plan.start) return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Nothing is scheduled yet.</p>;
  return (
    <div ref={scroller} data-plan-view="timeline" data-plan-scale={zoom} data-pan-surface="" data-help-key="gantt.views" style={{ flex: 1, minHeight: 120, overflow: 'auto' }}>
      <div style={{ position: 'sticky', top: 0, zIndex: 1, width: scale.width }}>
        <Ticks scale={scale} />
      </div>
      <div style={{ padding: '8px 0 12px', width: scale.width }}>
        <Band bars={bars} milestones={plan.milestones} scale={scale} today={plan.today} showCritical={showCritical} onBar={(id) => onSelectTask(top.find((t) => t.id === id)!)} />
      </div>
    </div>
  );
};

const MASTER_LEVELS: MasterLevels[] = ['1', '2', '3', '4', 'all'];
const toggled = (set: ReadonlySet<string>, id: string): ReadonlySet<string> => {
  const next = new Set(set);
  if (!next.delete(id)) next.add(id);
  return next;
};

const LEVELS_KEY = 'openv-plan-master-levels';

/** The level All projects opens on: the one last chosen in this browser, or Level 1. */
const rememberedLevels = (): MasterLevels => {
  try {
    const stored = localStorage.getItem(LEVELS_KEY) as MasterLevels | null;
    return stored && MASTER_LEVELS.includes(stored) ? stored : '1';
  } catch {
    return '1';
  }
};

const rememberLevels = (levels: MasterLevels): void => {
  try {
    if (levels === '1') localStorage.removeItem(LEVELS_KEY);
    else localStorage.setItem(LEVELS_KEY, levels);
  } catch {
    // A browser that keeps nothing still shows the level chosen for this visit.
  }
};

/** The width of the column that names each project, which stays put while the bands scroll. */
const NAME_WIDTH = 200;

/** Every project of the workspace the reader can see, each on a line, with the links between their tasks. */
export const PlanMaster: React.FC<{ currentProjectId: string } & LinesProps> = ({ currentProjectId, zoom = 'weeks', showCritical = true, todaySignal = 0 }) => {
  const [master, setMaster] = useState<MasterPlan | null>(null);
  const [error, setError] = useState('');
  // How far down each project's plan is drawn. Level 1, the top-level
  // tasks, until the reader chooses another, which this browser remembers:
  // a workspace of plans in full is too much to read.
  const [levels, setLevels] = useState<MasterLevels>(rememberedLevels);
  // A thumb needs a taller control, and a field under 16px makes a phone zoom in.
  const { isPhone } = useViewport();
  const [folded, setFolded] = useState<ReadonlySet<string>>(new Set());
  useEffect(() => {
    let live = true;
    planAPI
      .master(levels)
      .then((res) => live && setMaster(res.data))
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to load the master plan')));
    return () => {
      live = false;
    };
  }, [currentProjectId, levels]);
  const start = master?.start ?? null;
  const end = master?.end ?? null;
  const today = master?.today ?? '2000-01-01';
  const scale = useMemo(() => buildScale(zoom, start, end, today, viewWidth()), [zoom, start, end, today]);
  const scroller = useScroller(scale, start, end, today, todaySignal, NAME_WIDTH);
  if (error) return <p role="alert" style={{ padding: 16, margin: 0, color: 'var(--danger-strong)' }}>{error}</p>;
  if (!master) return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Loading every project's plan…</p>;
  // A project sits under its parent, as on the projects page, and a parent folds its children away.
  const rows = projectRows(master.projects.map((p) => ({ id: p.project_id, parent_project_id: p.parent_project_id, p }) as unknown as Project & { p: (typeof master.projects)[number] }), folded) as unknown as { project: { p: (typeof master.projects)[number] }; depth: number; below: number; folded: boolean }[];
  const name = (id: string) => master.projects.find((p) => p.project_id === id)?.name || 'Another project';
  const nameCell: React.CSSProperties = { position: 'sticky', left: 0, zIndex: 2, width: NAME_WIDTH, flexShrink: 0, boxSizing: 'border-box', padding: '6px 12px', fontSize: 12, background: 'var(--surface)', borderRight: '1px solid var(--border-soft)' };
  return (
    <div data-plan-view="master" data-plan-scale={zoom} data-help-key="gantt.master" style={{ flex: 1, minHeight: 180, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
      {master.projects.length === 0 && <span style={{ ...mutedStyle, padding: '12px 16px' }}>No project of this workspace has a plan yet.</span>}
      {master.projects.length > 0 && (
        <div ref={scroller} data-pan-surface="" style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
          <div style={{ position: 'sticky', top: 0, zIndex: 3, display: 'flex', width: NAME_WIDTH + scale.width }}>
            <div style={{ ...nameCell, zIndex: 4, background: 'var(--surface-alt)', borderBottom: '1px solid var(--border-soft)', fontWeight: 600, color: 'var(--text-muted)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
              Project
              <select value={levels} aria-label="Level of detail" title="How far down each project's tasks are drawn. Milestones always show." onChange={(e) => (rememberLevels(e.target.value as MasterLevels), setLevels(e.target.value as MasterLevels))} style={{ padding: '2px 6px', minHeight: isPhone ? 40 : 26, boxSizing: 'border-box', fontSize: isPhone ? 16 : 12, fontWeight: 400 }}>
                {MASTER_LEVELS.map((l) => (
                  <option key={l} value={l}>
                    {l === 'all' ? 'All levels' : `Level ${l}`}
                  </option>
                ))}
              </select>
            </div>
            <Ticks scale={scale} />
          </div>
          {rows.map(({ project: { p }, depth, below, folded: isFolded }) => (
            <section key={p.project_id} data-master-project={p.project_id} data-depth={depth} style={{ display: 'flex', width: NAME_WIDTH + scale.width, borderBottom: '1px solid var(--border-soft)' }}>
              <div style={{ ...nameCell, paddingLeft: 12 + Math.min(depth, 4) * 12 }}>
                {below > 0 && (
                  <button
                    type="button"
                    aria-expanded={!isFolded}
                    aria-label={`${isFolded ? 'Show' : 'Hide'} the ${below} ${below === 1 ? 'project' : 'projects'} under ${p.name}`}
                    onClick={() => setFolded((f) => toggled(f, p.project_id))}
                    style={{ border: 'none', background: 'none', padding: 0, marginRight: 4, minWidth: isPhone ? 32 : 16, minHeight: isPhone ? 32 : 16, cursor: 'pointer', color: 'var(--text-muted)', fontSize: 12 }}
                  >
                    {isFolded ? '▸' : '▾'}
                  </button>
                )}
                <Link to={`/projects/${p.project_id}/gantt`} style={{ fontWeight: 600, fontSize: 13 }}>
                  {p.name}
                </Link>
                {p.project_id === currentProjectId && <span style={mutedStyle}> · this project</span>}
                <div style={mutedStyle}>
                  {p.summary.progress}% · {p.summary.done}/{p.summary.tasks} tasks{p.summary.late ? ` · ${p.summary.late} late` : ''}
                </div>
                <div style={mutedStyle}>{p.start ? spanLabel(p.start, p.end, master.today) : 'Not scheduled'}</div>
                {isFolded && <div style={mutedStyle}>{below} {below === 1 ? 'project' : 'projects'} folded</div>}
              </div>
              <div style={{ padding: '6px 0' }}>
                <Band bars={p.tasks.map((t) => ({ ...t, summary: t.summary && p.tasks.some((c) => c.depth === t.depth + 1 && c.wbs.startsWith(`${t.wbs}.`)) }))} milestones={p.milestones} scale={scale} today={master.today} showCritical={showCritical} />
              </div>
            </section>
          ))}
        </div>
      )}
      {master.links.length > 0 && (
        <div style={{ padding: '10px 16px', borderTop: '1px solid var(--border-soft)' }}>
          <h3 style={eyebrowStyle}>Links between projects</h3>
          <ul style={{ margin: '6px 0 0', paddingLeft: 18, fontSize: 13 }}>
            {master.links.map((l, i) => (
              <li key={i} style={{ color: l.conflict ? 'var(--danger-strong)' : undefined }}>
                {name(l.from_project_id)} · <b>{l.from_title}</b> → {name(l.to_project_id)} · <b>{l.to_title}</b>
                <span style={mutedStyle}> ({relationshipLabel(l.type).toLowerCase()})</span>
                {l.conflict ? ' ⚠ the dates break it' : ''}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};
