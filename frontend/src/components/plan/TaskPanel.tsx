// One task of the plan, opened beside the chart (or in a sheet on a phone):
// its dates, the task it is a sub-task of, its relationships to the tasks
// that drive it, its to-dos and its sub-tasks. A task keeps its duration, so
// moving one of its dates moves the other; a to-do has one date, a deadline.
import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import type { PlanRelationshipType, PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, Meter, PanelHeader, Picker, buttonStyle, eyebrowStyle, mutedStyle, panelStyle, type PickerOption } from '../ui';
import { addDays, shortDate, spanLabel, toDay } from './ganttScale';
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
  onAddTodo: (task: PlanTask, text: string, dueDate: string) => void;
  onTickTodo: (todoId: string, done: boolean) => void;
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

const whole = (v: string): number => Math.max(0, Math.floor(Number(v) || 0));

/**
 * A task's schedule: its start, its due date, its duration in days and the
 * effort in it. The duration is what the task keeps: a new start moves the
 * due date, a new due date moves the start, and a new duration moves the due
 * date. Effort is a number of its own, the work in person-days.
 */
const Schedule: React.FC<Pick<TaskPanelProps, 'task' | 'editable' | 'busy' | 'onSchedule'>> = ({ task, editable, busy, onSchedule }) => {
  const initial = () => ({
    start: task.start || '',
    end: task.end || '',
    duration: task.subtasks_total > 0 && !(task.start && task.end) ? '' : task.duration_days ? String(task.duration_days) : '',
    effort: task.effort_days ? String(task.effort_days) : '',
  });
  const [v, setV] = useState(initial);
  useEffect(() => {
    setV(initial());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.id, task.start, task.end, task.duration_days, task.effort_days]);

  const days = whole(v.duration);
  const setStart = (start: string) => setV((p) => ({ ...p, start, end: start && days ? addDays(start, days - 1) : p.end }));
  const setEnd = (end: string) => setV((p) => ({ ...p, end, start: end && days ? addDays(end, 1 - days) : p.start }));
  const setDuration = (duration: string) =>
    setV((p) => {
      const n = whole(duration);
      if (!n) return { ...p, duration };
      if (p.start) return { ...p, duration, end: addDays(p.start, n - 1) };
      return { ...p, duration, start: p.end ? addDays(p.end, 1 - n) : p.start };
    });

  const was = initial();
  const changed = v.start !== was.start || v.end !== was.end || v.duration !== was.duration || v.effort !== was.effort;
  const badOrder = Boolean(v.start && v.end && v.start > v.end);
  const badEffort = v.effort !== '' && !(Number(v.effort) >= 0);
  const save = () =>
    onSchedule(task, {
      start_date: v.start,
      due_date: v.end,
      // Both dates decide the duration; without them it is kept as typed.
      duration_days: v.start && v.end ? toDay(v.end) - toDay(v.start) + 1 : days,
      effort_days: Number(v.effort) || 0,
    });

  return (
    <div style={sectionStyle} data-help-key="gantt.schedule">
      <h3 style={eyebrowStyle}>Schedule</h3>
      {task.subtasks_total > 0 && <span style={mutedStyle}>The bar spans this task's sub-tasks as well as its own dates.</span>}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <label style={fieldStyle}>
          Start
          <input type="date" value={v.start} disabled={!editable} onChange={(e) => setStart(e.target.value)} style={inputStyle} />
        </label>
        <label style={fieldStyle}>
          Due
          <input type="date" value={v.end} disabled={!editable} onChange={(e) => setEnd(e.target.value)} style={inputStyle} />
        </label>
        <label style={fieldStyle}>
          Duration (days)
          <input type="number" min={1} step={1} value={v.duration} disabled={!editable} placeholder="Not set" onChange={(e) => setDuration(e.target.value)} style={inputStyle} />
        </label>
        <label style={fieldStyle}>
          Effort (days)
          <input type="number" min={0} step={0.5} value={v.effort} disabled={!editable} placeholder="Not set" onChange={(e) => setV((p) => ({ ...p, effort: e.target.value }))} style={inputStyle} />
        </label>
      </div>
      <span style={mutedStyle}>
        The task keeps its duration: a new start moves the due date, a new due date moves the start. Change the duration to make it longer or shorter. Effort is the work in it, in person-days.
      </span>
      {badOrder && <span style={{ ...mutedStyle, color: 'var(--danger-strong)' }}>A task cannot start after it is due.</span>}
      {editable && changed && (
        <div style={{ display: 'flex', gap: 6 }}>
          <button type="button" disabled={busy || badOrder || badEffort} onClick={save} style={buttonStyle('primary', { disabled: busy || badOrder || badEffort })}>
            Save schedule
          </button>
          <button type="button" onClick={() => setV(initial())} style={buttonStyle('secondary')}>
            Cancel
          </button>
        </div>
      )}
    </div>
  );
};

/**
 * A task's to-dos: small jobs, each with one date at most, its deadline.
 * Ticking them off is what moves the task's progress. They are listed here
 * and on the board's card, never on the chart.
 */
const Todos: React.FC<Pick<TaskPanelProps, 'plan' | 'task' | 'editable' | 'busy' | 'onAddTodo' | 'onTickTodo'>> = ({
  plan,
  task,
  editable,
  busy,
  onAddTodo,
  onTickTodo,
}) => {
  const [text, setText] = useState('');
  const [due, setDue] = useState('');
  // A tick shows at once; the plan's answer then replaces it.
  const [ticked, setTicked] = useState<Record<string, boolean>>({});
  useEffect(() => setTicked({}), [task.todos]);
  return (
    <div style={sectionStyle} data-help-key="gantt.todos">
      <h3 style={eyebrowStyle}>
        To-dos · {task.todos_done}/{task.todos_total}
      </h3>
      {task.todos.length === 0 && <span style={mutedStyle}>None. A to-do is a small job, a day or two at most, with a deadline.</span>}
      {task.todos.map((td) => {
        const overdue = !td.done && td.due_date !== null && td.due_date < plan.today;
        return (
          <label key={td.id} style={{ display: 'flex', gap: 8, alignItems: 'flex-start', minHeight: 28, fontSize: 13, fontWeight: 400, color: td.done ? 'var(--text-muted)' : 'var(--text)', cursor: editable ? 'pointer' : 'default' }}>
            <input
              type="checkbox"
              checked={ticked[td.id] ?? td.done}
              disabled={!editable || busy}
              onChange={(e) => {
                setTicked((t) => ({ ...t, [td.id]: e.target.checked }));
                onTickTodo(td.id, e.target.checked);
              }}
              style={{ width: 'auto', flex: 'none', margin: '3px 0 0' }}
            />
            <span style={{ flex: 1, minWidth: 0, textDecoration: td.done ? 'line-through' : undefined, overflowWrap: 'anywhere' }}>{td.text}</span>
            {td.due_date && (
              <span style={{ ...mutedStyle, flexShrink: 0, color: overdue ? 'var(--danger-strong)' : 'var(--text-muted)' }} title={overdue ? 'Past its deadline' : 'Deadline'}>
                {shortDate(td.due_date, plan.today)}
              </span>
            )}
          </label>
        );
      })}
      {editable && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!text.trim()) return;
            onAddTodo(task, text.trim(), due);
            setText('');
            setDue('');
          }}
          style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}
        >
          <input value={text} onChange={(e) => setText(e.target.value)} placeholder="Add a to-do" aria-label="New to-do" style={{ ...inputStyle, flex: '1 1 140px', minWidth: 0 }} />
          <input type="date" value={due} onChange={(e) => setDue(e.target.value)} aria-label="Deadline of the new to-do" title="Deadline (optional)" style={{ ...inputStyle, flex: '0 1 132px', width: 'auto' }} />
          <button type="submit" disabled={busy || !text.trim()} style={buttonStyle('secondary', { disabled: busy || !text.trim() })}>
            Add
          </button>
        </form>
      )}
    </div>
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

export const TaskPanel: React.FC<TaskPanelProps> = ({
  plan,
  task,
  projectId,
  editable,
  busy,
  onSchedule,
  onAddSubtask,
  onAddTodo,
  onTickTodo,
  onSelectTask,
  onClose,
}) => {
  const parents = useMemo(() => {
    const below = descendants(plan.tasks, task.id);
    return plan.tasks.filter((t) => t.id !== task.id && !below.has(t.id)).map((t) => taskOption(t, plan.today));
  }, [plan.tasks, plan.today, task.id]);
  const subtasks = plan.tasks.filter((t) => t.parent_id === task.id);
  const tone = barTone(task);

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
          {task.critical && (
            <Chip tone="warning" title="On the critical path: it has no float, so any slip moves the plan's end">
              Critical
            </Chip>
          )}
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
              ? 'Progress comes from its to-dos and its sub-tasks, or from moving the card to Done.'
              : [
                  task.todos_total ? `${task.todos_done} of ${task.todos_total} to-dos ticked off` : '',
                  task.subtasks_total ? `${task.subtasks_done} of ${task.subtasks_total} sub-tasks done` : '',
                ]
                  .filter(Boolean)
                  .join(', ') + '.'}
        </span>
        {task.float_days !== null && !task.done && (
          <span style={mutedStyle}>
            {task.critical
              ? "No float: it is on the critical path, so a day's slip moves the end of the plan by a day."
              : `Float: ${task.float_days} ${task.float_days === 1 ? 'day' : 'days'} before it would move the end of the plan.`}
          </span>
        )}
      </div>

      <Schedule task={task} editable={editable} busy={busy} onSchedule={onSchedule} />

      <div style={sectionStyle}>
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

      <Todos plan={plan} task={task} editable={editable} busy={busy} onAddTodo={onAddTodo} onTickTodo={onTickTodo} />

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
