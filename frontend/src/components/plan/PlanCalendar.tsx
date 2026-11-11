// The plan's calendar, in a dialog opened from the chart's header: its
// holidays, days it does not work whatever the weekday, and the float at or
// below which a task counts as critical. The working weekdays are set in
// the header itself (WorkingDays.tsx).
import React, { useState } from 'react';
import type { PlanHoliday, PlanSettings, ProjectPlan } from '../../api/client';

/** A change to the plan's settings: the fields it names; an empty date clears it. */
export type PlanSettingsChange = Partial<Omit<PlanSettings, 'project_start' | 'project_finish' | 'budget' | 'status_date' | 'active_baseline_id'>> & { project_start?: string; project_finish?: string; budget?: number; status_date?: string; active_baseline_id?: string };
import { Modal, buttonStyle, eyebrowStyle, mutedStyle } from '../ui';
import { shortDate } from './ganttScale';

const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

export const PlanCalendar: React.FC<{
  plan: ProjectPlan;
  busy?: boolean;
  onChange: (change: PlanSettingsChange) => void;
  onClose: () => void;
}> = ({ plan, busy, onChange, onClose }) => {
  const [date, setDate] = useState('');
  const [name, setName] = useState('');
  const [slack, setSlack] = useState(String(plan.critical_slack_days));
  const holidays = plan.holidays;
  const add = () => {
    if (!date || holidays.some((h) => h.date === date)) return;
    onChange({ holidays: [...holidays, { date, name: name.trim() }] });
    setDate('');
    setName('');
  };
  const remove = (h: PlanHoliday) => onChange({ holidays: holidays.filter((x) => x.date !== h.date) });
  const threshold = /^\d{1,3}$/.test(slack.trim()) ? Number(slack.trim()) : null;
  return (
    <Modal title="Plan calendar and scheduling" width={480} onClose={onClose}>
      <div data-help-key="gantt.holidays" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <h3 style={eyebrowStyle}>Holidays · {holidays.length}</h3>
        <span style={mutedStyle}>
          Days the plan does not work whatever the weekday: public holidays, a shutdown. Durations and float skip them, and the chart shades them.
        </span>
        {holidays.length > 0 && (
          <ul style={{ listStyle: 'none', margin: 0, padding: 0, maxHeight: 220, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 2 }}>
            {holidays.map((h) => (
              <li key={h.date} data-holiday={h.date} style={{ display: 'flex', alignItems: 'center', gap: 8, minHeight: 30, fontSize: 13 }}>
                <span style={{ width: 96, flexShrink: 0, color: 'var(--text-secondary)' }}>{shortDate(h.date)}</span>
                <span style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere' }}>{h.name || <span style={mutedStyle}>No name</span>}</span>
                <button type="button" disabled={busy} aria-label={`Remove the holiday on ${shortDate(h.date)}`} onClick={() => remove(h)} style={buttonStyle('ghost', { compact: true, disabled: busy })}>
                  ✕
                </button>
              </li>
            ))}
          </ul>
        )}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            add();
          }}
          style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}
        >
          <input type="date" value={date} onChange={(e) => setDate(e.target.value)} aria-label="Date of the new holiday" style={{ ...inputStyle, flex: '0 1 150px', width: 'auto' }} />
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} placeholder="Name (optional)" aria-label="Name of the new holiday" style={{ ...inputStyle, flex: '1 1 140px', minWidth: 0 }} />
          <button type="submit" disabled={busy || !date} style={buttonStyle('secondary', { disabled: busy || !date })}>
            Add holiday
          </button>
        </form>

        <h3 style={{ ...eyebrowStyle, marginTop: 8 }}>Scheduling</h3>
        <div data-help-key="gantt.scheduling" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', gridColumn: '1 / -1' }}>
            Tasks are scheduled
            <select value={plan.scheduling_mode} disabled={busy} aria-label="Scheduling mode of the plan" onChange={(e) => onChange({ scheduling_mode: e.target.value as PlanSettings['scheduling_mode'] })} style={inputStyle}>
              <option value="manual">Manually: they stay where they are put, until you recalculate</option>
              <option value="auto">Automatically: placed after every change</option>
            </select>
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
            Plan starts
            <input type="date" value={plan.project_start || ''} disabled={busy} aria-label="Plan start date" onChange={(e) => onChange({ project_start: e.target.value })} style={inputStyle} />
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
            Must finish by
            <input type="date" value={plan.project_finish || ''} disabled={busy} aria-label="Plan finish date" onChange={(e) => onChange({ project_finish: e.target.value })} style={inputStyle} />
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', gridColumn: '1 / -1' }}>
            Schedule from
            <select value={plan.schedule_from} disabled={busy} aria-label="Schedule from" onChange={(e) => onChange({ schedule_from: e.target.value as PlanSettings['schedule_from'] })} style={inputStyle}>
              <option value="start">The start: automatic tasks go as soon as possible</option>
              <option value="finish">The finish: automatic tasks go as late as possible</option>
            </select>
          </label>
        </div>
        <span style={mutedStyle}>
          An automatic task starts no earlier than the plan does. Float is counted to the finish date when there is one, and goes negative for work that runs past it.
        </span>

        <h3 style={{ ...eyebrowStyle, marginTop: 8 }}>Critical path</h3>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, flexWrap: 'wrap' }}>
          A task is critical when its float is at most
          <input
            type="number"
            min={0}
            max={365}
            step={1}
            value={slack}
            disabled={busy}
            aria-label="Critical threshold in working days"
            onChange={(e) => setSlack(e.target.value)}
            onBlur={() => {
              if (threshold === null) setSlack(String(plan.critical_slack_days));
              else if (threshold !== plan.critical_slack_days) onChange({ critical_slack_days: threshold });
            }}
            style={{ ...inputStyle, width: 72, flex: 'none' }}
          />
          working days
        </label>
        <span style={mutedStyle}>0 marks only the tasks with no room at all. A higher number also marks the tasks that are nearly critical.</span>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
          <input type="checkbox" checked={plan.multiple_critical_paths} disabled={busy} onChange={(e) => onChange({ multiple_critical_paths: e.target.checked })} style={{ width: 'auto', flex: 'none', margin: 0 }} />
          A critical path for each chain of tasks, to its own last task
        </label>
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <button type="button" onClick={onClose} style={buttonStyle('primary')}>
            Done
          </button>
        </div>
      </div>
    </Modal>
  );
};
