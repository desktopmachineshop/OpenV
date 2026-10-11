import type { GuidedSession } from '../../api/client';
import { normalizeWizardAnswers } from '../../components/wizard/wizardEntries';
import type { SuggestionDraft } from '../../components/wizard/wizardSuggestions';
import { HAZARD_STEP, NEED_STEP, NFR_STEP, PERSONA_STEP, REQUIREMENT_STEP } from './steps';
import {
  readWizardAnswers,
  seedReopenedAnswers,
  serializeWizardAnswers,
  withEntryStepAnswers,
  withStubAnswers,
} from './wizardAnswers';

// Refactor plan X18a: the WizardAnswers serializer against the code that
// wrote the answers before it, copied here as it was (buildAnswersFrom from
// wizardSuggestions.ts, and the overrides, the hydrate and the reopen seed
// GuidedWizard.tsx held inline). Every comparison is of JSON text, so key
// order counts: the answers are stored data (I16).

const oldBuildAnswersFrom = (
  d: SuggestionDraft,
  applied: Record<string, boolean>,
  session: GuidedSession | null,
  stubSelected: Record<string, boolean>,
  stubCreated: Record<string, string>
): Record<string, any> => ({
  ...(session?.answers || {}),
  step_1: { vision: d.vision, problem_statement: d.problem, target_users: d.targetUsers },
  step_2: { personas: d.personas },
  step_2_ids: d.personas.map((p) => p.artifact_id).filter(Boolean),
  step_3: { needs: d.needs },
  step_3_ids: d.needs.map((n) => n.artifact_id).filter(Boolean),
  step_4: { requirements: d.requirements },
  step_4_ids: d.requirements.map((r) => r.artifact_id).filter(Boolean),
  step_5: { nfrs: d.nfrs },
  step_5_ids: d.nfrs.map((n) => n.artifact_id).filter(Boolean),
  step_6: { hazards: d.hazards },
  step_7: { selected: stubSelected, created: stubCreated },
  copilot_applied: Object.keys(applied).filter((k) => applied[k]),
});

// What each old handleNext block wrote over buildAnswers().
const oldOverrides: Record<number, (a: Record<string, any>, updated: any[], sections: Record<string, string>) => void> = {
  2: (answers, updated, sections) => {
    answers.section_ids = sections;
    answers.step_2 = { personas: updated };
    answers.step_2_ids = updated.map((p) => p.artifact_id).filter(Boolean);
  },
  3: (answers, updated, sections) => {
    answers.section_ids = sections;
    answers.step_3 = { needs: updated };
    answers.step_3_ids = updated.map((n) => n.artifact_id).filter(Boolean);
  },
  4: (answers, updated, sections) => {
    answers.section_ids = sections;
    answers.step_4 = { requirements: updated };
    answers.step_4_ids = updated.map((r) => r.artifact_id).filter(Boolean);
  },
  5: (answers, updated, sections) => {
    answers.section_ids = sections;
    answers.step_5 = { nfrs: updated };
    answers.step_5_ids = updated.map((n) => n.artifact_id).filter(Boolean);
  },
  6: (answers, updated, sections) => {
    answers.section_ids = sections;
    answers.step_6 = { hazards: updated };
  },
};

const form = (): SuggestionDraft => ({
  vision: 'V',
  problem: 'P',
  targetUsers: 'U',
  personas: [
    { id: 'p-1', name: 'Sam', role: 'Lead', goals: 'g', pains: 'p', artifact_id: 'art-1' },
    { id: 'p-2', name: 'Pat ', role: '', goals: '', pains: '' },
  ],
  needs: [{ id: 'n-1', persona_id: 'p-1', capability: 'c', outcome: 'o', artifact_id: 'art-2' }],
  requirements: [{ id: 'r-1', need_id: 'n-1', text: 't', fit_criterion: '', verification_method: 'test' }],
  nfrs: [{ id: 'f-1', category: 'Performance', text: 'n', fit_criterion: '', verification_method: 'test', artifact_id: 'art-3' }],
  hazards: [{ id: 'h-1', category: 'Safety', hazard: 'h', harm: '', severity: 'moderate', artifact_id: 'art-4' }],
  openNfr: { Performance: true },
  openHazard: {},
});

const session = (answers: Record<string, any>): GuidedSession => ({
  id: 'gs-1',
  project_id: 'p1',
  status: 'in-progress',
  current_step: 3,
  answers,
  draft_artifact_ids: [],
});

// The sessions the answers are built over: none, an empty one, one as the
// wizard wrote it, one as the server returns it (keys sorted) with unknown
// keys at every depth, and one with only unknown keys.
const sessions: [string, GuidedSession | null][] = [
  ['no session', null],
  ['empty answers', session({})],
  [
    'the wizard order',
    session({
      step_1: { vision: 'old' },
      step_2: { personas: [] },
      step_2_ids: [],
      step_7: { selected: { 'art-9': false }, created: {} },
      copilot_applied: ['m-1:1'],
      section_ids: { personas: 'art-h' },
    }),
  ],
  [
    'the server order, with unknown keys',
    session({
      copilot_applied: ['m-1:1'],
      legacy_note: 'kept',
      section_ids: { needs: 'art-n', personas: 'art-h' },
      step_1: { problem_statement: 'old', tagline: 'dropped', vision: 'old' },
      step_2: { note: 'dropped', personas: [] },
      step_3: { needs: [] },
      step_6_ids: ['written by nobody, kept'],
      step_7: { created: {}, selected: {} },
      zz_later: { b: 1, a: [2, 1] },
    }),
  ],
  ['only unknown keys', session({ zz: 1, aa: { x: null } })],
];

const STUBS = { selected: { 'art-5': false }, created: { 'art-6': 'art-7' } };

describe('serializeWizardAnswers, old against new', () => {
  it.each(sessions)('over %s', (_name, s) => {
    const applied = { 'm-1:1': true, 'm-2:1': false, 'm-3:1': true };
    expect(JSON.stringify(serializeWizardAnswers(form(), applied, s, STUBS))).toBe(
      JSON.stringify(oldBuildAnswersFrom(form(), applied, s, STUBS.selected, STUBS.created))
    );
  });
});

describe('withEntryStepAnswers, old against new', () => {
  const steps = [
    [PERSONA_STEP, form().personas],
    [NEED_STEP, form().needs],
    [REQUIREMENT_STEP, form().requirements],
    [NFR_STEP, form().nfrs],
    [HAZARD_STEP, form().hazards],
  ] as const;
  for (const [name, s] of sessions) {
    const sectionMaps: Record<string, string>[] = [{}, { personas: 'art-h', 'nfrs:Performance': 'art-np' }];
    for (const sections of sectionMaps) {
      it.each(steps.map(([d, entries]) => [d.step, d, entries] as const))(
        `step %s over ${name}, ${Object.keys(sections).length} sections`,
        (step, d, entries) => {
          const old = oldBuildAnswersFrom(form(), {}, s, STUBS.selected, STUBS.created);
          oldOverrides[step](old, entries as any[], sections);
          const now = withEntryStepAnswers(serializeWizardAnswers(form(), {}, s, STUBS), d as any, entries as any[], sections);
          expect(JSON.stringify(now)).toBe(JSON.stringify(old));
        }
      );
    }
  }

  it('writes no step_6_ids', () => {
    const now = withEntryStepAnswers(serializeWizardAnswers(form(), {}, null, STUBS), HAZARD_STEP, form().hazards, {});
    expect(Object.keys(now)).not.toContain('step_6_ids');
  });
});

describe('withStubAnswers, old against new', () => {
  it.each(sessions)('over %s', (_name, s) => {
    const sections = { tests: 'art-t' };
    const created = { 'art-6': 'art-7', 'art-8': 'art-9' };
    const old = oldBuildAnswersFrom(form(), {}, s, STUBS.selected, STUBS.created);
    old.section_ids = sections;
    old.step_7 = { selected: STUBS.selected, created };
    const now = withStubAnswers(serializeWizardAnswers(form(), {}, s, STUBS), sections, {
      selected: STUBS.selected,
      created,
    });
    expect(JSON.stringify(now)).toBe(JSON.stringify(old));
  });
});

// Entry ids the normalizer makes for entries without one come from
// crypto.randomUUID; a counter restarted before each side keeps them equal.
const withIds = <T,>(fn: () => T): T => {
  let n = 0;
  const spy = vi.spyOn(crypto, 'randomUUID').mockImplementation(
    () => `00000000-0000-4000-8000-${String(++n).padStart(12, '0')}` as `${string}-${string}-${string}-${string}-${string}`
  );
  try {
    return fn();
  } finally {
    spy.mockRestore();
  }
};

// Stored answers of every vintage: id-based, index-based (legacy), partial,
// junk, with unknown keys.
const stored: [string, Record<string, any>][] = [
  ['empty', {}],
  [
    'id-based, server order, unknown keys',
    {
      copilot_applied: ['m-1:1', '', 7, 'm-2:3'],
      legacy_note: 'kept',
      section_ids: { personas: 'art-h' },
      step_1: { problem_statement: 'P', target_users: 'U', vision: 'V', tagline: 'x' },
      step_2: { personas: [{ id: 'p-1', name: 'Sam', role: 'Lead', goals: 'g', pains: 'p', artifact_id: 'art-1', extra: true }] },
      step_3: { needs: [{ id: 'n-1', persona_id: 'p-1', capability: 'c', outcome: 'o' }] },
      step_7: { selected: { 'art-1': false }, created: { 'art-2': 'art-3' } },
      zz_later: [1, 2],
    },
  ],
  [
    'index-based (legacy)',
    {
      step_2: { personas: [{ name: 'Sam', role: '', goals: '', pains: '' }, { name: 'Pat' }] },
      step_3: { needs: [{ persona_index: 1, capability: 'c', outcome: '' }] },
      step_4: { requirements: [{ need_index: 0, text: 't', fit_criterion: '' }] },
      step_5: { nfrs: [{ category: 'performance', text: 'n' }] },
      step_6: { hazards: [{ category: 'nope', hazard: 'h' }] },
    },
  ],
  ['junk', { step_1: null, step_2: { personas: 'nope' }, step_7: { selected: null }, section_ids: null, copilot_applied: 'x' }],
];

describe('readWizardAnswers, old against new', () => {
  // What hydrateFromAnswers set, in the order it set it.
  const oldHydrate = (answers: Record<string, any>) => {
    const s1 = answers.step_1 || {};
    const norm = normalizeWizardAnswers(answers);
    const applied: Record<string, boolean> = {};
    norm.appliedSuggestionKeys.forEach((k) => {
      applied[k] = true;
    });
    return [
      s1.vision || '',
      s1.problem_statement || '',
      s1.target_users || '',
      norm.personas,
      norm.needs,
      norm.requirements,
      norm.nfrs,
      norm.hazards,
      applied,
      (answers.step_7?.selected as Record<string, boolean>) || {},
      (answers.step_7?.created as Record<string, string>) || {},
      (answers.section_ids as Record<string, string>) || {},
    ];
  };
  const newHydrate = (answers: Record<string, any>) => {
    const read = readWizardAnswers(answers);
    return [
      read.vision,
      read.problem,
      read.targetUsers,
      read.entries.personas,
      read.entries.needs,
      read.entries.requirements,
      read.entries.nfrs,
      read.entries.hazards,
      read.applied,
      read.stubs.selected,
      read.stubs.created,
      read.sectionIds,
    ];
  };

  it.each(stored)('%s', (_name, answers) => {
    const before = withIds(() => oldHydrate(answers));
    const after = withIds(() => newHydrate(answers));
    expect(JSON.stringify(after)).toBe(JSON.stringify(before));
    // The stored objects themselves where the old code passed them through.
    if (answers.step_7?.selected) expect(after[9]).toBe(answers.step_7.selected);
    if (answers.step_7?.created) expect(after[10]).toBe(answers.step_7.created);
    if (answers.section_ids) expect(after[11]).toBe(answers.section_ids);
  });
});

describe('seedReopenedAnswers, old against new', () => {
  const oldSeed = (from: { answers?: Record<string, any> }) => {
    const norm = normalizeWizardAnswers(from.answers || {});
    return {
      ...(from.answers || {}),
      step_2: { personas: norm.personas },
      step_3: { needs: norm.needs },
      step_4: { requirements: norm.requirements },
      step_5: { nfrs: norm.nfrs },
      step_6: { hazards: norm.hazards },
      copilot_applied: [],
    };
  };

  it.each(stored)('%s', (_name, answers) => {
    const before = withIds(() => oldSeed({ answers }));
    const after = withIds(() => seedReopenedAnswers(answers || {}));
    expect(JSON.stringify(after)).toBe(JSON.stringify(before));
  });

  it('seeds a session with no answers', () => {
    const before = withIds(() => oldSeed({}));
    const after = withIds(() => seedReopenedAnswers({}));
    expect(JSON.stringify(after)).toBe(JSON.stringify(before));
  });
});
