import type { CopilotSuggestionLike } from './suggestionDrafts';
import type { HazardEntry, NeedEntry, NfrEntry, PersonaEntry, ReqEntry } from './wizardEntries';

/**
 * The title and body of the artifacts the wizard and the assistant write, in
 * both of the variants that write them.
 *
 * The guided wizard writes an artifact from an entry in its form when the
 * person presses Next (GuidedWizard). The notes panel writes one straight from
 * a suggestion card, beside a project that already exists (planSuggestion in
 * suggestionDrafts.ts). Both trim every field they write; they differ in
 * places, such as which titles are cut, and that difference is kept
 * (refactor plan Q16); both live here so it is in one place.
 */

/** An artifact's title and body. */
export interface ArtifactText {
  title: string;
  body: string;
}

// ---------------------------------------------------------------------------
// The guided wizard
// ---------------------------------------------------------------------------

export const wizardPersonaText = (p: PersonaEntry): ArtifactText => ({
  title: p.name.trim(),
  body: `**Role:** ${p.role.trim()}\n\n**Goals:**\n${p.goals.trim()}\n\n**Pain points:**\n${p.pains.trim()}`,
});

export const wizardNeedText = (n: NeedEntry, persona: PersonaEntry | undefined): ArtifactText => {
  // The persona as its own artifact is titled: trimmed.
  const personaName = persona?.name.trim() || 'a user';
  const sentence = `As ${personaName}, I need ${n.capability.trim()} so that ${n.outcome.trim() || '…'}`;
  return {
    title: sentence.length > 120 ? `${sentence.slice(0, 117)}…` : sentence,
    body: sentence,
  };
};

export const wizardRequirementText = (r: ReqEntry): ArtifactText => ({
  title: r.text.trim().length > 120 ? `${r.text.trim().slice(0, 117)}…` : r.text.trim(),
  body: `${r.text.trim()}${r.fit_criterion.trim() ? `\n\n**Fit criterion:** ${r.fit_criterion.trim()}` : ''}`,
});

export const wizardNfrText = (n: NfrEntry): ArtifactText => ({
  title: n.text.trim().length > 100 ? `${n.text.trim().slice(0, 97)}…` : n.text.trim(),
  body: `${n.text.trim()}${n.fit_criterion.trim() ? `\n\n**Fit criterion:** ${n.fit_criterion.trim()}` : ''}`,
});

export const wizardHazardText = (h: HazardEntry): ArtifactText => ({
  title: h.hazard.trim(),
  body: `**Category:** ${h.category}\n\n**Potential harm:** ${h.harm.trim()}\n\n**Severity:** ${h.severity}`,
});

/** A verification stub for a requirement the wizard materialized. */
export const wizardTestStubText = (c: { title: string }): ArtifactText => ({
  title: `Verify: ${c.title}`,
  body: `Test case stub for requirement: ${c.title}\n\nDefine steps, preconditions and expected results.`,
});

// ---------------------------------------------------------------------------
// The notes panel
// ---------------------------------------------------------------------------

/** Artifact titles are a line, not a paragraph. */
export const asTitle = (text: string, limit = 120): string =>
  text.length > limit ? `${text.slice(0, limit - 3)}…` : text;

export const text = (value: unknown): string => String(value ?? '').trim();

export const notesPanelPersonaText = (name: string, s: CopilotSuggestionLike): ArtifactText => ({
  title: asTitle(name),
  body: `**Role:** ${text(s.role)}\n\n**Goals:**\n${text(s.goals)}\n\n**Pain points:**\n${text(s.pains)}`,
});

export const notesPanelNeedText = (capability: string, s: CopilotSuggestionLike): ArtifactText => {
  // The same "As … I need … so that …" sentence the wizard writes, so a
  // need added from the chat reads like every other one.
  const who = text(s.persona) || 'a user';
  const sentence = `As ${who}, I need ${capability} so that ${text(s.outcome) || '…'}`;
  return {
    title: asTitle(sentence),
    body: sentence,
  };
};

export const notesPanelRequirementText = (statement: string, fit: string): ArtifactText => ({
  title: asTitle(statement),
  body: `${statement}${fit ? `\n\n**Fit criterion:** ${fit}` : ''}`,
});

export const notesPanelNfrText = (statement: string, fit: string): ArtifactText => ({
  title: asTitle(statement, 100),
  body: `${statement}${fit ? `\n\n**Fit criterion:** ${fit}` : ''}`,
});

export const notesPanelHazardText = (
  hazard: string,
  category: string,
  severity: string,
  s: CopilotSuggestionLike
): ArtifactText => ({
  title: asTitle(hazard),
  body: `**Category:** ${category}\n\n**Potential harm:** ${text(s.harm)}\n\n**Severity:** ${severity}`,
});
