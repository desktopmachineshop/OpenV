// The plan's defaults, in a dialog opened from the chart's header: how its
// tasks are scheduled unless one says otherwise, and whether a new task's
// effort decides its duration. Both are the plan's settings; a task's own
// panel overrides them for that task.
import React from 'react';
import type { PlanSettings, ProjectPlan } from '../../api/client';
import { Modal, buttonStyle, eyebrowStyle, mutedStyle } from '../ui';
import type { PlanSettingsChange } from './PlanCalendar';

const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

export const PlanDefaults: React.FC<{ plan: ProjectPlan; busy?: boolean; onChange: (change: PlanSettingsChange) => void; onClose: () => void }> = ({ plan, busy, onChange, onClose }) => (
  <Modal title="Plan defaults" width={460} onClose={onClose}>
    <div data-help-key="gantt.defaults" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <span style={mutedStyle}>What a task does until it says otherwise. A task's own panel can set either for that task alone.</span>
      <h3 style={eyebrowStyle}>Scheduling mode</h3>
      <select value={plan.scheduling_mode} disabled={busy} aria-label="Default scheduling mode" onChange={(e) => onChange({ scheduling_mode: e.target.value as PlanSettings['scheduling_mode'] })} style={inputStyle}>
        <option value="manual">Manual: tasks stay where they are put, until you recalculate</option>
        <option value="auto">Automatic: tasks are placed after every change</option>
      </select>
      <h3 style={{ ...eyebrowStyle, marginTop: 6 }}>Duration</h3>
      <label style={{ display: 'flex', alignItems: 'flex-start', gap: 8, fontSize: 13 }}>
        <input type="checkbox" checked={plan.effort_driven_default} disabled={busy} aria-label="Effort decides the duration of new tasks" onChange={(e) => onChange({ effort_driven_default: e.target.checked })} style={{ width: 'auto', flex: 'none', margin: '3px 0 0' }} />
        <span>
          Effort decides the duration of new tasks
          <span style={{ ...mutedStyle, display: 'block' }}>
            A task's effort, shared among the people and planned resources on it, sets how long it runs. It applies to tasks that have not been scheduled yet; a task already scheduled keeps what it was given.
          </span>
        </span>
      </label>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <button type="button" onClick={onClose} style={buttonStyle('primary')}>
          Done
        </button>
      </div>
    </div>
  </Modal>
);
