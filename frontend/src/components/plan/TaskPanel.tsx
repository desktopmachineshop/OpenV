// One task of the plan, opened beside the chart (or in a sheet on a phone):
// its dates, the task it is a sub-task of, its relationships (TaskRelationships.tsx),
// its to-dos and its sub-tasks. A task keeps its duration, counted in the
// plan's working days, so moving one of its dates moves the other; a task
// with sub-tasks takes its dates from them. A to-do has one date, a deadline.
import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import type { PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { Chip, Meter, PanelHeader, Picker, buttonStyle, eyebrowStyle, mutedStyle, panelStyle, type PickerOption } from '../ui';
import { shortDate, spanLabel } from './ganttScale';
import { TaskRelationships, type TaskRelationshipsProps } from './TaskRelationships';
import { BAR_COLORS, TASK_TONE, WEEKDAYS, barTone, columnLabel, descendants, workEnd, workSpan, workStart } from './planModel';

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
  onRelate: TaskRelationshipsProps['onRelate'];
  onUnrelate: TaskRelationshipsProps['onUnrelate'];
  onAddSubtask: (task: PlanTask, title: string) => void;
  onAddTodo: (task: PlanTask, text: string, dueDate: string) => void;
  onTickTodo: (todoId: string, done: boolean) => void;
  onSelectTask: (task: PlanTask) => void;
  /** Opens the task in full: its description, assignee, linked artifacts and activity. */
  onOpenTask?: (task: PlanTask) => void;
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

const plural = (n: number, one: string) => `${n} ${n === 1 ? one : `${one}s`}`;

/** The plan's working days in words: "Monday to Friday", or the days named. */
const workingDaysText = (days: number[]): string => {
  const names = WEEKDAYS.filter((w) => days.includes(w.day)).map((w) => w.name);
  const run = days.length > 2 && days.every((d, i) => i === 0 || d === days[i - 1] + 1);
  return run ? `${names[0]} to ${names[names.length - 1]}` : names.join(', ');
};

/** A task with sub-tasks: its dates, duration and effort are theirs, read here and set on them. */
const FolderSchedule: React.FC<{ task: PlanTask; today: string }> = ({ task, today }) => (
  <div style={sectionStyle} data-help-key="gantt.schedule">
    <h3 style={eyebrowStyle}>Schedule</h3>
    <div style={{ fontSize: 13, color: 'var(--text)' }}>
      {task.span_start ? spanLabel(task.span_start, task.span_end, today) : 'No dates yet'}
      {task.duration_days ? ` · ${plural(task.duration_days, 'working day')}` : ''}
      {task.effort_days ? ` · ${plural(task.effort_days, 'day')} of effort` : ''}
    </div>
    <span style={mutedStyle}>
      A task with sub-tasks has no dates of its own: it starts with its first sub-task and finishes with its last, and its effort is theirs added up. Schedule the sub-tasks to move it.
    </span>
  </div>
);

/**
 * A task's schedule: its start, its due date, its duration in working days
 * and the effort in it. The duration is what the task keeps: a new start
 * moves the due date, a new due date moves the start, and a new duration
 * moves the due date. Effort is a number of its own, the work in person-days.
 */
const Schedule: React.FC<Pick<TaskPanelProps, 'task' | 'editable' | 'busy' | 'onSchedule'> & { workingDays: number[] }> = ({
  task,
  editable,
  busy,
  onSchedule,
  workingDays,
}) => {
  const initial = () => ({
    start: task.start || '',
    end: task.end || '',
    duration: task.duration_days ? String(task.duration_days) : '',
    effort: task.effort_days ? String(task.effort_days) : '',
  });
  const [v, setV] = useState(initial);
  useEffect(() => {
    setV(initial());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.id, task.start, task.end, task.duration_days, task.effort_days]);

  const days = whole(v.duration);
  const setStart = (start: string) => setV((p) => ({ ...p, start, end: start && days ? workEnd(workingDays, start, days) : p.end }));
  const setEnd = (end: string) => setV((p) => ({ ...p, end, start: end && days ? workStart(workingDays, end, days) : p.start }));
  const setDuration = (duration: string) =>
    setV((p) => {
      const n = whole(duration);
      if (!n) return { ...p, duration };
      if (p.start) return { ...p, duration, end: workEnd(workingDays, p.start, n) };
      return { ...p, duration, start: p.end ? workStart(workingDays, p.end, n) : p.start };
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
      duration_days: v.start && v.end ? workSpan(workingDays, v.start, v.end) : days,
      effort_days: Number(v.effort) || 0,
    });

  return (
    <div style={sectionStyle} data-help-key="gantt.schedule">
      <h3 style={eyebrowStyle}>Schedule</h3>
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
          Duration (working days)
          <input type="number" min={1} step={1} value={v.duration} disabled={!editable} placeholder="Not set" onChange={(e) => setDuration(e.target.value)} style={inputStyle} />
        </label>
        <label style={fieldStyle}>
          Effort (days)
          <input type="number" min={0} step={0.5} value={v.effort} disabled={!editable} placeholder="Not set" onChange={(e) => setV((p) => ({ ...p, effort: e.target.value }))} style={inputStyle} />
        </label>
      </div>
      <span style={mutedStyle}>
        The task keeps its duration, counted in the plan's working days ({workingDaysText(workingDays)}): a new start moves the due date, a new due date moves the start. Change the duration to make it longer or shorter. Effort is the work in it, in person-days.
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

export const TaskPanel: React.FC<TaskPanelProps> = ({
  plan,
  task,
  projectId,
  editable,
  busy,
  onSchedule,
  onRelate,
  onUnrelate,
  onAddSubtask,
  onAddTodo,
  onTickTodo,
  onSelectTask,
  onOpenTask,
  onClose,
}) => {
  const parents = useMemo(() => {
    const below = descendants(plan.tasks, task.id);
    return plan.tasks.filter((t) => t.id !== task.id && !below.has(t.id)).map((t) => taskOption(t, plan.today));
  }, [plan.tasks, plan.today, task.id]);
  const subtasks = plan.tasks.filter((t) => t.parent_id === task.id);
  const milestones = useMemo<PickerOption[]>(
    () => plan.milestones.map((m) => ({ value: m.key, label: m.name, meta: m.date ? shortDate(m.date, plan.today) : 'No date' })),
    [plan.milestones, plan.today],
  );
  const inherited = task.milestone_inherited ? plan.milestones.find((m) => m.key === task.milestone) : undefined;
  const tone = barTone(task);

  return (
    <aside aria-label={`Task ${task.title}`} data-help-key="gantt.task" style={{ ...panelStyle, display: 'flex', flexDirection: 'column', flex: '1 0 auto' }}>
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
            <Chip tone="warning" title="On the critical path: the chain of tasks that decides when the plan ends">
              Critical
            </Chip>
          )}
          {task.critical_subtasks && (
            <Chip tone="warning" title="The critical path runs through its sub-tasks: a slip in one of them moves the plan's end">
              Critical sub-tasks
            </Chip>
          )}
          <span style={mutedStyle}>{spanLabel(task.span_start, task.span_end, plan.today)}</span>
          {onOpenTask ? (
            <button type="button" onClick={() => onOpenTask(task)} style={{ marginLeft: 'auto', padding: 0, border: 'none', background: 'none', font: 'inherit', fontSize: 12, color: 'var(--accent-text)', textDecoration: 'underline', cursor: 'pointer' }}>
              Open the full task
            </button>
          ) : (
            <Link to={`/projects/${projectId}/board`} style={{ fontSize: 12, marginLeft: 'auto' }}>
              Open the board
            </Link>
          )}
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
        {task.float_days !== null && !task.done && task.subtasks_total === 0 && (
          <span style={mutedStyle}>
            {task.critical && task.float_days > 0
              ? `On the critical path, with ${plural(task.float_days, 'working day')} of float before it pushes the next task.`
              : task.critical
              ? "No float: it is on the critical path, so a day's slip moves the end of the plan by a day."
              : `Float: ${plural(task.float_days, 'working day')} before it would move the end of the plan.`}
          </span>
        )}
      </div>

      {task.subtasks_total > 0 ? (
        <FolderSchedule task={task} today={plan.today} />
      ) : (
        <Schedule task={task} editable={editable} busy={busy} onSchedule={onSchedule} workingDays={plan.working_days} />
      )}

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

      <div style={sectionStyle} data-help-key="gantt.task-milestone">
        <label htmlFor="gantt-task-milestone" style={fieldStyle}>
          Milestone
        </label>
        <Picker
          id="gantt-task-milestone"
          value={task.milestone && !task.milestone_inherited ? task.milestone : ''}
          options={milestones}
          searchable
          disabled={!editable || busy}
          noneLabel={inherited ? `From its parent task: ${inherited.name}` : 'No milestone'}
          placeholder="Search for a milestone…"
          onChange={(key) => onSchedule(task, { milestone: key })}
        />
        <span style={mutedStyle}>
          {inherited
            ? `It counts toward ${inherited.name} because a task above it is assigned there. Choose a milestone to give it its own.`
            : 'The milestone counts this task among the tasks it needs done.'}
        </span>
      </div>

      <TaskRelationships plan={plan} task={task} editable={editable} busy={busy} onRelate={onRelate} onUnrelate={onUnrelate} onSelectTask={onSelectTask} />

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
