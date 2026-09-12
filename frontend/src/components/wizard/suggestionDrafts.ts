import {
  SUBSECTION_SPECS,
  canonicalHazardCategory,
  canonicalNfrCategory,
  subSectionKey,
} from './wizardEntries';

/**
 * Turning one of the assistant's suggestions into the thing it describes.
 *
 * Inside the wizard a suggestion edits the form the person is filling in, and
 * only becomes an artifact when they press Next. Outside it there is no form
 * — the project already exists — so the same suggestion has to describe an
 * artifact directly. This module is the shared answer to "what does this
 * suggestion actually mean", so the two paths cannot drift into disagreeing
 * about what a `need` is.
 *
 * It is deliberately pure: no API, no React. Deciding what a suggestion means
 * is the part worth testing, and the part that would otherwise be buried in a
 * component the ESM-only markdown stack keeps out of the test runner.
 */

/** One structured proposal embedded in a copilot reply. */
export interface CopilotSuggestionLike {
  kind: string;
  [key: string]: any;
}

/** An artifact a suggestion asks for, ready to be created. */
export interface SuggestionDraft {
  type: string;
  title: string;
  body: string;
  attributes: Record<string, any>;
  /**
   * Heading the artifact belongs under, as a wizard section key
   * ("requirements", or "nfrs:Performance" for a category sub-heading). The
   * caller resolves it to a real heading artifact.
   */
  sectionKey: string;
}

/** A suggestion that edits the product framing rather than adding anything. */
export interface FramingUpdate {
  field: 'vision' | 'problem_statement' | 'target_users';
  text: string;
}

export type SuggestionPlan =
  | { outcome: 'artifact'; draft: SuggestionDraft }
  | { outcome: 'framing'; update: FramingUpdate }
  | { outcome: 'refused'; reason: string };

/** Artifact titles are a line, not a paragraph. */
const asTitle = (text: string, limit = 120): string =>
  text.length > limit ? `${text.slice(0, limit - 3)}…` : text;

const text = (value: unknown): string => String(value ?? '').trim();

/**
 * What this suggestion means as a change to the project.
 *
 * A refusal carries the reason a person can act on, because the alternative —
 * a button that does nothing — is what made the assistant's suggestions feel
 * broken in the first place.
 */
export const planSuggestion = (s: CopilotSuggestionLike): SuggestionPlan => {
  switch (s.kind) {
    case 'framing': {
      const body = text(s.text);
      if (!body) return { outcome: 'refused', reason: 'The suggestion has no text.' };
      const field = String(s.field || '');
      if (field !== 'vision' && field !== 'problem_statement' && field !== 'target_users') {
        return { outcome: 'refused', reason: `Unknown framing field "${s.field}".` };
      }
      return { outcome: 'framing', update: { field, text: body } };
    }

    case 'persona': {
      const name = text(s.name);
      if (!name) return { outcome: 'refused', reason: 'The persona suggestion has no name.' };
      return {
        outcome: 'artifact',
        draft: {
          type: 'persona',
          title: asTitle(name),
          body: `**Role:** ${text(s.role)}\n\n**Goals:**\n${text(s.goals)}\n\n**Pain points:**\n${text(s.pains)}`,
          attributes: {},
          sectionKey: 'personas',
        },
      };
    }

    case 'need': {
      const capability = text(s.capability);
      if (!capability) return { outcome: 'refused', reason: 'The need suggestion has no capability.' };
      // The same "As … I need … so that …" sentence the wizard writes, so a
      // need added from the chat reads like every other one.
      const who = text(s.persona) || 'a user';
      const sentence = `As ${who}, I need ${capability} so that ${text(s.outcome) || '…'}`;
      return {
        outcome: 'artifact',
        draft: {
          type: 'user-need',
          title: asTitle(sentence),
          body: sentence,
          attributes: {},
          sectionKey: 'needs',
        },
      };
    }

    case 'requirement': {
      const statement = text(s.text);
      if (!statement) return { outcome: 'refused', reason: 'The requirement suggestion has no text.' };
      const fit = text(s.fit_criterion);
      return {
        outcome: 'artifact',
        draft: {
          type: 'requirement',
          title: asTitle(statement),
          body: `${statement}${fit ? `\n\n**Fit criterion:** ${fit}` : ''}`,
          attributes: { verification_method: text(s.verification_method) || 'test' },
          sectionKey: 'requirements',
        },
      };
    }

    case 'nfr': {
      const statement = text(s.text);
      if (!statement) return { outcome: 'refused', reason: 'The NFR suggestion has no text.' };
      const category = canonicalNfrCategory(s.category);
      const fit = text(s.fit_criterion);
      return {
        outcome: 'artifact',
        draft: {
          type: 'requirement',
          title: asTitle(statement, 100),
          body: `${statement}${fit ? `\n\n**Fit criterion:** ${fit}` : ''}`,
          attributes: { verification_method: text(s.verification_method) || 'test', category },
          sectionKey: subSectionKey('nfrs', category),
        },
      };
    }

    case 'hazard': {
      const hazard = text(s.hazard);
      if (!hazard) return { outcome: 'refused', reason: 'The hazard suggestion has no hazard.' };
      const category = canonicalHazardCategory(s.category);
      const severity = text(s.severity) || 'Medium';
      return {
        outcome: 'artifact',
        draft: {
          type: 'hazard',
          title: asTitle(hazard),
          body: `**Category:** ${category}\n\n**Potential harm:** ${text(s.harm)}\n\n**Severity:** ${severity}`,
          attributes: { severity, category },
          sectionKey: subSectionKey('hazards', category),
        },
      };
    }

    default:
      return { outcome: 'refused', reason: `I do not know how to add a "${s.kind}".` };
  }
};

/**
 * The heading a section key names, and the heading above it when the key
 * names a category sub-heading. Titles match the wizard's exactly so both
 * paths land in the same place rather than growing a second set of headings
 * beside the first.
 */
export const SECTION_TITLES: Record<string, { title: string; sort: number }> = {
  personas: { title: 'Personas', sort: 10 },
  needs: { title: 'User Needs', sort: 20 },
  requirements: { title: 'Requirements', sort: 30 },
  nfrs: { title: 'Non-Functional Requirements & Constraints', sort: 40 },
  hazards: { title: 'Hazards & Risks', sort: 50 },
  tests: { title: 'Verification Tests', sort: 60 },
};

export interface SectionHeading {
  key: string;
  title: string;
  sort: number;
  /** Key of the heading this one sits under, empty for a top-level section. */
  parentKey: string;
}

/**
 * The headings a section key needs, outermost first, so a caller can find or
 * create them in order. "nfrs:Performance" needs its parent section before
 * its own sub-heading exists.
 */
export const headingsFor = (sectionKey: string): SectionHeading[] => {
  const colon = sectionKey.indexOf(':');
  if (colon === -1) {
    const spec = SECTION_TITLES[sectionKey];
    return spec ? [{ key: sectionKey, title: spec.title, sort: spec.sort, parentKey: '' }] : [];
  }
  const parentKey = sectionKey.slice(0, colon);
  const category = sectionKey.slice(colon + 1);
  const parentSpec = SECTION_TITLES[parentKey];
  const subSpec = SUBSECTION_SPECS[parentKey];
  if (!parentSpec || !subSpec) return [];
  return [
    { key: parentKey, title: parentSpec.title, sort: parentSpec.sort, parentKey: '' },
    {
      key: sectionKey,
      title: subSpec.title(category),
      sort: 10 * (subSpec.categories.indexOf(category) + 1),
      parentKey,
    },
  ];
};
