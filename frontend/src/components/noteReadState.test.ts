import { mockApi } from '../test/mockApi';
import { commentReadAPI, type DomainEvent } from '../api/client';
import { commentsNeedingMe, isOwnComment, isUnread } from './noteReadState';

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    commentReadAPI: {
      list: vi.fn(() => Promise.resolve({ data: { comment_reads: [{ artifact_id: 'a1', read_at: '2026-11-09T10:00:00Z' }] } })),
    },
  })
);

const ev = (id: string, actor: string, artifact: string, at: string): DomainEvent => ({
  id,
  event_type: 'chatter.created',
  actor,
  payload: { artifact_id: artifact, entry_type: 'comment' },
  created_at: at,
});
const older = ev('e1', 'user:u2', 'a1', '2026-11-09T09:00:00Z');
const newer = ev('e2', 'user:u2', 'a1', '2026-11-09T10:00:00.5Z');
const neverRead = ev('e3', 'user:u3', 'a2', '2026-11-01T00:00:00Z');
const mine = ev('e4', 'user:me', 'a3', '2026-11-09T11:00:00Z');

describe('comment read state', () => {
  it('knows the member’s own comments by the event’s actor', () => {
    expect(isOwnComment(mine, 'me')).toBe(true);
    expect(isOwnComment(older, 'me')).toBe(false);
    expect(isOwnComment(mine, null)).toBe(false);
  });

  it('counts a comment unread when written after its artifact was read, or never read', () => {
    const reads = new Map([['a1', Date.parse('2026-11-09T10:00:00Z')]]);
    expect(isUnread(older, reads)).toBe(false);
    expect(isUnread(newer, reads)).toBe(true);
    expect(isUnread(neverRead, reads)).toBe(true);
  });

  it('leaves out the member’s own comments, with or without read state', async () => {
    expect((await commentsNeedingMe([older, mine], { userId: 'me' })).map((c) => c.id)).toEqual(['e1']);
    expect(commentReadAPI.list).not.toHaveBeenCalled();
  });

  it('with read state, keeps only the unread comments', async () => {
    const got = await commentsNeedingMe([older, newer, neverRead, mine], { userId: 'me', readState: true });
    expect(got.map((c) => c.id)).toEqual(['e2', 'e3']);
    expect(commentReadAPI.list).toHaveBeenCalledWith(['a1', 'a2']);
  });
});
