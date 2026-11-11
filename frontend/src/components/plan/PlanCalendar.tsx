// The plan's calendar, in a dialog opened from the chart's header: its
// holidays, days it does not work whatever the weekday, and the float at or
// below which a task counts as critical. The working weekdays are set in
// the header itself (WorkingDays.tsx).
import React, { useState } from 'react';
import type { PlanHoliday, PlanSettings, ProjectPlan } from '../../api/client';
import { Modal, buttonStyle, eyebrowStyle, mutedStyle } from '../ui';
import { shortDate } from './ganttScale';

const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

export const PlanCalendar: React.FC<{
  plan: ProjectPlan;
  busy?: boolean;
  onChange: (change: Partial<PlanSettings>) => void;
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
    <Modal title="Plan calendar" width={460} onClose={onClose}>
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
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <button type="button" onClick={onClose} style={buttonStyle('primary')}>
            Done
          </button>
        </div>
      </div>
    </Modal>
  );
};
