import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  ChatterEntry,
  NoteTodo as NoteTodoRef,
  ProjectMember,
  membersAPI,
  workItemsAPI,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { DEFAULT_TODO_COLUMN, columnFor } from './kanban/columns';

/** How much of a note becomes a to-do title before it is cut short. */
const TITLE_LIMIT = 120;

/**
 * The first line of a note, trimmed to something that reads as a title.
 *
 * Exported for its own test: a to-do whose title is three paragraphs of
 * someone's thinking is useless in a list, and a title cut mid-word reads
 * as a bug, so both cases are pinned.
 */
export const noteTitle = (message: string): string => {
  const firstLine = (message || '').trim().split('\n')[0].trim();
  if (firstLine.length <= TITLE_LIMIT) return firstLine;
  const cut = firstLine.slice(0, TITLE_LIMIT);
  const lastSpace = cut.lastIndexOf(' ');
  return `${(lastSpace > TITLE_LIMIT / 2 ? cut.slice(0, lastSpace) : cut).trimEnd()}…`;
};

const chipStyle = (color: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '0 6px',
  borderRadius: 8,
  background: 'var(--surface)',
  // The status is spelled out; the colour only repeats what the words say.
  border: `1px solid ${color}`,
  color: 'var(--text-muted)',
  fontSize: 10,
  fontWeight: 700,
  textTransform: 'uppercase',
  letterSpacing: '0.02em',
  whiteSpace: 'nowrap',
});

/**
 * The smart link a note shows once a to-do has been raised from it: where
 * the to-do went, who has it, and what state it is in now.
 *
 * The status is read from the work item on every load rather than copied
 * onto the note, so a card moved on the board changes what the note says.
 */
export const NoteTodoChip: React.FC<{ projectId: string; todo: NoteTodoRef }> = ({
  projectId,
  todo,
}) => {
  const col = columnFor(todo.status);
  return (
    <Link
      to={`/projects/${projectId}/todos?item=${encodeURIComponent(todo.work_item_id)}`}
      title={`To-do: ${todo.title}${todo.assignee_name ? ` — ${todo.assignee_name}` : ''}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        marginTop: 8,
        maxWidth: '100%',
        fontSize: 11,
        color: 'var(--text-muted)',
        textDecoration: 'none',
      }}
    >
      <span aria-hidden="true">☑</span>
      <span style={chipStyle(col.color)}>{col.label}</span>
      <span
        style={{
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          textDecoration: 'underline',
        }}
      >
        {todo.assignee_name || 'Unassigned'}
      </span>
    </Link>
  );
};

interface AddTodoProps {
  projectId: string;
  entry: ChatterEntry;
  /** Reload the feed so the new to-do's chip appears on the note. */
  onCreated: () => void;
}

/**
 * Raise a to-do from a note.
 *
 * Shared by the two ways of doing it — the control below, and the "@@name"
 * shortcut the composer applies as a note is posted — so the card they
 * produce is the same one. The note is carried across in full as the
 * description: the title is a summary of it, and the detail should not be
 * lost on the way to the board.
 */
export const createNoteTodo = (
  projectId: string,
  entry: Pick<ChatterEntry, 'id' | 'artifact_id' | 'message'>,
  options: { title?: string; assigneeId?: string | null; dueDate?: string | null } = {}
) =>
  workItemsAPI.create(projectId, {
    title: options.title || noteTitle(entry.message),
    description: entry.message,
    column: DEFAULT_TODO_COLUMN,
    assignee_type: 'user',
    assignee_id: options.assigneeId || null,
    artifact_ids: [entry.artifact_id],
    source_chatter_id: entry.id,
    due_date: options.dueDate || null,
  });

/**
 * The "Add to-do" control on a note.
 *
 * Deliberately a control and not an automatic rule: an @mention is how
 * people talk to each other in a comment thread, and turning every one of
 * them into a card would fill the board with "thanks @dave". The mention
 * still does the work of choosing who — it just does not decide that a
 * to-do exists.
 */
export const AddTodoControl: React.FC<AddTodoProps> = ({ projectId, entry, onCreated }) => {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [assignee, setAssignee] = useState('');
  const [due, setDue] = useState('');
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const mentioned = entry.mentions && entry.mentions.length > 0 ? entry.mentions[0] : undefined;

  const start = useCallback(() => {
    setTitle(noteTitle(entry.message));
    setAssignee(mentioned?.user_id || '');
    setDue('');
    setError('');
    setOpen(true);
  }, [entry.message, mentioned]);

  // The member list is only needed once someone opens the form, and only
  // to let them pick somebody other than whoever the note named.
  useEffect(() => {
    if (!open || !projectId || members.length) return;
    let cancelled = false;
    membersAPI
      .list(projectId)
      .then((res) => {
        if (!cancelled) setMembers(res.data || []);
      })
      .catch(() => {
        /* the mention already chose someone; the picker is the fallback */
      });
    return () => {
      cancelled = true;
    };
  }, [open, projectId, members.length]);

  const submit = async () => {
    const trimmed = title.trim();
    if (!trimmed) {
      setError('Give the to-do a title.');
      return;
    }
    setSaving(true);
    setError('');
    try {
      await createNoteTodo(projectId, entry, {
        title: trimmed,
        assigneeId: assignee || null,
        dueDate: due ? new Date(`${due}T00:00:00`).toISOString() : null,
      });
      setOpen(false);
      onCreated();
    } catch (err) {
      setError(apiErrorMessage(err, 'Could not add the to-do'));
    } finally {
      setSaving(false);
    }
  };

  if (!open) {
    return (
      <button
        type="button"
        onClick={start}
        style={{
          marginTop: 8,
          padding: '2px 8px',
          minHeight: 28,
          background: 'none',
          border: '1px solid var(--border)',
          borderRadius: 4,
          color: 'var(--text-muted)',
          fontSize: 11,
          cursor: 'pointer',
        }}
      >
        + Add to-do{mentioned ? ` for ${mentioned.name}` : ''}
      </button>
    );
  }

  const fieldStyle: React.CSSProperties = {
    width: '100%',
    padding: '4px 6px',
    fontSize: 12,
    fontFamily: 'inherit',
    border: '1px solid var(--neutral-mid)',
    borderRadius: 4,
    boxSizing: 'border-box',
  };

  return (
    <div
      style={{
        marginTop: 8,
        padding: 8,
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: 4,
        display: 'grid',
        gap: 6,
      }}
    >
      <label style={{ fontSize: 11, color: 'var(--text-muted)' }}>
        To-do
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          style={fieldStyle}
          autoFocus
        />
      </label>
      <label style={{ fontSize: 11, color: 'var(--text-muted)' }}>
        For
        <select
          value={assignee}
          onChange={(e) => setAssignee(e.target.value)}
          style={fieldStyle}
        >
          <option value="">Nobody yet</option>
          {/* Whoever the note named is offered even before the member list
              arrives, so the common case never waits on a request. */}
          {members.length === 0 && mentioned && (
            <option value={mentioned.user_id}>{mentioned.name}</option>
          )}
          {members.map((m) => (
            <option key={m.user_id} value={m.user_id}>
              {m.user_name?.trim() || m.user_email}
            </option>
          ))}
        </select>
      </label>
      <label style={{ fontSize: 11, color: 'var(--text-muted)' }}>
        Due (optional)
        <input
          type="date"
          value={due}
          onChange={(e) => setDue(e.target.value)}
          style={fieldStyle}
        />
      </label>
      {error && <div style={{ fontSize: 11, color: 'var(--danger)' }}>{error}</div>}
      <div style={{ display: 'flex', gap: 6 }}>
        <button
          type="button"
          onClick={submit}
          disabled={saving}
          style={{
            flex: 1,
            minHeight: 32,
            background: 'var(--accent)',
            border: 'none',
            borderRadius: 4,
            color: 'var(--on-accent, #fff)',
            fontSize: 12,
            cursor: saving ? 'not-allowed' : 'pointer',
          }}
        >
          {saving ? 'Adding…' : 'Add'}
        </button>
        <button
          type="button"
          onClick={() => setOpen(false)}
          disabled={saving}
          style={{
            minHeight: 32,
            padding: '0 10px',
            background: 'none',
            border: '1px solid var(--border)',
            borderRadius: 4,
            color: 'var(--text-muted)',
            fontSize: 12,
            cursor: 'pointer',
          }}
        >
          Cancel
        </button>
      </div>
    </div>
  );
};
