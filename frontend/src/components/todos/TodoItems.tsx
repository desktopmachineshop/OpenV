import React, { useMemo, useState } from 'react';
import { ProjectMember, Todo, todosAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { TodoActions, TodoRow } from './TodoRow';

/**
 * The actions every to-do list offers, over the API: each throws an Error
 * with the server's message, which the row shows, and reloads the list once
 * the change is saved. A promotion also reports the board task it made.
 */
export const todoActions = (
  reload: () => void | Promise<void>,
  opts: { promote?: boolean; onPromoted?: (workItemId: string) => void } = {}
): TodoActions => {
  const call = async (fn: () => Promise<unknown>, fallback: string) => {
    try {
      await fn();
    } catch (err) {
      throw new Error(apiErrorMessage(err, fallback));
    }
    await reload();
  };
  return {
    tick: (todo, done) => call(() => todosAPI.setDone(todo.id, done), 'Could not tick the to-do off'),
    save: (todo, change) =>
      call(
        () => todosAPI.update(todo.id, { text: change.text, assignee_id: change.assigneeId, due_date: change.dueDate }),
        'Could not save the to-do'
      ),
    remove: (todo) => call(() => todosAPI.remove(todo.id), 'Could not delete the to-do'),
    promote: opts.promote
      ? (todo) =>
          call(async () => {
            const res = await todosAPI.promote(todo.id);
            opts.onPromoted?.(res.data.work_item.id);
          }, 'Could not promote the to-do')
      : undefined,
  };
};

interface Props {
  todos: Todo[];
  actions: TodoActions;
  members?: ProjectMember[];
  /** Project names by id, for My to-dos. */
  projectNames?: Record<string, string>;
  /** The to-do a link landed on: highlighted, and its Completed group opened. */
  highlightId?: string | null;
  emptyText: string;
}

/**
 * A to-do list: the open ones, then the completed ones collapsed behind one
 * line, so ticking a to-do off clears it away without losing it.
 */
export const TodoItems: React.FC<Props> = ({ todos, actions, members, projectNames, highlightId, emptyText }) => {
  const open = useMemo(() => todos.filter((t) => !t.done), [todos]);
  const done = useMemo(() => todos.filter((t) => t.done), [todos]);
  const [showDone, setShowDone] = useState(false);
  const doneOpen = showDone || done.some((t) => t.id === highlightId);

  const row = (t: Todo) => (
    <TodoRow
      key={t.id}
      todo={t}
      actions={actions}
      members={members}
      projectName={t.project_id ? projectNames?.[t.project_id] : undefined}
      highlighted={t.id === highlightId}
    />
  );

  return (
    <div>
      {open.length === 0 ? (
        <p style={{ color: 'var(--text-muted)', fontSize: 14, margin: '8px 0' }}>{emptyText}</p>
      ) : (
        <ul aria-label="Open to-dos" style={{ margin: 0, padding: 0 }}>
          {open.map(row)}
        </ul>
      )}
      {done.length > 0 && (
        <div style={{ marginTop: 12 }}>
          <button
            type="button"
            aria-expanded={doneOpen}
            onClick={() => setShowDone(!doneOpen)}
            style={{
              background: 'none',
              border: 'none',
              padding: '6px 0',
              minHeight: 32,
              color: 'var(--text-muted)',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            {doneOpen ? '▾' : '▸'} Completed ({done.length})
          </button>
          {doneOpen && (
            <ul aria-label="Completed to-dos" style={{ margin: 0, padding: 0 }}>
              {done.map(row)}
            </ul>
          )}
        </div>
      )}
    </div>
  );
};
