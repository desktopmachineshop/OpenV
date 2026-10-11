import React from 'react';
import { HAZARD_CATEGORIES, HazardEntry, newEntryId } from '../../components/wizard/wizardEntries';
import { SEVERITIES } from '../../components/wizard/wizardSuggestions';
import { helpText } from './stepStyles';
import { HAZARD_STEP } from './steps';

interface HazardStepProps {
  hazards: HazardEntry[];
  setHazards: (next: HazardEntry[]) => void;
  /** Categories opened or closed by hand; the rest open when they hold entries. */
  openHazardCategories: Record<string, boolean>;
  setOpenHazardCategories: (next: Record<string, boolean>) => void;
}

/**
 * Step 6: the hazards, by risk category (optional).
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderHazardStep = ({ hazards, setHazards, openHazardCategories, setOpenHazardCategories }: HazardStepProps): React.ReactElement => {
  const hazardHints: Record<string, string> = {
    Safety: 'harm to people when the product fails or is misused',
    Technical: 'design or implementation risks that threaten the product working at all',
    Security: 'malicious use, data exposure, or abuse of the product',
    Programme: 'schedule, cost, dependency, or resourcing risks to delivery',
    Operational: 'in-service risks — environment, wear, support, or user error',
  };
  return (
    <div>
      <div className="card" style={{ marginBottom: 12 }}>
        <h3>Hazards &amp; risks (optional)</h3>
        <div style={{ ...helpText, marginBottom: 0 }}>
          Work through the risk categories that apply — each becomes its own section in the
          requirements module. Skip this step if none apply.
        </div>
      </div>
      {HAZARD_CATEGORIES.map((cat) => {
        const catEntries = hazards.filter((h) => h.category === cat);
        const open = openHazardCategories[cat] ?? catEntries.length > 0;
        return (
          <div key={cat} className="card" style={{ marginBottom: 10, padding: 14 }}>
            <div
              style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer' }}
              onClick={() => setOpenHazardCategories({ ...openHazardCategories, [cat]: !open })}
            >
              <input
                type="checkbox"
                checked={open}
                onChange={() => setOpenHazardCategories({ ...openHazardCategories, [cat]: !open })}
                style={{ width: 'auto' }}
                onClick={(e) => e.stopPropagation()}
              />
              <span style={{ fontWeight: 600, color: 'var(--text)' }}>{cat}</span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)', flex: 1 }}>{hazardHints[cat]}</span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                {catEntries.length > 0 ? `${catEntries.length} hazard${catEntries.length > 1 ? 's' : ''}` : ''}
              </span>
            </div>
            {open && (
              <div style={{ marginTop: 10 }}>
                {catEntries.map((h) => (
                  <div key={h.id} style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
                    {h.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                    <input
                      value={h.hazard}
                      onChange={(e) => setHazards(hazards.map((x) => (x.id === h.id ? { ...x, hazard: e.target.value } : x)))}
                      placeholder={`${cat} hazard (what could go wrong)`}
                      style={{ padding: '6px 8px', fontSize: 13 }}
                      disabled={!!h.artifact_id}
                    />
                    <input
                      value={h.harm}
                      onChange={(e) => setHazards(hazards.map((x) => (x.id === h.id ? { ...x, harm: e.target.value } : x)))}
                      placeholder="Potential harm / impact"
                      style={{ padding: '6px 8px', fontSize: 13 }}
                      disabled={!!h.artifact_id}
                    />
                    <select
                      value={h.severity}
                      onChange={(e) => setHazards(hazards.map((x) => (x.id === h.id ? { ...x, severity: e.target.value } : x)))}
                      style={{ padding: '6px 8px', fontSize: 13, width: 130 }}
                      disabled={!!h.artifact_id}
                    >
                      {SEVERITIES.map((s) => (
                        <option key={s} value={s}>{s}</option>
                      ))}
                    </select>
                    {!h.artifact_id && (
                      <button
                        onClick={() => setHazards(hazards.filter((x) => x.id !== h.id))}
                        style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                        title="Remove"
                      >
                        ✕
                      </button>
                    )}
                  </div>
                ))}
                <button
                  className="button-secondary"
                  style={{ padding: '5px 10px', fontSize: 12 }}
                  onClick={() =>
                    setHazards([...hazards, { id: newEntryId(), category: cat, hazard: '', harm: '', severity: HAZARD_STEP.defaultSeverity }])
                  }
                >
                  + Add {cat.toLowerCase()} hazard
                </button>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};
