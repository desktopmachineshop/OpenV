// The chart's panel on the Plan page (views/Gantt.tsx): its header with the
// working days, the critical path switch, Today and the zoom; the chart; the
// milestones that have no date yet; and a line on how to use it.
import React, { useState } from 'react';
import type { PlanMilestone, PlanSettings, ProjectPlan } from '../../api/client';
import { PanelHeader, SegmentedControl, buttonStyle, mutedStyle, panelStyle } from '../ui';
import { GanttChart, type GanttChartProps } from './GanttChart';
import { PlanCalendar } from './PlanCalendar';
import { WorkingDays } from './WorkingDays';
import { ZOOMS, Zoom } from './ganttScale';

export interface PlanChartPanelProps extends Omit<GanttChartProps, 'zoom' | 'showCritical' | 'todaySignal' | 'onSelectMilestone'> {
  plan: ProjectPlan;
  zoom: Zoom;
  onZoom: (zoom: Zoom) => void;
  onWorkingDays: (days: number[]) => void;
  /** Changes the plan's holidays or critical threshold. */
  onSettings: (change: Partial<PlanSettings>) => void;
  /** Moves the driven tasks to the dates their relationships ask for. */
  onRecalculate: () => void;
  onSelectMilestone: (m: PlanMilestone) => void;
}

export const PlanChartPanel: React.FC<PlanChartPanelProps> = ({ plan, zoom, onZoom, onWorkingDays, onSettings, onRecalculate, onSelectMilestone, busy, touch, ...chart }) => {
  const [showCritical, setShowCritical] = useState(true);
  const [onlyCritical, setOnlyCritical] = useState(false);
  const [calendar, setCalendar] = useState(false);
  const [todaySignal, setTodaySignal] = useState(0);
  const undated = plan.milestones.filter((m) => !m.date);
  const openMilestone = onSelectMilestone;
  const coarsePointer = touch;
  return (
        <section aria-label="Plan" style={{ ...panelStyle, flex: 1, minWidth: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
          <PanelHeader
            title="Plan"
            action={
              <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                <WorkingDays days={plan.working_days} busy={busy} onChange={onWorkingDays} />
                <button type="button" onClick={() => setCalendar(true)} data-help-key="gantt.holidays" title="Holidays, and when a task counts as critical" style={buttonStyle('secondary', { compact: true })}>
                  Holidays{plan.holidays.length ? ` · ${plan.holidays.length}` : ''}
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={onRecalculate}
                  data-help-key="gantt.recalculate"
                  title="Move each task that another drives to the dates its relationships and their offsets ask for"
                  style={buttonStyle('secondary', { compact: true, disabled: busy })}
                >
                  Recalculate
                </button>
                <button
                  type="button"
                  aria-pressed={showCritical}
                  onClick={() => setShowCritical((on) => !on)}
                  data-help-key="gantt.critical"
                  title="Mark the chain of tasks that decides when the plan ends, and show each task's float after its bar"
                  style={{
                    ...buttonStyle('secondary', { compact: true }),
                    ...(showCritical ? { background: 'var(--tint-yellow)', borderColor: 'var(--tint-yellow-border)', color: 'var(--warning-text)' } : {}),
                  }}
                >
                  <span aria-hidden style={{ width: 7, height: 7, borderRadius: '50%', background: 'var(--warning)' }} />
                  Critical path
                </button>
                {showCritical && (
                  <button
                    type="button"
                    aria-pressed={onlyCritical}
                    onClick={() => setOnlyCritical((on) => !on)}
                    title="List only the tasks on the critical path"
                    style={{ ...buttonStyle('secondary', { compact: true }), ...(onlyCritical ? { background: 'var(--tint-yellow)', borderColor: 'var(--tint-yellow-border)', color: 'var(--warning-text)' } : {}) }}
                  >
                    Only critical
                  </button>
                )}
                <button type="button" onClick={() => setTodaySignal((n) => n + 1)} style={buttonStyle('secondary', { compact: true })}>
                  Today
                </button>
                <SegmentedControl aria-label="Zoom" options={ZOOMS} value={zoom} onChange={onZoom} style={{ fontSize: 12 }} />
              </span>
            }
          />
          <GanttChart {...chart} plan={plan} zoom={zoom} busy={busy} touch={touch} onSelectMilestone={onSelectMilestone} showCritical={showCritical} onlyCritical={showCritical && onlyCritical} todaySignal={todaySignal} />
          {calendar && <PlanCalendar plan={plan} busy={busy} onChange={onSettings} onClose={() => setCalendar(false)} />}
          {undated.length > 0 && (
            <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap', padding: '8px 12px', borderTop: '1px solid var(--border-soft)' }}>
              <span style={mutedStyle}>No date yet:</span>
              {undated.map((m) => (
                <button key={m.key} type="button" onClick={() => openMilestone(m)} style={buttonStyle('secondary', { compact: true })}>
                  ◆ {m.name} · {m.progress}%
                </button>
              ))}
            </div>
          )}
          <p style={{ ...mutedStyle, margin: 0, padding: '8px 12px', borderTop: '1px solid var(--border-soft)' }}>
            A task's bar fills as its to-dos are ticked off and its sub-tasks finish. A milestone's band fills as its
            criteria are met. {coarsePointer ? 'Select a task to set its dates.' : 'Drag a bar to move it or an end to resize it, drag the + at an end to another bar to link them, and click or drag across the row of a task with no dates to schedule it. Drag the empty timeline to scroll.'}
          </p>
        </section>
  );
};
