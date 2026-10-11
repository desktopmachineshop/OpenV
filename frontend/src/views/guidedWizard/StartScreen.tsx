import React from 'react';
import { GuidedSession } from '../../api/client';
import { ErrorBanner } from '../../components/ui';

interface StartScreenProps {
  /** The newest committed definition, which Modify reopens. */
  latestCommitted: GuidedSession | null;
  error: string;
  setError: (next: string) => void;
  busy: boolean;
  modifySession: (from: GuidedSession) => void;
  startSession: () => void;
}

/**
 * What a project with no session in progress shows: start, or reopen or redo the committed definition.
 *
 * A function that returns the screen, not a component: it replaces the wizard's
 * whole tree, and React reuses the page's nodes where the element types match,
 * which keeps the DOM, down to its attribute order, as it was when the screen
 * was inline. A component here would mount new nodes.
 */
export const renderStartScreen = ({
  latestCommitted,
  error,
  setError,
  busy,
  modifySession,
  startSession,
}: StartScreenProps): React.ReactElement => {
  return (
    <div style={{ padding: 24, maxWidth: 700, margin: '0 auto' }}>
      <div className="card" style={{ textAlign: 'center', padding: 40 }}>
        <h2 style={{ color: 'var(--text)', marginBottom: 12 }}>Guided requirements definition</h2>
        {latestCommitted ? (
          <p style={{ color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.6 }}>
            A guided definition has already been completed for this project. Reopen it to refine or
            extend the committed set — existing entries stay linked to their artifacts, and anything
            you add becomes new draft artifacts to review and commit.
          </p>
        ) : (
          <p style={{ color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.6 }}>
            A step-by-step flow that walks you from product framing through personas, user needs,
            requirements, hazards and verification stubs — creating traceable draft artifacts as you go.
            You review and commit everything at the end.
          </p>
        )}
        <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />
        {latestCommitted ? (
          <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
            <button
              className="button"
              style={{ background: 'var(--accent)' }}
              onClick={() => modifySession(latestCommitted)}
              disabled={busy}
            >
              {busy ? 'Opening…' : 'Modify guided definition'}
            </button>
            <button className="button-secondary" onClick={startSession} disabled={busy}>
              Start over from scratch
            </button>
          </div>
        ) : (
          <button className="button" style={{ background: 'var(--accent)' }} onClick={startSession} disabled={busy}>
            {busy ? 'Starting…' : 'Start guided definition'}
          </button>
        )}
      </div>
    </div>
  );
};
