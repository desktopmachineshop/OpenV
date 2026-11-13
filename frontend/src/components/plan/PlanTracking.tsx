// The plan's tracking, in a dialog opened from the chart's header: the day
// progress is reported as of, the baselines saved and the one measured
// against, earned value at that day, and moving the work that has not
// started up to it.
import React from 'react';
import { Link } from 'react-router-dom';
import type { ProjectPlan } from '../../api/client';
import { Chip, Modal, buttonStyle, eyebrowStyle, mutedStyle } from '../ui';
import { shortDate } from './ganttScale';
import type { PlanSettingsChange } from './PlanCalendar';
import { money } from './planModel';

/** What the dialog asks of the page. */
export type TrackingAction = { kind: 'save'; name: string } | { kind: 'delete'; id: string } | { kind: 'reschedule' };

const inputStyle: React.CSSProperties = { padding: '4px 6px', minHeight: 30, boxSizing: 'border-box' };

/** An index: at or above 1 is on plan or better. */
const index = (n: number) => (n ? n.toFixed(2) : '–');

export const PlanTracking: React.FC<{
  plan: ProjectPlan;
  busy?: boolean;
  showBaseline: boolean;
  onShowBaseline: (on: boolean) => void;
  onSettings: (change: PlanSettingsChange) => void;
  onAction: (action: TrackingAction) => void;
  onClose: () => void;
}> = ({ plan, busy, showBaseline, onShowBaseline, onSettings, onAction, onClose }) => {
  const [name, setName] = React.useState('');
  const ev = plan.earned_value;
  const behind = plan.tasks.filter((t) => t.behind_schedule && t.subtasks_total === 0).length;
  const cell: React.CSSProperties = { padding: '6px 8px', borderBottom: '1px solid var(--border-soft)', textAlign: 'left', fontSize: 13 };
  const figure = (label: string, value: string, hint: string, bad = false) => (
    <div title={hint} style={{ flex: '1 1 110px', padding: '6px 8px', border: '1px solid var(--border-soft)', borderRadius: 6 }}>
      <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{label}</div>
      <div style={{ fontSize: 15, fontWeight: 600, color: bad ? 'var(--danger-strong)' : undefined }}>{value}</div>
    </div>
  );
  return (
    <Modal title="Tracking" width={720} onClose={onClose}>
      <div data-help-key="gantt.tracking" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', fontSize: 13 }}>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
            Status date
            <input
              type="date"
              value={plan.status_date || ''}
              disabled={busy}
              aria-label="Status date"
              onChange={(e) => onSettings({ status_date: e.target.value })}
              style={inputStyle}
            />
          </label>
          <span style={mutedStyle}>
            {plan.status_date ? `Progress is reported as of ${shortDate(plan.status_day, plan.today)}.` : 'Progress is reported as of today. Set a date to report as of another day.'}
          </span>
        </div>
        <h3 style={eyebrowStyle}>Baselines</h3>
        {plan.baselines.length === 0 ? (
          <span style={mutedStyle}>No baseline yet. Save one once the plan is agreed: the chart then shows where each task was, and how far it has moved.</span>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <tbody>
              {plan.baselines.map((b) => (
                <tr key={b.id} data-baseline={b.id}>
                  <td style={cell}>
                    <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
                      <input
                        type="radio"
                        name="active-baseline"
                        checked={plan.baseline_id === b.id}
                        disabled={busy}
                        onChange={() => onSettings({ active_baseline_id: b.id })}
                        style={{ width: 'auto', flex: 'none', margin: 0 }}
                      />
                      {b.name}
                    </label>
                    {plan.baseline_id === b.id && <span style={{ marginLeft: 8 }}><Chip tone="info">measured against</Chip></span>}
                  </td>
                  <td style={{ ...cell, color: 'var(--text-muted)' }}>
                    {shortDate(b.created_at.slice(0, 10), plan.today)} · {b.tasks} {b.tasks === 1 ? 'task' : 'tasks'}
                  </td>
                  <td style={{ ...cell, textAlign: 'right' }}>
                    {b.project ? (
                      // A baseline of the project records the definition too: it is deleted where it is compared.
                      <Link to={`/projects/${plan.project_id}/baselines`} title="This baseline records the project's artifacts as well as its plan. Compare it, or delete it, on the Baselines page." style={{ fontSize: 12 }}>
                        Project baseline
                      </Link>
                    ) : (
                      <button type="button" disabled={busy} onClick={() => onAction({ kind: 'delete', id: b.id })} aria-label={`Delete ${b.name}`} style={buttonStyle('ghost', { compact: true, disabled: busy })}>
                        Delete
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <input value={name} maxLength={80} disabled={busy} aria-label="Baseline name" placeholder={`Baseline ${plan.today}`} onChange={(e) => setName(e.target.value)} style={{ ...inputStyle, flex: '1 1 200px' }} />
          <button
            type="button"
            data-save-baseline
            disabled={busy}
            title="Records the plan and the project's artifacts together, as a baseline of the project"
            onClick={() => {
              onAction({ kind: 'save', name: name.trim() });
              setName('');
            }}
            style={buttonStyle('secondary', { disabled: busy })}
          >
            Save a baseline
          </button>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13 }}>
            <input type="checkbox" checked={showBaseline} disabled={!plan.baseline_id} onChange={(e) => onShowBaseline(e.target.checked)} style={{ width: 'auto', flex: 'none', margin: 0 }} />
            Show on the chart
          </label>
        </div>
        <h3 style={eyebrowStyle}>Earned value</h3>
        {!plan.baseline_id ? (
          <span style={mutedStyle}>Earned value is measured against a baseline. Save one, and give people a day rate under Resources so that tasks have a cost.</span>
        ) : ev.bac === 0 ? (
          <span style={mutedStyle}>The baseline has no costs. Give people a day rate under Resources, or tasks a fixed cost, then save a new baseline.</span>
        ) : (
          <div data-earned-value style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {figure('Budget (BAC)', money(ev.bac, plan.currency), "The baseline's cost")}
            {figure('Planned (PV)', money(ev.pv, plan.currency), 'What the baseline had done by the status date')}
            {figure('Earned (EV)', money(ev.ev, plan.currency), 'The baseline cost of the work really done')}
            {figure('Spent (AC)', money(ev.ac, plan.currency), 'What the work done has cost')}
            {figure('Schedule variance', money(ev.sv, plan.currency), 'Earned less planned: negative is behind', ev.sv < 0)}
            {figure('Cost variance', money(ev.cv, plan.currency), 'Earned less spent: negative is over cost', ev.cv < 0)}
            {figure('SPI', index(ev.spi), 'Earned over planned: below 1 is behind', ev.spi > 0 && ev.spi < 1)}
            {figure('CPI', index(ev.cpi), 'Earned over spent: below 1 is over cost', ev.cpi > 0 && ev.cpi < 1)}
            {figure('Forecast (EAC)', money(ev.eac, plan.currency), 'What the plan will cost at this rate', ev.eac > ev.bac)}
          </div>
        )}
        <h3 style={eyebrowStyle}>Remaining work</h3>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
          <span style={{ ...mutedStyle, flex: '1 1 320px' }}>
            {behind === 0 ? 'No task is behind the status date.' : `${behind} ${behind === 1 ? 'task is' : 'tasks are'} behind the status date.`} Rescheduling moves the tasks that should have started by it and
            have not to that day, and the tasks they drive after them.
          </span>
          <button type="button" data-reschedule-remaining disabled={busy} onClick={() => onAction({ kind: 'reschedule' })} style={buttonStyle('secondary', { disabled: busy })}>
            Reschedule remaining work
          </button>
          <button type="button" onClick={onClose} style={buttonStyle('primary')}>
            Done
          </button>
        </div>
      </div>
    </Modal>
  );
};
