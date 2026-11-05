import React, { useState } from 'react';
import { ProjectMember } from '../../api/client';
import { buttonStyle } from '../ui';
import { TODO_TEXT_LIMIT, dueFromInput, isOneParagraph } from './todoText';

export interface TodoDraft {
  text: string;
  assigneeId: string | null;
  dueDate: string | null;
}

interface Props {
  /** Saves the draft; a rejection's message is shown under the field. */
  onAdd: (draft: TodoDraft) => Promise<void>;
  /** The project's members, to choose who has it; omitted for a personal to-do. */
  members?: ProjectMember[];
  placeholder?: string;
}

export const memberName = (m: ProjectMember): string => m.user_name?.trim() || m.user_email || 'Unknown member';

const fieldStyle: React.CSSProperties = {
  padding: '6px 8px',
  minHeight: 36,
  fontSize: 14,
  fontFamily: 'inherit',
  border: '1px solid var(--border)',
  borderRadius: 6,
  background: 'var(--surface)',
  color: 'var(--text)',
  boxSizing: 'border-box',
};

/**
 * The quick add at the top of a to-do list: type, press Enter, and it is on
 * the list. Who has it and when it is due are optional and stay beside the
 * field, so adding the common to-do is one line and a keypress.
 */
export const TodoQuickAdd: React.FC<Props> = ({ onAdd, members, placeholder }) => {
  const [text, setText] = useState('');
  const [assignee, setAssignee] = useState('');
  const [due, setDue] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const submit = async () => {
    const trimmed = text.trim();
    if (!trimmed) return;
    if (!isOneParagraph(trimmed)) {
      setError('A to-do is one paragraph. Put a bigger piece of work on the board.');
      return;
    }
    setSaving(true);
    setError('');
    try {
      await onAdd({ text: trimmed, assigneeId: assignee || null, dueDate: dueFromInput(due) });
      setText('');
      setDue('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not add the to-do');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      style={{ display: 'grid', gap: 6, marginBottom: 16 }}
    >
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        <input
          aria-label="New to-do"
          value={text}
          maxLength={TODO_TEXT_LIMIT}
          placeholder={placeholder || 'Add a to-do and press Enter'}
          onChange={(e) => setText(e.target.value)}
          style={{ ...fieldStyle, flex: '1 1 260px', minWidth: 0 }}
        />
        {members && (
          <select
            aria-label="Who has it"
            value={assignee}
            onChange={(e) => setAssignee(e.target.value)}
            style={{ ...fieldStyle, flex: '0 1 180px' }}
          >
            <option value="">Nobody yet</option>
            {members.map((m) => (
              <option key={m.user_id} value={m.user_id}>
                {memberName(m)}
              </option>
            ))}
          </select>
        )}
        <input
          type="date"
          aria-label="Due"
          value={due}
          onChange={(e) => setDue(e.target.value)}
          style={{ ...fieldStyle, flex: '0 1 150px' }}
        />
        <button type="submit" disabled={saving || !text.trim()} style={buttonStyle('primary', { disabled: saving || !text.trim() })}>
          {saving ? 'Adding…' : 'Add'}
        </button>
      </div>
      {error && (
        <div role="alert" style={{ fontSize: 12, color: 'var(--danger)' }}>
          {error}
        </div>
      )}
    </form>
  );
};
