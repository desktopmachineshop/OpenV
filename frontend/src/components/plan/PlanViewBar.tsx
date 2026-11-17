// The bar under the chart's header: find tasks by name, leave some out,
// sort and group them, choose the columns beside their names, move the
// selected task up, down, in or out of the outline, and undo or redo an
// edit.
import React from 'react';
import type { PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { buttonStyle } from '../ui';
import type { PlanHistory } from './planHistory';
import { COLUMNS, FILTERS, GROUPS, PlanView, RowMove, SORTS, isOutline, moveChange, rememberDated } from './planView';

const control: React.CSSProperties = { minHeight: 28, height: 28, padding: '2px 6px', boxSizing: 'border-box', fontSize: 12, width: 'auto' };

const MOVES: { move: RowMove; label: string; glyph: string }[] = [
  { move: 'up', label: 'Move up', glyph: '↑' },
  { move: 'down', label: 'Move down', glyph: '↓' },
  { move: 'outdent', label: 'Outdent: make it a task of the level above', glyph: '⇤' },
  { move: 'indent', label: 'Indent: make it a sub-task of the task above', glyph: '⇥' },
];

export const PlanViewBar: React.FC<{
  plan: ProjectPlan;
  view: PlanView;
  onView: (view: PlanView) => void;
  selectedTaskId: string;
  busy?: boolean;
  /** Set where the reader may edit: the move buttons and undo. */
  onEdit?: (t: PlanTask, change: PlanScheduleInput) => void;
  history?: Pick<PlanHistory, 'undo' | 'redo' | 'canUndo' | 'canRedo'>;
}> = ({ plan, view, onView, selectedTaskId, busy, onEdit, history }) => {
  const selected = plan.tasks.find((t) => t.id === selectedTaskId);
  const set = <K extends keyof PlanView>(key: K, value: PlanView[K]) => onView({ ...view, [key]: value });
  const icon = (off: boolean): React.CSSProperties => ({ ...buttonStyle('secondary', { compact: true, disabled: off }), minWidth: 28, padding: '2px 6px' });
  return (
    <div data-help-key="gantt.view" style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap', padding: '6px 12px', borderBottom: '1px solid var(--border-soft)', fontSize: 12 }}>
      <input type="search" value={view.search} onChange={(e) => set('search', e.target.value)} placeholder="Find a task…" aria-label="Find a task" style={{ ...control, width: 150 }} />
      <select value={view.filter} onChange={(e) => set('filter', e.target.value as PlanView['filter'])} aria-label="Filter" style={control}>
        {FILTERS.map((f) => (
          <option key={f.value} value={f.value}>
            {f.label}
          </option>
        ))}
      </select>
      <label title="Leave out the tasks with no dates, which have nothing to draw on the chart" style={{ display: 'inline-flex', alignItems: 'center', gap: 4, margin: 0, fontWeight: 400, whiteSpace: 'nowrap', cursor: 'pointer' }}>
        <input
          type="checkbox"
          checked={view.dated}
          onChange={(e) => {
            rememberDated(e.target.checked);
            set('dated', e.target.checked);
          }}
          style={{ width: 'auto', margin: 0 }}
        />
        Dated only
      </label>
      <select value={view.sort} onChange={(e) => set('sort', e.target.value as PlanView['sort'])} aria-label="Sort" style={control}>
        {SORTS.map((s) => (
          <option key={s.value} value={s.value}>
            Sort: {s.label}
          </option>
        ))}
      </select>
      <select value={view.group} onChange={(e) => set('group', e.target.value as PlanView['group'])} aria-label="Group" style={control}>
        {GROUPS.map((g) => (
          <option key={g.value} value={g.value}>
            {g.value === 'none' ? g.label : `Group: ${g.label}`}
          </option>
        ))}
      </select>
      <details data-columns style={{ position: 'relative' }}>
        <summary style={{ ...buttonStyle('secondary', { compact: true }), listStyle: 'none', cursor: 'pointer' }}>Columns{view.columns.length ? ` · ${view.columns.length}` : ''}</summary>
        <div style={{ position: 'absolute', zIndex: 20, top: '100%', left: 0, marginTop: 4, padding: 8, display: 'flex', flexDirection: 'column', gap: 4, minWidth: 140, background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 6, boxShadow: '0 4px 14px rgba(0,0,0,0.12)' }}>
          {COLUMNS.map((c) => (
            <label key={c.key} style={{ display: 'flex', alignItems: 'center', gap: 6, whiteSpace: 'nowrap' }}>
              <input
                type="checkbox"
                checked={view.columns.includes(c.key)}
                onChange={(e) => set('columns', e.target.checked ? [...view.columns, c.key] : view.columns.filter((k) => k !== c.key))}
                style={{ width: 'auto', flex: 'none', margin: 0 }}
              />
              {c.label}
            </label>
          ))}
        </div>
      </details>
      {onEdit && (
        <span role="group" aria-label="Move the selected task" style={{ display: 'inline-flex', gap: 2, marginLeft: 6 }}>
          {MOVES.map((m) => {
            const change = selected && isOutline(view) ? moveChange(plan.tasks, selected.id, m.move) : null;
            const off = busy || !change;
            return (
              <button key={m.move} type="button" data-move={m.move} disabled={off} aria-label={m.label} title={selected ? m.label : 'Select a task to move it'} onClick={() => selected && change && onEdit(selected, change)} style={icon(off)}>
                {m.glyph}
              </button>
            );
          })}
        </span>
      )}
      {history && (
        <span style={{ display: 'inline-flex', gap: 2, marginLeft: 'auto' }}>
          <button type="button" data-undo disabled={busy || !history.canUndo} onClick={history.undo} title="Undo the last edit to a task (Ctrl+Z)" style={buttonStyle('secondary', { compact: true, disabled: busy || !history.canUndo })}>
            ↶ Undo
          </button>
          <button type="button" data-redo disabled={busy || !history.canRedo} onClick={history.redo} title="Redo (Ctrl+Y)" style={buttonStyle('secondary', { compact: true, disabled: busy || !history.canRedo })}>
            ↷ Redo
          </button>
        </span>
      )}
    </div>
  );
};
