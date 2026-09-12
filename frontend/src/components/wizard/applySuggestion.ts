import { Artifact, artifactAPI, productProfileAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import {
  CopilotSuggestionLike,
  FramingUpdate,
  SectionHeading,
  SuggestionDraft,
  headingsFor,
  planSuggestion,
} from './suggestionDrafts';

/**
 * Applying an assistant suggestion to a project that already exists.
 *
 * In the wizard a suggestion fills in the form, and the artifacts appear when
 * the person presses Next. Outside it there is no form to fill: the Notes
 * panel's assistant talks about a project whose requirements are already
 * there, so "add this" has to mean "add it to the project, now".
 *
 * Artifacts arrive as DRAFTS, under the same headings the wizard uses. Draft
 * because a suggestion is a proposal and the reader has not reviewed it yet —
 * the normal review workflow is what promotes it — and under the same
 * headings so applying from the chat does not grow a second set of sections
 * beside the wizard's.
 */

/** Everything an apply needs to know about where it is happening. */
export interface ApplyContext {
  projectId: string;
  /** The project's artifacts, used to find headings that already exist. */
  artifacts: Artifact[];
  /** Called after a write so the caller can refresh what it is showing. */
  onChanged?: () => void;
}

const headingTitle = (a: Artifact): string => (a.title || '').trim().toLowerCase();

/**
 * The id of a heading with this title, creating it when the project has none.
 *
 * Matched on title because that is the only thing the two paths share — the
 * wizard records its section ids in its own session answers, which a project
 * that never ran the wizard does not have.
 */
const findOrCreateHeading = async (
  ctx: ApplyContext,
  heading: SectionHeading,
  parentId: string | undefined,
  cache: Record<string, string>
): Promise<string> => {
  if (cache[heading.key]) return cache[heading.key];

  const wanted = heading.title.trim().toLowerCase();
  const existing = ctx.artifacts.find(
    (a) => a.type === 'heading' && headingTitle(a) === wanted && (!parentId || a.parent_id === parentId)
  );
  if (existing) {
    cache[heading.key] = existing.id;
    return existing.id;
  }

  const created = await artifactAPI.create({
    project_id: ctx.projectId,
    type: 'heading',
    title: heading.title,
    body: '',
    sort_order: heading.sort,
    ...(parentId ? { parent_id: parentId } : {}),
  } as Partial<Artifact>);
  const id = created.data?.id || '';
  if (id) cache[heading.key] = id;
  return id;
};

/** Resolve a draft's section key to the heading it belongs under. */
const resolveSection = async (
  ctx: ApplyContext,
  sectionKey: string,
  cache: Record<string, string>
): Promise<string | undefined> => {
  let parentId: string | undefined;
  for (const heading of headingsFor(sectionKey)) {
    const id = await findOrCreateHeading(ctx, heading, parentId, cache);
    if (!id) return parentId;
    parentId = id;
  }
  return parentId;
};

const createFromDraft = async (
  ctx: ApplyContext,
  draft: SuggestionDraft,
  cache: Record<string, string>
): Promise<void> => {
  const parentId = await resolveSection(ctx, draft.sectionKey, cache);
  await artifactAPI.create({
    project_id: ctx.projectId,
    type: draft.type,
    title: draft.title,
    body: draft.body,
    // A suggestion is a proposal until somebody reads it, so it lands in the
    // same draft state anything else added by an agent would.
    attributes: { ...draft.attributes, status: 'draft' },
    ...(parentId ? { parent_id: parentId } : {}),
  } as Partial<Artifact>);
};

/**
 * Apply a batch of suggestions to the project, returning one result per
 * suggestion: null when it was applied, or the reason it was not.
 *
 * One heading cache is shared across the batch, so applying five NFRs at once
 * creates their section heading once rather than five times.
 */
export const applySuggestionsToProject = async (
  ctx: ApplyContext,
  items: { suggestion: CopilotSuggestionLike; key: string }[]
): Promise<(string | null)[]> => {
  const cache: Record<string, string> = {};
  const results: (string | null)[] = [];
  let wrote = false;

  // Framing edits are gathered rather than written as they are met: the
  // profile endpoint replaces the whole profile, so each one has to be a
  // read-modify-write, and doing that per suggestion would let two framing
  // suggestions in one batch overwrite each other.
  const framing: FramingUpdate[] = [];
  const framingAt: number[] = [];

  for (const { suggestion } of items) {
    const plan = planSuggestion(suggestion);
    if (plan.outcome === 'refused') {
      results.push(plan.reason);
      continue;
    }
    if (plan.outcome === 'framing') {
      framing.push(plan.update);
      framingAt.push(results.length);
      results.push(null);
      continue;
    }
    try {
      await createFromDraft(ctx, plan.draft, cache);
      wrote = true;
      results.push(null);
    } catch (err: any) {
      results.push(apiErrorMessage(err, 'The change could not be saved.'));
    }
  }

  if (framing.length > 0) {
    try {
      // Read the profile and send it back whole with the suggested fields
      // changed. A partial PUT would blank every field it left out — the
      // endpoint assigns the request's values unconditionally.
      const current = await productProfileAPI.get(ctx.projectId);
      const profile = current.data || ({} as any);
      const payload: Record<string, any> = {
        vision: profile.vision || '',
        problem_statement: profile.problem_statement || '',
        target_users: profile.target_users || '',
        constraints: profile.constraints || [],
        success_metrics: profile.success_metrics || [],
        settings: profile.settings || {},
      };
      framing.forEach((u) => {
        payload[u.field] = u.text;
      });
      await productProfileAPI.update(ctx.projectId, payload);
      wrote = true;
    } catch (err: any) {
      const reason = apiErrorMessage(err, 'The framing could not be saved.');
      framingAt.forEach((i) => {
        results[i] = reason;
      });
    }
  }

  if (wrote) ctx.onChanged?.();
  return results;
};
