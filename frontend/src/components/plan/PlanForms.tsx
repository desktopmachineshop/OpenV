// The Plan page's add forms and its export control: a new task (a card on
// the board, placed on the plan), a new milestone from a preset of
// criteria, and the plan as a file.
import React, { useState } from 'react';
import type { PlanCriterion, PlanExportFormat, PlanMilestoneInput, ProjectPlan } from '../../api/client';
import { buttonStyle, panelStyle } from '../ui';
import { CriteriaEditor } from './MilestonePanel';
import { MILESTONE_PRESETS } from './planModel';
import { fieldStyle, inputStyle } from './TaskPanel';

const formStyle: React.CSSProperties = { ...panelStyle, padding: 12, display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' };

export interface NewTask {
  title: string;
  start: string;
  end: string;
}

export const AddTaskForm: React.FC<{ busy: boolean; onSubmit: (t: NewTask) => void; onCancel: () => void }> = ({ busy, onSubmit, onCancel }) => {
  const [v, setV] = useState<NewTask>({ title: '', start: '', end: '' });
  const bad = Boolean(v.start && v.end && v.start > v.end);
  const off = busy || !v.title.trim() || bad;
  return (
    <form
      aria-label="Add task"
      data-help-key="gantt.add-task"
      onSubmit={(e) => {
        e.preventDefault();
        if (!off) onSubmit({ ...v, title: v.title.trim() });
      }}
      style={formStyle}
    >
      <label style={{ ...fieldStyle, flex: '1 1 220px' }}>
        Task
        <input value={v.title} onChange={(e) => setV({ ...v, title: e.target.value })} placeholder="What needs doing" required autoFocus style={inputStyle} />
      </label>
      <label style={fieldStyle}>
        Start
        <input type="date" value={v.start} max={v.end || undefined} onChange={(e) => setV({ ...v, start: e.target.value })} />
      </label>
      <label style={fieldStyle}>
        Due
        <input type="date" value={v.end} min={v.start || undefined} onChange={(e) => setV({ ...v, end: e.target.value })} />
      </label>
      <div style={{ display: 'flex', gap: 8 }}>
        <button type="submit" disabled={off} style={buttonStyle('primary', { disabled: off })}>
          Add task
        </button>
        <button type="button" onClick={onCancel} style={buttonStyle('secondary')}>
          Cancel
        </button>
      </div>
    </form>
  );
};

export const AddMilestoneForm: React.FC<{
  plan: ProjectPlan;
  busy: boolean;
  onSubmit: (m: PlanMilestoneInput) => void;
  onCancel: () => void;
}> = ({ plan, busy, onSubmit, onCancel }) => {
  const [preset, setPreset] = useState('definition');
  const [name, setName] = useState('Definition complete');
  const [date, setDate] = useState('');
  const [criteria, setCriteria] = useState<PlanCriterion[]>(MILESTONE_PRESETS[1].criteria);
  const choose = (key: string) => {
    const p = MILESTONE_PRESETS.find((x) => x.key === key)!;
    // A name the reader typed is kept; a preset's own is replaced by the next preset's.
    if (!name.trim() || MILESTONE_PRESETS.some((x) => x.name === name)) setName(p.name);
    setPreset(key);
    setCriteria(p.criteria);
  };
  const off = busy || !name.trim();
  return (
    <form
      aria-label="Add milestone"
      data-help-key="gantt.add-milestone"
      onSubmit={(e) => {
        e.preventDefault();
        if (!off) onSubmit({ name: name.trim(), ...(date ? { target_date: date } : {}), criteria });
      }}
      style={{ ...formStyle, alignItems: 'flex-start' }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10, flex: '1 1 240px', minWidth: 0 }}>
        <label style={fieldStyle}>
          Start from
          <select value={preset} onChange={(e) => choose(e.target.value)} style={inputStyle}>
            {MILESTONE_PRESETS.map((p) => (
              <option key={p.key} value={p.key}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
        <label style={fieldStyle}>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Definition complete" required style={inputStyle} />
        </label>
        <label style={fieldStyle}>
          Date
          <input type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        </label>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flex: '2 1 320px', minWidth: 0 }}>
        <span style={fieldStyle}>Met when</span>
        <CriteriaEditor plan={plan} value={criteria} onChange={setCriteria} />
        <div style={{ display: 'flex', gap: 8 }}>
          <button type="submit" disabled={off} style={buttonStyle('primary', { disabled: off })}>
            Add milestone
          </button>
          <button type="button" onClick={onCancel} style={buttonStyle('secondary')}>
            Cancel
          </button>
        </div>
      </div>
    </form>
  );
};

const FORMATS: { value: PlanExportFormat; label: string }[] = [
  { value: 'pdf', label: 'PDF (the chart)' },
  { value: 'excel', label: 'Excel' },
  { value: 'csv', label: 'CSV' },
  { value: 'json', label: 'JSON' },
];

/** Export as a select that acts: choosing a format saves the file. */
export const ExportControl: React.FC<{ busy: boolean; onExport: (f: PlanExportFormat) => void }> = ({ busy, onExport }) => (
  <select
    value=""
    disabled={busy}
    aria-label="Export the plan"
    data-help-key="gantt.export"
    onChange={(e) => e.target.value && onExport(e.target.value as PlanExportFormat)}
    style={{ ...buttonStyle('secondary'), width: 'auto', paddingRight: 4 }}
  >
    <option value="">{busy ? 'Exporting…' : 'Export…'}</option>
    {FORMATS.map((f) => (
      <option key={f.value} value={f.value}>
        {f.label}
      </option>
    ))}
  </select>
);
