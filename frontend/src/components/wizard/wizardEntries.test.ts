import {
  FUNCTIONAL_TEST_CATEGORY,
  HAZARD_CATEGORIES,
  NFR_CATEGORIES,
  SUBSECTION_SPECS,
  TEST_CATEGORIES,
  canonicalNfrCategory,
  newEntryId,
  normalizeWizardAnswers,
  subSectionKey,
} from './wizardEntries';

describe('newEntryId', () => {
  it('generates non-empty unique ids', () => {
    const ids = new Set(Array.from({ length: 100 }, () => newEntryId()));
    expect(ids.size).toBe(100);
    ids.forEach((id) => expect(id.length).toBeGreaterThan(0));
  });
});

describe('normalizeWizardAnswers', () => {
  it('returns empty lists for empty or malformed answers', () => {
    expect(normalizeWizardAnswers({})).toEqual({
      personas: [],
      needs: [],
      requirements: [],
      nfrs: [],
      hazards: [],
      appliedSuggestionKeys: [],
    });
    const junk = normalizeWizardAnswers({
      step_2: { personas: 'nope' },
      step_3: { needs: [null, 42] },
      step_4: 7,
      copilot_applied: 'nope',
    } as any);
    expect(junk.personas).toEqual([]);
    expect(junk.needs).toEqual([]);
    expect(junk.requirements).toEqual([]);
    expect(junk.appliedSuggestionKeys).toEqual([]);
  });

  it('migrates legacy index-based references to stable ids', () => {
    const norm = normalizeWizardAnswers({
      step_2: {
        personas: [
          { name: 'Maya', role: 'Machinist', goals: 'g', pains: 'p' },
          { name: 'Ola', role: 'Operator', goals: 'g2', pains: 'p2' },
        ],
      },
      step_3: {
        needs: [
          { persona_index: 1, capability: 'cap A', outcome: 'out A' },
          { persona_index: 0, capability: 'cap B', outcome: 'out B' },
        ],
      },
      step_4: {
        requirements: [{ need_index: 1, text: 'The system shall X', fit_criterion: 'f', verification_method: 'test' }],
      },
    });
    expect(norm.personas).toHaveLength(2);
    norm.personas.forEach((p) => expect(p.id).toBeTruthy());
    // Need 0 pointed at persona index 1 (Ola); need 1 at persona index 0 (Maya).
    expect(norm.needs[0].persona_id).toBe(norm.personas[1].id);
    expect(norm.needs[1].persona_id).toBe(norm.personas[0].id);
    // Requirement pointed at need index 1 ("cap B").
    expect(norm.requirements[0].need_id).toBe(norm.needs[1].id);
    // Legacy index fields are dropped from the normalized entries.
    expect(norm.needs[0]).not.toHaveProperty('persona_index');
    expect(norm.requirements[0]).not.toHaveProperty('need_index');
  });

  it('preserves existing ids and id-based references untouched', () => {
    const answers = {
      step_2: { personas: [{ id: 'p-1', name: 'Maya', role: '', goals: '', pains: '', artifact_id: 'art-p' }] },
      step_3: { needs: [{ id: 'n-1', persona_id: 'p-1', capability: 'c', outcome: 'o' }] },
      step_4: {
        requirements: [
          { id: 'r-1', need_id: 'n-1', text: 't', fit_criterion: '', verification_method: 'analysis', artifact_id: 'art-r' },
        ],
      },
    };
    const norm = normalizeWizardAnswers(answers);
    expect(norm.personas[0].id).toBe('p-1');
    expect(norm.personas[0].artifact_id).toBe('art-p');
    expect(norm.needs[0].id).toBe('n-1');
    expect(norm.needs[0].persona_id).toBe('p-1');
    expect(norm.requirements[0].need_id).toBe('n-1');
    expect(norm.requirements[0].verification_method).toBe('analysis');
    expect(norm.requirements[0].artifact_id).toBe('art-r');
  });

  it('prefers an id reference over a stale legacy index when both exist', () => {
    const norm = normalizeWizardAnswers({
      step_2: {
        personas: [
          { id: 'p-a', name: 'A', role: '', goals: '', pains: '' },
          { id: 'p-b', name: 'B', role: '', goals: '', pains: '' },
        ],
      },
      step_3: { needs: [{ id: 'n-1', persona_id: 'p-b', persona_index: 0, capability: 'c', outcome: 'o' }] },
    });
    expect(norm.needs[0].persona_id).toBe('p-b');
  });

  it('maps out-of-range or invalid legacy indices to an unassigned reference', () => {
    const norm = normalizeWizardAnswers({
      step_2: { personas: [{ name: 'Only', role: '', goals: '', pains: '' }] },
      step_3: {
        needs: [
          { persona_index: 5, capability: 'dangling', outcome: '' },
          { persona_index: -1, capability: 'negative', outcome: '' },
          { persona_index: 0.5, capability: 'fractional', outcome: '' },
          { capability: 'missing', outcome: '' },
        ],
      },
      step_4: { requirements: [{ need_index: 99, text: 'orphan', fit_criterion: '', verification_method: 'test' }] },
    });
    norm.needs.forEach((n) => expect(n.persona_id).toBe(''));
    expect(norm.requirements[0].need_id).toBe('');
  });

  it('assigns ids to nfrs and hazards and fills defaults', () => {
    const norm = normalizeWizardAnswers({
      step_5: { nfrs: [{ category: 'Security', text: 't', fit_criterion: '' }] },
      step_6: { hazards: [{ hazard: 'h', harm: '' }] },
    });
    expect(norm.nfrs[0].id).toBeTruthy();
    expect(norm.nfrs[0].verification_method).toBe('test');
    expect(norm.hazards[0].id).toBeTruthy();
    expect(norm.hazards[0].severity).toBe('moderate');
  });

  it('extracts applied copilot suggestion keys, dropping non-strings', () => {
    const norm = normalizeWizardAnswers({
      copilot_applied: ['msg-1:0', 'msg-1:2', 42, null, ''],
    } as any);
    expect(norm.appliedSuggestionKeys).toEqual(['msg-1:0', 'msg-1:2']);
  });

  it('is stable across repeated normalization once ids exist', () => {
    const first = normalizeWizardAnswers({
      step_2: { personas: [{ name: 'Maya', role: '', goals: '', pains: '' }] },
      step_3: { needs: [{ persona_index: 0, capability: 'c', outcome: 'o' }] },
    });
    const roundTripped = normalizeWizardAnswers({
      step_2: { personas: first.personas },
      step_3: { needs: first.needs },
    });
    expect(roundTripped.personas).toEqual(first.personas);
    expect(roundTripped.needs).toEqual(first.needs);
  });
});

// A category is a place in the document, not a prefix on a title: the wizard
// files each NFR and hazard under a section named for its category, so the
// key it computes has to name a section that actually gets created.
describe('canonicalNfrCategory', () => {
  it('maps free-form text onto the canonical category', () => {
    expect(canonicalNfrCategory('performance')).toBe('Performance');
    expect(canonicalNfrCategory('  REGULATORY  ')).toBe('Regulatory');
  });

  it('returns empty for a category the step does not offer', () => {
    expect(canonicalNfrCategory('Sustainability')).toBe('');
    expect(canonicalNfrCategory(undefined)).toBe('');
    expect(canonicalNfrCategory(null)).toBe('');
  });
});

describe('subSectionKey', () => {
  it('files a known category under its own sub-section', () => {
    expect(subSectionKey('nfrs', 'Performance')).toBe('nfrs:Performance');
    expect(subSectionKey('nfrs', 'Regulatory')).toBe('nfrs:Regulatory');
    expect(subSectionKey('hazards', 'Safety')).toBe('hazards:Safety');
  });

  it('files a verification stub beside the kind of requirement it verifies', () => {
    expect(subSectionKey('tests', FUNCTIONAL_TEST_CATEGORY)).toBe('tests:Functional');
    expect(subSectionKey('tests', 'Regulatory')).toBe('tests:Regulatory');
    // Every quality attribute an NFR can carry has a verification section to
    // land in, so a stub never falls back to the flat list by accident.
    NFR_CATEGORIES.forEach((c) => expect(subSectionKey('tests', c)).toBe(`tests:${c}`));
  });

  it('falls back to the step heading for a category the step no longer offers', () => {
    // Inventing "nfrs:Sustainability" would create a heading no step can
    // refill, so the draft sits directly under the NFR section instead.
    expect(subSectionKey('nfrs', 'Sustainability')).toBe('nfrs');
    expect(subSectionKey('nfrs', '')).toBe('nfrs');
    expect(subSectionKey('hazards', 'Financial')).toBe('hazards');
  });

  it('leaves a step without sub-sections alone', () => {
    expect(subSectionKey('personas', 'Performance')).toBe('personas');
    expect(subSectionKey('tests', 'Sustainability')).toBe('tests');
  });

  it('leads the verification sections with Functional', () => {
    expect(TEST_CATEGORIES[0]).toBe(FUNCTIONAL_TEST_CATEGORY);
    expect(TEST_CATEGORIES.slice(1)).toEqual(NFR_CATEGORIES);
  });

  it('names a section for every category the steps offer', () => {
    NFR_CATEGORIES.forEach((c) => expect(SUBSECTION_SPECS.nfrs.title(c)).toBe(c));
    HAZARD_CATEGORIES.forEach((c) => expect(SUBSECTION_SPECS.hazards.title(c)).toBe(`${c} Hazards`));
    TEST_CATEGORIES.forEach((c) => expect(SUBSECTION_SPECS.tests.title(c)).toBe(c));
  });
});
