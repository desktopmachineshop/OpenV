import {
  ArtifactText,
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

/**
 * The guided wizard's steps as data (refactor plan X18, fe-suite-org-2).
 *
 * Each row says what its step does today, quirks included. These rows name
 * the quirks; they do not fix them (R7):
 *
 * - Titles are cut at 120 characters for needs, requirements, hazards and
 *   verification stubs, at 100 for NFRs, and never for personas. The cut is
 *   made by the F5 templates in `components/wizard/artifactTemplates.ts`.
 *   `titleLimit` records it beside each row's template, and
 *   `steps.test.ts` holds the two to agree.
 * - A hazard the step adds has severity `moderate`. The notes panel writes
 *   `Medium` instead (`suggestionDrafts.ts`), and a card's "Medium" is not
 *   one of the wizard's severities, so it is stored as `moderate`.
 * - Hazards are saved with no ids key: `idsKey` is null, so there is no
 *   `step_6_ids`.
 * - Only steps 6 and 7 offer Skip. Skip saves and moves on, and never
 *   materialises.
 * - Next and Skip nudge the assistant before anything is saved, so its
 *   thinking indicator shows at once (`nudgeEvent` is the message).
 */

/** One draft artifact as `guidedAPI.materializeDrafts` takes it. */
export interface DraftSpec {
  type: string;
  title: string;
  body: string;
  parent_id?: string;
  sort_order?: number;
  attributes?: Record<string, any>;
  links?: { type: string; to_id: string }[];
}

export interface WizardStep {
  step: number;
  label: string;
  /** Shows Skip beside Next. Skip saves and moves on; it never materialises. */
  skippable: boolean;
}

// A literal list: S13's vocabParity test reads it, by this name, as the
// `wizard_step_labels` vocabulary the Go side also holds.
export const STEP_LABELS = [
  'Product framing',
  'Personas',
  'User needs',
  'Requirements',
  'NFRs & constraints',
  'Hazards',
  'Verification stubs',
  'Review & commit',
];

/** The steps that offer Skip. */
export const SKIPPABLE_STEPS = [6, 7];

export const WIZARD_STEPS: readonly WizardStep[] = STEP_LABELS.map((label, i) => ({
  step: i + 1,
  label,
  skippable: SKIPPABLE_STEPS.includes(i + 1),
}));

/** The last step, where the drafts are reviewed and committed. */
export const REVIEW_STEP = WIZARD_STEPS.length;

export const isSkippable = (step: number): boolean => !!WIZARD_STEPS[step - 1]?.skippable;

/** What the assistant is told when Next ("saved") or Skip ("skipped") leaves a step. */
export const nudgeEvent = (verb: 'saved' | 'skipped', step: number): string =>
  `${verb} step ${step} ("${STEP_LABELS[step - 1]}") and moved on to step ${step + 1} ("${STEP_LABELS[step]}")`;

// Section headings the wizard materializes artifacts under, in module order.
// NFR, hazard and verification categories become sub-headings under their
// step's section.
export const SECTION_SPECS: Record<string, { title: string; sort: number }> = {
  personas: { title: 'Personas', sort: 10 },
  needs: { title: 'User Needs', sort: 20 },
  requirements: { title: 'Requirements', sort: 30 },
  nfrs: { title: 'Non-Functional Requirements & Constraints', sort: 40 },
  hazards: { title: 'Hazards & Risks', sort: 50 },
  tests: { title: 'Verification Tests', sort: 60 },
};

/** What a step's drafts may refer to: the entries of the steps before it. */
export interface DraftContext {
  personas: PersonaEntry[];
  needs: NeedEntry[];
}

/**
 * A step whose entries become draft artifacts on Next (steps 2 to 6). An
 * entry is materialised when it has no artifact yet and its `required` text
 * is not blank. Its draft is `{ type, title, body, ...extra }`, filed under
 * the section `sectionKey` names, and the step saves
 * `answers[answersKey] = { [listKey]: entries }` and, unless `idsKey` is
 * null, the ids of the entries' artifacts under `idsKey`.
 */
export interface EntryStep<E extends { artifact_id?: string }> {
  step: number;
  answersKey: string;
  listKey: string;
  idsKey: string | null;
  artifactType: string;
  /** The longest title the template writes before it cuts; null: never cut. */
  titleLimit: number | null;
  template: (entry: E, ctx: DraftContext) => ArtifactText;
  required: (entry: E) => string;
  sectionKey: (entry: E) => string;
  /** The draft's keys after its title and body, in order. */
  extra: (entry: E, ctx: DraftContext) => Pick<DraftSpec, 'attributes' | 'links'>;
}

const personaOf = (n: NeedEntry, ctx: DraftContext) => ctx.personas.find((p) => p.id === n.persona_id);

export const PERSONA_STEP: EntryStep<PersonaEntry> = {
  step: 2,
  answersKey: 'step_2',
  listKey: 'personas',
  idsKey: 'step_2_ids',
  artifactType: 'persona',
  titleLimit: null,
  template: (p) => wizardPersonaText(p),
  required: (p) => p.name,
  sectionKey: () => 'personas',
  extra: () => ({}),
};

export const NEED_STEP: EntryStep<NeedEntry> = {
  step: 3,
  answersKey: 'step_3',
  listKey: 'needs',
  idsKey: 'step_3_ids',
  artifactType: 'user-need',
  titleLimit: 120,
  template: (n, ctx) => wizardNeedText(n, personaOf(n, ctx)),
  required: (n) => n.capability,
  sectionKey: () => 'needs',
  extra: (n, ctx) => {
    const persona = personaOf(n, ctx);
    return { links: persona?.artifact_id ? [{ type: 'relates-to', to_id: persona.artifact_id }] : [] };
  },
};

export const REQUIREMENT_STEP: EntryStep<ReqEntry> = {
  step: 4,
  answersKey: 'step_4',
  listKey: 'requirements',
  idsKey: 'step_4_ids',
  artifactType: 'requirement',
  titleLimit: 120,
  template: (r) => wizardRequirementText(r),
  required: (r) => r.text,
  sectionKey: () => 'requirements',
  extra: (r, ctx) => {
    const need = ctx.needs.find((nd) => nd.id === r.need_id);
    return {
      attributes: { verification_method: r.verification_method },
      links: need?.artifact_id ? [{ type: 'derives-from', to_id: need.artifact_id }] : [],
    };
  },
};

// The category is the section the draft lands in, so it is not spelled into
// the title as well.
export const NFR_STEP: EntryStep<NfrEntry> = {
  step: 5,
  answersKey: 'step_5',
  listKey: 'nfrs',
  idsKey: 'step_5_ids',
  artifactType: 'requirement',
  titleLimit: 100,
  template: (n) => wizardNfrText(n),
  required: (n) => n.text,
  sectionKey: (n) => subSectionKey('nfrs', n.category),
  extra: (n) => ({ attributes: { verification_method: n.verification_method, category: n.category } }),
};

export const HAZARD_STEP: EntryStep<HazardEntry> & { defaultSeverity: string } = {
  step: 6,
  answersKey: 'step_6',
  listKey: 'hazards',
  idsKey: null,
  artifactType: 'hazard',
  titleLimit: 120,
  template: (h) => wizardHazardText(h),
  required: (h) => h.hazard,
  sectionKey: (h) => subSectionKey('hazards', h.category),
  extra: (h) => ({ attributes: { severity: h.severity, category: h.category } }),
  defaultSeverity: 'moderate',
};

/** Step 7: a stub per requirement with method test, its title cut at 120. */
export const STUB_STEP = {
  step: 7,
  answersKey: 'step_7',
  artifactType: 'test-case',
  titleLimit: 120 as number | null,
  template: (c: { title: string }): ArtifactText => wizardTestStubText(c),
};

/** The drafts a step's Next writes, and which entries they are. */
export interface EntryDrafts {
  /** The index in the entry list of each draft. */
  entryIndex: number[];
  /** The section key each draft is filed under. */
  sectionKeys: string[];
  /** The drafts, without their parent, which the sections give. */
  drafts: DraftSpec[];
}

export const planEntryDrafts = <E extends { artifact_id?: string }>(
  d: EntryStep<E>,
  entries: E[],
  ctx: DraftContext
): EntryDrafts => {
  const out: EntryDrafts = { entryIndex: [], sectionKeys: [], drafts: [] };
  entries.forEach((e, i) => {
    if (!e.artifact_id && d.required(e).trim()) {
      out.entryIndex.push(i);
      out.sectionKeys.push(d.sectionKey(e));
      out.drafts.push({ type: d.artifactType, ...d.template(e, ctx), ...d.extra(e, ctx) });
    }
  });
  return out;
};
