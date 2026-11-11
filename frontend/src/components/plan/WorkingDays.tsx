// The plan's working days: a toggle per day of the week. A task's duration
// and float count only the days switched on, and the chart shades the rest.
import React from 'react';
import { mutedStyle } from '../ui';
import { WEEKDAYS } from './planModel';

export const WorkingDays: React.FC<{
  days: number[];
  /** Absent where the reader cannot edit: the days are shown, not toggled. */
  onChange?: (days: number[]) => void;
  busy?: boolean;
}> = ({ days, onChange, busy }) => (
  <span role="group" aria-label="Working days" data-help-key="gantt.working-days" style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
    <span style={mutedStyle}>Working days</span>
    <span style={{ display: 'inline-flex', border: '1px solid var(--border)', borderRadius: 4, overflow: 'hidden' }}>
      {WEEKDAYS.map((w) => {
        const on = days.includes(w.day);
        // A plan works at least one day.
        const last = on && days.length === 1;
        return (
          <button
            key={w.day}
            type="button"
            aria-pressed={on}
            aria-label={w.name}
            title={last ? `${w.name}: a plan works at least one day` : `${w.name}: ${on ? 'a working day' : 'not a working day'}`}
            disabled={!onChange || busy || last}
            onClick={() => onChange?.(on ? days.filter((d) => d !== w.day) : [...days, w.day].sort((a, b) => a - b))}
            style={{
              width: 24,
              height: 24,
              padding: 0,
              border: 'none',
              borderLeft: w.day > 1 ? '1px solid var(--border-soft)' : 'none',
              background: on ? 'var(--tint-blue)' : 'var(--surface-inset)',
              color: on ? 'var(--accent-text)' : 'var(--text-muted)',
              fontSize: 11,
              fontWeight: on ? 600 : 400,
              cursor: onChange && !busy && !last ? 'pointer' : 'default',
            }}
          >
            {w.short}
          </button>
        );
      })}
    </span>
  </span>
);
