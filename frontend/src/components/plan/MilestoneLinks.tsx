// What a milestone waits for, in its panel (MilestonePanel.tsx): the tasks
// that must finish before it, and whether its date is fixed or follows
// them. Dated by the plan, the milestone falls on the day the last of those
// tasks finishes and moves when they do; with a fixed date it stays put,
// and says so when one of them finishes after it.
import React, { useMemo } from 'react';
import type { PlanMilestone, PlanMilestoneInput, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, Picker, buttonStyle, eyebrowStyle, mutedStyle, type PickerOption } from '../ui';
import { shortDate, spanLabel } from './ganttScale';

const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

export interface MilestoneLinksProps {
  plan: ProjectPlan;
  milestone: PlanMilestone;
  editable: boolean;
  busy: boolean;
  /** Changes the tasks the milestone waits for, or how its date is set. */
  onLink: (m: PlanMilestone, change: Pick<PlanMilestoneInput, 'waits_for' | 'date_mode'>) => void;
  onSelectTask: (task: PlanTask) => void;
}

export const MilestoneLinks: React.FC<MilestoneLinksProps> = ({ plan, milestone: m, editable, busy, onLink, onSelectTask }) => {
  const byId = useMemo(() => new Map(plan.tasks.map((t) => [t.id, t])), [plan.tasks]);
  const waits = m.waits_for.map((id) => byId.get(id)).filter((t): t is PlanTask => Boolean(t));
  const candidates = useMemo<PickerOption[]>(() => {
    const has = new Set(m.waits_for);
    // A task with sub-tasks is not waited for: one of its sub-tasks is.
    return plan.tasks
      .filter((t) => !has.has(t.id) && t.subtasks_total === 0)
      .map((t) => ({ value: t.id, label: `${t.wbs} ${t.title}`, meta: t.span_start ? spanLabel(t.span_start, t.span_end, plan.today) : 'Not scheduled', depth: t.depth }));
  }, [plan.tasks, plan.today, m.waits_for]);
  const release = m.kind === 'release';
  const byPlan = m.date_mode === 'plan';
  const scheduled = waits.some((t) => t.span_end);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderBottom: '1px solid var(--border-soft)' }} data-help-key="gantt.milestone-links">
      <h3 style={eyebrowStyle}>Waits for · {waits.length}</h3>
      {!release && (
        <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
          Its date is
          <select value={m.date_mode} disabled={!editable || busy} aria-label="How the milestone's date is set" onChange={(e) => onLink(m, { date_mode: e.target.value as PlanMilestone['date_mode'] })} style={inputStyle}>
            <option value="fixed">Fixed: the date it is given</option>
            <option value="plan">By the plan: when the last task it waits for finishes</option>
          </select>
        </label>
      )}
      {byPlan && (
        <span style={mutedStyle}>
          {scheduled
            ? `It falls on ${shortDate(m.date, plan.today)}, and moves when the tasks below do.`
            : `Nothing it waits for is scheduled yet, so it keeps ${m.fixed_date ? `its own date, ${shortDate(m.fixed_date, plan.today)}` : 'no date'}.`}
        </span>
      )}
      {m.conflict && (
        <Chip tone="danger" title="Move the milestone, bring the task forward, or date the milestone by the plan">
          A task it waits for finishes after {shortDate(m.date, plan.today)}
        </Chip>
      )}
      {waits.length === 0 && !byPlan && <span style={mutedStyle}>Nothing. Choose the tasks that must finish before this milestone, or drag a link from a task's bar onto its diamond.</span>}
      {waits.map((t) => {
        const after = Boolean(!byPlan && m.date && t.span_end && t.span_end > m.date);
        return (
          <div key={t.id} data-milestone-waits={t.id} style={{ display: 'flex', alignItems: 'center', gap: 6, minHeight: 28 }}>
            <button
              type="button"
              onClick={() => onSelectTask(t)}
              style={{ flex: 1, minWidth: 0, padding: 0, border: 'none', background: 'none', font: 'inherit', color: 'var(--accent-text)', textAlign: 'left', cursor: 'pointer', overflowWrap: 'anywhere' }}
            >
              {t.wbs} {t.title}
            </button>
            <span style={{ ...mutedStyle, flexShrink: 0, color: after ? 'var(--danger-strong)' : 'var(--text-muted)' }}>{t.span_end ? `ends ${shortDate(t.span_end, plan.today)}` : 'Not scheduled'}</span>
            {editable && (
              <button
                type="button"
                disabled={busy}
                aria-label={`Stop waiting for ${t.title}`}
                onClick={() => onLink(m, { waits_for: m.waits_for.filter((id) => id !== t.id) })}
                style={buttonStyle('ghost', { compact: true, disabled: busy })}
              >
                ✕
              </button>
            )}
          </div>
        );
      })}
      {editable && candidates.length > 0 && (
        <Picker
          id="gantt-milestone-waits"
          value=""
          options={candidates}
          searchable
          disabled={busy}
          placeholder="Search for a task…"
          noneLabel="Wait for a task…"
          onChange={(id) => id && onLink(m, { waits_for: [...m.waits_for, id] })}
        />
      )}
    </div>
  );
};
