import React from 'react';
import { Link } from 'react-router-dom';
import { ErrorBanner } from '../../components/ui';

interface CommittedScreenProps {
  baselineCreated: boolean;
  error: string;
  setError: (next: string) => void;
  busy: boolean;
  handleCreateBaseline: () => void;
  /** Reopens the committed definition for editing. */
  onModify: () => void;
}

/**
 * What the wizard shows once the definition is committed.
 *
 * A function that returns the screen, not a component: it replaces the wizard's
 * whole tree, and React reuses the page's nodes where the element types match,
 * which keeps the DOM, down to its attribute order, as it was when the screen
 * was inline. A component here would mount new nodes.
 */
export const renderCommittedScreen = ({
  baselineCreated,
  error,
  setError,
  busy,
  handleCreateBaseline,
  onModify,
}: CommittedScreenProps): React.ReactElement => {
  return (
    <div style={{ padding: 24, maxWidth: 700, margin: '0 auto' }}>
      <div className="card" style={{ textAlign: 'center', padding: 40 }}>
        <div style={{ fontSize: 40, marginBottom: 10 }}>✓</div>
        <h2 style={{ color: 'var(--success)', marginBottom: 12 }}>Requirements committed</h2>
        <p style={{ color: 'var(--text-muted)', marginBottom: 24 }}>
          All draft artifacts are now live. You can baseline this initial set to keep an immutable
          snapshot for traceability.
        </p>
        <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />
        <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
          {baselineCreated ? (
            <span style={{ color: 'var(--success)', fontSize: 14, alignSelf: 'center' }}>
              ✓ Baseline "Initial requirements" created
            </span>
          ) : (
            <button className="button" onClick={handleCreateBaseline} disabled={busy}>
              {busy ? 'Creating…' : 'Create baseline'}
            </button>
          )}
          <Link to="../requirements" className="button-secondary" style={{ textDecoration: 'none' }}>
            Go to Requirements
          </Link>
          <button
            className="button-secondary"
            onClick={onModify}
            disabled={busy}
          >
            Modify guided definition
          </button>
        </div>
      </div>
    </div>
  );
};
