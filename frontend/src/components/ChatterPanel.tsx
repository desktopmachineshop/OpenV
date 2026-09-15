import React, { useState, useEffect, useCallback } from 'react';
import { Artifact, ChatterEntry, artifactAPI, chatterAPI } from '../api/client';
import { GuidedChatPanel } from './wizard/GuidedChatPanel';
import { resolveAssistantSessionId } from './wizard/assistantSession';
import {
  ASSISTANT_EDITS_FEATURE,
  GATED_REASON,
  applySuggestionsToProject,
  isProjectEditKind,
} from './wizard/applySuggestion';
import { useFeature } from '../hooks/useFeature';
import { AddTodoControl, NoteTodoChip } from './NoteTodo';
import { TODO_LIST_FEATURE } from '../views/TodoList';

interface ChatterPanelProps {
  /** The artifact whose notes these are; absent when nothing is selected. */
  artifactId?: string;
  /** Project the notes belong to — the assistant tab needs it with or without an artifact. */
  projectId?: string;
  isOpen: boolean;
  /** Cycle the panel's visibility: pinned → auto-hide → hidden. */
  onToggle: () => void;
  /** The panel's current visibility, for the control's label. */
  modeLabel?: string;
  /** What clicking the control would switch to. */
  nextModeLabel?: string;
  /**
   * Refresh the project after the assistant adds something. Without it an
   * applied suggestion is saved but invisible until the reader navigates,
   * which reads as the button having done nothing.
   */
  onArtifactsChanged?: () => void;
}

type Tab = 'comments' | 'assistant';

export const ChatterPanel: React.FC<ChatterPanelProps> = ({
  artifactId,
  projectId,
  isOpen,
  onToggle,
  modeLabel,
  nextModeLabel,
  onArtifactsChanged,
}) => {
  // Comments belong to an artifact; the assistant does not, so with nothing
  // selected the panel opens on the tab that still has something to show.
  const [tab, setTab] = useState<Tab>(artifactId ? 'comments' : 'assistant');
  // Resolved lazily and only for the assistant tab: finding the conversation
  // can create a guided session, which should not happen just because someone
  // opened an artifact.
  const [assistantSessionId, setAssistantSessionId] = useState('');
  const [assistantError, setAssistantError] = useState('');
  const [entries, setEntries] = useState<ChatterEntry[]>([]);
  const [newMessage, setNewMessage] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');
  // Which suggestions this panel has applied. The wizard persists its own in
  // the session answers; here the project is the record, so remembering
  // for the life of the panel is what stops a second click adding twice.
  const [applied, setApplied] = useState<Record<string, boolean>>({});
  const editsEnabled = useFeature(ASSISTANT_EDITS_FEATURE);
  const todosEnabled = useFeature(TODO_LIST_FEATURE);

  const loadChatterEntries = useCallback(async () => {
    if (!artifactId) return;

    setIsLoading(true);
    setError('');

    try {
      const response = await chatterAPI.list(artifactId);
      setEntries(response.data || []);
    } catch (err: any) {
      console.error('Failed to load chatter entries:', err);
      setError(`Failed to load chatter: ${err.message}`);
    } finally {
      setIsLoading(false);
    }
  }, [artifactId]);

  useEffect(() => {
    if (artifactId && isOpen) {
      loadChatterEntries();
    }
  }, [artifactId, isOpen, loadChatterEntries]);

  // With no artifact there are no comments to show, so keep the panel on the
  // assistant rather than an empty tab.
  useEffect(() => {
    if (!artifactId) setTab('assistant');
  }, [artifactId]);

  useEffect(() => {
    if (!isOpen || tab !== 'assistant' || !projectId || assistantSessionId) return;
    let cancelled = false;
    (async () => {
      const id = await resolveAssistantSessionId(projectId);
      if (cancelled) return;
      if (id) {
        setAssistantSessionId(id);
        setAssistantError('');
      } else {
        setAssistantError('The assistant conversation could not be opened. Try again in a moment.');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [isOpen, tab, projectId, assistantSessionId]);

  /**
   * Add what the assistant suggested to the project.
   *
   * The wizard applies a suggestion into the form somebody is filling in;
   * here there is no form, so it goes straight into the project as a draft
   * artifact under the heading the wizard would have used. The project's
   * artifacts are read at apply time rather than held in state: a heading
   * created a minute ago by somebody else should be found, not duplicated.
   */
  const applySuggestions = useCallback(
    async (items: { suggestion: any; key: string }[]): Promise<(string | null)[]> => {
      if (!projectId) return items.map(() => 'No project is open.');
      let artifacts: Artifact[] = [];
      try {
        const res = await artifactAPI.list(projectId);
        artifacts = res.data || [];
      } catch {
        // Without the list, headings cannot be matched and would be created
        // again. Refusing is better than quietly growing a second set.
        return items.map(() => 'The project could not be read, so nothing was added.');
      }
      // A gated kind is answered, not applied: the server does not offer
      // these shapes to a gated workspace, but a transcript can carry one
      // from before the gate closed, or from another member's channel.
      const allowed = editsEnabled ? items : items.filter((i) => !isProjectEditKind(i.suggestion?.kind));
      const applyResults = await applySuggestionsToProject(
        { projectId, artifacts, onChanged: onArtifactsChanged },
        allowed
      );
      const byKey = new Map(allowed.map((i, n) => [i.key, applyResults[n]]));
      const results = items.map((i) => (byKey.has(i.key) ? byKey.get(i.key)! : GATED_REASON));
      setApplied((prev) => {
        const next = { ...prev };
        items.forEach((i, n) => {
          if (results[n] === null) next[i.key] = true;
        });
        return next;
      });
      return results;
    },
    [projectId, onArtifactsChanged, editsEnabled]
  );

  const handleAddMessage = async () => {
    if (!newMessage.trim() || !artifactId) {
      return;
    }

    setError('');
    try {
      const response = await chatterAPI.create({
        artifact_id: artifactId,
        message: newMessage,
      });

      // Add the new entry to the top of the list
      setEntries([response.data, ...entries]);
      setNewMessage('');
    } catch (err: any) {
      console.error('Failed to add chatter entry:', err);
      setError(`Failed to add message: ${err.message}`);
    }
  };

  if (!isOpen) {
    return null;
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        backgroundColor: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: '4px',
        height: '100%',
        boxShadow: '0 2px 4px rgba(0,0,0,0.05)',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '15px',
          borderBottom: '1px solid var(--neutral-soft)',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          backgroundColor: 'var(--surface-alt)',
        }}
      >
        <h3 style={{ margin: 0, fontSize: '14px', fontWeight: 'bold' }}>
          💬 Notes
        </h3>
        {/* One control for how much room the panel takes, rather than a
            close button that gave no way back. */}
        <button
          onClick={onToggle}
          style={{
            background: 'none',
            border: '1px solid var(--border)',
            borderRadius: 4,
            cursor: 'pointer',
            fontSize: 12,
            padding: '3px 8px',
            minHeight: 24,
            color: 'var(--text-muted)',
          }}
          title={
            modeLabel && nextModeLabel
              ? `Notes: ${modeLabel} — click for ${nextModeLabel}`
              : 'Hide notes'
          }
        >
          {modeLabel || 'Hide'}
        </button>
      </div>

      {/* Tabs: the artifact's own comments, and the project's assistant. */}
      <div style={{ display: 'flex', borderBottom: '1px solid var(--neutral-soft)' }}>
        {([
          { id: 'comments' as Tab, label: 'Comments' },
          { id: 'assistant' as Tab, label: 'V&V Assistant' },
        ]).map((t) => {
          const active = tab === t.id;
          const disabled = t.id === 'comments' && !artifactId;
          return (
            <button
              key={t.id}
              onClick={() => !disabled && setTab(t.id)}
              disabled={disabled}
              title={disabled ? 'Select an artifact to read and add its comments' : undefined}
              style={{
                flex: 1,
                padding: '8px 6px',
                background: 'none',
                border: 'none',
                borderBottom: active ? '2px solid var(--accent)' : '2px solid transparent',
                color: disabled ? 'var(--neutral)' : active ? 'var(--text)' : 'var(--text-muted)',
                fontWeight: active ? 700 : 400,
                fontSize: 12,
                cursor: disabled ? 'not-allowed' : 'pointer',
              }}
            >
              {t.label}
            </button>
          );
        })}
      </div>

      {tab === 'comments' ? (
        <>
      {/* Entries list */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '10px',
        }}
      >
        {isLoading && <p style={{ fontSize: '12px', color: 'var(--text-body)' }}>Loading notes...</p>}
        {error && <p style={{ fontSize: '12px', color: 'var(--danger)' }}>{error}</p>}
        {entries.map((entry) => (
          <div
            key={entry.id}
            style={{
              marginBottom: '12px',
              padding: '8px',
              backgroundColor: entry.is_auto_entry ? 'var(--tint-blue)' : 'var(--tint-yellow)',
              border: '1px solid var(--border)',
              borderRadius: '4px',
            }}
          >
            <div style={{ fontSize: '11px', color: 'var(--text-body)', marginBottom: '4px' }}>
              {entry.is_auto_entry ? '🔄' : '✏️'}{' '}
              <span style={{ fontWeight: 'bold' }}>
                {entry.author_name || (entry.is_auto_entry ? 'System' : 'Unknown')}
              </span>{' '}
              · {new Date(entry.created_at).toLocaleString()}
            </div>
            <div
              style={{
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                lineHeight: '1.4',
                color: 'var(--text)',
              }}
            >
              {entry.message}
            </div>
            {/* A note carries at most one to-do: once it has one it shows
                its live status, and until then it offers to raise one for
                whoever the note names. System and agent entries are not
                somebody asking for something, so they get neither. */}
            {todosEnabled && projectId && !entry.is_auto_entry && (
              entry.todo ? (
                <NoteTodoChip projectId={projectId} todo={entry.todo} />
              ) : (
                <AddTodoControl
                  projectId={projectId}
                  entry={entry}
                  onCreated={loadChatterEntries}
                />
              )
            )}
          </div>
        ))}
      </div>

      {/* Input form */}
      <div
        style={{
          padding: '10px',
          borderTop: '1px solid var(--neutral-soft)',
          backgroundColor: 'var(--surface-alt)',
        }}
      >
        <textarea
          value={newMessage}
          onChange={(e) => setNewMessage(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && e.ctrlKey) {
              e.preventDefault();
              handleAddMessage();
            }
          }}
          placeholder="Add a note..."
          enterKeyHint="enter"
          style={{
            width: '100%',
            minHeight: '60px',
            padding: '8px',
            border: '1px solid var(--neutral-mid)',
            borderRadius: '4px',
            fontFamily: 'inherit',
            fontSize: '12px',
            resize: 'vertical',
            boxSizing: 'border-box',
          }}
        />
        <button
          onClick={handleAddMessage}
          disabled={!newMessage.trim()}
          title="Add note (Ctrl+Enter)"
          style={{
            width: '100%',
            marginTop: '8px',
            padding: '8px',
            backgroundColor: newMessage.trim() ? 'var(--accent)' : 'var(--neutral-mid)',
            color: 'var(--accent-fg)',
            border: 'none',
            borderRadius: '4px',
            cursor: newMessage.trim() ? 'pointer' : 'not-allowed',
            fontSize: '12px',
            fontWeight: 'bold',
          }}
        >
          Add Note
        </button>
      </div>
        </>
      ) : (
        // One conversation per project: the same transcript the wizard shows,
        // with the artifact on screen passed as this turn's context.
        <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          {assistantError ? (
            <div style={{ padding: 12, fontSize: 12, color: 'var(--danger)' }}>{assistantError}</div>
          ) : !projectId ? (
            <div style={{ padding: 12, fontSize: 12, color: 'var(--text-muted)' }}>
              Open a project to chat with the V&amp;V Assistant.
            </div>
          ) : !assistantSessionId ? (
            <div style={{ padding: 12, fontSize: 12, color: 'var(--text-muted)' }}>
              Opening the conversation…
            </div>
          ) : (
            <GuidedChatPanel
              sessionId={assistantSessionId}
              artifactId={artifactId}
              embedded
              applied={applied}
              applyTarget="project"
              onApplySuggestions={applySuggestions}
              subtitle={
                artifactId
                  ? 'Answers about the artifact on screen — same conversation as the wizard.'
                  : 'Same conversation as the guided wizard, for the project as a whole.'
              }
            />
          )}
        </div>
      )}
    </div>
  );
};

export default ChatterPanel;
