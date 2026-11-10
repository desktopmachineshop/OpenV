// One task of the plan, opened beside the chart (or in a sheet on a phone):
// its dates, the task it is a sub-task of, what it depends on, and its
// to-dos, whose ticks are what moves its progress.
import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import type { PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, Meter, PanelHeader, buttonStyle, eyebrowStyle, mutedStyle, panelStyle } from '../ui';
import { spanLabel } from './ganttScale';
import { BAR_COLORS, TASK_TONE, barTone, columnLabel, dependents, descendants } from './planModel';

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

export interface TaskPanelProps {
  plan: ProjectPlan;
  task: PlanTask;
  projectId: string;
  /** False where the reader cannot edit: the fields are read as text. */
  editable: boolean;
  busy: boolean;
  onSchedule: (task: PlanTask, change: PlanScheduleInput) => void;
  onAddTodo: (task: PlanTask, text: string) => void;
  onTickTodo: (todoId: string, done: boolean) => void;
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

export const TaskPanel: React.FC<TaskPanelProps> = ({
  plan,
  task,
  projectId,
  editable,
  busy,
  onSchedule,
  onAddTodo,
  onTickTodo,
  onAddSubtask,
  onSelectTask,
  onClose,
}) => {
  const [start, setStart] = useState(task.start || '');
  const [end, setEnd] = useState(task.end || '');
  useEffect(() => {
    setStart(task.start || '');
    setEnd(task.end || '');
  }, [task.id, task.start, task.end]);
  // A tick shows at once; the plan's answer then replaces it.
  const [ticked, setTicked] = useState<Record<string, boolean>>({});
  useEffect(() => setTicked({}), [task.todos]);

  const byId = new Map(plan.tasks.map((t) => [t.id, t]));
  const below = descendants(plan.tasks, task.id);
  const parents = plan.tasks.filter((t) => t.id !== task.id && !below.has(t.id));
  const loop = dependents(plan.tasks, task.id);
  const candidates = plan.tasks.filter((t) => !loop.has(t.id) && !task.depends_on.includes(t.id));
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
              ? 'Progress comes from to-dos and sub-tasks: add some below, or move the card to Done.'
              : `${task.todos_done} of ${task.todos_total} to-dos ticked off${task.subtasks_total ? `, ${task.subtasks_done} of ${task.subtasks_total} sub-tasks done` : ''}.`}
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
        <label style={fieldStyle}>
          Sub-task of
          <select value={task.parent_id || ''} disabled={!editable || busy} onChange={(e) => onSchedule(task, { parent_id: e.target.value })} style={inputStyle}>
            <option value="">No parent: a top-level task</option>
            {parents.map((t) => (
              <option key={t.id} value={t.id}>
                {t.wbs} {t.title}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div style={sectionStyle}>
        <h3 style={eyebrowStyle}>Depends on · {task.depends_on.length}</h3>
        {task.depends_on.length === 0 && <span style={mutedStyle}>Nothing: it can start at any time.</span>}
        {task.depends_on.map((id) => {
          const on = byId.get(id);
          if (!on) return null;
          const conflict = task.conflicts.includes(id);
          return (
            <div key={id} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <button
                type="button"
                onClick={() => onSelectTask(on)}
                style={{ flex: 1, minWidth: 0, padding: 0, border: 'none', background: 'none', font: 'inherit', color: 'var(--accent-text)', textAlign: 'left', cursor: 'pointer', overflowWrap: 'anywhere' }}
              >
                {on.wbs} {on.title}
              </button>
              {conflict && <Chip tone="danger" title="This task starts before that one ends">Overlaps</Chip>}
              {editable && (
                <button
                  type="button"
                  disabled={busy}
                  aria-label={`Stop depending on ${on.title}`}
                  onClick={() => onSchedule(task, { depends_on: task.depends_on.filter((d) => d !== id) })}
                  style={buttonStyle('ghost', { compact: true, disabled: busy })}
                >
                  ✕
                </button>
              )}
            </div>
          );
        })}
        {editable && candidates.length > 0 && (
          <select
            value=""
            disabled={busy}
            aria-label="Add a task this one depends on"
            style={inputStyle}
            onChange={(e) => e.target.value && onSchedule(task, { depends_on: [...task.depends_on, e.target.value] })}
          >
            <option value="">Add a task this one waits for…</option>
            {candidates.map((t) => (
              <option key={t.id} value={t.id}>
                {t.wbs} {t.title}
              </option>
            ))}
          </select>
        )}
      </div>

      <div style={sectionStyle}>
        <h3 style={eyebrowStyle}>
          To-dos · {task.todos_done}/{task.todos_total}
        </h3>
        {task.todos.map((td) => (
          <label key={td.id} style={{ display: 'flex', gap: 8, alignItems: 'flex-start', minHeight: 28, fontSize: 13, color: td.done ? 'var(--text-muted)' : 'var(--text)', cursor: editable ? 'pointer' : 'default' }}>
            <input
              type="checkbox"
              checked={ticked[td.id] ?? td.done}
              disabled={!editable || busy}
              onChange={(e) => {
                setTicked((t) => ({ ...t, [td.id]: e.target.checked }));
                onTickTodo(td.id, e.target.checked);
              }} style={{ width: 'auto', flex: 'none', margin: '3px 0 0' }} />
            <span style={{ textDecoration: td.done ? 'line-through' : undefined, overflowWrap: 'anywhere' }}>{td.text}</span>
          </label>
        ))}
        {editable && <QuickAdd label="New to-do" placeholder="Add a to-do to tick off" busy={busy} onAdd={(text) => onAddTodo(task, text)} />}
      </div>

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
