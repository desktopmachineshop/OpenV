import { wizardHazardText, wizardNeedText, wizardPersonaText } from './artifactTemplates';

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
