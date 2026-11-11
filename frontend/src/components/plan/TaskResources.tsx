// Who does a task, in its panel (TaskPanel.tsx): the people and agents
// assigned to it on the plan, each for a share of their time; whether its
// effort decides its duration; its fixed cost, and what it costs in all.
import React, { useEffect, useMemo, useState } from 'react';
import type { PlanAssignment, PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, Picker, buttonStyle, eyebrowStyle, mutedStyle, type PickerOption } from '../ui';
import { money, personName } from './planModel';

const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

/** A share of someone's time, saved when the field is left or Enter is pressed. */
const Units: React.FC<{ value: number; label: string; disabled?: boolean; onCommit: (units: number) => void }> = ({ value, label, disabled, onCommit }) => {
  const [text, setText] = useState(String(value));
  useEffect(() => setText(String(value)), [value]);
  const commit = () => {
    const n = Number(text);
    if (!Number.isInteger(n) || n < 1 || n > 1000) setText(String(value));
    else if (n !== value) onCommit(n);
  };
  return (
    <input
      type="number"
      min={1}
      max={1000}
      step={5}
      value={text}
      disabled={disabled}
      aria-label={label}
      onChange={(e) => setText(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          commit();
        }
      }}
      style={{ ...inputStyle, width: 68, flex: 'none' }}
    />
  );
};

export const TaskResources: React.FC<{
  plan: ProjectPlan;
  task: PlanTask;
  editable: boolean;
  busy: boolean;
  onSchedule: (task: PlanTask, change: PlanScheduleInput) => void;
}> = ({ plan, task, editable, busy, onSchedule }) => {
  const [fixed, setFixed] = useState(task.fixed_cost ? String(task.fixed_cost) : '');
  useEffect(() => setFixed(task.fixed_cost ? String(task.fixed_cost) : ''), [task.id, task.fixed_cost]);
  const assigned = task.assignments;
  const save = (assignments: PlanAssignment[]) => onSchedule(task, { assignments });
  const candidates = useMemo<PickerOption[]>(() => {
    const has = new Set(assigned.map((a) => `${a.type}:${a.id}`));
    return plan.people
      .filter((p) => !has.has(`${p.type}:${p.id}`))
      .map((p) => ({ value: `${p.type}:${p.id}`, label: p.name || p.id, meta: p.type === 'agent' ? 'Agent' : undefined }));
  }, [plan.people, assigned]);
  const commitFixed = () => {
    const n = fixed.trim() === '' ? 0 : Number(fixed);
    if (!(n >= 0)) setFixed(task.fixed_cost ? String(task.fixed_cost) : '');
    else if (n !== task.fixed_cost) onSchedule(task, { fixed_cost: n });
  };
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderBottom: '1px solid var(--border-soft)' }} data-help-key="gantt.resources">
      <h3 style={eyebrowStyle}>Resources · {assigned.length}</h3>
      {assigned.length === 0 && <span style={mutedStyle}>Nobody is on this task. Assign the people and agents who do it, each for a share of their time.</span>}
      {assigned.map((a) => (
        <div key={`${a.type}:${a.id}`} data-assignment={a.id} style={{ display: 'flex', alignItems: 'center', gap: 6, minHeight: 32 }}>
          <span style={{ flex: 1, minWidth: 0, fontSize: 13, overflowWrap: 'anywhere' }}>
            {personName(plan.people, a)}
            {a.type === 'agent' && <span style={mutedStyle}> · agent</span>}
          </span>
          {editable ? (
            <>
              <Units
                value={a.units}
                disabled={busy}
                label={`Share of ${personName(plan.people, a)}'s time, in percent`}
                onCommit={(units) => save(assigned.map((x) => (x.type === a.type && x.id === a.id ? { ...x, units } : x)))}
              />
              <span style={mutedStyle}>%</span>
              <button
                type="button"
                disabled={busy}
                aria-label={`Take ${personName(plan.people, a)} off this task`}
                onClick={() => save(assigned.filter((x) => !(x.type === a.type && x.id === a.id)))}
                style={buttonStyle('ghost', { compact: true, disabled: busy })}
              >
                ✕
              </button>
            </>
          ) : (
            <span style={mutedStyle}>{a.units}%</span>
          )}
        </div>
      ))}
      {task.overallocated && (
        <Chip tone="danger" title="Someone on this task is asked for more than their capacity while it runs. Resources, above the chart, shows who and when, and can level the plan.">
          Over-allocated
        </Chip>
      )}
      {editable && candidates.length > 0 && (
        <Picker
          id="gantt-assign"
          value=""
          options={candidates}
          searchable
          disabled={busy}
          placeholder="Search for a person or an agent…"
          noneLabel="Assign someone…"
          onChange={(key) => {
            if (!key) return;
            const [type, id] = [key.slice(0, key.indexOf(':')) as PlanAssignment['type'], key.slice(key.indexOf(':') + 1)];
            save([...assigned, { type, id, units: 100 }]);
          }}
        />
      )}
      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }} title="Its effort, shared among the people on it, decides how long the task runs">
        <input
          type="checkbox"
          checked={task.effort_driven}
          disabled={!editable || busy}
          onChange={(e) => onSchedule(task, { effort_driven: e.target.checked })}
          style={{ width: 'auto', flex: 'none', margin: 0 }}
        />
        Effort decides the duration
      </label>
      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
        Fixed cost
        <input
          type="number"
          min={0}
          step={50}
          value={fixed}
          disabled={!editable || busy}
          placeholder="None"
          aria-label="Fixed cost"
          onChange={(e) => setFixed(e.target.value)}
          onBlur={commitFixed}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              commitFixed();
            }
          }}
          style={{ ...inputStyle, width: 110, flex: 'none' }}
        />
        <span style={{ ...mutedStyle, fontWeight: 400, marginLeft: 'auto' }}>
          Cost <b style={{ color: 'var(--text)' }}>{money(task.cost, plan.currency)}</b>
        </span>
      </label>
    </div>
  );
};
