import React from 'react';
import { Artifact } from '../../api/client';
import { helpText } from './stepStyles';

interface ReviewStepProps {
  draftArtifacts: Artifact[];
  draftsLoading: boolean;
  busy: boolean;
  discardDraft: (artifact: Artifact) => void;
  handleBack: () => void;
  handleCommit: () => void;
}

/**
 * Step 8: the drafts the flow made, to discard or commit.
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderReviewStep = ({
  draftArtifacts,
  draftsLoading,
  busy,
  discardDraft,
  handleBack,
  handleCommit,
}: ReviewStepProps): React.ReactElement => {
  const grouped: Record<string, Artifact[]> = {};
  draftArtifacts.forEach((a) => {
    (grouped[a.type] = grouped[a.type] || []).push(a);
  });
  return (
    <div className="card">
      <h3>Review &amp; commit</h3>
      <div style={helpText}>
        These draft artifacts were created during this flow. Discard any you don't want, then commit
        to make them all live.
      </div>
      {draftsLoading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading drafts…</div>
      ) : draftArtifacts.length === 0 ? (
        <div style={{ color: 'var(--neutral)', fontSize: 13, marginBottom: 12 }}>
          No new drafts to approve. Commit to save this definition.
        </div>
      ) : (
        Object.keys(grouped)
          .sort()
          .map((type) => (
            <div key={type} style={{ marginBottom: 16 }}>
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: 6 }}>
                {type} ({grouped[type].length})
              </div>
              <div className="table-scroll">
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <tbody>
                  {grouped[type].map((a) => (
                    <tr key={a.id} style={{ borderBottom: '1px solid var(--surface-hover)' }}>
                      <td style={{ padding: '6px 8px', fontSize: 13, color: 'var(--text)' }}>{a.title}</td>
                      <td style={{ padding: '6px 8px', width: 80, textAlign: 'right' }}>
                        <button
                          onClick={() => discardDraft(a)}
                          style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', fontSize: 12, padding: 2 }}
                        >
                          Discard
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </div>
            </div>
          ))
      )}
      <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 16 }}>
        <button className="button-secondary" onClick={handleBack} disabled={busy}>
          ← Back
        </button>
        {/* A reopened definition that adds nothing has no drafts, and
            still needs its commit to close the session. */}
        <button className="button" onClick={handleCommit} disabled={busy}>
          {busy
            ? 'Committing…'
            : draftArtifacts.length === 0
              ? 'Commit definition'
              : `Commit ${draftArtifacts.length} artifact${draftArtifacts.length === 1 ? '' : 's'}`}
        </button>
      </div>
    </div>
  );
};
