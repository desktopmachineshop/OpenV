// Planned resources, in the Resources dialog: placeholders for people to
// be chosen later. One has a name, a day rate and a capacity, tasks are
// assigned to it like anyone else, and it costs and loads the plan the same
// way, so the plan can be sized before it is staffed. Staffing one gives its
// tasks to the person or agent chosen, who takes its place.
import React, { useState } from 'react';
import type { PlanResourceLoad, ProjectPlan } from '../../api/client';
import { buttonStyle, eyebrowStyle, mutedStyle } from '../ui';

/** What can be done with a planned resource. */
export type PlannedAction =
  | { kind: 'add'; name: string; daily_rate: number; capacity: number }
  | { kind: 'remove'; id: string }
  | { kind: 'staff'; id: string; with: { type: 'user' | 'agent'; id: string } };

const inputStyle: React.CSSProperties = { padding: '4px 6px', minHeight: 30, boxSizing: 'border-box' };

export const PlanPlanned: React.FC<{ plan: ProjectPlan; busy?: boolean; onPlanned: (action: PlannedAction) => void }> = ({ plan, busy, onPlanned }) => {
  const [name, setName] = useState('');
  const [rate, setRate] = useState('');
  const [capacity, setCapacity] = useState('100');
  const planned = plan.resources.filter((r) => r.type === 'planned');
  const people = plan.people.filter((p) => p.type !== 'planned');
  const dailyRate = rate.trim() === '' ? 0 : Number(rate);
  const share = Number(capacity);
  const valid = name.trim() !== '' && dailyRate >= 0 && Number.isInteger(share) && share >= 1 && share <= 1000;
  const label = (r: PlanResourceLoad) => r.planned_name || r.name || r.id;
  return (
    <section aria-label="Planned resources" data-help-key="gantt.planned" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <h3 style={eyebrowStyle}>Planned resources · {planned.length}</h3>
      <span style={mutedStyle}>
        A placeholder for someone you will choose later: a welder, a second developer. Give it a rate and a capacity, assign tasks to it, and the plan's cost, load and effort-driven durations count it like a person. When you know who it is, staff it: its tasks go to them.
      </span>
      {planned.map((r) => (
        <div key={r.id} data-planned={r.id} style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', fontSize: 13, minHeight: 32 }}>
          <b style={{ flex: '1 1 160px', minWidth: 0, overflowWrap: 'anywhere' }}>{label(r)}</b>
          <span style={mutedStyle}>
            {r.tasks} task{r.tasks === 1 ? '' : 's'} · {r.work_days} days
          </span>
          <select
            value=""
            disabled={busy || people.length === 0}
            aria-label={`Staff ${label(r)} with`}
            onChange={(e) => {
              const key = e.target.value;
              if (!key) return;
              onPlanned({ kind: 'staff', id: r.id, with: { type: key.slice(0, key.indexOf(':')) as 'user' | 'agent', id: key.slice(key.indexOf(':') + 1) } });
            }}
            style={{ ...inputStyle, width: 'auto', maxWidth: 220 }}
          >
            <option value="">Staff with…</option>
            {people.map((p) => (
              <option key={`${p.type}:${p.id}`} value={`${p.type}:${p.id}`}>
                {p.name || p.id}
                {p.type === 'agent' ? ' (agent)' : ''}
              </option>
            ))}
          </select>
          <button type="button" disabled={busy} aria-label={`Remove ${label(r)}`} onClick={() => onPlanned({ kind: 'remove', id: r.id })} style={buttonStyle('ghost', { compact: true, disabled: busy })}>
            Remove
          </button>
        </div>
      ))}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!valid) return;
          onPlanned({ kind: 'add', name: name.trim(), daily_rate: dailyRate, capacity: share });
          setName('');
          setRate('');
          setCapacity('100');
        }}
        style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center' }}
      >
        <input value={name} maxLength={120} onChange={(e) => setName(e.target.value)} placeholder="Role or name, e.g. Welder" aria-label="Name of the new planned resource" style={{ ...inputStyle, flex: '1 1 180px', minWidth: 0 }} />
        <input type="number" min={0} value={rate} onChange={(e) => setRate(e.target.value)} placeholder="Day rate" aria-label="Day rate of the new planned resource" style={{ ...inputStyle, width: 96, flex: 'none' }} />
        <input type="number" min={1} max={1000} value={capacity} onChange={(e) => setCapacity(e.target.value)} aria-label="Capacity of the new planned resource, in percent" style={{ ...inputStyle, width: 72, flex: 'none' }} />
        <span style={mutedStyle}>%</span>
        <button type="submit" disabled={busy || !valid} style={buttonStyle('secondary', { disabled: busy || !valid })}>
          Add planned resource
        </button>
      </form>
    </section>
  );
};
