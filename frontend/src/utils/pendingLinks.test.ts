import {
  PENDING_LINK_ID_PREFIX,
  applyLinkDeletion,
  createPendingLinkId,
  isPendingLinkId,
  serializePendingAdds,
  LinkChangeState,
} from './pendingLinks';

describe('createPendingLinkId / isPendingLinkId', () => {
  it('generates unique ids with the pending prefix', () => {
    const a = createPendingLinkId();
    const b = createPendingLinkId();
    expect(a).not.toEqual(b);
    expect(a.startsWith(PENDING_LINK_ID_PREFIX)).toBe(true);
    expect(isPendingLinkId(a)).toBe(true);
  });

  it('does not classify persisted UUIDs or empty values as pending', () => {
    expect(isPendingLinkId('550e8400-e29b-41d4-a716-446655440000')).toBe(false);
    expect(isPendingLinkId('')).toBe(false);
    expect(isPendingLinkId(undefined)).toBe(false);
    expect(isPendingLinkId(null)).toBe(false);
  });
});

describe('applyLinkDeletion', () => {
  const pendingId = `${PENDING_LINK_ID_PREFIX}1-1`;
  const otherPendingId = `${PENDING_LINK_ID_PREFIX}2-2`;
  const persistedId = '550e8400-e29b-41d4-a716-446655440000';

  const baseState = (): LinkChangeState => ({
    pendingLinkAdds: [
      { id: pendingId, from_id: 'a', to_id: 'b', type: 'satisfies' },
      { id: otherPendingId, from_id: 'a', to_id: 'c', type: 'verifies' },
    ],
    pendingLinkRemoves: [],
  });

  it('deleting a persisted link lands in pendingLinkRemoves even when pending adds exist (issue #18)', () => {
    // Regression: the old check `l.from_id && l.to_id && l.type` was true for
    // ANY pending add, so persisted-link deletions were misclassified and
    // never reached pendingLinkRemoves.
    const next = applyLinkDeletion(baseState(), persistedId);
    expect(next.pendingLinkRemoves).toEqual([persistedId]);
    expect(next.pendingLinkAdds).toHaveLength(2);
  });

  it('deleting a pending link removes exactly that entry and nothing else', () => {
    const next = applyLinkDeletion(baseState(), pendingId);
    expect(next.pendingLinkAdds).toHaveLength(1);
    expect(next.pendingLinkAdds[0].id).toEqual(otherPendingId);
    expect(next.pendingLinkRemoves).toEqual([]);
  });

  it('does not add duplicate ids to pendingLinkRemoves', () => {
    const once = applyLinkDeletion(baseState(), persistedId);
    const twice = applyLinkDeletion(once, persistedId);
    expect(twice.pendingLinkRemoves).toEqual([persistedId]);
  });

  it('ignores an empty/undefined link id instead of wiping pending adds', () => {
    const state = baseState();
    expect(applyLinkDeletion(state, '')).toBe(state);
    expect(applyLinkDeletion(state, undefined)).toBe(state);
    expect(state.pendingLinkAdds).toHaveLength(2);
  });

  it('does not mutate the input state', () => {
    const state = baseState();
    applyLinkDeletion(state, persistedId);
    applyLinkDeletion(state, pendingId);
    expect(state.pendingLinkAdds).toHaveLength(2);
    expect(state.pendingLinkRemoves).toEqual([]);
  });
});

describe('serializePendingAdds', () => {
  it('strips client-side temp ids but keeps the link payload', () => {
    const adds = [
      { id: createPendingLinkId(), from_id: 'a', to_id: 'b', type: 'satisfies', attributes: {} },
    ];
    const serialized = serializePendingAdds(adds);
    expect(serialized).toEqual([
      { from_id: 'a', to_id: 'b', type: 'satisfies', attributes: {} },
    ]);
    expect(serialized[0]).not.toHaveProperty('id');
  });
});
