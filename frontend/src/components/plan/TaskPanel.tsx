// One task of the plan, opened beside the chart (or in a sheet on a phone):
// its dates, the task it is a sub-task of, its relationships to the tasks
// that drive it, and its sub-tasks. Its to-dos are not here: they are ticked
// off on the board's card, and the plan only counts them.
import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import type { PlanRelationshipType, PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, Meter, PanelHeader, Picker, buttonStyle, eyebrowStyle, mutedStyle, panelStyle, type PickerOption } from '../ui';
import { spanLabel } from './ganttScale';
import {
  BAR_COLORS,
  RELATIONSHIP_BROKEN,
  RELATIONSHIP_TYPES,
  TASK_TONE,
  barTone,
  columnLabel,
  dependents,
  descendants,
  relationshipLabel,
} from './planModel';

export const fieldStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 4,
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--text-secondary)',
  minWidth: 0,
};

/** A panel's field: the global input, tightened to the kit's 32 px row. */
export const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box' };

export const sectionStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 8,
  padding: 12,
  borderBottom: '1px solid var(--border-soft)',
};

/** A task as a row of a searchable picker: found by its outline number or its title. */
export const taskOption = (t: PlanTask, today: string): PickerOption => ({
  value: t.id,
  label: `${t.wbs} ${t.title}`,
  meta: t.span_start ? spanLabel(t.span_start, t.span_end, today) : 'Not scheduled',
  depth: t.depth,
});

export interface TaskPanelProps {
  plan: ProjectPlan;
  task: PlanTask;
  projectId: string;
  /** False where the reader cannot edit: the fields are read as text. */
  editable: boolean;
  busy: boolean;
  onSchedule: (task: PlanTask, change: PlanScheduleInput) => void;
  onAddSubtask: (task: PlanTask, title: string) => void;
  onSelectTask: (task: PlanTask) => void;
  onClose: () => void;
}

/** A one-line add form: a text box and its button. */
const QuickAdd: React.FC<{ label: string; placeholder: string; busy: boolean; onAdd: (text: string) => void }> = ({
  label,
  placeholder,
  busy,
  onAdd,
}) => {
  const [text, setText] = useState('');
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (!text.trim()) return;
        onAdd(text.trim());
        setText('');
      }}
      style={{ display: 'flex', gap: 6 }}
    >
      <input value={text} onChange={(e) => setText(e.target.value)} placeholder={placeholder} aria-label={label} style={{ ...inputStyle, flex: 1, minWidth: 0 }} />
      <button type="submit" disabled={busy || !text.trim()} style={buttonStyle('secondary', { disabled: busy || !text.trim() })}>
        Add
      </button>
    </form>
  );
};

/**
 * A task's relationships: the tasks that drive it, each with its type (finish
 * to start, start to start, finish to finish), and a searchable picker that
 * adds another. Several may drive one task.
 */
const Relationships: React.FC<Pick<TaskPanelProps, 'plan' | 'task' | 'editable' | 'busy' | 'onSchedule' | 'onSelectTask'>> = ({
  plan,
  task,
  editable,
  busy,
  onSchedule,
  onSelectTask,
}) => {
  const [type, setType] = useState<PlanRelationshipType>('FS');
  const byId = useMemo(() => new Map(plan.tasks.map((t) => [t.id, t])), [plan.tasks]);
  const current = task.relationships.map(({ task_id, type: kind }) => ({ task_id, type: kind }));
  const candidates = useMemo(() => {
    const loop = dependents(plan.tasks, task.id);
    const has = new Set(task.depends_on);
    return plan.tasks.filter((t) => !loop.has(t.id) && !has.has(t.id)).map((t) => taskOption(t, plan.today));
  }, [plan.tasks, plan.today, task.id, task.depends_on]);
  const save = (relationships: PlanScheduleInput['relationships']) => onSchedule(task, { relationships });

  return (
    <div style={sectionStyle} data-help-key="gantt.relationships">
      <h3 style={eyebrowStyle}>Relationships · {task.relationships.length}</h3>
      {task.relationships.length === 0 && <span style={mutedStyle}>None: no other task drives when this one starts or finishes.</span>}
      {task.relationships.map((rel) => {
        const on = byId.get(rel.task_id);
        if (!on) return null;
        return (
          <div key={rel.task_id} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <button
                type="button"
                onClick={() => onSelectTask(on)}
                style={{ flex: 1, minWidth: 0, padding: 0, border: 'none', background: 'none', font: 'inherit', color: 'var(--accent-text)', textAlign: 'left', cursor: 'pointer', overflowWrap: 'anywhere' }}
              >
                {on.wbs} {on.title}
              </button>
              {rel.conflict && (
                <Chip tone="danger" title={RELATIONSHIP_BROKEN[rel.type]}>
                  Broken
                </Chip>
              )}
              {editable && (
                <button
                  type="button"
                  disabled={busy}
                  aria-label={`Remove the relationship to ${on.title}`}
                  onClick={() => save(current.filter((r) => r.task_id !== rel.task_id))}
                  style={buttonStyle('ghost', { compact: true, disabled: busy })}
                >
                  ✕
                </button>
              )}
            </div>
            {editable ? (
              <select
                value={rel.type}
                disabled={busy}
                aria-label={`Type of the relationship to ${on.title}`}
                onChange={(e) => save(current.map((r) => (r.task_id === rel.task_id ? { ...r, type: e.target.value as PlanRelationshipType } : r)))}
                style={inputStyle}
              >
                {RELATIONSHIP_TYPES.map((r) => (
                  <option key={r.value} value={r.value}>
                    {r.label} ({r.value})
                  </option>
                ))}
              </select>
            ) : (
              <span style={mutedStyle}>{relationshipLabel(rel.type)}</span>
            )}
          </div>
        );
      })}
      {editable && candidates.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, paddingTop: 4 }}>
          <label htmlFor="gantt-relationship-add" style={fieldStyle}>
            Add a relationship
          </label>
          <select value={type} onChange={(e) => setType(e.target.value as PlanRelationshipType)} aria-label="Type of the new relationship" style={inputStyle}>
            {RELATIONSHIP_TYPES.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label} ({r.value}): {r.hint.toLowerCase()}
              </option>
            ))}
          </select>
          <Picker
            id="gantt-relationship-add"
            value=""
            options={candidates}
            searchable
            disabled={busy}
            placeholder="Search for the task that drives this one…"
            noneLabel="Choose a task…"
            onChange={(id) => id && save([...current, { task_id: id, type }])}
          />
        </div>
      )}
    </div>
  );
};

export const TaskPanel: React.FC<TaskPanelProps> = ({ plan, task, projectId, editable, busy, onSchedule, onAddSubtask, onSelectTask, onClose }) => {
  const [start, setStart] = useState(task.start || '');
  const [end, setEnd] = useState(task.end || '');
  useEffect(() => {
    setStart(task.start || '');
    setEnd(task.end || '');
  }, [task.id, task.start, task.end]);

  const parents = useMemo(() => {
    const below = descendants(plan.tasks, task.id);
    return plan.tasks.filter((t) => t.id !== task.id && !below.has(t.id)).map((t) => taskOption(t, plan.today));
  }, [plan.tasks, plan.today, task.id]);
  const subtasks = plan.tasks.filter((t) => t.parent_id === task.id);
  const tone = barTone(task);
  const datesChanged = start !== (task.start || '') || end !== (task.end || '');
  const badOrder = Boolean(start && end && start > end);

  return (
    <aside aria-label={`Task ${task.title}`} data-help-key="gantt.task" style={{ ...panelStyle, display: 'flex', flexDirection: 'column' }}>
      <PanelHeader
        title={`Task ${task.wbs}`}
        action={
          <button type="button" onClick={onClose} aria-label="Close task" style={buttonStyle('ghost', { compact: true })}>
            ✕
          </button>
        }
      />
      <div style={sectionStyle}>
        <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text)', overflowWrap: 'anywhere' }}>{task.title}</div>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
          <Chip tone={TASK_TONE[tone]}>{task.late ? 'Late' : columnLabel(task.column)}</Chip>
          <span style={mutedStyle}>{spanLabel(task.span_start, task.span_end, plan.today)}</span>
          <Link to={`/projects/${projectId}/board`} style={{ fontSize: 12, marginLeft: 'auto' }}>
            Open the board
          </Link>
        </div>
        <Meter label="Done" value={task.progress} color={BAR_COLORS[tone].fill} />
        <span style={mutedStyle}>
          {task.done
            ? 'In Done on the board: 100%.'
            : task.todos_total + task.subtasks_total === 0
              ? 'Progress comes from the to-dos on its board card and from its sub-tasks, or from moving the card to Done.'
              : [
                  task.todos_total ? `${task.todos_done} of ${task.todos_total} to-dos ticked off on its board card` : '',
                  task.subtasks_total ? `${task.subtasks_done} of ${task.subtasks_total} sub-tasks done` : '',
                ]
                  .filter(Boolean)
                  .join(', ') + '.'}
        </span>
      </div>

      <div style={sectionStyle}>
        <h3 style={eyebrowStyle}>Schedule</h3>
        {task.subtasks_total > 0 && <span style={mutedStyle}>The bar spans this task's sub-tasks as well as its own dates.</span>}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
          <label style={fieldStyle}>
            Start
            <input type="date" value={start} max={end || undefined} disabled={!editable} onChange={(e) => setStart(e.target.value)} style={inputStyle} />
          </label>
          <label style={fieldStyle}>
            Due
            <input type="date" value={end} min={start || undefined} disabled={!editable} onChange={(e) => setEnd(e.target.value)} style={inputStyle} />
          </label>
        </div>
        {badOrder && <span style={{ ...mutedStyle, color: 'var(--danger-strong)' }}>A task cannot start after it is due.</span>}
        {editable && datesChanged && (
          <div style={{ display: 'flex', gap: 6 }}>
            <button
              type="button"
              disabled={busy || badOrder}
              onClick={() => onSchedule(task, { start_date: start, due_date: end })}
              style={buttonStyle('primary', { disabled: busy || badOrder })}
            >
              Save dates
            </button>
            <button
              type="button"
              onClick={() => {
                setStart(task.start || '');
                setEnd(task.end || '');
              }}
              style={buttonStyle('secondary')}
            >
              Cancel
            </button>
          </div>
        )}
        <label htmlFor="gantt-parent" style={fieldStyle}>
          Sub-task of
        </label>
        <Picker
          id="gantt-parent"
          value={task.parent_id || ''}
          options={parents}
          searchable
          disabled={!editable || busy}
          noneLabel="No parent: a top-level task"
          placeholder="Search for a task…"
          onChange={(id) => onSchedule(task, { parent_id: id })}
        />
      </div>

      <Relationships plan={plan} task={task} editable={editable} busy={busy} onSchedule={onSchedule} onSelectTask={onSelectTask} />

      <div style={{ ...sectionStyle, borderBottom: 'none' }}>
        <h3 style={eyebrowStyle}>
          Sub-tasks · {task.subtasks_done}/{task.subtasks_total}
        </h3>
        {subtasks.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => onSelectTask(t)}
            style={{ display: 'flex', gap: 8, alignItems: 'center', minHeight: 28, padding: 0, border: 'none', background: 'none', font: 'inherit', color: 'var(--text)', textAlign: 'left', cursor: 'pointer' }}
          >
            <span style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere' }}>
              {t.wbs} {t.title}
            </span>
            <span style={mutedStyle}>{t.progress}%</span>
          </button>
        ))}
        {editable && <QuickAdd label="New sub-task" placeholder="Add a sub-task" busy={busy} onAdd={(title) => onAddSubtask(task, title)} />}
      </div>
    </aside>
  );
};
