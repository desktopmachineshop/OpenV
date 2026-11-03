import React from 'react';
import { buttonStyle } from '../../components/ui';
import { NEXT_STATUSES, TRANSITION_LABELS, isForward, useStatusChange } from '../../components/artifactStatusChange';
import { statusOf } from '../../utils/artifactFacts';
import type { Artifact } from './shared';

interface PhoneActionBarProps {
  artifact: Artifact;
  /** Open the notes pane, where comments are written. */
  onComment: () => void;
  /** The new version a status change creates. */
  onStatusChange: (artifact: Artifact) => void;
}

// A phone's artifact ends in a bar under the thumb: Comment, and the review
// step the artifact can take next (Submit for review, Approve, …), the step
// forward as the one primary button. It stays at the bottom of the document
// pane while the document scrolls above it.
export const PhoneActionBar: React.FC<PhoneActionBarProps> = ({ artifact, onComment, onStatusChange }) => {
  const { change, changing } = useStatusChange(artifact.id, onStatusChange);
  const next = NEXT_STATUSES[statusOf(artifact)];
  const big: React.CSSProperties = { minHeight: 44, fontSize: 15, flex: 1, borderRadius: 8 };
  return (
    <div
      data-help-key="review"
      style={{
        position: 'sticky',
        bottom: 0,
        zIndex: 2,
        display: 'flex',
        gap: 8,
        padding: '10px 0 12px',
        marginTop: 12,
        background: 'var(--surface)',
        borderTop: '1px solid var(--border)',
        // The bar paints the strip below it too, so nothing scrolls into view
        // between it and the phone's tab bar.
        boxShadow: '0 -8px 8px -8px rgba(0,0,0,0.12), 0 40px 0 0 var(--surface)',
      }}
    >
      <button type="button" onClick={onComment} style={{ ...buttonStyle('secondary'), ...big }}>
        Comment
      </button>
      {next.map((status) => (
        <button
          key={status}
          type="button"
          disabled={changing}
          onClick={() => change(status)}
          style={{ ...buttonStyle(isForward(status) ? 'primary' : 'secondary', { disabled: changing }), ...big, flex: isForward(status) ? 1.4 : 1 }}
        >
          {TRANSITION_LABELS[status]}
        </button>
      ))}
    </div>
  );
};
