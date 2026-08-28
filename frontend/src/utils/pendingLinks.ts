import { Link } from '../api/client';

/**
 * Helpers for managing traceability-link changes made inside the artifact
 * editor before they are persisted ("pending" adds/removes that are only
 * applied when the user clicks Update).
 *
 * Pending (unsaved) links never have a server-assigned id, so they get a
 * stable client-side temp id. The prefix guarantees a temp id can never
 * collide with a persisted link's UUID, which is what deletion uses to
 * classify a link as "pending add" vs "persisted".
 */

export const PENDING_LINK_ID_PREFIX = 'pending-link-';

let pendingLinkCounter = 0;

/** Generate a stable client-side key for a not-yet-persisted link. */
export const createPendingLinkId = (): string => {
  pendingLinkCounter += 1;
  return `${PENDING_LINK_ID_PREFIX}${Date.now()}-${pendingLinkCounter}`;
};

/** True when the id is a client-side temp id for an unsaved link. */
export const isPendingLinkId = (id: string | null | undefined): boolean =>
  typeof id === 'string' && id.startsWith(PENDING_LINK_ID_PREFIX);

export interface LinkChangeState {
  /** Links added in this edit session; each carries a temp id (see above). */
  pendingLinkAdds: Partial<Link>[];
  /** Ids of persisted links marked for deletion on save. */
  pendingLinkRemoves: string[];
}

/**
 * Apply deletion of a link during an edit session.
 *
 * - Deleting a pending (unsaved) add removes exactly that entry and nothing
 *   else — it must NOT land in pendingLinkRemoves (there is nothing to
 *   delete server-side).
 * - Deleting a persisted link records its id in pendingLinkRemoves (once),
 *   so the Update request deletes it server-side.
 *
 * Returns a new state object; the input is not mutated.
 */
export const applyLinkDeletion = (
  state: LinkChangeState,
  linkId: string | null | undefined
): LinkChangeState => {
  if (!linkId) {
    // No id: nothing can be safely matched — leave state untouched rather
    // than wiping unrelated pending adds.
    return state;
  }

  const isNewlyAdded = state.pendingLinkAdds.some((l) => l.id === linkId);

  if (isNewlyAdded) {
    return {
      pendingLinkAdds: state.pendingLinkAdds.filter((l) => l.id !== linkId),
      pendingLinkRemoves: state.pendingLinkRemoves,
    };
  }

  return {
    pendingLinkAdds: state.pendingLinkAdds,
    pendingLinkRemoves: state.pendingLinkRemoves.includes(linkId)
      ? state.pendingLinkRemoves
      : [...state.pendingLinkRemoves, linkId],
  };
};

/**
 * Strip client-side temp ids before sending pending adds to the server.
 * The backend creates links from from_id/to_id/type/attributes and assigns
 * its own ids.
 */
export const serializePendingAdds = (adds: Partial<Link>[]): Partial<Link>[] =>
  adds.map(({ id, ...rest }) => rest);
