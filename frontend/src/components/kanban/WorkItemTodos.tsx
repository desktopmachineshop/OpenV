// The to-dos on a board card. A card is a task of the project's plan, and
// the plan counts these toward the task's progress: each one ticked off
// fills the task's bar on the Gantt. They are ordinary project to-dos, so
// the To-dos page lists them too.
import React, { useCallback, useEffect, useState } from 'react';
import { planAPI, Todo, todosAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { buttonStyle, mutedStyle } from '../ui';

export const WorkItemTodos: React.FC<{ workItemId: string; onChanged?: () => void }> = ({ workItemId, onChanged }) => {
  const [todos, setTodos] = useState<Todo[] | null>(null);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    planAPI
      .listTodos(workItemId)
      .then((res) => setTodos(res.data || []))
      .catch((err) => setError(apiErrorMessage(err, 'Failed to load the to-dos')));
  }, [workItemId]);

  useEffect(() => {
    setTodos(null);
    load();
  }, [load]);

  const run = async (fn: () => Promise<unknown>, fallback: string) => {
    setBusy(true);
    setError('');
    try {
      await fn();
      onChanged?.();
    } catch (err) {
      setError(apiErrorMessage(err, fallback));
    } finally {
      setBusy(false);
      load();
    }
  };

  const done = (todos || []).filter((t) => t.done).length;
  return (
    <div style={{ marginBottom: 16 }} data-help-key="board.todos">
      <label htmlFor={`card-todo-${workItemId}`}>
        To-dos{todos && todos.length > 0 ? ` · ${done}/${todos.length}` : ''}
      </label>
      <div style={{ ...mutedStyle, marginBottom: 6 }}>Ticking these off moves this task's progress on the Gantt.</div>
      {error && <div style={{ ...mutedStyle, color: 'var(--danger-strong)', marginBottom: 6 }}>{error}</div>}
      {(todos || []).map((t) => (
        <label
          key={t.id}
          style={{ display: 'flex', gap: 8, alignItems: 'flex-start', minHeight: 28, fontSize: 13, fontWeight: 400, color: t.done ? 'var(--text-muted)' : 'var(--text)', cursor: 'pointer' }}
        >
          <input
            type="checkbox"
            checked={t.done}
            disabled={busy}
            onChange={(e) => {
              const next = e.target.checked;
              setTodos((list) => (list || []).map((x) => (x.id === t.id ? { ...x, done: next } : x)));
              void run(() => todosAPI.setDone(t.id, next), 'Failed to save the to-do');
            }}
            style={{ width: 'auto', flex: 'none', margin: '3px 0 0' }}
          />
          <span style={{ textDecoration: t.done ? 'line-through' : undefined, overflowWrap: 'anywhere' }}>{t.text}</span>
        </label>
      ))}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const value = text.trim();
          if (!value) return;
          setText('');
          void run(() => planAPI.addTodo(workItemId, value), 'Failed to add the to-do');
        }}
        style={{ display: 'flex', gap: 6, marginTop: 6 }}
      >
        <input
          id={`card-todo-${workItemId}`}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Add a to-do to tick off"
          style={{ flex: 1, minWidth: 0, padding: '5px 8px', fontSize: 13 }}
        />
        <button type="submit" disabled={busy || !text.trim()} style={buttonStyle('secondary', { disabled: busy || !text.trim() })}>
          Add
        </button>
      </form>
    </div>
  );
};
