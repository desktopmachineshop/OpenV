import {
  notesPanelHazardText,
  notesPanelNeedText,
  notesPanelNfrText,
  notesPanelPersonaText,
  notesPanelRequirementText,
  wizardHazardText,
  wizardNeedText,
  wizardNfrText,
  wizardPersonaText,
  wizardRequirementText,
  wizardTestStubText,
} from './artifactTemplates';
import { planSuggestion } from './suggestionDrafts';

// What the guided wizard writes into an artifact is trimmed, field by field,
// as the notes panel's templates are: a space typed at either end of a
// persona's name, role, goals or pains, or of a hazard's harm, never reaches
// a title, a sentence or a body (#379, bug 110).
describe('the guided wizard trims what it writes', () => {
  const persona = {
    id: 'p-1',
    name: ' Pat the Planner ',
    role: ' Maintenance planner ',
    goals: '\nPlan downtime\n',
    pains: ' Surprise breakdowns ',
  };

  it('writes a persona with no space at either end of a field', () => {
    expect(wizardPersonaText(persona)).toEqual({
      title: 'Pat the Planner',
      body: '**Role:** Maintenance planner\n\n**Goals:**\nPlan downtime\n\n**Pain points:**\nSurprise breakdowns',
    });
  });

  it("names the persona in a need's sentence as its title does", () => {
    const need = { id: 'n-1', persona_id: 'p-1', capability: ' to see who started a job ', outcome: ' I can follow up ' };
    const sentence = 'As Pat the Planner, I need to see who started a job so that I can follow up';
    expect(wizardNeedText(need, persona)).toEqual({ title: sentence, body: sentence });
  });

  it("writes a hazard's harm with no space at either end", () => {
    const hazard = { id: 'h-1', category: 'Operational', hazard: ' Coolant mist hides the lamp ', harm: ' Operator misreads machine state ', severity: 'moderate' };
    expect(wizardHazardText(hazard)).toEqual({
      title: 'Coolant mist hides the lamp',
      body: '**Category:** Operational\n\n**Potential harm:** Operator misreads machine state\n\n**Severity:** moderate',
    });
  });
});

// Golden strings for both template variants (refactor plan Q16, F5): every
// title and body each one writes, character for character, and where each
// cuts a title. The two differ only where the plan keeps them apart; a
// change to either is a change to every artifact it writes (#379, test gap
// 115).
describe('golden strings: the guided wizard', () => {
  const persona = { id: 'p-1', name: 'Maya the Machinist', role: 'CNC operator', goals: 'Run jobs without surprises', pains: 'Spindles start without warning' };

  it('writes a persona', () => {
    expect(wizardPersonaText(persona)).toEqual({
      title: 'Maya the Machinist',
      body: '**Role:** CNC operator\n\n**Goals:**\nRun jobs without surprises\n\n**Pain points:**\nSpindles start without warning',
    });
  });

  it('writes a need as its sentence, and names "a user" without a persona', () => {
    const need = { id: 'n-1', persona_id: 'p-1', capability: 'to see the spindle state', outcome: 'I never reach into a live machine' };
    const sentence = 'As Maya the Machinist, I need to see the spindle state so that I never reach into a live machine';
    expect(wizardNeedText(need, persona)).toEqual({ title: sentence, body: sentence });
    expect(wizardNeedText({ ...need, outcome: ' ' }, undefined)).toEqual({
      title: 'As a user, I need to see the spindle state so that …',
      body: 'As a user, I need to see the spindle state so that …',
    });
  });

  it("cuts a need's title at 120 characters and keeps the whole sentence in the body", () => {
    const need = { id: 'n-1', persona_id: 'p-1', capability: `to ${'x'.repeat(100)}`, outcome: 'it works' };
    const sentence = `As Maya the Machinist, I need to ${'x'.repeat(100)} so that it works`;
    const text = wizardNeedText(need, persona);
    expect(text.title).toBe(`${sentence.slice(0, 117)}…`);
    expect(text.body).toBe(sentence);
  });

  it('writes a requirement with its fit criterion, its title cut at 120', () => {
    expect(wizardRequirementText({ id: 'r-1', need_id: 'n-1', text: ' The spindle shall stop within 200 ms. ', fit_criterion: ' Ten trials. ', verification_method: 'test' })).toEqual({
      title: 'The spindle shall stop within 200 ms.',
      body: 'The spindle shall stop within 200 ms.\n\n**Fit criterion:** Ten trials.',
    });
    expect(wizardRequirementText({ id: 'r-1', need_id: 'n-1', text: 'r'.repeat(120), fit_criterion: '', verification_method: 'test' })).toEqual({
      title: 'r'.repeat(120),
      body: 'r'.repeat(120),
    });
    expect(wizardRequirementText({ id: 'r-1', need_id: 'n-1', text: 'r'.repeat(121), fit_criterion: '', verification_method: 'test' }).title).toBe(
      `${'r'.repeat(117)}…`
    );
  });

  it("cuts an NFR's title at 100 characters, not 120", () => {
    const nfr = (text: string, fit = '') => ({ id: 'f-1', category: 'Performance', text, fit_criterion: fit, verification_method: 'test' });
    expect(wizardNfrText(nfr('n'.repeat(100)))).toEqual({ title: 'n'.repeat(100), body: 'n'.repeat(100) });
    expect(wizardNfrText(nfr('n'.repeat(101), 'Measured.'))).toEqual({
      title: `${'n'.repeat(97)}…`,
      body: `${'n'.repeat(101)}\n\n**Fit criterion:** Measured.`,
    });
  });

  it('writes a hazard, its title never cut', () => {
    const long = `A ${'h'.repeat(130)}`;
    expect(wizardHazardText({ id: 'h-1', category: 'Safety', hazard: long, harm: 'Crushed fingers', severity: 'moderate' })).toEqual({
      title: long,
      body: '**Category:** Safety\n\n**Potential harm:** Crushed fingers\n\n**Severity:** moderate',
    });
  });

  it("writes a requirement's verification stub, its title never cut", () => {
    const title = 'v'.repeat(130);
    expect(wizardTestStubText({ title })).toEqual({
      title: `Verify: ${title}`,
      body: `Test case stub for requirement: ${title}\n\nDefine steps, preconditions and expected results.`,
    });
  });
});

describe('golden strings: the notes panel', () => {
  it('writes a persona, its title cut at 120', () => {
    expect(notesPanelPersonaText('Maya the Machinist', { kind: 'persona', role: ' CNC operator ', goals: 'Run jobs', pains: 'Surprises' })).toEqual({
      title: 'Maya the Machinist',
      body: '**Role:** CNC operator\n\n**Goals:**\nRun jobs\n\n**Pain points:**\nSurprises',
    });
    expect(notesPanelPersonaText('p'.repeat(121), { kind: 'persona' })).toEqual({
      title: `${'p'.repeat(117)}…`,
      body: '**Role:** \n\n**Goals:**\n\n\n**Pain points:**\n',
    });
  });

  it('writes a need as the wizard does', () => {
    const sentence = 'As the chief engineer, I need to swap the robot so that the line keeps running';
    expect(notesPanelNeedText('to swap the robot', { kind: 'need', persona: ' the chief engineer ', outcome: 'the line keeps running' })).toEqual({
      title: sentence,
      body: sentence,
    });
    expect(notesPanelNeedText('to export the matrix', { kind: 'need' })).toEqual({
      title: 'As a user, I need to export the matrix so that …',
      body: 'As a user, I need to export the matrix so that …',
    });
  });

  it('writes a requirement with its fit criterion, its title cut at 120', () => {
    expect(notesPanelRequirementText('The spindle shall stop.', 'Ten trials.')).toEqual({
      title: 'The spindle shall stop.',
      body: 'The spindle shall stop.\n\n**Fit criterion:** Ten trials.',
    });
    expect(notesPanelRequirementText('r'.repeat(121), '')).toEqual({ title: `${'r'.repeat(117)}…`, body: 'r'.repeat(121) });
  });

  it("cuts an NFR's title at 100 characters, not 120", () => {
    expect(notesPanelNfrText('n'.repeat(100), '')).toEqual({ title: 'n'.repeat(100), body: 'n'.repeat(100) });
    expect(notesPanelNfrText('n'.repeat(101), 'Measured.')).toEqual({
      title: `${'n'.repeat(97)}…`,
      body: `${'n'.repeat(101)}\n\n**Fit criterion:** Measured.`,
    });
  });

  it('writes a hazard, its title cut at 120', () => {
    expect(notesPanelHazardText('Pinch point at the fence', 'Safety', 'Medium', { kind: 'hazard', harm: ' Crushed fingers ' })).toEqual({
      title: 'Pinch point at the fence',
      body: '**Category:** Safety\n\n**Potential harm:** Crushed fingers\n\n**Severity:** Medium',
    });
    expect(notesPanelHazardText('h'.repeat(121), 'Safety', 'Medium', { kind: 'hazard' }).title).toBe(`${'h'.repeat(117)}…`);
  });

  // The path a card takes: planSuggestion reads it, trims it, picks the
  // defaults (a "Medium" severity, a category it knows) and writes it with
  // the templates above.
  it('reaches the same strings through planSuggestion', () => {
    const persona = planSuggestion({ kind: 'persona', name: ' Maya the Machinist ', role: 'CNC operator', goals: 'Run jobs', pains: 'Surprises' });
    if (persona.outcome !== 'artifact') throw new Error('expected an artifact');
    expect([persona.draft.title, persona.draft.body]).toEqual([
      'Maya the Machinist',
      '**Role:** CNC operator\n\n**Goals:**\nRun jobs\n\n**Pain points:**\nSurprises',
    ]);

    const hazard = planSuggestion({ kind: 'hazard', hazard: 'Pinch point at the fence', category: 'safety', harm: 'Crushed fingers' });
    if (hazard.outcome !== 'artifact') throw new Error('expected an artifact');
    expect([hazard.draft.title, hazard.draft.body]).toEqual([
      'Pinch point at the fence',
      '**Category:** Safety\n\n**Potential harm:** Crushed fingers\n\n**Severity:** Medium',
    ]);

    const nfr = planSuggestion({ kind: 'nfr', text: 'n'.repeat(101), fit_criterion: 'Measured.', category: 'Performance' });
    if (nfr.outcome !== 'artifact') throw new Error('expected an artifact');
    expect([nfr.draft.title, nfr.draft.body]).toEqual([`${'n'.repeat(97)}…`, `${'n'.repeat(101)}\n\n**Fit criterion:** Measured.`]);
  });
});
