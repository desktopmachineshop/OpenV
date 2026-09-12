import { headingsFor, planSuggestion } from './suggestionDrafts';

describe('what a suggestion means', () => {
  it('turns a requirement into the artifact it describes', () => {
    const plan = planSuggestion({
      kind: 'requirement',
      text: 'The system shall stop within 200 ms of the estop.',
      fit_criterion: 'Measured at the tool tip, ten trials.',
      verification_method: 'test',
    });
    expect(plan.outcome).toBe('artifact');
    if (plan.outcome !== 'artifact') return;
    expect(plan.draft.type).toBe('requirement');
    expect(plan.draft.title).toBe('The system shall stop within 200 ms of the estop.');
    expect(plan.draft.body).toContain('**Fit criterion:** Measured at the tool tip');
    expect(plan.draft.attributes.verification_method).toBe('test');
    expect(plan.draft.sectionKey).toBe('requirements');
  });

  it('writes a need as the sentence the wizard writes', () => {
    const plan = planSuggestion({
      kind: 'need',
      persona: 'the chief engineer',
      capability: 'to swap the robot without re-cutting the enclosure',
      outcome: 'the line keeps running',
    });
    if (plan.outcome !== 'artifact') throw new Error('expected an artifact');
    expect(plan.draft.type).toBe('user-need');
    expect(plan.draft.body).toBe(
      'As the chief engineer, I need to swap the robot without re-cutting the enclosure so that the line keeps running'
    );
  });

  // A need with nobody named still reads as a sentence rather than trailing
  // off into "As , I need".
  it('names a user when the suggestion names no persona', () => {
    const plan = planSuggestion({ kind: 'need', capability: 'to export the matrix' });
    if (plan.outcome !== 'artifact') throw new Error('expected an artifact');
    expect(plan.draft.body).toContain('As a user, I need to export the matrix');
  });

  it('files an NFR and a hazard under their category sub-heading', () => {
    const nfr = planSuggestion({ kind: 'nfr', text: 'Runs for 8 hours.', category: 'Performance' });
    if (nfr.outcome !== 'artifact') throw new Error('expected an artifact');
    expect(nfr.draft.sectionKey).toBe('nfrs:Performance');
    expect(nfr.draft.attributes.category).toBe('Performance');

    const hazard = planSuggestion({ kind: 'hazard', hazard: 'Pinch point at the fence', category: 'Safety' });
    if (hazard.outcome !== 'artifact') throw new Error('expected an artifact');
    expect(hazard.draft.type).toBe('hazard');
    expect(hazard.draft.sectionKey).toBe('hazards:Safety');
  });

  it('reads framing as a change to the project, not an artifact', () => {
    const plan = planSuggestion({ kind: 'framing', field: 'vision', text: 'A mill on every bench.' });
    expect(plan.outcome).toBe('framing');
    if (plan.outcome !== 'framing') return;
    expect(plan.update).toEqual({ field: 'vision', text: 'A mill on every bench.' });
  });

  // A refusal has to say why. A button that does nothing and explains nothing
  // is what made the assistant's suggestions feel broken to begin with.
  it('refuses with a reason a person can act on', () => {
    const empty = planSuggestion({ kind: 'requirement', text: '   ' });
    expect(empty.outcome).toBe('refused');
    if (empty.outcome === 'refused') expect(empty.reason).toContain('no text');

    const unknown = planSuggestion({ kind: 'invention' });
    if (unknown.outcome !== 'refused') throw new Error('expected a refusal');
    expect(unknown.reason).toContain('invention');

    const badField = planSuggestion({ kind: 'framing', field: 'mood', text: 'Upbeat' });
    if (badField.outcome !== 'refused') throw new Error('expected a refusal');
    expect(badField.reason).toContain('mood');
  });

  // Titles are a line in a tree, not a paragraph.
  it('shortens a title that runs on, keeping the whole statement in the body', () => {
    const long = `The system shall ${'x'.repeat(300)}`;
    const plan = planSuggestion({ kind: 'requirement', text: long });
    if (plan.outcome !== 'artifact') throw new Error('expected an artifact');
    expect(plan.draft.title.length).toBeLessThanOrEqual(120);
    expect(plan.draft.title.endsWith('…')).toBe(true);
    expect(plan.draft.body).toContain(long);
  });
});

describe('the headings a suggestion lands under', () => {
  it('names one heading for a plain section', () => {
    expect(headingsFor('requirements')).toEqual([
      { key: 'requirements', title: 'Requirements', sort: 30, parentKey: '' },
    ]);
  });

  // Outermost first, so a caller can create the parent before the child.
  it('names the parent before its category sub-heading', () => {
    const headings = headingsFor('hazards:Safety');
    expect(headings.map((h) => h.key)).toEqual(['hazards', 'hazards:Safety']);
    expect(headings[0].parentKey).toBe('');
    expect(headings[1].parentKey).toBe('hazards');
    expect(headings[1].title).toBe('Safety Hazards');
  });

  it('has nothing to say about a section it does not know', () => {
    expect(headingsFor('invented')).toEqual([]);
    expect(headingsFor('invented:Category')).toEqual([]);
  });
});
