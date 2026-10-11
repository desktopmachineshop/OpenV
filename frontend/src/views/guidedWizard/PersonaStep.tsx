import React from 'react';
import { RepeatingCardList } from '../../components/wizard/RepeatingCardList';
import { PersonaEntry, newEntryId } from '../../components/wizard/wizardEntries';
import { helpText } from './stepStyles';

interface PersonaStepProps {
  personas: PersonaEntry[];
  setPersonas: (next: PersonaEntry[]) => void;
}

/**
 * Step 2: the personas, each a card; one with a draft is locked.
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderPersonaStep = ({ personas, setPersonas }: PersonaStepProps): React.ReactElement => {
  return (
    <div>
      <div className="card" style={{ marginBottom: 12 }}>
        <h3>Personas</h3>
        <div style={{ ...helpText, marginBottom: 0 }}>
          Describe the key people who will use the product. Each persona becomes a draft artifact
          that user needs link back to. Personas already created are marked with a green dot.
        </div>
      </div>
      <RepeatingCardList<PersonaEntry>
        items={personas}
        onChange={setPersonas}
        makeNew={() => ({ id: newEntryId(), name: '', role: '', goals: '', pains: '' })}
        itemKey={(p) => p.id}
        addLabel="Add persona"
        emptyText="No personas yet — add the first one."
        renderItem={(p, i, update) => (
          <div>
            <div style={{ display: 'flex', gap: 10, marginBottom: 10, alignItems: 'center' }}>
              {p.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
              <input
                value={p.name}
                onChange={(e) => update({ name: e.target.value })}
                placeholder="Persona name (e.g. Maya the Machinist)"
                style={{ fontWeight: 600 }}
                disabled={!!p.artifact_id}
              />
            </div>
            <div className="form-group">
              <label style={{ fontSize: 12 }}>Role</label>
              <input value={p.role} onChange={(e) => update({ role: e.target.value })} placeholder="Their job / context" disabled={!!p.artifact_id} />
            </div>
            <div className="form-group">
              <label style={{ fontSize: 12 }}>Goals</label>
              <textarea rows={2} value={p.goals} onChange={(e) => update({ goals: e.target.value })} placeholder="What are they trying to achieve?" disabled={!!p.artifact_id} />
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label style={{ fontSize: 12 }}>Pain points</label>
              <textarea rows={2} value={p.pains} onChange={(e) => update({ pains: e.target.value })} placeholder="What frustrates them today?" disabled={!!p.artifact_id} />
            </div>
          </div>
        )}
        canRemove={(p) => !p.artifact_id}
      />
    </div>
  );
};
