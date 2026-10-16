import {
  wizardHazardText,
  wizardNeedText,
  wizardNfrText,
  wizardPersonaText,
  wizardRequirementText,
  wizardTestStubText,
} from '../../components/wizard/artifactTemplates';
import {
  HazardEntry,
  NeedEntry,
  NfrEntry,
  PersonaEntry,
  ReqEntry,
  subSectionKey,
} from '../../components/wizard/wizardEntries';
import {
  DraftContext,
  DraftSpec,
  EntryStep,
  HAZARD_STEP,
  NEED_STEP,
  NFR_STEP,
  PERSONA_STEP,
  REQUIREMENT_STEP,
  REVIEW_STEP,
  SECTION_SPECS,
  STEP_LABELS,
  STUB_STEP,
  WIZARD_STEPS,
  isSkippable,
  nudgeEvent,
  planEntryDrafts,
} from './steps';

// Refactor plan X18a: the step descriptors against what GuidedWizard.tsx
// held inline before them. The old code is copied here as it was, so each
// test is an old-versus-new proof; the wizard's own characterization
// (GuidedWizard.snapshot.test.tsx, GuidedWizard.answers.test.tsx) pins the
// whole flow.

const OLD_STEP_LABELS = [
  'Product framing',
  'Personas',
  'User needs',
  'Requirements',
  'NFRs & constraints',
  'Hazards',
  'Verification stubs',
  'Review & commit',
];

const OLD_SECTION_SPECS: Record<string, { title: string; sort: number }> = {
  personas: { title: 'Personas', sort: 10 },
  needs: { title: 'User Needs', sort: 20 },
  requirements: { title: 'Requirements', sort: 30 },
  nfrs: { title: 'Non-Functional Requirements & Constraints', sort: 40 },
  hazards: { title: 'Hazards & Risks', sort: 50 },
  tests: { title: 'Verification Tests', sort: 60 },
};

describe('the step table', () => {
  it('holds the labels, the sections and the review step as they were', () => {
    expect(STEP_LABELS).toStrictEqual(OLD_STEP_LABELS);
    expect(JSON.stringify(SECTION_SPECS)).toBe(JSON.stringify(OLD_SECTION_SPECS));
    expect(REVIEW_STEP).toBe(8);
    expect(WIZARD_STEPS.map((s) => s.step)).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
  });

  it('offers Skip on steps 6 and 7 only', () => {
    for (let step = 0; step <= 9; step++) {
      expect(isSkippable(step), `step ${step}`).toBe(step === 6 || step === 7);
    }
  });

  it('nudges the assistant with the text it did', () => {
    for (let step = 1; step <= 7; step++) {
      expect(nudgeEvent('saved', step)).toBe(
        `saved step ${step} ("${OLD_STEP_LABELS[step - 1]}") and moved on to step ${step + 1} ("${OLD_STEP_LABELS[step]}")`
      );
      expect(nudgeEvent('skipped', step)).toBe(
        `skipped step ${step} ("${OLD_STEP_LABELS[step - 1]}") and moved on to step ${step + 1} ("${OLD_STEP_LABELS[step]}")`
      );
    }
  });

  it('names the quirks it keeps', () => {
    expect(HAZARD_STEP.defaultSeverity).toBe('moderate');
    expect([PERSONA_STEP, NEED_STEP, REQUIREMENT_STEP, NFR_STEP, HAZARD_STEP].map((d) => [d.step, d.answersKey, d.listKey, d.idsKey])).toEqual([
      [2, 'step_2', 'personas', 'step_2_ids'],
      [3, 'step_3', 'needs', 'step_3_ids'],
      [4, 'step_4', 'requirements', 'step_4_ids'],
      [5, 'step_5', 'nfrs', 'step_5_ids'],
      [6, 'step_6', 'hazards', null],
    ]);
    expect([PERSONA_STEP, NEED_STEP, REQUIREMENT_STEP, NFR_STEP, HAZARD_STEP, STUB_STEP].map((d) => d.titleLimit)).toEqual([
      null,
      120,
      120,
      100,
      120,
      120,
    ]);
    expect(STUB_STEP.template).toBeDefined();
  });
});

// ---- each row's titleLimit against its template ---------------------------

const PERSONA: PersonaEntry = { id: 'p-1', name: 'Sam', role: 'Lead', goals: 'g', pains: 'p', artifact_id: 'art-p' };
const CTX: DraftContext = { personas: [PERSONA], needs: [] };

// An entry of each step whose title source is `len` characters long.
const NEED_PREFIX = 'As Sam, I need ';
const NEED_SUFFIX = ' so that x';
const sized: [string, EntryStep<any> | typeof STUB_STEP, (len: number) => any][] = [
  ['persona', PERSONA_STEP, (len) => ({ ...PERSONA, artifact_id: undefined, name: 'p'.repeat(len) })],
  [
    'need',
    NEED_STEP,
    (len) => ({ id: 'n-1', persona_id: 'p-1', capability: 'c'.repeat(len - NEED_PREFIX.length - NEED_SUFFIX.length), outcome: 'x' }),
  ],
  ['requirement', REQUIREMENT_STEP, (len) => ({ id: 'r-1', need_id: '', text: 'r'.repeat(len), fit_criterion: '', verification_method: 'test' })],
  ['nfr', NFR_STEP, (len) => ({ id: 'f-1', category: 'Performance', text: 'n'.repeat(len), fit_criterion: '', verification_method: 'test' })],
  ['hazard', HAZARD_STEP, (len) => ({ id: 'h-1', category: 'Safety', hazard: 'h'.repeat(len), harm: '', severity: 'moderate' })],
  ['stub', STUB_STEP, (len) => ({ title: 's'.repeat(len - 'Verify: '.length) })],
];

describe("each row's titleLimit is where its template cuts", () => {
  it.each(sized)('%s', (_name, d, entry) => {
    const title = (len: number): string => (d.template as (e: any, ctx: DraftContext) => { title: string })(entry(len), CTX).title;
    if (d.titleLimit === null) {
      for (const len of [100, 101, 120, 121, 500]) {
        expect(title(len), `length ${len}`).toHaveLength(len);
      }
      return;
    }
    const limit = d.titleLimit;
    expect(title(limit)).toHaveLength(limit);
    expect(title(limit)).not.toContain('…');
    expect(title(limit + 1)).toHaveLength(limit - 2);
    expect(title(limit + 1).endsWith('…')).toBe(true);
    expect(title(limit + 1).slice(0, -1)).toBe(title(limit + 1).slice(0, limit - 3));
  });
});

// ---- the drafts each step writes, old against new --------------------------

// The five blocks handleNext held before X18a, cut to what they computed
// before any call: which entries, in what sections, with what drafts.
type OldPlan = { newIdx: number[]; sectionKeys: string[]; drafts: DraftSpec[] };

const oldPersonas = (personas: PersonaEntry[]): OldPlan => {
  const updated = personas.slice();
  const newIdx: number[] = [];
  const drafts: DraftSpec[] = [];
  updated.forEach((p, i) => {
    if (!p.artifact_id && p.name.trim()) {
      newIdx.push(i);
      drafts.push({
        type: 'persona',
        ...wizardPersonaText(p),
      });
    }
  });
  return { newIdx, sectionKeys: drafts.map(() => 'personas'), drafts };
};

const oldNeeds = (needs: NeedEntry[], personas: PersonaEntry[]): OldPlan => {
  const updated = needs.slice();
  const newIdx: number[] = [];
  const drafts: DraftSpec[] = [];
  updated.forEach((n, i) => {
    if (!n.artifact_id && n.capability.trim()) {
      const persona = personas.find((p) => p.id === n.persona_id);
      newIdx.push(i);
      drafts.push({
        type: 'user-need',
        ...wizardNeedText(n, persona),
        links: persona?.artifact_id ? [{ type: 'relates-to', to_id: persona.artifact_id }] : [],
      });
    }
  });
  return { newIdx, sectionKeys: drafts.map(() => 'needs'), drafts };
};

const oldRequirements = (requirements: ReqEntry[], needs: NeedEntry[]): OldPlan => {
  const updated = requirements.slice();
  const newIdx: number[] = [];
  const drafts: DraftSpec[] = [];
  updated.forEach((r, i) => {
    if (!r.artifact_id && r.text.trim()) {
      const need = needs.find((nd) => nd.id === r.need_id);
      newIdx.push(i);
      drafts.push({
        type: 'requirement',
        ...wizardRequirementText(r),
        attributes: { verification_method: r.verification_method },
        links: need?.artifact_id ? [{ type: 'derives-from', to_id: need.artifact_id }] : [],
      });
    }
  });
  return { newIdx, sectionKeys: drafts.map(() => 'requirements'), drafts };
};

const oldNfrs = (nfrs: NfrEntry[]): OldPlan => {
  const updated = nfrs.slice();
  const newIdx: number[] = [];
  const drafts: DraftSpec[] = [];
  const draftSections: string[] = [];
  updated.forEach((n, i) => {
    if (!n.artifact_id && n.text.trim()) {
      newIdx.push(i);
      draftSections.push(subSectionKey('nfrs', n.category));
      drafts.push({
        type: 'requirement',
        ...wizardNfrText(n),
        attributes: { verification_method: n.verification_method, category: n.category },
      });
    }
  });
  return { newIdx, sectionKeys: draftSections, drafts };
};

const oldHazards = (hazards: HazardEntry[]): OldPlan => {
  const updated = hazards.slice();
  const newIdx: number[] = [];
  const drafts: DraftSpec[] = [];
  const draftCategories: string[] = [];
  updated.forEach((h, i) => {
    if (!h.artifact_id && h.hazard.trim()) {
      newIdx.push(i);
      draftCategories.push(h.category);
      drafts.push({
        type: 'hazard',
        ...wizardHazardText(h),
        attributes: { severity: h.severity, category: h.category },
      });
    }
  });
  return { newIdx, sectionKeys: draftCategories.map((c) => subSectionKey('hazards', c)), drafts };
};

// The sections the old blocks asked ensureSections for: one constant key for
// steps 2 to 4 (only when there were drafts), the distinct keys in draft
// order for steps 5 and 6.
const oldEnsured = (step: number, plan: OldPlan): string[] =>
  step <= 4 ? [['personas', 'needs', 'requirements'][step - 2]] : Array.from(new Set(plan.sectionKeys));

const personas: PersonaEntry[] = [
  { id: 'p-1', name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe', pains: 'No overview', artifact_id: 'art-1' },
  { id: 'p-2', name: ' Pat the Planner ', role: ' Planner ', goals: '\nPlan\n', pains: ' Breakdowns ' },
  { id: 'p-3', name: '   ', role: 'r', goals: 'g', pains: 'p' },
  { id: 'p-4', name: 'p'.repeat(130), role: '', goals: '', pains: '' },
];
const needs: NeedEntry[] = [
  { id: 'n-1', persona_id: 'p-1', capability: 'to see the spindle state', outcome: 'I stay safe', artifact_id: 'art-2' },
  { id: 'n-2', persona_id: 'p-1', capability: `to ${'x'.repeat(100)}`, outcome: 'it works' },
  { id: 'n-3', persona_id: 'p-2', capability: ' to plan ', outcome: ' ' },
  { id: 'n-4', persona_id: 'p-3', capability: 'to know who is on shift', outcome: '' },
  { id: 'n-5', persona_id: 'missing', capability: 'orphaned', outcome: 'still written' },
  { id: 'n-6', persona_id: 'p-1', capability: '  ', outcome: 'never written' },
];
const requirements: ReqEntry[] = [
  { id: 'r-1', need_id: 'n-1', text: 'The system shall stop.', fit_criterion: ' Ten trials. ', verification_method: 'test' },
  { id: 'r-2', need_id: 'n-2', text: 'r'.repeat(121), fit_criterion: '', verification_method: 'analysis' },
  { id: 'r-3', need_id: 'n-1', text: 'r'.repeat(120), fit_criterion: '', verification_method: 'inspection', artifact_id: 'art-3' },
  { id: 'r-4', need_id: '', text: ' ', fit_criterion: 'none', verification_method: 'test' },
  { id: 'r-5', need_id: 'missing', text: 'Orphan requirement', fit_criterion: '', verification_method: 'demonstration' },
];
const nfrs: NfrEntry[] = [
  { id: 'f-1', category: 'Security', text: 'n'.repeat(101), fit_criterion: 'Measured.', verification_method: 'test' },
  { id: 'f-2', category: 'Performance', text: 'n'.repeat(100), fit_criterion: '', verification_method: 'analysis' },
  { id: 'f-3', category: 'Security', text: 'Badge to clear', fit_criterion: '', verification_method: 'test' },
  { id: 'f-4', category: 'Retired', text: 'An old category', fit_criterion: '', verification_method: 'test' },
  { id: 'f-5', category: 'Usability', text: 'Done', fit_criterion: '', verification_method: 'test', artifact_id: 'art-4' },
  { id: 'f-6', category: 'Usability', text: '', fit_criterion: '', verification_method: 'test' },
];
const hazards: HazardEntry[] = [
  { id: 'h-1', category: 'Operational', hazard: 'Coolant mist hides the lamp', harm: ' Misread state ', severity: 'moderate' },
  { id: 'h-2', category: 'Safety', hazard: `A ${'h'.repeat(130)}`, harm: 'Crushed fingers', severity: 'critical' },
  { id: 'h-3', category: 'Operational', hazard: 'Second operational', harm: '', severity: 'minor' },
  { id: 'h-4', category: 'Unknown', hazard: 'No such category', harm: '', severity: 'serious' },
  { id: 'h-5', category: 'Safety', hazard: 'Already made', harm: '', severity: 'moderate', artifact_id: 'art-5' },
  { id: 'h-6', category: 'Safety', hazard: ' ', harm: '', severity: 'moderate' },
];

const ctx: DraftContext = { personas, needs };

const cases: [string, number, () => OldPlan, () => ReturnType<typeof planEntryDrafts>][] = [
  ['personas', 2, () => oldPersonas(personas), () => planEntryDrafts(PERSONA_STEP, personas, ctx)],
  ['needs', 3, () => oldNeeds(needs, personas), () => planEntryDrafts(NEED_STEP, needs, ctx)],
  ['requirements', 4, () => oldRequirements(requirements, needs), () => planEntryDrafts(REQUIREMENT_STEP, requirements, ctx)],
  ['nfrs', 5, () => oldNfrs(nfrs), () => planEntryDrafts(NFR_STEP, nfrs, ctx)],
  ['hazards', 6, () => oldHazards(hazards), () => planEntryDrafts(HAZARD_STEP, hazards, ctx)],
];

describe('the drafts each entry step writes, old against new', () => {
  it.each(cases)('%s', (_name, step, oldPlan, newPlan) => {
    const before = oldPlan();
    const after = newPlan();
    expect(before.drafts.length).toBeGreaterThan(0);
    expect(after.entryIndex).toEqual(before.newIdx);
    expect(after.sectionKeys).toEqual(before.sectionKeys);
    expect(Array.from(new Set(after.sectionKeys))).toEqual(oldEnsured(step, before));
    // Byte for byte, key order included.
    expect(JSON.stringify(after.drafts)).toBe(JSON.stringify(before.drafts));
  });

  it.each(cases)('%s with nothing new to write', (_name, _step, _oldPlan, _newPlan) => {
    const d = [PERSONA_STEP, NEED_STEP, REQUIREMENT_STEP, NFR_STEP, HAZARD_STEP][_step - 2] as EntryStep<any>;
    expect(planEntryDrafts(d, [], ctx)).toEqual({ entryIndex: [], sectionKeys: [], drafts: [] });
  });

  it('writes a stub as the old step 7 did', () => {
    const c = { id: 'art-9', title: 'v'.repeat(130), category: 'Performance' };
    const old = { type: 'test-case', ...wizardTestStubText(c), parent_id: 'art-s', links: [{ type: 'verifies', to_id: c.id }] };
    const now = { type: STUB_STEP.artifactType, ...STUB_STEP.template(c), parent_id: 'art-s', links: [{ type: 'verifies', to_id: c.id }] };
    expect(JSON.stringify(now)).toBe(JSON.stringify(old));
  });
});
