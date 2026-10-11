import React from 'react';
import { NeedEntry, PersonaEntry, ReqEntry, newEntryId } from '../../components/wizard/wizardEntries';
import { VERIFICATION_METHODS } from '../../components/wizard/wizardSuggestions';
import { helpText } from './stepStyles';

interface RequirementsStepProps {
  personas: PersonaEntry[];
  needs: NeedEntry[];
  requirements: ReqEntry[];
  setRequirements: (next: ReqEntry[]) => void;
}

/**
 * Step 4: the requirements under their needs, and those whose need is gone.
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderRequirementsStep = ({ personas, needs, requirements, setRequirements }: RequirementsStepProps): React.ReactElement => {
  const usableNeeds = needs.filter((n) => n.capability.trim());
  const orphanReqs = requirements.filter((r) => !needs.some((n) => n.id === r.need_id && n.capability.trim()));
  return (
    <div>
      <div className="card" style={{ marginBottom: 12 }}>
        <h3>Requirements</h3>
        <div style={{ ...helpText, marginBottom: 0 }}>
          Turn each user need into one or more testable "The system shall …" requirements with a fit
          criterion and a verification method.
        </div>
      </div>
      {usableNeeds.length === 0 && (
        <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
          No user needs defined — go back to step 3.
        </div>
      )}
      {needs.map((n) => {
        if (!n.capability.trim()) return null;
        const personaName = personas.find((p) => p.id === n.persona_id)?.name.trim() || 'a user';
        return (
          <div key={n.id} className="card" style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 10 }}>
              Need: <em>As {personaName}, I need {n.capability} so that {n.outcome || '…'}</em>
            </div>
            {requirements.map((r) =>
              r.need_id === n.id ? (
                <div key={r.id} style={{ border: '1px solid var(--border-soft)', borderRadius: 4, padding: 10, marginBottom: 8 }}>
                  <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
                    {r.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                    <input
                      value={r.text}
                      onChange={(e) => setRequirements(requirements.map((x) => (x.id === r.id ? { ...x, text: e.target.value } : x)))}
                      placeholder="The system shall …"
                      style={{ padding: '6px 8px', fontSize: 13 }}
                      disabled={!!r.artifact_id}
                    />
                    {!r.artifact_id && (
                      <button
                        onClick={() => setRequirements(requirements.filter((x) => x.id !== r.id))}
                        style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                        title="Remove requirement"
                      >
                        ✕
                      </button>
                    )}
                  </div>
                  <div style={{ display: 'flex', gap: 8 }}>
                    <textarea
                      rows={2}
                      value={r.fit_criterion}
                      onChange={(e) => setRequirements(requirements.map((x) => (x.id === r.id ? { ...x, fit_criterion: e.target.value } : x)))}
                      placeholder="Fit criterion — how do we know it is met?"
                      style={{ fontSize: 13, minHeight: 50, flex: 1 }}
                      disabled={!!r.artifact_id}
                    />
                    <div style={{ width: 170 }}>
                      <label style={{ fontSize: 11, color: 'var(--text-muted)' }}>Verification method</label>
                      <select
                        value={r.verification_method}
                        onChange={(e) => setRequirements(requirements.map((x) => (x.id === r.id ? { ...x, verification_method: e.target.value } : x)))}
                        style={{ padding: '6px 8px', fontSize: 13 }}
                        disabled={!!r.artifact_id}
                      >
                        {VERIFICATION_METHODS.map((m) => (
                          <option key={m} value={m}>{m}</option>
                        ))}
                      </select>
                    </div>
                  </div>
                </div>
              ) : null
            )}
            <button
              className="button-secondary"
              style={{ padding: '6px 12px', fontSize: 13 }}
              onClick={() =>
                setRequirements([
                  ...requirements,
                  { id: newEntryId(), need_id: n.id, text: 'The system shall ', fit_criterion: '', verification_method: 'test' },
                ])
              }
            >
              + Add requirement
            </button>
          </div>
        );
      })}
      {orphanReqs.length > 0 && (
        <div className="card" style={{ marginBottom: 12, border: '1px solid var(--tint-yellow-border)', background: 'var(--tint-yellow)' }}>
          <div style={{ fontWeight: 600, color: 'var(--warning-text)', marginBottom: 4 }}>Requirements without a user need</div>
          <div style={{ fontSize: 12, color: 'var(--warning-text)', marginBottom: 10 }}>
            The need these requirements derived from was removed or left empty. Reattach each
            requirement to a need, or remove it.
          </div>
          {orphanReqs.map((r) => (
            <div key={r.id} style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
              {r.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
              <span style={{ flex: 1, fontSize: 13, color: 'var(--text)' }}>{r.text || '…'}</span>
              <select
                value=""
                onChange={(e) => {
                  const needId = e.target.value;
                  if (needId) setRequirements(requirements.map((x) => (x.id === r.id ? { ...x, need_id: needId } : x)));
                }}
                style={{ padding: '6px 8px', fontSize: 13, width: 220 }}
                disabled={usableNeeds.length === 0}
              >
                <option value="">Attach to need…</option>
                {usableNeeds.map((n) => (
                  <option key={n.id} value={n.id}>{n.capability}</option>
                ))}
              </select>
              {!r.artifact_id && (
                <button
                  onClick={() => setRequirements(requirements.filter((x) => x.id !== r.id))}
                  style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                  title="Remove requirement"
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
