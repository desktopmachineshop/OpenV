import type { GuidedSession } from '../../api/client';
import type { CopilotSuggestion } from './GuidedChatPanel';
import {
  PersonaEntry,
  NeedEntry,
  ReqEntry,
  NfrEntry,
  HazardEntry,
  NFR_CATEGORIES,
  canonicalHazardCategory,
  canonicalNfrCategory,
  newEntryId,
} from './wizardEntries';

// The guided wizard's side of the assistant's suggestion cards: matching a
// card to the entry it names, applying it to a working copy of the form, and
// the session answers saved from that copy. Pure: no API, no React; the wizard
// passes in what it holds in state.

export const VERIFICATION_METHODS = ['inspection', 'analysis', 'demonstration', 'test'];
export const SEVERITIES = ['minor', 'moderate', 'serious', 'critical'];

// Resolve a suggestion's target against existing entries. Entries carry
// stable ids (visible to the copilot in the wizard state), so an exact id
// match wins outright; otherwise fall back to an exact case-insensitive
// title match. Several entries sharing the title is ambiguous — return -1
// rather than guess, so callers append instead of replacing the wrong one.
export const matchEntry = <T extends { id: string },>(items: T[], key: (t: T) => string, target: string): number => {
  const raw = target.trim();
  if (!raw) return -1;
  const byId = items.findIndex((it) => it.id === raw);
  if (byId >= 0) return byId;
  const t = raw.toLowerCase();
  const hits: number[] = [];
  items.forEach((it, i) => {
    if (key(it).trim().toLowerCase() === t) hits.push(i);
  });
  return hits.length === 1 ? hits[0] : -1;
};

// Working copy of every suggestion-editable wizard section, so a batch of
// suggestions applies in order against ONE snapshot (later entries can
// reference earlier ones) and commits with a single state update per list.
export interface SuggestionDraft {
  vision: string;
  problem: string;
  targetUsers: string;
  personas: PersonaEntry[];
  needs: NeedEntry[];
  requirements: ReqEntry[];
  nfrs: NfrEntry[];
  hazards: HazardEntry[];
  openNfr: Record<string, boolean>;
  openHazard: Record<string, boolean>;
}

// Insert or replace one copilot suggestion in the draft. Returns null on
// success, or a human-readable reason it could not apply.
export const applySuggestionToDraft = (d: SuggestionDraft, s: CopilotSuggestion): string | null => {
  const verificationMethod = (v: any, fallback: string) =>
    VERIFICATION_METHODS.includes(String(v)) ? String(v) : fallback;

  switch (s.kind) {
    case 'framing': {
      const text = String(s.text || '').trim();
      if (!text) return 'The suggestion has no text.';
      switch (s.field) {
        case 'vision':
          d.vision = text;
          return null;
        case 'problem_statement':
          d.problem = text;
          return null;
        case 'target_users':
          d.targetUsers = text;
          return null;
        default:
          return `Unknown framing field "${s.field}".`;
      }
    }
    case 'persona': {
      const name = String(s.name || '').trim();
      if (!name) return 'The persona suggestion has no name.';
      if (s.replaces) {
        // No unambiguous match falls through to append below — adding a new
        // entry beats overwriting the wrong one.
        const i = matchEntry(d.personas, (p) => p.name, String(s.replaces));
        if (i >= 0) {
          if (d.personas[i].artifact_id) return 'That persona is already saved as an artifact and cannot be replaced here.';
          d.personas[i] = {
            ...d.personas[i],
            name,
            role: s.role !== undefined ? String(s.role) : d.personas[i].role,
            goals: s.goals !== undefined ? String(s.goals) : d.personas[i].goals,
            pains: s.pains !== undefined ? String(s.pains) : d.personas[i].pains,
          };
          return null;
        }
      }
      d.personas.push({
        id: newEntryId(),
        name,
        role: String(s.role || ''),
        goals: String(s.goals || ''),
        pains: String(s.pains || ''),
      });
      return null;
    }
    case 'need': {
      if (s.replaces) {
        const i = matchEntry(d.needs, (n) => n.capability, String(s.replaces));
        if (i >= 0) {
          if (d.needs[i].artifact_id) return 'That need is already saved as an artifact and cannot be replaced here.';
          let personaId = d.needs[i].persona_id;
          if (s.persona) {
            const hit = matchEntry(d.personas, (p) => p.name, String(s.persona));
            if (hit >= 0) personaId = d.personas[hit].id;
          }
          d.needs[i] = {
            ...d.needs[i],
            persona_id: personaId,
            capability: s.capability !== undefined ? String(s.capability) : d.needs[i].capability,
            outcome: s.outcome !== undefined ? String(s.outcome) : d.needs[i].outcome,
          };
          return null;
        }
      }
      const usable = d.personas.filter((p) => p.name.trim());
      if (usable.length === 0) return 'Add a persona first — user needs attach to a persona.';
      const wanted = String(s.persona || '').trim().toLowerCase();
      const hit = usable.find((p) => p.name.trim().toLowerCase() === wanted);
      d.needs.push({
        id: newEntryId(),
        persona_id: (hit || usable[0]).id,
        capability: String(s.capability || ''),
        outcome: String(s.outcome || ''),
      });
      return null;
    }
    case 'requirement': {
      if (s.replaces) {
        const i = matchEntry(d.requirements, (r) => r.text, String(s.replaces));
        if (i >= 0) {
          if (d.requirements[i].artifact_id) return 'That requirement is already saved as an artifact and cannot be replaced here.';
          let needId = d.requirements[i].need_id;
          if (s.need) {
            const hit = matchEntry(d.needs, (n) => n.capability, String(s.need));
            if (hit >= 0) needId = d.needs[hit].id;
          }
          d.requirements[i] = {
            ...d.requirements[i],
            need_id: needId,
            text: s.text !== undefined ? String(s.text) : d.requirements[i].text,
            fit_criterion: s.fit_criterion !== undefined ? String(s.fit_criterion) : d.requirements[i].fit_criterion,
            verification_method: verificationMethod(s.verification_method, d.requirements[i].verification_method),
          };
          return null;
        }
      }
      // A replace miss (or a plain add) with no text would only append the
      // empty "The system shall " stub — refuse instead of adding noise.
      if (!String(s.text || '').trim()) return 'The requirement suggestion has no text.';
      const usable = d.needs.filter((n) => n.capability.trim());
      if (usable.length === 0) return 'Add a user need first — requirements derive from needs.';
      const wanted = String(s.need || '').trim().toLowerCase();
      let needId = usable[0].id;
      if (wanted) {
        const hit = usable.find((n) => {
          const cap = n.capability.trim().toLowerCase();
          return cap === wanted || cap.includes(wanted) || wanted.includes(cap);
        });
        if (hit) needId = hit.id;
      }
      d.requirements.push({
        id: newEntryId(),
        need_id: needId,
        text: String(s.text),
        fit_criterion: String(s.fit_criterion || ''),
        verification_method: verificationMethod(s.verification_method, 'test'),
      });
      return null;
    }
    case 'nfr': {
      const category = canonicalNfrCategory(s.category) || NFR_CATEGORIES[0];
      if (s.replaces) {
        const i = matchEntry(d.nfrs, (n) => n.text, String(s.replaces));
        if (i >= 0) {
          if (d.nfrs[i].artifact_id) return 'That NFR is already saved as an artifact and cannot be replaced here.';
          const nextCategory = s.category !== undefined ? category : d.nfrs[i].category;
          d.nfrs[i] = {
            ...d.nfrs[i],
            category: nextCategory,
            text: s.text !== undefined ? String(s.text) : d.nfrs[i].text,
            fit_criterion: s.fit_criterion !== undefined ? String(s.fit_criterion) : d.nfrs[i].fit_criterion,
            verification_method: verificationMethod(s.verification_method, d.nfrs[i].verification_method),
          };
          d.openNfr[nextCategory] = true;
          return null;
        }
      }
      if (!String(s.text || '').trim()) return 'The NFR suggestion has no text.';
      d.nfrs.push({
        id: newEntryId(),
        category,
        text: String(s.text),
        fit_criterion: String(s.fit_criterion || ''),
        verification_method: verificationMethod(s.verification_method, 'test'),
      });
      d.openNfr[category] = true;
      return null;
    }
    case 'hazard': {
      if (s.replaces) {
        const i = matchEntry(d.hazards, (h) => h.hazard, String(s.replaces));
        if (i >= 0) {
          if (d.hazards[i].artifact_id) return 'That hazard is already saved as an artifact and cannot be replaced here.';
          d.hazards[i] = {
            ...d.hazards[i],
            category: canonicalHazardCategory((s as any).category) || d.hazards[i].category,
            hazard: s.hazard !== undefined ? String(s.hazard) : d.hazards[i].hazard,
            harm: s.harm !== undefined ? String(s.harm) : d.hazards[i].harm,
            severity: SEVERITIES.includes(String(s.severity)) ? String(s.severity) : d.hazards[i].severity,
          };
          d.openHazard[d.hazards[i].category] = true;
          return null;
        }
      }
      if (!String(s.hazard || '').trim()) return 'The hazard suggestion has no description.';
      const category = canonicalHazardCategory((s as any).category) || 'Safety';
      d.hazards.push({
        id: newEntryId(),
        category,
        hazard: String(s.hazard),
        harm: String(s.harm || ''),
        severity: SEVERITIES.includes(String(s.severity)) ? String(s.severity) : 'moderate',
      });
      d.openHazard[category] = true;
      return null;
    }
    default:
      return `Unknown suggestion kind "${s.kind}".`;
  }
};

// Answers payload built from an explicit snapshot (used right after a
// suggestion batch, before React state has re-rendered).
export const buildAnswersFrom = (
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
