// The chart's panel on the Plan page (views/Gantt.tsx): its header with the
// working days, the critical path switch, Today and the zoom; the chart; the
// milestones that have no date yet; and a line on how to use it.
import React, { useState } from 'react';
import type { PlanMilestone, PlanResourceLoad, ProjectPlan } from '../../api/client';
import { Modal, PanelHeader, SegmentedControl, buttonStyle, mutedStyle, panelStyle } from '../ui';
import { GanttChart, type GanttChartProps } from './GanttChart';
import { PlanCalendar, type PlanSettingsChange } from './PlanCalendar';
import { PlanResources } from './PlanResources';
import { PlanTracking, type TrackingAction } from './PlanTracking';
import { WorkingDays } from './WorkingDays';
import { ZOOMS, Zoom } from './ganttScale';

export interface PlanChartPanelProps extends Omit<GanttChartProps, 'zoom' | 'showCritical' | 'todaySignal' | 'onSelectMilestone' | 'showBaseline'> {
  plan: ProjectPlan;
  zoom: Zoom;
  onZoom: (zoom: Zoom) => void;
  onWorkingDays: (days: number[]) => void;
  /** Changes the plan's holidays or critical threshold. */
  onSettings: (change: PlanSettingsChange) => void;
  /** Sets a person's or an agent's day rate and capacity. */
  onResource: (res: PlanResourceLoad, change: { daily_rate: number; capacity: number }) => void;
  /** Delays tasks until nobody is over-allocated. */
  onLevel: () => void;
  /** Saves or deletes a baseline, or moves the work that has not started to the status date. */
  onTracking: (action: TrackingAction) => void;
  /** Moves the placed tasks to the dates their relationships and constraints ask for; onlyLater never moves one earlier. */
  onRecalculate: (onlyLater: boolean) => void;
  onSelectMilestone: (m: PlanMilestone) => void;
}

export const PlanChartPanel: React.FC<PlanChartPanelProps> = ({ plan, zoom, onZoom, onWorkingDays, onSettings, onResource, onLevel, onTracking, onRecalculate, onSelectMilestone, busy, touch, ...chart }) => {
  const [showCritical, setShowCritical] = useState(true);
  const [onlyCritical, setOnlyCritical] = useState(false);
  const [calendar, setCalendar] = useState(false);
  const [resources, setResources] = useState(false);
  const [tracking, setTracking] = useState(false);
  const [showBaseline, setShowBaseline] = useState(true);
  const [recalculating, setRecalculating] = useState(false);
  const [onlyLater, setOnlyLater] = useState(false);
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
                <button type="button" onClick={() => setCalendar(true)} data-help-key="gantt.holidays" title="Holidays, how the plan is scheduled, and when a task counts as critical" style={buttonStyle('secondary', { compact: true })}>
                  Calendar{plan.holidays.length ? ` · ${plan.holidays.length}` : ''}
                  {plan.scheduling_mode === 'auto' ? ' · auto' : ''}
                </button>
                <button
                  type="button"
                  onClick={() => setResources(true)}
                  data-help-key="gantt.resources"
                  title="Who does the work, what it costs, and who is asked for more than they have"
                  style={{
                    ...buttonStyle('secondary', { compact: true }),
                    ...(plan.summary.overallocated ? { background: 'var(--tint-red)', borderColor: 'var(--danger)', color: 'var(--danger-strong)' } : {}),
                  }}
                >
                  Resources{plan.summary.overallocated ? ` · ${plan.summary.overallocated} over` : ''}
                </button>
                <button type="button" onClick={() => setTracking(true)} data-help-key="gantt.tracking" title="Baselines, the status date, earned value and work that is behind" style={buttonStyle('secondary', { compact: true })}>
                  Tracking{plan.baselines.length ? ` · ${plan.baselines.length}` : ''}
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => setRecalculating(true)}
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
          <GanttChart {...chart} plan={plan} zoom={zoom} busy={busy} touch={touch} onSelectMilestone={onSelectMilestone} showBaseline={showBaseline && Boolean(plan.baseline_id)} showCritical={showCritical} onlyCritical={showCritical && onlyCritical} todaySignal={todaySignal} />
          {recalculating && (
            <Modal title="Recalculate the plan" width={440} onClose={() => setRecalculating(false)}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12, fontSize: 13 }}>
                <span>
                  Move every task that another task drives, or that a constraint holds to a date, to the dates those ask for? Each keeps its duration. Tasks nothing places, and tasks that are done, stay where they are.
                </span>
                <label style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                  <input type="checkbox" checked={onlyLater} onChange={(e) => setOnlyLater(e.target.checked)} style={{ width: 'auto', flex: 'none', margin: '3px 0 0' }} />
                  <span>
                    Only move tasks later
                    <span style={{ ...mutedStyle, display: 'block' }}>Relationships are minimums: a task that already starts on or after the day asked is left where it is.</span>
                  </span>
                </label>
                <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                  <button type="button" onClick={() => setRecalculating(false)} style={buttonStyle('secondary')}>
                    Cancel
                  </button>
                  <button
                    type="button"
                    data-recalculate-confirm
                    onClick={() => {
                      setRecalculating(false);
                      onRecalculate(onlyLater);
                    }}
                    style={buttonStyle('primary')}
                  >
                    Recalculate
                  </button>
                </div>
              </div>
            </Modal>
          )}
          {tracking && <PlanTracking plan={plan} busy={busy} showBaseline={showBaseline} onShowBaseline={setShowBaseline} onSettings={onSettings} onAction={onTracking} onClose={() => setTracking(false)} />}
          {resources && <PlanResources plan={plan} busy={busy} onResource={onResource} onSettings={onSettings} onLevel={onLevel} onClose={() => setResources(false)} />}
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
