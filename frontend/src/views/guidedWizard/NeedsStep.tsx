import React from 'react';
import { NeedEntry, PersonaEntry, newEntryId } from '../../components/wizard/wizardEntries';
import { helpText } from './stepStyles';

interface NeedsStepProps {
  personas: PersonaEntry[];
  needs: NeedEntry[];
  setNeeds: (next: NeedEntry[]) => void;
}

/**
 * Step 3: the user needs under their personas, and those whose persona is gone.
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderNeedsStep = ({ personas, needs, setNeeds }: NeedsStepProps): React.ReactElement => {
  const usablePersonas = personas.filter((p) => p.name.trim());
  const orphanNeeds = needs.filter((n) => !personas.some((p) => p.id === n.persona_id && p.name.trim()));
  return (
    <div>
      <div className="card" style={{ marginBottom: 12 }}>
        <h3>User needs</h3>
        <div style={{ ...helpText, marginBottom: 0 }}>
          For each persona, capture needs as "As <em>persona</em>, I need <em>capability</em> so
          that <em>outcome</em>". Each need becomes a draft artifact linked to its persona.
        </div>
      </div>
      {usablePersonas.length === 0 && (
        <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
          No personas defined — go back to step 2 to add at least one persona.
        </div>
      )}
      {personas.map((p) =>
        p.name.trim() ? (
          <div key={p.id} className="card" style={{ marginBottom: 12 }}>
            <div style={{ fontWeight: 600, color: 'var(--text)', marginBottom: 10 }}>{p.name}</div>
            {needs.map((n) =>
              n.persona_id === p.id ? (
                <div key={n.id} style={{ border: '1px solid var(--border-soft)', borderRadius: 4, padding: 10, marginBottom: 8 }}>
                  <div style={{ display: 'flex', gap: 8, marginBottom: 6, alignItems: 'center' }}>
                    {n.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                    <span style={{ fontSize: 13, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>I need</span>
                    <input
                      value={n.capability}
                      onChange={(e) => setNeeds(needs.map((x) => (x.id === n.id ? { ...x, capability: e.target.value } : x)))}
                      placeholder="capability"
                      style={{ padding: '6px 8px', fontSize: 13 }}
                      disabled={!!n.artifact_id}
                    />
                    <span style={{ fontSize: 13, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>so that</span>
                    <input
                      value={n.outcome}
                      onChange={(e) => setNeeds(needs.map((x) => (x.id === n.id ? { ...x, outcome: e.target.value } : x)))}
                      placeholder="outcome"
                      style={{ padding: '6px 8px', fontSize: 13 }}
                      disabled={!!n.artifact_id}
                    />
                    {!n.artifact_id && (
                      <button
                        onClick={() => setNeeds(needs.filter((x) => x.id !== n.id))}
                        style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                        title="Remove need"
                      >
                        ✕
                      </button>
                    )}
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--neutral)', fontStyle: 'italic' }}>
                    As {p.name.trim()}, I need {n.capability || '…'} so that {n.outcome || '…'}
                  </div>
                </div>
              ) : null
            )}
            <button
              className="button-secondary"
              style={{ padding: '6px 12px', fontSize: 13 }}
              onClick={() => setNeeds([...needs, { id: newEntryId(), persona_id: p.id, capability: '', outcome: '' }])}
            >
              + Add need for {p.name}
            </button>
          </div>
        ) : null
      )}
      {orphanNeeds.length > 0 && (
        <div className="card" style={{ marginBottom: 12, border: '1px solid var(--tint-yellow-border)', background: 'var(--tint-yellow)' }}>
          <div style={{ fontWeight: 600, color: 'var(--warning-text)', marginBottom: 4 }}>Needs without a persona</div>
          <div style={{ fontSize: 12, color: 'var(--warning-text)', marginBottom: 10 }}>
            The persona these needs belonged to was removed or unnamed. Reassign each need to a
            persona, or remove it.
          </div>
          {orphanNeeds.map((n) => (
            <div key={n.id} style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
              {n.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
              <span style={{ flex: 1, fontSize: 13, color: 'var(--text)' }}>
                I need {n.capability || '…'} so that {n.outcome || '…'}
              </span>
              <select
                value=""
                onChange={(e) => {
                  const personaId = e.target.value;
                  if (personaId) setNeeds(needs.map((x) => (x.id === n.id ? { ...x, persona_id: personaId } : x)));
                }}
                style={{ padding: '6px 8px', fontSize: 13, width: 200 }}
                disabled={usablePersonas.length === 0}
              >
                <option value="">Assign to persona…</option>
                {usablePersonas.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </select>
              {!n.artifact_id && (
                <button
                  onClick={() => setNeeds(needs.filter((x) => x.id !== n.id))}
                  style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                  title="Remove need"
                >
                  ✕
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
