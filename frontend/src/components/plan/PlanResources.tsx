// The plan's resources, in a dialog opened from the chart's header: each
// person and agent with work on the plan, what a day of theirs costs and how
// much of a day they can give, their work, their cost and when they are
// asked for more than they have; the plan's currency and budget; and
// levelling, which delays tasks until nobody is over-allocated.
import React, { useEffect, useState } from 'react';
import type { PlanResourceLoad, ProjectPlan } from '../../api/client';
import { Chip, Modal, buttonStyle, eyebrowStyle, mutedStyle } from '../ui';
import { spanLabel } from './ganttScale';
import type { PlanSettingsChange } from './PlanCalendar';
import { money } from './planModel';
import { PlanPlanned, type PlannedAction } from './PlanPlanned';
import { useFeature } from '../../hooks/useFeature';
import { PLAN_SIZING_FEATURE } from '../../features';

const inputStyle: React.CSSProperties = { padding: '4px 6px', minHeight: 30, boxSizing: 'border-box' };

/** A number saved when the field is left or Enter is pressed. */
const NumberField: React.FC<{ value: number; label: string; width: number; min?: number; max?: number; disabled?: boolean; onCommit: (n: number) => void }> = ({
  value,
  label,
  width,
  min = 0,
  max = 1e9,
  disabled,
  onCommit,
}) => {
  const [text, setText] = useState(value ? String(value) : '');
  useEffect(() => setText(value ? String(value) : ''), [value]);
  const commit = () => {
    const n = text.trim() === '' ? 0 : Number(text);
    if (!(n >= min && n <= max)) setText(value ? String(value) : '');
    else if (n !== value) onCommit(n);
  };
  return (
    <input
      type="number"
      min={min}
      value={text}
      disabled={disabled}
      aria-label={label}
      placeholder="0"
      onChange={(e) => setText(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          commit();
        }
      }}
      style={{ ...inputStyle, width, flex: 'none' }}
    />
  );
};

export const PlanResources: React.FC<{
  plan: ProjectPlan;
  busy?: boolean;
  onResource: (res: PlanResourceLoad, change: { daily_rate: number; capacity: number }) => void;
  onSettings: (change: PlanSettingsChange) => void;
  onLevel: () => void;
  /** Adds, removes or staffs a planned resource; absent where the plan cannot be edited that way. */
  onPlanned?: (action: PlannedAction) => void;
  onClose: () => void;
}> = ({ plan, busy, onResource, onSettings, onLevel, onPlanned, onClose }) => {
  const sizing = useFeature(PLAN_SIZING_FEATURE);
  const [currency, setCurrency] = useState(plan.currency);
  useEffect(() => setCurrency(plan.currency), [plan.currency]);
  const over = plan.resources.filter((r) => r.overallocations.length > 0);
  const cell: React.CSSProperties = { padding: '6px 8px', borderBottom: '1px solid var(--border-soft)', textAlign: 'left', verticalAlign: 'top', fontSize: 13 };
  const head: React.CSSProperties = { ...cell, fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' };
  return (
    <Modal title="Resources" width={760} onClose={onClose}>
      <div data-help-key="gantt.resources" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', fontSize: 13 }}>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
            Currency
            <input
              value={currency}
              maxLength={3}
              disabled={busy}
              aria-label="Currency"
              placeholder="GBP"
              onChange={(e) => setCurrency(e.target.value.toUpperCase())}
              onBlur={() => currency !== plan.currency && onSettings({ currency })}
              style={{ ...inputStyle, width: 64 }}
            />
          </label>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
            Budget
            <NumberField value={plan.budget || 0} label="Budget" width={120} max={1e12} disabled={busy} onCommit={(n) => onSettings({ budget: n })} />
          </label>
          <span style={{ marginLeft: 'auto' }}>
            Plan cost <b>{money(plan.summary.cost, plan.currency)}</b>
            {plan.budget ? (
              <span style={{ color: plan.summary.cost > plan.budget ? 'var(--danger-strong)' : 'var(--text-muted)' }}>
                {' '}
                of {money(plan.budget, plan.currency)}
                {plan.summary.cost > plan.budget ? `: ${money(plan.summary.cost - plan.budget, plan.currency)} over` : ''}
              </span>
            ) : null}
          </span>
        </div>
        {plan.resources.length === 0 ? (
          <span style={mutedStyle}>Nobody has work on the plan yet. Assign people to tasks, in a task's panel or on its card on the board, and they are listed here.</span>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={head}>Who</th>
                  <th style={head}>Day rate</th>
                  <th style={head}>Capacity %</th>
                  <th style={head}>Tasks</th>
                  <th style={head}>Work (days)</th>
                  <th style={head}>Cost</th>
                  <th style={head}>Peak</th>
                </tr>
              </thead>
              <tbody>
                {plan.resources.map((r) => (
                  <tr key={`${r.type}:${r.id}`} data-resource={r.id}>
                    <td style={cell}>
                      {r.name || r.id}
                      {r.type === 'agent' && <span style={mutedStyle}> · agent</span>}
                      {r.type === 'planned' && <span style={mutedStyle}> · planned</span>}
                      {r.overallocations.map((o) => (
                        <div key={o.start} style={{ fontSize: 12, color: 'var(--danger-strong)' }}>
                          {o.units}% on {spanLabel(o.start, o.end, plan.today)}
                        </div>
                      ))}
                      {r.time_off.map((t) => (
                        <div key={t.id} data-time-off={t.id} style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                          Away {spanLabel(t.start, t.end, plan.today)}
                          {t.name ? ` · ${t.name}` : ''}
                        </div>
                      ))}
                    </td>
                    <td style={cell}>
                      <NumberField value={r.daily_rate} label={`Day rate of ${r.name || r.id}`} width={96} disabled={busy} onCommit={(n) => onResource(r, { daily_rate: n, capacity: r.capacity })} />
                    </td>
                    <td style={cell}>
                      <NumberField value={r.capacity} label={`Capacity of ${r.name || r.id}, in percent`} width={72} min={1} max={1000} disabled={busy} onCommit={(n) => onResource(r, { daily_rate: r.daily_rate, capacity: n })} />
                    </td>
                    <td style={cell}>{r.tasks}</td>
                    <td style={cell}>{r.work_days}</td>
                    <td style={cell}>{money(r.cost, plan.currency)}</td>
                    <td style={cell}>{r.overallocations.length > 0 ? <Chip tone="danger">{r.peak}%</Chip> : `${r.peak}%`}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {sizing && onPlanned && <PlanPlanned plan={plan} busy={busy} onPlanned={onPlanned} />}
        <h3 style={eyebrowStyle}>Levelling</h3>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
          <span style={{ ...mutedStyle, flex: '1 1 320px' }}>
            {over.length === 0
              ? 'Nobody is asked for more than their capacity on any day.'
              : `${over.length} ${over.length === 1 ? 'is' : 'are'} asked for more than their capacity. Levelling delays the tasks with the most float until nobody is; the tasks those drive follow. No task moves earlier.`}
          </span>
          <button type="button" disabled={busy || over.length === 0} onClick={onLevel} data-level style={buttonStyle('secondary', { disabled: busy || over.length === 0 })}>
            Level the plan
          </button>
          <button type="button" onClick={onClose} style={buttonStyle('primary')}>
            Done
          </button>
        </div>
      </div>
    </Modal>
  );
};
