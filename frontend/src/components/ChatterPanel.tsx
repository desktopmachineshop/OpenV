import React, { useState, useEffect } from 'react';
import { GuidedChatPanel } from './wizard/GuidedChatPanel';
import { resolveAssistantSessionId } from './wizard/assistantSession';
import { useProjectSuggestions } from './wizard/useProjectSuggestions';
import { ArtifactHistory } from './ArtifactHistory';

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
  /**
   * Follow a "#REQ-12" a note cites. Without it the citations still render as
   * marked text rather than dead links.
   */
  onReferenceClick?: (ref: string) => void;
}

type Tab = 'history' | 'assistant';

export const ChatterPanel: React.FC<ChatterPanelProps> = ({
  artifactId,
  projectId,
  isOpen,
  onToggle,
  modeLabel,
  nextModeLabel,
  onArtifactsChanged,
  onReferenceClick,
}) => {
  // The history belongs to an artifact; the assistant does not, so with
  // nothing selected the panel opens on the tab that still has something to
  // show.
  const [tab, setTab] = useState<Tab>(artifactId ? 'history' : 'assistant');
  // Resolved lazily and only for the assistant tab: finding the conversation
  // can create a guided session, which should not happen just because someone
  // opened an artifact.
  const [assistantSessionId, setAssistantSessionId] = useState('');
  const [assistantError, setAssistantError] = useState('');
  const { applied, applySuggestions } = useProjectSuggestions(projectId, onArtifactsChanged);

  // With no artifact there is no history to show, so keep the panel on the
  // assistant rather than an empty tab.
  useEffect(() => {
    if (!artifactId) setTab('assistant');
  }, [artifactId]);

  // The conversation is the project's: with no artifact selected, the panel
  // stays mounted when the reader moves to another project, so it finds that
  // project's own instead of keeping the last one (#379, bug 218).
  useEffect(() => {
    setAssistantSessionId('');
    setAssistantError('');
  }, [projectId]);

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

      {/* Tabs: the artifact's own history, and the project's assistant. */}
      <div style={{ display: 'flex', borderBottom: '1px solid var(--neutral-soft)' }}>
        {([
          { id: 'history' as Tab, label: 'History', help: 'requirements.history' },
          { id: 'assistant' as Tab, label: 'V&V Assistant', help: 'guided.assistant' },
        ]).map((t) => {
          const active = tab === t.id;
          const disabled = t.id === 'history' && !artifactId;
          return (
            <button
              key={t.id}
              data-help-pane={t.help}
              onClick={() => !disabled && setTab(t.id)}
              disabled={disabled}
              title={disabled ? 'Select an artifact to read its history and add comments' : undefined}
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

      {tab === 'history' && artifactId ? (
        <ArtifactHistory
          artifactId={artifactId}
          projectId={projectId}
          onReferenceClick={onReferenceClick}
        />
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
