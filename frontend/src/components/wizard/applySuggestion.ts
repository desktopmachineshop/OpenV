import { Artifact, artifactAPI, productProfileAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { nextSortOrder, planPlacement, saveSiblingOrder } from '../../utils/artifactOrder';
import {
  ArtifactEdit,
  ArtifactMove,
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
 *
 * Beside the project the assistant can also name a place by reference, edit
 * an artifact it has read, or move one. Those go through the same calls the
 * module view makes for the same actions, so a card and a drag end in the
 * same state.
 */

/** Feature gate for the assistant's project changes (REQ-137). */
export const ASSISTANT_EDITS_FEATURE = 'assistant-project-edits';

/**
 * The suggestion kinds that change the project rather than add a wizard
 * entry to it. They wait for the workspace's stable release like any other
 * new feature; until then their cards explain instead of acting.
 */
export const PROJECT_EDIT_KINDS: ReadonlySet<string> = new Set(['artifact', 'edit', 'move']);
export const isProjectEditKind = (kind: unknown): boolean => PROJECT_EDIT_KINDS.has(String(kind));
export const GATED_REASON =
  'Editing and moving artifacts from the assistant reaches this workspace with its next stable release.';

/** Everything an apply needs to know about where it is happening. */
export interface ApplyContext {
  projectId: string;
  /**
   * The project's artifacts, used to find headings, references and
   * siblings. A working copy: each write in a batch folds its result back
   * in, so a later suggestion sees what an earlier one did.
   */
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

/**
 * The artifact a reference names, or null. References are minted upper-case
 * and the assistant is told to copy them, but a person reading the card
 * should not be failed by a "req-12". An artifact id is accepted too: a
 * locked wizard entry knows its artifact by id, not by reference.
 */
const byRef = (artifacts: Artifact[], reference: string): Artifact | null => {
  const wanted = reference.trim();
  const upper = wanted.toUpperCase();
  return artifacts.find((a) => (a.ref || '').toUpperCase() === upper || a.id === wanted) || null;
};

/** The heading a place names, or an explanation of why it cannot be found. */
const resolvePlace = (
  ctx: ApplyContext,
  parentRef: string
): { parentId: string | null } | { refused: string } => {
  if (!parentRef) return { parentId: null };
  const parent = byRef(ctx.artifacts, parentRef);
  if (!parent) return { refused: `There is no artifact ${parentRef} to file this under.` };
  return { parentId: parent.id };
};

const createFromDraft = async (
  ctx: ApplyContext,
  draft: SuggestionDraft,
  cache: Record<string, string>
): Promise<Artifact> => {
  let parentId: string | undefined | null;
  let sortOrder: number | undefined;
  if (draft.place) {
    const place = resolvePlace(ctx, draft.place.parentRef);
    if ('refused' in place) throw new Error(place.refused);
    parentId = place.parentId;
    // A new artifact goes last under its parent unless the assistant named
    // the sibling it follows; the follow-up placement below does the rest.
    sortOrder = nextSortOrder(ctx.artifacts, parentId);
  } else {
    parentId = await resolveSection(ctx, draft.sectionKey, cache);
  }
  const created = await artifactAPI.create({
    project_id: ctx.projectId,
    type: draft.type,
    title: draft.title,
    body: draft.body,
    // A suggestion is a proposal until somebody reads it, so it lands in the
    // same draft state anything else added by an agent would.
    attributes: { ...draft.attributes, status: 'draft' },
    ...(parentId ? { parent_id: parentId } : {}),
    ...(sortOrder !== undefined ? { sort_order: sortOrder } : {}),
  } as Partial<Artifact>);
  const artifact = created.data;
  // Keep the working copy current so a later suggestion in the same batch
  // can place something after this one, or move it.
  ctx.artifacts = [...ctx.artifacts, artifact];

  if (draft.place?.afterRef) {
    const anchor = byRef(ctx.artifacts, draft.place.afterRef);
    if (!anchor) throw new Error(`Added, but there is no ${draft.place.afterRef} to place it after.`);
    const plan = planPlacement(ctx.artifacts, artifact.id, { afterId: anchor.id });
    if (!plan) throw new Error(`Added, but ${draft.place.afterRef} is not under the same heading.`);
    fold(ctx, await saveSiblingOrder(plan, artifact.id));
  }
  return artifact;
};

/** Replace the working copies of the artifacts a write returned. */
const fold = (ctx: ApplyContext, written: Artifact[]): void => {
  const byId = new Map(written.map((a) => [a.id, a]));
  ctx.artifacts = ctx.artifacts.map((a) => byId.get(a.id) || a);
};

/**
 * Change an artifact's content. Only the fields the suggestion names are
 * sent, so the API leaves the rest alone — except attributes, which the API
 * replaces wholesale and so are merged here over the current ones.
 */
const applyEdit = async (ctx: ApplyContext, edit: ArtifactEdit): Promise<void> => {
  const target = byRef(ctx.artifacts, edit.ref);
  if (!target) throw new Error(`There is no artifact ${edit.ref} to change.`);
  const payload: Partial<Artifact> = {};
  if (edit.title !== undefined) payload.title = edit.title;
  if (edit.body !== undefined) payload.body = edit.body;
  if (edit.attributes) payload.attributes = { ...(target.attributes || {}), ...edit.attributes };
  const res = await artifactAPI.update(target.id, payload);
  fold(ctx, [res.data]);
};

/**
 * Move an artifact to the place the suggestion describes. The plan is the
 * same shape a drag produces and is saved the same way, so a move from the
 * chat cannot leave the tree in a state a drag could not.
 */
const applyMove = async (ctx: ApplyContext, move: ArtifactMove): Promise<void> => {
  const source = byRef(ctx.artifacts, move.ref);
  if (!source) throw new Error(`There is no artifact ${move.ref} to move.`);
  let parentId: string | null | undefined;
  if (move.parentRef !== undefined) {
    const place = resolvePlace(ctx, move.parentRef);
    if ('refused' in place) throw new Error(place.refused.replace('file this under', `move ${move.ref} under`));
    parentId = place.parentId;
  }
  const anchorRef = move.beforeRef || move.afterRef;
  let anchor: Artifact | null = null;
  if (anchorRef) {
    anchor = byRef(ctx.artifacts, anchorRef);
    if (!anchor) throw new Error(`There is no artifact ${anchorRef} to place ${move.ref} beside.`);
  }
  const plan = planPlacement(ctx.artifacts, source.id, {
    parentId,
    beforeId: move.beforeRef && anchor ? anchor.id : undefined,
    afterId: move.afterRef && anchor ? anchor.id : undefined,
    position: move.position,
  });
  if (!plan) {
    // Either nothing changes, or the move is impossible (into its own
    // subtree, beside an artifact under another heading). Say which.
    if (parentId && source.id === parentId) throw new Error(`${move.ref} cannot be moved under itself.`);
    if (anchorRef) throw new Error(`${anchorRef} is not under the heading ${move.ref} would move to.`);
    return;
  }
  fold(ctx, await saveSiblingOrder(plan, source.id));
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
      if (plan.outcome === 'edit') await applyEdit(ctx, plan.edit);
      else if (plan.outcome === 'move') await applyMove(ctx, plan.move);
      else await createFromDraft(ctx, plan.draft, cache);
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
