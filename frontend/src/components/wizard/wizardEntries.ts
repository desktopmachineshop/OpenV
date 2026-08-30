// Wizard entry types and pure helpers for the guided requirements wizard.
//
// Every repeating entry carries a stable client-generated `id`, and
// cross-references between entries (need -> persona, requirement -> need)
// are stored by id — never by array index — so removing an entry can no
// longer silently shift references onto the wrong persona or need.
//
// Older sessions persisted index-based references (`persona_index`,
// `need_index`) and entries without ids. `normalizeWizardAnswers` migrates
// such payloads on load: it assigns ids and converts index references to id
// references in a single pass, so the first subsequent save persists the
// id-based format.

export interface PersonaEntry {
  id: string;
  name: string;
  role: string;
  goals: string;
  pains: string;
  artifact_id?: string;
}

export interface NeedEntry {
  id: string;
  /** Stable id of the persona this need belongs to ('' when unassigned). */
  persona_id: string;
  capability: string;
  outcome: string;
  artifact_id?: string;
}

export interface ReqEntry {
  id: string;
  /** Stable id of the user need this requirement derives from ('' when unassigned). */
  need_id: string;
  text: string;
  fit_criterion: string;
  verification_method: string;
  artifact_id?: string;
}

export interface NfrEntry {
  id: string;
  category: string;
  text: string;
  fit_criterion: string;
  verification_method: string;
  artifact_id?: string;
}

// Hazard sections: harm to people (Safety), design/implementation risk
// (Technical), malicious use or data exposure (Security), schedule/cost/
// resourcing risk (Programme), and in-service risks like misuse or
// environment (Operational).
export const HAZARD_CATEGORIES = ['Safety', 'Technical', 'Security', 'Programme', 'Operational'];

/** Map free-form category text onto the canonical list ('' when unknown). */
export const canonicalHazardCategory = (value: unknown): string =>
  HAZARD_CATEGORIES.find((c) => c.toLowerCase() === String(value ?? '').trim().toLowerCase()) || '';

export interface HazardEntry {
  id: string;
  category: string;
  hazard: string;
  harm: string;
  severity: string;
  artifact_id?: string;
}

export interface NormalizedWizardAnswers {
  personas: PersonaEntry[];
  needs: NeedEntry[];
  requirements: ReqEntry[];
  nfrs: NfrEntry[];
  hazards: HazardEntry[];
  /** "<messageId>:<segmentIndex>" keys of copilot suggestions already applied. */
  appliedSuggestionKeys: string[];
}

/** Generate a stable client-side id for a wizard entry. */
export const newEntryId = (): string => {
  const c: any = typeof crypto !== 'undefined' ? crypto : undefined;
  if (c && typeof c.randomUUID === 'function') return c.randomUUID();
  return `wz-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
};

const str = (v: any): string => (v === undefined || v === null ? '' : String(v));

const entryId = (v: any): string => (typeof v === 'string' && v ? v : newEntryId());

const withArtifactId = <T extends object>(entry: T, raw: any): T =>
  raw && typeof raw.artifact_id === 'string' && raw.artifact_id
    ? { ...entry, artifact_id: raw.artifact_id }
    : entry;

const rawList = (v: any): any[] => (Array.isArray(v) ? v.filter((e) => e && typeof e === 'object') : []);

/**
 * Resolve a reference that may be stored as a stable id (new format) or a
 * legacy array index (old format). Returns '' when it cannot be resolved.
 */
const resolveRef = (rawId: any, legacyIndex: any, targets: { id: string }[]): string => {
  if (typeof rawId === 'string' && rawId) return rawId;
  if (typeof legacyIndex === 'number' && Number.isInteger(legacyIndex)) {
    return targets[legacyIndex]?.id || '';
  }
  return '';
};

/**
 * Build normalized, id-based wizard entry lists from a session answers
 * payload — whether it was saved in the current id-based format or the
 * legacy index-based one.
 */
export const normalizeWizardAnswers = (answers: Record<string, any>): NormalizedWizardAnswers => {
  const a = answers || {};

  const personas: PersonaEntry[] = rawList(a.step_2?.personas).map((p) =>
    withArtifactId(
      {
        id: entryId(p.id),
        name: str(p.name),
        role: str(p.role),
        goals: str(p.goals),
        pains: str(p.pains),
      },
      p
    )
  );

  const needs: NeedEntry[] = rawList(a.step_3?.needs).map((n) =>
    withArtifactId(
      {
        id: entryId(n.id),
        persona_id: resolveRef(n.persona_id, n.persona_index, personas),
        capability: str(n.capability),
        outcome: str(n.outcome),
      },
      n
    )
  );

  const requirements: ReqEntry[] = rawList(a.step_4?.requirements).map((r) =>
    withArtifactId(
      {
        id: entryId(r.id),
        need_id: resolveRef(r.need_id, r.need_index, needs),
        text: str(r.text),
        fit_criterion: str(r.fit_criterion),
        verification_method: str(r.verification_method) || 'test',
      },
      r
    )
  );

  const nfrs: NfrEntry[] = rawList(a.step_5?.nfrs).map((n) =>
    withArtifactId(
      {
        id: entryId(n.id),
        category: str(n.category),
        text: str(n.text),
        fit_criterion: str(n.fit_criterion),
        verification_method: str(n.verification_method) || 'test',
      },
      n
    )
  );

  const hazards: HazardEntry[] = rawList(a.step_6?.hazards).map((h) =>
    withArtifactId(
      {
        id: entryId(h.id),
        // Entries saved before hazard sections existed default to Safety —
        // the step's original framing was harm from failure or misuse.
        category: canonicalHazardCategory(h.category) || 'Safety',
        hazard: str(h.hazard),
        harm: str(h.harm),
        severity: str(h.severity) || 'moderate',
      },
      h
    )
  );

  const appliedSuggestionKeys: string[] = Array.isArray(a.copilot_applied)
    ? a.copilot_applied.filter((k: any): k is string => typeof k === 'string' && !!k)
    : [];

  return { personas, needs, requirements, nfrs, hazards, appliedSuggestionKeys };
};
