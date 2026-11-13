// The plan on a phone. A Gantt needs width a phone does not have: bars a
// thumb cannot grab and a timeline that shows a week at a time tell less
// than a list. So the phone gets the same plan as a schedule: the figures
// first, then the tasks grouped under the milestone they lead to, each with
// its dates and progress bar, and the milestones with their criteria one tab
// away. Tapping a row opens it in a sheet (Gantt.tsx) to edit.
import React, { useState } from 'react';
import type { PlanMilestone, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, PanelHeader, buttonStyle, ellipsis, mutedStyle, pageType, panelStyle } from '../ui';
import { shortDate, spanLabel } from './ganttScale';
import { ExternalMark } from './GanttRows';
import { BAR_COLORS, barTone, columnLabel, milestoneColor, milestoneState, phoneGroups } from './planModel';

type Tab = 'schedule' | 'milestones';

const tabStyle = (on: boolean): React.CSSProperties => ({
  minHeight: 40,
  padding: '0 12px',
  border: 'none',
  borderBottom: `2px solid ${on ? 'var(--accent)' : 'transparent'}`,
  background: 'transparent',
  color: on ? 'var(--text)' : 'var(--text-muted)',
  fontSize: 14,
  fontWeight: on ? 600 : 500,
  cursor: 'pointer',
});

const tile: React.CSSProperties = { ...panelStyle, display: 'flex', flexDirection: 'column', padding: 8 };

const rowButton: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 4,
  width: '100%',
  minHeight: 56,
  padding: '8px 12px',
  border: 'none',
  borderBottom: '1px solid var(--border-soft)',
  background: 'var(--surface)',
  font: 'inherit',
  color: 'var(--text)',
  textAlign: 'left',
  cursor: 'pointer',
};

const Bar: React.FC<{ value: number; color: string; label: string }> = ({ value, color, label }) => (
  <span role="img" aria-label={label} style={{ display: 'flex', height: 6, borderRadius: 3, background: 'var(--neutral-soft)', overflow: 'hidden' }}>
    <span style={{ width: `${value}%`, background: color }} />
  </span>
);

const TaskRow: React.FC<{ task: PlanTask; today: string; onOpen: () => void }> = ({ task: t, today, onOpen }) => {
  const tone = barTone(t);
  return (
    <button type="button" onClick={onOpen} style={{ ...rowButton, paddingLeft: 12 + t.depth * 14 }}>
      <span style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <span style={{ ...ellipsis, flex: 1, fontWeight: t.subtasks_total > 0 ? 600 : 400 }}>{t.title}</span>
        {t.external_links.length > 0 && <ExternalMark task={t} />}
        {t.late ? <Chip tone="danger">Late</Chip> : t.done ? <Chip tone="success">Done</Chip> : t.critical ? <Chip tone="warning">Critical</Chip> : null}
        <b style={{ fontSize: 13 }}>{t.progress}%</b>
      </span>
      <Bar value={t.progress} color={BAR_COLORS[tone].fill} label={`${t.progress}% complete`} />
      <span style={mutedStyle}>
        {spanLabel(t.span_start, t.span_end, today)}
        {t.duration_days ? ` · ${t.duration_days} ${t.duration_days === 1 ? 'day' : 'days'}` : ''} · {columnLabel(t.column)}
        {t.subtasks_total > 0 ? ` · ${t.subtasks_done}/${t.subtasks_total} sub-tasks` : ''}
        {t.conflicts.length > 0 ? ' · its dates break a relationship' : ''}
      </span>
    </button>
  );
};

const MilestoneHead: React.FC<{ m: PlanMilestone; today: string; onOpen: () => void }> = ({ m, today, onOpen }) => {
  const state = milestoneState(m);
  return (
    <button type="button" onClick={onOpen} style={{ ...rowButton, background: 'var(--surface-alt)' }}>
      <span style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <span aria-hidden style={{ width: 10, height: 10, transform: 'rotate(45deg)', borderRadius: 2, background: milestoneColor(m), flexShrink: 0 }} />
        <span style={{ ...ellipsis, flex: 1, fontWeight: 600 }}>{m.name}</span>
        <Chip tone={state.tone}>{state.label}</Chip>
        <b style={{ fontSize: 13 }}>{m.progress}%</b>
      </span>
      <Bar value={m.progress} color={milestoneColor(m)} label={`${m.progress}% of its criteria met`} />
      <span style={mutedStyle}>
        {m.kind === 'release' ? 'Release · ' : ''}
        {m.date ? shortDate(m.date, today) : 'No date'} · {m.criteria.filter((c) => c.met).length}/{m.criteria.length} criteria met
      </span>
    </button>
  );
};

export interface PlanPhoneProps {
  plan: ProjectPlan;
  head: React.ReactNode;
  forms: React.ReactNode;
  error: React.ReactNode;
  onOpenTask: (t: PlanTask) => void;
  onOpenMilestone: (m: PlanMilestone) => void;
}

export const PlanPhone: React.FC<PlanPhoneProps> = ({ plan, head, forms, error, onOpenTask, onOpenMilestone }) => {
  const [tab, setTab] = useState<Tab>('schedule');
  // Tasks with no dates say nothing about the schedule: they are folded away until asked for.
  const [showUnscheduled, setShowUnscheduled] = useState(false);
  const s = plan.summary;
  const groups = phoneGroups(plan.tasks, plan.milestones);
  const under = (parent: PlanTask): PlanTask[] => {
    const i = plan.tasks.indexOf(parent);
    const out: PlanTask[] = [];
    for (let j = i + 1; j < plan.tasks.length && plan.tasks[j].depth > parent.depth; j++) out.push(plan.tasks[j]);
    return out;
  };
  return (
    <div style={{ ...pageType(true), padding: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
      {error}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(0, 1fr))', gap: 6 }}>
        <div style={tile}>
          <b style={{ fontSize: 18 }}>{s.progress}%</b>
          <span style={mutedStyle}>complete</span>
        </div>
        <div style={tile}>
          <b style={{ fontSize: 18 }}>{s.tasks}</b>
          <span style={mutedStyle}>tasks</span>
        </div>
        <div style={tile}>
          <b style={{ fontSize: 18, color: 'var(--success-text)' }}>{s.done}</b>
          <span style={mutedStyle}>done</span>
        </div>
        <div style={tile}>
          <b style={{ fontSize: 18, color: s.late ? 'var(--danger-strong)' : undefined }}>{s.late}</b>
          <span style={mutedStyle}>late</span>
        </div>
      </div>
      {head}
      {forms}
      <div role="tablist" aria-label="Plan" style={{ display: 'flex', borderBottom: '1px solid var(--border)' }}>
        <button type="button" role="tab" aria-selected={tab === 'schedule'} onClick={() => setTab('schedule')} style={tabStyle(tab === 'schedule')}>
          Schedule
        </button>
        <button type="button" role="tab" aria-selected={tab === 'milestones'} onClick={() => setTab('milestones')} style={tabStyle(tab === 'milestones')}>
          Milestones {plan.milestones.length}
        </button>
      </div>

      {tab === 'schedule' &&
        groups.map((g) => (
          <section key={g.key} aria-label={g.title} style={{ ...panelStyle, overflow: 'hidden' }}>
            {g.milestone ? (
              <MilestoneHead m={g.milestone} today={plan.today} onOpen={() => onOpenMilestone(g.milestone!)} />
            ) : (
              <PanelHeader
                title={g.title}
                count={g.tasks.length}
                action={
                  g.key === 'unscheduled' ? (
                    <button type="button" aria-expanded={showUnscheduled} onClick={() => setShowUnscheduled((v) => !v)} style={{ ...buttonStyle('secondary', { compact: true }), minHeight: 36 }}>
                      {showUnscheduled ? 'Hide' : 'Show'}
                    </button>
                  ) : undefined
                }
              />
            )}
            {g.tasks.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No task is assigned to this milestone.</p>}
            {(g.key === 'unscheduled' && !showUnscheduled ? [] : g.tasks).map((t) => (
              <React.Fragment key={t.id}>
                <TaskRow task={t} today={plan.today} onOpen={() => onOpenTask(t)} />
                {under(t).map((c) => (
                  <TaskRow key={c.id} task={c} today={plan.today} onOpen={() => onOpenTask(c)} />
                ))}
              </React.Fragment>
            ))}
          </section>
        ))}
      {tab === 'schedule' && groups.length === 0 && (
        <p style={{ ...mutedStyle, margin: 0 }}>No tasks yet. Add one here, or on the Board: every card on the board is a task of the plan.</p>
      )}

      {tab === 'milestones' && (
        <section aria-label="Milestones" style={{ ...panelStyle, overflow: 'hidden' }}>
          {plan.milestones.length === 0 && (
            <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No milestones yet. Add one, or add a release: each release is a milestone here.</p>
          )}
          {plan.milestones.map((m) => (
            <MilestoneHead key={m.key} m={m} today={plan.today} onOpen={() => onOpenMilestone(m)} />
          ))}
        </section>
      )}
    </div>
  );
};
