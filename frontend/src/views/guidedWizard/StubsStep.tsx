import React from 'react';
import { helpText } from './stepStyles';

interface StubsStepProps {
  /** The requirements with method test that have artifacts, to stub. */
  candidates: { id: string; title: string; category?: string }[];
  /** Keyed by requirement artifact id. */
  stubSelected: Record<string, boolean>;
  stubCreated: Record<string, string>;
  setStubSelected: (next: Record<string, boolean>) => void;
}

/**
 * Step 7: the verification stubs to make, one per tested requirement (optional).
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderStubsStep = ({
  candidates,
  stubSelected,
  stubCreated,
  setStubSelected,
}: StubsStepProps): React.ReactElement => {
  return (
    <div className="card">
      <h3>Verification stubs (optional)</h3>
      <div style={helpText}>
        Requirements with verification method <strong>test</strong> are listed below. Checked
        requirements get a draft test case ("Verify: …") linked with a <em>verifies</em>
        relationship, filed under a section matching the requirement it verifies.
      </div>
      {candidates.length === 0 ? (
        <div style={{ color: 'var(--neutral)', fontSize: 13 }}>
          No requirements with method "test" were created — nothing to stub. Use Skip or Next.
        </div>
      ) : (
        candidates.map((c) => (
          <label
            key={c.id}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '8px 10px',
              border: '1px solid var(--border-soft)',
              borderRadius: 4,
              marginBottom: 6,
              cursor: stubCreated[c.id] ? 'default' : 'pointer',
              fontWeight: 400,
            }}
          >
            <input
              type="checkbox"
              style={{ width: 'auto' }}
              checked={stubCreated[c.id] ? true : stubSelected[c.id] ?? true}
              disabled={!!stubCreated[c.id]}
              onChange={(e) => setStubSelected({ ...stubSelected, [c.id]: e.target.checked })}
            />
            <span style={{ flex: 1, fontSize: 13, color: 'var(--text)' }}>{c.title}</span>
            {c.category && (
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{c.category}</span>
            )}
            {stubCreated[c.id] && <span style={{ fontSize: 12, color: 'var(--success)' }}>✓ stub created</span>}
          </label>
        ))
      )}
    </div>
  );
};
