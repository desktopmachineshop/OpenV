import type { GuidedSession } from '../../api/client';
import {
  HazardEntry,
  NeedEntry,
  NfrEntry,
  NormalizedWizardAnswers,
  PersonaEntry,
  ReqEntry,
  normalizeWizardAnswers,
} from '../../components/wizard/wizardEntries';
import { SuggestionDraft, buildAnswersFrom } from '../../components/wizard/wizardSuggestions';
import type { EntryStep } from './steps';

/**
 * A guided session's `answers`, as the wizard writes them (refactor plan
 * X18, fe-suite-org-4). They are stored data (I16): the server merges them
 * at the top level and keeps every key, so the keys are a contract with the
 * Go side and the assistant's prompt.
 *
 * What the serializer keeps, byte for byte:
 *
 * - Keys the wizard does not know survive at the top level, and the keys
 *   keep the order of the session the answers are built over. The server
 *   returns them sorted, as a Go map encodes. Keys the session lacks follow
 *   in the order below.
 * - `step_1`, the five entry lists and the entries themselves are rebuilt,
 *   so an unknown key inside them does not survive.
 * - Hazards have no ids key: there is no `step_6_ids`.
 * - `section_ids` is the session's copy, except when Next writes the
 *   section map the wizard holds.
 */
export interface WizardAnswers {
  step_1?: { vision: string; problem_statement: string; target_users: string };
  step_2?: { personas: PersonaEntry[] };
  step_2_ids?: string[];
  step_3?: { needs: NeedEntry[] };
  step_3_ids?: string[];
  step_4?: { requirements: ReqEntry[] };
  step_4_ids?: string[];
  step_5?: { nfrs: NfrEntry[] };
  step_5_ids?: string[];
  step_6?: { hazards: HazardEntry[] };
  /** Keyed by requirement artifact id. */
  step_7?: { selected: Record<string, boolean>; created: Record<string, string> };
  /** "<messageId>:<segmentIndex>" keys of the assistant's cards already applied. */
  copilot_applied?: string[];
  /** Section key ("personas", "hazards:Safety") to heading artifact id. */
  section_ids?: Record<string, string>;
  [unknown: string]: unknown;
}

/** What the wizard holds besides the form: the parts of the answers it reads back. */
export interface StubChoices {
  selected: Record<string, boolean>;
  created: Record<string, string>;
}

/**
 * The answers for the wizard's whole state, over the session's own: the
 * assistant's view of the state, and every save.
 */
export const serializeWizardAnswers = (
  form: SuggestionDraft,
  applied: Record<string, boolean>,
  session: GuidedSession | null,
  stubs: StubChoices
): WizardAnswers => buildAnswersFrom(form, applied, session, stubs.selected, stubs.created) as WizardAnswers;

/**
 * What Next on an entry step writes over the answers: the section map it
 * holds, the step's entries with their artifacts, and their ids unless the
 * step has no ids key. In that order; a key already present keeps its place.
 */
export const withEntryStepAnswers = <E extends { artifact_id?: string }>(
  answers: WizardAnswers,
  d: EntryStep<E>,
  entries: E[],
  sections: Record<string, string>
): WizardAnswers => {
  answers.section_ids = sections;
  answers[d.answersKey] = { [d.listKey]: entries };
  if (d.idsKey) answers[d.idsKey] = entries.map((e) => e.artifact_id).filter(Boolean);
  return answers;
};

/** What Next on the verification step writes over the answers. */
export const withStubAnswers = (
  answers: WizardAnswers,
  sections: Record<string, string>,
  stubs: StubChoices
): WizardAnswers => {
  answers.section_ids = sections;
  answers.step_7 = { selected: stubs.selected, created: stubs.created };
  return answers;
};

/** The wizard's state, read from a session's answers. */
export interface ReadAnswers {
  vision: string;
  problem: string;
  targetUsers: string;
  /** Id-based entries; sessions saved with index references are migrated. */
  entries: NormalizedWizardAnswers;
  applied: Record<string, boolean>;
  stubs: StubChoices;
  sectionIds: Record<string, string>;
}

export const readWizardAnswers = (answers: Record<string, any>): ReadAnswers => {
  const s1 = answers.step_1 || {};
  const entries = normalizeWizardAnswers(answers);
  const applied: Record<string, boolean> = {};
  entries.appliedSuggestionKeys.forEach((k) => {
    applied[k] = true;
  });
  return {
    vision: s1.vision || '',
    problem: s1.problem_statement || '',
    targetUsers: s1.target_users || '',
    entries,
    applied,
    stubs: {
      selected: (answers.step_7?.selected as Record<string, boolean>) || {},
      created: (answers.step_7?.created as Record<string, string>) || {},
    },
    sectionIds: (answers.section_ids as Record<string, string>) || {},
  };
};

/**
 * The first answers of a session reopened from a committed one: the
 * committed answers, every key kept in place, with the entry lists in their
 * id-normalized form (so old index-based sessions are migrated once) and the
 * applied card keys reset, since they name the old session's transcript.
 */
export const seedReopenedAnswers = (committed: Record<string, any>): WizardAnswers => {
  const norm = normalizeWizardAnswers(committed || {});
  return {
    ...(committed || {}),
    step_2: { personas: norm.personas },
    step_3: { needs: norm.needs },
    step_4: { requirements: norm.requirements },
    step_5: { nfrs: norm.nfrs },
    step_6: { hazards: norm.hazards },
    copilot_applied: [],
  };
};
