import { pickVersion } from './useArtifactHistoryLink';

// A source artifact's versions around a link's life: v2 is written when the
// link is added (its snapshot holds it), v3 an unrelated edit, v4 when the
// link is removed (the snapshot no longer holds it).
const v = (version: number, from: string, to: string | null, links: string[] = []) => ({
  version,
  valid_from: from,
  valid_to: to,
  attributes: { links_snapshot: links.map((id) => ({ id })) },
});
const versions = [
  v(1, '2026-11-01T10:00:00Z', '2026-11-02T10:00:00Z'),
  v(2, '2026-11-02T10:00:00Z', '2026-11-03T10:00:00Z', ['l1']),
  v(3, '2026-11-03T10:00:00Z', '2026-11-04T10:00:00Z', ['l1']),
  v(4, '2026-11-04T10:00:00Z', null),
];

describe('pickVersion', () => {
  it('finds the last version that still had a removed link', () => {
    expect(pickVersion(versions, 'l1', '2026-11-04T10:00:00.400Z')?.version).toBe(3);
  });

  it('allows a moment of slack for the versions a link write makes before its event', () => {
    expect(pickVersion(versions, 'l1', '2026-11-02T09:59:59.500Z')?.version).toBe(2);
  });

  it('falls back to the version current at the time when no snapshot holds the link', () => {
    expect(pickVersion(versions, 'nope', '2026-11-03T12:00:00Z')?.version).toBe(3);
    expect(pickVersion(versions, null, '2026-11-01T12:00:00Z')?.version).toBe(1);
  });

  it('means the current version when nothing says when', () => {
    expect(pickVersion(versions, null, null)).toBeNull();
  });
});
