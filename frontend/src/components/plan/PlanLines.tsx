// Two charts that fit the page's width with no scrolling: the timeline, a
// project's top-level tasks and milestones on one band; and the master
// plan, every project of the workspace on a line of its own with the links
// between their tasks.
import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { planAPI, type MasterPlan, type PlanTask, type ProjectPlan } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ellipsis, eyebrowStyle, mutedStyle } from '../ui';
import { shortDate, spanLabel, toDay } from './ganttScale';
import { relationshipLabel } from './planModel';

interface Bar {
  id: string;
  title: string;
  start: string | null;
  end: string | null;
  progress: number;
  done: boolean;
  late: boolean;
  critical: boolean;
}

/** Where a day falls between the chart's first and last, as a percentage of its width. */
const useSpan = (start: string | null, end: string | null) => {
  const a = toDay(start || end || '2000-01-01') - 2;
  const b = toDay(end || start || '2000-01-01') + 3;
  return { left: (d: string) => ((toDay(d) - a) / (b - a)) * 100, width: (s: string, e: string) => ((toDay(e) + 1 - toDay(s)) / (b - a)) * 100 };
};

/** Bars packed onto as few lanes as will hold them without overlapping. */
const lanes = (bars: Bar[]): Bar[][] => {
  const out: Bar[][] = [];
  for (const b of bars.filter((x) => x.start && x.end).sort((x, y) => x.start!.localeCompare(y.start!))) {
    const lane = out.find((l) => l[l.length - 1].end! < b.start!);
    if (lane) lane.push(b);
    else out.push([b]);
  }
  return out;
};

const Band: React.FC<{
  bars: Bar[];
  milestones: { key: string; name: string; date: string | null; met: boolean; late: boolean }[];
  span: ReturnType<typeof useSpan>;
  today: string;
  onBar?: (id: string) => void;
}> = ({ bars, milestones, span, today, onBar }) => {
  const rows = lanes(bars);
  const todayAt = span.left(today);
  return (
    <div style={{ position: 'relative', height: 26 + Math.max(rows.length, 1) * 24, minWidth: 0 }}>
      {todayAt >= 0 && todayAt <= 100 && <span aria-hidden style={{ position: 'absolute', left: `${todayAt}%`, top: 0, bottom: 0, borderLeft: '2px solid var(--danger)', opacity: 0.6 }} />}
      {milestones
        .filter((m) => m.date)
        .map((m) => (
          <span key={m.key} title={`${m.name}, ${shortDate(m.date, today)}`} style={{ ...ellipsis, position: 'absolute', left: `${span.left(m.date!)}%`, top: 0, maxWidth: 160, fontSize: 11, color: m.met ? 'var(--success-text)' : m.late ? 'var(--danger-strong)' : 'var(--text-secondary)', transform: 'translateX(-5px)' }}>
            ◆ {m.name}
          </span>
        ))}
      {rows.map((row, i) =>
        row.map((b) => (
          <button
            key={b.id}
            type="button"
            disabled={!onBar}
            onClick={() => onBar?.(b.id)}
            title={`${b.title}: ${spanLabel(b.start, b.end, today)}, ${b.progress}%`}
            style={{ ...ellipsis, position: 'absolute', left: `${span.left(b.start!)}%`, width: `${Math.max(span.width(b.start!, b.end!), 0.6)}%`, top: 24 + i * 24, height: 20, boxSizing: 'border-box', padding: '0 6px', fontSize: 11, lineHeight: '18px', textAlign: 'left', cursor: onBar ? 'pointer' : 'default', borderRadius: 4, border: `1px solid ${b.critical ? 'var(--warning)' : b.late ? 'var(--danger)' : 'var(--border)'}`, color: 'var(--text)', background: `linear-gradient(90deg, ${b.done ? 'var(--success)' : 'var(--accent)'} ${b.progress}%, var(--surface-alt) ${b.progress}%)` }}
          >
            <span style={{ background: 'color-mix(in srgb, var(--surface) 70%, transparent)', borderRadius: 2, padding: '0 2px' }}>{b.title}</span>
          </button>
        )),
      )}
    </div>
  );
};

/** The project's top-level tasks and milestones on one band. */
export const PlanTimeline: React.FC<{ plan: ProjectPlan; onSelectTask: (t: PlanTask) => void }> = ({ plan, onSelectTask }) => {
  const span = useSpan(plan.start, plan.end);
  const top = plan.tasks.filter((t) => t.depth === 0);
  const bars = top.map((t) => ({ id: t.id, title: t.title, start: t.span_start, end: t.span_end, progress: t.progress, done: t.done, late: t.late, critical: t.critical || t.critical_subtasks }));
  if (!plan.start) return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Nothing is scheduled yet.</p>;
  return (
    <div data-plan-view="timeline" data-help-key="gantt.views" style={{ flex: 1, minHeight: 120, overflow: 'auto', padding: '12px 16px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>
        <span>{shortDate(plan.start, plan.today)}</span>
        <span>{shortDate(plan.end, plan.today)}</span>
      </div>
      <Band bars={bars} milestones={plan.milestones} span={span} today={plan.today} onBar={(id) => onSelectTask(top.find((t) => t.id === id)!)} />
    </div>
  );
};

/** Every project of the workspace the reader can see, each on a line, with the links between their tasks. */
export const PlanMaster: React.FC<{ currentProjectId: string }> = ({ currentProjectId }) => {
  const [master, setMaster] = useState<MasterPlan | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let live = true;
    planAPI
      .master()
      .then((res) => live && setMaster(res.data))
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to load the master plan')));
    return () => {
      live = false;
    };
  }, [currentProjectId]);
  const span = useSpan(master?.start ?? null, master?.end ?? null);
  if (error) return <p role="alert" style={{ padding: 16, margin: 0, color: 'var(--danger-strong)' }}>{error}</p>;
  if (!master) return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>Loading every project's plan…</p>;
  const name = (id: string) => master.projects.find((p) => p.project_id === id)?.name || 'Another project';
  return (
    <div data-plan-view="master" data-help-key="gantt.master" style={{ flex: 1, minHeight: 180, overflow: 'auto', padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 }}>
      {master.projects.length === 0 && <span style={mutedStyle}>No project of this workspace has a plan yet.</span>}
      {master.projects.map((p) => (
        <section key={p.project_id} data-master-project={p.project_id} style={{ display: 'flex', gap: 12, alignItems: 'flex-start', paddingBottom: 8, borderBottom: '1px solid var(--border-soft)' }}>
          <div style={{ width: 190, flexShrink: 0, fontSize: 12 }}>
            <Link to={`/projects/${p.project_id}/gantt`} style={{ fontWeight: 600, fontSize: 13 }}>
              {p.name}
            </Link>
            {p.project_id === currentProjectId && <span style={mutedStyle}> · this project</span>}
            <div style={mutedStyle}>
              {p.summary.progress}% · {p.summary.done}/{p.summary.tasks} tasks{p.summary.late ? ` · ${p.summary.late} late` : ''}
            </div>
            <div style={mutedStyle}>{p.start ? spanLabel(p.start, p.end, master.today) : 'Not scheduled'}</div>
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <Band bars={p.tasks} milestones={p.milestones} span={span} today={master.today} />
          </div>
        </section>
      ))}
      {master.links.length > 0 && (
        <div>
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
