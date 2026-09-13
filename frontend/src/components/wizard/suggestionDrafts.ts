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

/**
 * The artifact types a suggestion may create. Mirrors the server's catalogue
 * (internal/domain/artifacts/types.go), which stays the authority — this list
 * only lets a typo be refused with a reason instead of a 400.
 */
export const ARTIFACT_TYPES = [
  'heading',
  'description',
  'persona',
  'user-need',
  'requirement',
  'design-item',
  'test-case',
  'hazard',
  'other',
] as const;

/** An artifact a suggestion asks for, ready to be created. */
export interface SuggestionDraft {
  type: string;
  title: string;
  body: string;
  attributes: Record<string, any>;
  /**
   * Heading the artifact belongs under, as a wizard section key
   * ("requirements", or "nfrs:Performance" for a category sub-heading). The
   * caller resolves it to a real heading artifact. Empty when the draft
   * names its place by reference instead.
   */
  sectionKey: string;
  /**
   * Where the artifact goes when the assistant named a place rather than a
   * kind: the parent heading's reference (empty for the top level) and,
   * optionally, the sibling it follows. Absent when sectionKey applies.
   */
  place?: { parentRef: string; afterRef?: string };
}

/** A change to an artifact that already exists, named by its reference. */
export interface ArtifactEdit {
  ref: string;
  title?: string;
  body?: string;
  /** Merged over the artifact's current attributes, never replacing them. */
  attributes?: Record<string, any>;
}

/** A move of an existing artifact, named by references. */
export interface ArtifactMove {
  ref: string;
  /**
   * New parent heading's reference; "" for the top level; undefined keeps
   * the current parent and only changes the position.
   */
  parentRef?: string;
  beforeRef?: string;
  afterRef?: string;
  position?: 'first' | 'last';
}

/** A suggestion that edits the product framing rather than adding anything. */
export interface FramingUpdate {
  field: 'vision' | 'problem_statement' | 'target_users';
  text: string;
}

export type SuggestionPlan =
  | { outcome: 'artifact'; draft: SuggestionDraft }
  | { outcome: 'framing'; update: FramingUpdate }
  | { outcome: 'edit'; edit: ArtifactEdit }
  | { outcome: 'move'; move: ArtifactMove }
  | { outcome: 'refused'; reason: string };

/** Artifact titles are a line, not a paragraph. */
const asTitle = (text: string, limit = 120): string =>
  text.length > limit ? `${text.slice(0, limit - 3)}…` : text;

const text = (value: unknown): string => String(value ?? '').trim();

/** A reference as the assistant wrote it, normalised the way refs are minted. */
const ref = (value: unknown): string => text(value).toUpperCase();

/** An attributes object, or nothing: a stray string here is not attributes. */
const attributesOf = (value: unknown): Record<string, any> | undefined =>
  value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, any>) : undefined;

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

    // The three shapes the assistant has beside the project rather than the
    // wizard: any artifact by type and place, a change to one, a move of one.
    case 'artifact': {
      const type = text(s.type);
      if (!(ARTIFACT_TYPES as readonly string[]).includes(type)) {
        return { outcome: 'refused', reason: `"${type || '?'}" is not an artifact type.` };
      }
      const title = text(s.title);
      if (!title) return { outcome: 'refused', reason: 'The artifact suggestion has no title.' };
      const afterRef = ref(s.after);
      return {
        outcome: 'artifact',
        draft: {
          type,
          title: asTitle(title),
          body: text(s.body),
          attributes: attributesOf(s.attributes) || {},
          sectionKey: '',
          place: { parentRef: ref(s.parent), ...(afterRef ? { afterRef } : {}) },
        },
      };
    }

    case 'edit': {
      const target = ref(s.ref);
      if (!target) return { outcome: 'refused', reason: 'The edit names no artifact.' };
      const edit: ArtifactEdit = { ref: target };
      if (s.title !== undefined) edit.title = asTitle(text(s.title));
      if (s.body !== undefined) edit.body = String(s.body ?? '');
      const attributes = attributesOf(s.attributes);
      if (attributes && Object.keys(attributes).length > 0) edit.attributes = attributes;
      if (edit.title === undefined && edit.body === undefined && !edit.attributes) {
        return { outcome: 'refused', reason: `The edit to ${target} changes nothing.` };
      }
      if (edit.title === '') return { outcome: 'refused', reason: 'An artifact cannot have an empty title.' };
      return { outcome: 'edit', edit };
    }

    case 'move': {
      const target = ref(s.ref);
      if (!target) return { outcome: 'refused', reason: 'The move names no artifact.' };
      const move: ArtifactMove = { ref: target };
      if (s.parent !== undefined) move.parentRef = ref(s.parent);
      const before = ref(s.before);
      const after = ref(s.after);
      if (before && after) {
        return { outcome: 'refused', reason: `The move of ${target} names both a before and an after.` };
      }
      if (before) move.beforeRef = before;
      if (after) move.afterRef = after;
      const position = text(s.position);
      if (position && position !== 'first' && position !== 'last') {
        return { outcome: 'refused', reason: `"${position}" is not a position; use first or last.` };
      }
      if (position) move.position = position as 'first' | 'last';
      if (move.parentRef === undefined && !before && !after && !position) {
        return { outcome: 'refused', reason: `The move of ${target} says nowhere to move it.` };
      }
      return { outcome: 'move', move };
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
