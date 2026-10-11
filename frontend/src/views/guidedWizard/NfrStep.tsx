import React from 'react';
import { NFR_CATEGORIES, NfrEntry, newEntryId } from '../../components/wizard/wizardEntries';
import { VERIFICATION_METHODS } from '../../components/wizard/wizardSuggestions';
import { helpText } from './stepStyles';

interface NfrStepProps {
  nfrs: NfrEntry[];
  setNfrs: (next: NfrEntry[]) => void;
  /** Categories opened or closed by hand; the rest open when they hold entries. */
  openNfrCategories: Record<string, boolean>;
  setOpenNfrCategories: (next: Record<string, boolean>) => void;
}

/**
 * Step 5: the non-functional requirements, by quality attribute.
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderNfrStep = ({ nfrs, setNfrs, openNfrCategories, setOpenNfrCategories }: NfrStepProps): React.ReactElement => {
  return (
    <div>
      <div className="card" style={{ marginBottom: 12 }}>
        <h3>Non-functional requirements &amp; constraints</h3>
        <div style={{ ...helpText, marginBottom: 0 }}>
          Expand the quality attributes that matter for this product and add requirements under
          each — every attribute you use becomes its own section in the requirements module.
        </div>
      </div>
      {NFR_CATEGORIES.map((cat) => {
        const catEntries = nfrs.filter((n) => n.category === cat);
        const open = openNfrCategories[cat] ?? catEntries.length > 0;
        return (
          <div key={cat} className="card" style={{ marginBottom: 10, padding: 14 }}>
            <div
              style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer' }}
              onClick={() => setOpenNfrCategories({ ...openNfrCategories, [cat]: !open })}
            >
              <input
                type="checkbox"
                checked={open}
                onChange={() => setOpenNfrCategories({ ...openNfrCategories, [cat]: !open })}
                style={{ width: 'auto' }}
                onClick={(e) => e.stopPropagation()}
              />
              <span style={{ fontWeight: 600, color: 'var(--text)', flex: 1 }}>{cat}</span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                {catEntries.length > 0 ? `${catEntries.length} requirement${catEntries.length > 1 ? 's' : ''}` : ''}
              </span>
            </div>
            {open && (
              <div style={{ marginTop: 10 }}>
                {catEntries.map((n) => (
                  <div key={n.id} style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
                    {n.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                    <input
                      value={n.text}
                      onChange={(e) => setNfrs(nfrs.map((x) => (x.id === n.id ? { ...x, text: e.target.value } : x)))}
                      placeholder={`The system shall … (${cat.toLowerCase()})`}
                      style={{ padding: '6px 8px', fontSize: 13 }}
                      disabled={!!n.artifact_id}
                    />
                    <select
                      value={n.verification_method}
                      onChange={(e) => setNfrs(nfrs.map((x) => (x.id === n.id ? { ...x, verification_method: e.target.value } : x)))}
                      style={{ padding: '6px 8px', fontSize: 13, width: 150 }}
                      disabled={!!n.artifact_id}
                    >
                      {VERIFICATION_METHODS.map((m) => (
                        <option key={m} value={m}>{m}</option>
                      ))}
                    </select>
                    {!n.artifact_id && (
                      <button
                        onClick={() => setNfrs(nfrs.filter((x) => x.id !== n.id))}
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
                    setNfrs([...nfrs, { id: newEntryId(), category: cat, text: '', fit_criterion: '', verification_method: 'test' }])
                  }
                >
                  + Add {cat.toLowerCase()} requirement
                </button>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};
