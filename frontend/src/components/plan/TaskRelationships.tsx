// A task's relationships, in its panel (TaskPanel.tsx): every task it is
// related to, whichever of the two drives the other, each worded from this
// task's side ("finish to start" on the task that comes first, "start to
// finish" on the one that follows), so
// a relationship is read, changed and removed from either task.
import React, { useEffect, useMemo, useState } from 'react';
import type { PlanRelationship, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, Picker, buttonStyle, eyebrowStyle, mutedStyle, type PickerOption } from '../ui';
import { spanLabel } from './ganttScale';
import { RELATIONSHIP_BROKEN, RELATIONSHIP_VIEWS, RelationshipView, relationshipViewLabel, viewKey } from './planModel';

const fieldStyle: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', minWidth: 0 };
const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

const fromKey = (key: string): RelationshipView => RELATIONSHIP_VIEWS.find((v) => viewKey(v) === key) ?? RELATIONSHIP_VIEWS[0];

const ViewSelect: React.FC<{ value: RelationshipView; label: string; disabled?: boolean; onChange: (v: RelationshipView) => void }> = ({
  value,
  label,
  disabled,
  onChange,
}) => (
  <select value={viewKey(value)} disabled={disabled} aria-label={label} onChange={(e) => onChange(fromKey(e.target.value))} style={inputStyle}>
    <optgroup label="That task drives this one">
      {RELATIONSHIP_VIEWS.filter((v) => v.direction === 'driven_by').map((v) => (
        <option key={viewKey(v)} value={viewKey(v)}>
          {v.label} ({v.type})
        </option>
      ))}
    </optgroup>
    <optgroup label="This task drives that one">
      {RELATIONSHIP_VIEWS.filter((v) => v.direction === 'drives').map((v) => (
        <option key={viewKey(v)} value={viewKey(v)}>
          {v.label} ({v.type})
        </option>
      ))}
    </optgroup>
  </select>
);

/** A whole number of working days, or null for text that is not one. */
const wholeDays = (text: string): number | null => (/^-?\d{1,4}$/.test(text.trim()) ? Number(text.trim()) : text.trim() === '' ? 0 : null);

/**
 * A relationship's offset: the working days the driven task keeps after
 * what the type asks for, or before it when negative. With onCommit it
 * saves when the field is left or Enter is pressed; with onChange it is a
 * field of the add form.
 */
const OffsetField: React.FC<{ value: number; label: string; disabled?: boolean; onCommit?: (days: number) => void; onChange?: (days: number) => void }> = ({
  value,
  label,
  disabled,
  onCommit,
  onChange,
}) => {
  const [text, setText] = useState(String(value));
  useEffect(() => setText(String(value)), [value]);
  const commit = () => {
    const days = wholeDays(text);
    if (days === null) setText(String(value));
    else if (days !== value) onCommit?.(days);
  };
  return (
    <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)' }}>
      Offset
      <input
        type="number"
        step={1}
        value={text}
        disabled={disabled}
        aria-label={label}
        onChange={(e) => {
          setText(e.target.value);
          const days = wholeDays(e.target.value);
          if (days !== null) onChange?.(days);
        }}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            commit();
          }
        }}
        style={{ ...inputStyle, width: 72, flex: 'none' }}
      />
      <span style={mutedStyle}>working days{value > 0 ? ' later' : value < 0 ? ' earlier' : ''}</span>
    </label>
  );
};

export interface TaskRelationshipsProps {
  plan: ProjectPlan;
  task: PlanTask;
  editable: boolean;
  busy: boolean;
  /** Makes or changes the relationship to another task, with its offset in working days. */
  onRelate: (task: PlanTask, otherId: string, view: RelationshipView, lagDays: number) => void;
  onUnrelate: (task: PlanTask, otherId: string) => void;
  onSelectTask: (task: PlanTask) => void;
}

export const TaskRelationships: React.FC<TaskRelationshipsProps> = ({ plan, task, editable, busy, onRelate, onUnrelate, onSelectTask }) => {
  const [adding, setAdding] = useState<RelationshipView>(RELATIONSHIP_VIEWS[0]);
  const [addingLag, setAddingLag] = useState(0);
  const byId = useMemo(() => new Map(plan.tasks.map((t) => [t.id, t])), [plan.tasks]);
  const rows: { rel: PlanRelationship; view: RelationshipView }[] = [
    ...task.relationships.map((rel) => ({ rel, view: { direction: 'driven_by' as const, type: rel.type } })),
    ...task.drives.map((rel) => ({ rel, view: { direction: 'drives' as const, type: rel.type } })),
  ];
  const candidates = useMemo<PickerOption[]>(() => {
    const has = new Set([task.id, ...task.relationships.map((r) => r.task_id), ...task.drives.map((r) => r.task_id)]);
    return plan.tasks
      .filter((t) => !has.has(t.id))
      .map((t) => ({ value: t.id, label: `${t.wbs} ${t.title}`, meta: t.span_start ? spanLabel(t.span_start, t.span_end, plan.today) : 'Not scheduled', depth: t.depth }));
  }, [plan.tasks, plan.today, task.id, task.relationships, task.drives]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderBottom: '1px solid var(--border-soft)' }} data-help-key="gantt.relationships">
      <h3 style={eyebrowStyle}>Relationships · {rows.length}</h3>
      {rows.length === 0 && <span style={mutedStyle}>None: this task neither waits on another nor holds one up.</span>}
      {rows.map(({ rel, view }) => {
        const other = byId.get(rel.task_id);
        if (!other) return null;
        return (
          <div key={rel.task_id} data-relationship={rel.task_id} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <button
                type="button"
                onClick={() => onSelectTask(other)}
                style={{ flex: 1, minWidth: 0, padding: 0, border: 'none', background: 'none', font: 'inherit', color: 'var(--accent-text)', textAlign: 'left', cursor: 'pointer', overflowWrap: 'anywhere' }}
              >
                {other.wbs} {other.title}
              </button>
              {rel.conflict && (
                <Chip tone="danger" title={view.direction === 'driven_by' ? RELATIONSHIP_BROKEN[rel.type] : `The dates break it: this task does not yet do what "${relationshipViewLabel(view).toLowerCase()}" says`}>
                  Broken
                </Chip>
              )}
              {editable && (
                <button type="button" disabled={busy} aria-label={`Remove the relationship to ${other.title}`} onClick={() => onUnrelate(task, other.id)} style={buttonStyle('ghost', { compact: true, disabled: busy })}>
                  ✕
                </button>
              )}
            </div>
            {editable ? (
              <>
                <ViewSelect value={view} disabled={busy} label={`How this task relates to ${other.title}`} onChange={(v) => onRelate(task, other.id, v, rel.lag_days)} />
                <OffsetField value={rel.lag_days} disabled={busy} label={`Offset of the relationship to ${other.title}, in working days`} onCommit={(days) => onRelate(task, other.id, view, days)} />
              </>
            ) : (
              <span style={mutedStyle}>
                {relationshipViewLabel(view)}
                {rel.lag_days ? `, ${Math.abs(rel.lag_days)} working ${Math.abs(rel.lag_days) === 1 ? 'day' : 'days'} ${rel.lag_days > 0 ? 'later' : 'earlier'}` : ''}
              </span>
            )}
          </div>
        );
      })}
      {editable && candidates.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, paddingTop: 4 }}>
          <label htmlFor="gantt-relationship-add" style={fieldStyle}>
            Add a relationship from this task
          </label>
          <ViewSelect value={adding} label="How this task relates to the task chosen" onChange={setAdding} />
          <OffsetField value={addingLag} label="Offset of the new relationship, in working days" onChange={setAddingLag} />
          <Picker
            id="gantt-relationship-add"
            value=""
            options={candidates}
            searchable
            disabled={busy}
            placeholder="Search for the other task…"
            noneLabel="Choose a task…"
            onChange={(id) => {
              if (!id) return;
              onRelate(task, id, adding, addingLag);
              setAddingLag(0);
            }}
          />
          <span style={mutedStyle}>Or drag from the + at an end of a bar on the chart to another bar.</span>
        </div>
      )}
    </div>
  );
};
