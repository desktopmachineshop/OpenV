import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { ProjectMember, Todo } from '../../api/client';
import { buttonStyle } from '../ui';
import { memberName } from './TodoQuickAdd';
import {
  TODO_TEXT_LIMIT,
  dueFromInput,
  dueToInput,
  formatTodoDue,
  isOneParagraph,
  todoOverdue,
} from './todoText';

/** What a row may do; each resolves once the change is saved, or throws its message. */
export interface TodoActions {
  tick: (todo: Todo, done: boolean) => Promise<void>;
  save: (todo: Todo, change: { text: string; assigneeId: string | null; dueDate: string | null }) => Promise<void>;
  promote?: (todo: Todo) => Promise<void>;
  remove: (todo: Todo) => Promise<void>;
}

interface Props {
  todo: Todo;
  actions: TodoActions;
  /** The project's members, for names and the assignee picker. */
  members?: ProjectMember[];
  /** The project's name, shown on My to-dos where rows come from many projects. */
  projectName?: string;
  highlighted?: boolean;
}

const meta: React.CSSProperties = { color: 'var(--text-muted)', fontSize: 12 };
const field: React.CSSProperties = {
  padding: '4px 6px',
  fontSize: 13,
  fontFamily: 'inherit',
  border: '1px solid var(--border)',
  borderRadius: 4,
  background: 'var(--surface)',
  color: 'var(--text)',
  boxSizing: 'border-box',
};

/**
 * One to-do: a checkbox to tick it off, its text, who has it and when it is
 * due, where it came from, and the row's actions: edit, promote to a board
 * task (a project's open to-do), delete.
 */
export const TodoRow: React.FC<Props> = ({ todo, actions, members, projectName, highlighted }) => {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(todo.text);
  const [assignee, setAssignee] = useState(todo.assignee_id || '');
  const [due, setDue] = useState(dueToInput(todo.due_date));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save the to-do');
    } finally {
      setBusy(false);
    }
  };

  const startEdit = () => {
    setText(todo.text);
    setAssignee(todo.assignee_id || '');
    setDue(dueToInput(todo.due_date));
    setEditing(true);
  };

  const save = () =>
    run(async () => {
      if (!isOneParagraph(text)) throw new Error('A to-do is one paragraph.');
      await actions.save(todo, { text: text.trim(), assigneeId: assignee || null, dueDate: dueFromInput(due) });
      setEditing(false);
    });

  const assigneeMember = todo.assignee_id ? members?.find((m) => m.user_id === todo.assignee_id) : undefined;
  const assigneeName = assigneeMember ? memberName(assigneeMember) : undefined;
  const overdue = todoOverdue(todo);
  const pid = todo.project_id;

  return (
    <li
      data-testid="todo-row"
      style={{
        listStyle: 'none',
        display: 'flex',
        gap: 10,
        alignItems: 'flex-start',
        padding: '8px 6px',
        borderTop: '1px solid var(--border)',
        background: highlighted ? 'var(--tint-blue, var(--surface-alt))' : undefined,
      }}
    >
      <input
        type="checkbox"
        aria-label={todo.done ? `Open again: ${todo.text}` : `Tick off: ${todo.text}`}
        checked={todo.done}
        disabled={busy}
        onChange={(e) => run(() => actions.tick(todo, e.target.checked))}
        style={{ width: 20, height: 20, marginTop: 2, flex: '0 0 auto', cursor: 'pointer' }}
      />
      <div style={{ flex: 1, minWidth: 0, display: 'grid', gap: 4 }}>
        {editing ? (
          <>
            <textarea
              aria-label="To-do text"
              value={text}
              maxLength={TODO_TEXT_LIMIT}
              rows={2}
              onChange={(e) => setText(e.target.value)}
              style={{ ...field, width: '100%', resize: 'vertical' }}
              autoFocus
            />
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {members && pid && (
                <select aria-label="Who has it" value={assignee} onChange={(e) => setAssignee(e.target.value)} style={field}>
                  <option value="">Nobody yet</option>
                  {members.map((m) => (
                    <option key={m.user_id} value={m.user_id}>
                      {memberName(m)}
                    </option>
                  ))}
                </select>
              )}
              <input type="date" aria-label="Due" value={due} onChange={(e) => setDue(e.target.value)} style={field} />
              <button type="button" disabled={busy || !text.trim()} onClick={save} style={buttonStyle('primary', { compact: true })}>
                Save
              </button>
              <button type="button" disabled={busy} onClick={() => setEditing(false)} style={buttonStyle('secondary', { compact: true })}>
                Cancel
              </button>
            </div>
          </>
        ) : (
          <span
            style={{
              color: todo.done ? 'var(--text-muted)' : 'var(--text)',
              textDecoration: todo.done ? 'line-through' : undefined,
              whiteSpace: 'pre-wrap',
              overflowWrap: 'anywhere',
            }}
          >
            {todo.text}
          </span>
        )}
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'center' }}>
          {projectName && pid && (
            <Link to={`/projects/${pid}/todos?todo=${todo.id}`} style={meta}>
              {projectName}
            </Link>
          )}
          {!pid && <span style={meta}>Personal</span>}
          {pid && members && (
            <span style={meta}>{assigneeName || (todo.assignee_id ? 'Former member' : 'Nobody yet')}</span>
          )}
          {todo.due_date && (
            <span style={{ ...meta, fontWeight: overdue ? 700 : 400, color: overdue ? 'var(--danger)' : meta.color }}>
              {overdue ? 'Overdue ' : 'Due '}
              {formatTodoDue(todo.due_date)}
            </span>
          )}
          {pid && todo.source_artifact_id && (
            <Link to={`/projects/${pid}/requirements?artifact=${todo.source_artifact_id}`} style={meta}>
              {todo.source_chatter_id ? 'from a note' : 'from an artifact'}
            </Link>
          )}
          {pid && todo.promoted_work_item_id && (
            <Link to={`/projects/${pid}/todos?item=${todo.promoted_work_item_id}`} style={meta}>
              promoted to a board task
            </Link>
          )}
          {!editing && (
            <span style={{ display: 'inline-flex', gap: 4, marginLeft: 'auto' }}>
              <button type="button" disabled={busy} onClick={startEdit} style={buttonStyle('ghost', { compact: true })}>
                Edit
              </button>
              {actions.promote && pid && !todo.done && !todo.promoted_work_item_id && (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => run(() => actions.promote!(todo))}
                  title="Make a board task of it: a card in To Do carrying the text, and this to-do ticked off"
                  style={buttonStyle('secondary', { compact: true })}
                >
                  Promote to board task
                </button>
              )}
              <button
                type="button"
                disabled={busy}
                aria-label={`Delete: ${todo.text}`}
                onClick={() => run(() => actions.remove(todo))}
                style={buttonStyle('ghost', { compact: true })}
              >
                Delete
              </button>
            </span>
          )}
        </div>
        {error && (
          <div role="alert" style={{ fontSize: 12, color: 'var(--danger)' }}>
            {error}
          </div>
        )}
      </div>
    </li>
  );
};
