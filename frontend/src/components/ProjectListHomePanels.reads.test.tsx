import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { commentReadAPI } from '../api/client';
import { NeedsPanel } from './ProjectListHomePanels';
import type { TodoItem } from './ProjectListHomeData';

// Comment read state on Needs you (comment-reads): Mark read on a comment's
// row takes every comment on that artifact out, Mark all read takes them
// all, and Undo marks them unread again, from the earliest one listed.
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), { commentReadAPI: { set: vi.fn(() => Promise.resolve({ data: { comment_reads: [] } })) } })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const comment = (key: string, artifactId: string, at: string, ref: string): TodoItem => ({
  key,
  kind: 'comment',
  ref,
  title: 'Answer in time',
  meta: 'Priya: which threshold?',
  action: 'Open',
  to: `/projects/p1/requirements?artifact=${artifactId}&notes=1`,
  open: `/projects/p1/requirements?artifact=${artifactId}&notes=1`,
  comment: { artifactId, at },
});
const c1 = comment('comment-e1', 'a1', '2026-11-09T10:00:00Z', 'REQ-1');
const c2 = comment('comment-e2', 'a1', '2026-11-09T09:00:00Z', 'REQ-1');
const c3 = comment('comment-e3', 'a2', '2026-11-09T08:00:00Z', 'REQ-2');
const approve: TodoItem = {
  key: 'approve-a9',
  kind: 'approve',
  ref: 'REQ-9',
  title: 'Detect wheel slip',
  meta: 'In review · 2h',
  action: 'Review',
  to: '/projects/p1/requirements?artifact=a9',
  open: '/projects/p1/requirements?artifact=a9',
};

const render = async (el: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(<MemoryRouter>{el}</MemoryRouter>);
  });
};
const click = (b: HTMLButtonElement) =>
  act(async () => {
    b.click();
    for (let i = 0; i < 4; i++) await Promise.resolve();
  });
const byLabel = (label: RegExp) =>
  Array.from(container.querySelectorAll('button')).find((b) => label.test(b.getAttribute('aria-label') || b.textContent || ''))!;
const set = commentReadAPI.set as unknown as ReturnType<typeof vi.fn>;

beforeEach(() => vi.clearAllMocks());
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('NeedsPanel read state', () => {
  it('marks an artifact read from its row, and Undo marks it unread from the earliest comment', async () => {
    await render(<NeedsPanel items={[c1, c2, c3, approve]} limit={8} emptyText="Nothing" />);
    expect(container.querySelector('h2')?.textContent).toBe('Needs you 4');
    await click(container.querySelectorAll<HTMLButtonElement>('button[aria-label^="Mark read"]')[0]);
    expect(set).toHaveBeenCalledWith([{ artifact_id: 'a1' }]);
    expect(container.querySelector('h2')?.textContent).toBe('Needs you 2');
    expect(container.textContent).toContain('Marked read: REQ-1 Answer in time');

    await click(byLabel(/^Mark unread: REQ-1/));
    expect(set).toHaveBeenLastCalledWith([{ artifact_id: 'a1', read_at: '2026-11-09T08:59:59.999Z' }]);
    expect(container.querySelector('h2')?.textContent).toBe('Needs you 4');
    expect(container.textContent).not.toContain('Marked read');
  });

  it('marks every comment read from the header, leaving the other rows', async () => {
    await render(<NeedsPanel items={[c1, c2, c3, approve]} limit={8} emptyText="Nothing" />);
    await click(byLabel(/^Mark all read$/));
    expect(set).toHaveBeenCalledWith([{ artifact_id: 'a1' }, { artifact_id: 'a2' }]);
    expect(container.querySelector('h2')?.textContent).toBe('Needs you 1');
    expect(container.textContent).toContain('Marked read: 3 comments');
    expect(byLabel(/^Mark all read$/)).toBeUndefined();
  });

  it('offers no read controls on rows without read state', async () => {
    const { comment: _, ...plain } = c1;
    await render(<NeedsPanel items={[plain, approve]} limit={8} emptyText="Nothing" />);
    expect(container.querySelector('button')).toBeNull();
  });

  it('keeps the row when marking read fails, and says why', async () => {
    set.mockImplementationOnce(() => Promise.reject(new Error('offline')));
    await render(<NeedsPanel items={[c3]} limit={8} emptyText="Nothing" />);
    await click(byLabel(/^Mark read: REQ-2/));
    expect(container.querySelector('h2')?.textContent).toBe('Needs you 1');
    expect(container.querySelector('[role="alert"]')?.textContent).toBeTruthy();
  });
});
