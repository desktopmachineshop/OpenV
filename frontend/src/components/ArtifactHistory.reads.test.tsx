import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { chatterAPI, commentReadAPI } from '../api/client';
import { ArtifactHistory } from './ArtifactHistory';
import { useAppStore } from '../state/store';

// Reading an artifact's comments marks them read (comment-reads): someone
// else's comment written since the member last read them is New, the panel
// marks them read once, and Mark unread on a comment puts it back in Needs
// you without the panel marking it read again.
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    commentReadAPI: {
      list: vi.fn(() => Promise.resolve({ data: { comment_reads: [{ artifact_id: 'art-1', read_at: '2026-11-09T09:30:00Z' }] } })),
      set: vi.fn(() => Promise.resolve({ data: { comment_reads: [] } })),
    },
  })
);
let readsOn = true;
vi.mock('../hooks/useFeature', () => ({ useFeature: (key: string) => key === 'comment-reads' && readsOn }));
vi.mock('./NoteTodo', () => ({ NoteTodoChip: () => null, AddTodoControl: () => null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const entry = (id: string, by: string, at: string, message: string) => ({
  id,
  artifact_id: 'art-1',
  message,
  is_auto_entry: false,
  entry_type: 'comment',
  created_by: by,
  author_name: by,
  created_at: at,
  updated_at: at,
});
const NEW = entry('n1', 'priya', '2026-11-09T10:00:00Z', 'Which threshold?');
const OLD = entry('o1', 'priya', '2026-11-09T09:00:00Z', 'Looks right');
const MINE = entry('m1', 'me', '2026-11-09T11:00:00Z', 'Mine');

let container: HTMLDivElement;
let root: Root;
const set = commentReadAPI.set as unknown as ReturnType<typeof vi.fn>;
const settle = () =>
  act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });

const mount = async (entries: unknown[]) => {
  vi.mocked(chatterAPI.list, { partial: true, deep: true }).mockResolvedValue({ data: entries } as any);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(<ArtifactHistory artifactId="art-1" projectId="p1" />);
  });
  await settle();
};
const card = (text: string) =>
  Array.from(container.querySelectorAll<HTMLElement>('div')).filter((d) => d.textContent?.includes(text)).pop()!.parentElement!;

beforeEach(() => {
  vi.clearAllMocks();
  readsOn = true;
  useAppStore.setState({ currentUser: { id: 'me' } as any });
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('ArtifactHistory read state', () => {
  it('shows New on comments since the last read, and marks them read once', async () => {
    await mount([MINE, NEW, OLD]);
    expect(card('Which threshold?').textContent).toContain('New');
    expect(card('Looks right').textContent).not.toContain('New');
    expect(card('Mine').textContent).not.toContain('Mark unread');
    expect(set).toHaveBeenCalledTimes(1);
    expect(set).toHaveBeenCalledWith([{ artifact_id: 'art-1' }]);
  });

  it('writes nothing when nothing is new', async () => {
    await mount([MINE, OLD]);
    expect(set).not.toHaveBeenCalled();
  });

  it('Mark unread keeps a comment, and every later one, unread', async () => {
    await mount([NEW, OLD]);
    const button = Array.from(card('Looks right').querySelectorAll('button')).find((b) => b.textContent === 'Mark unread')!;
    await act(async () => {
      button.click();
    });
    await settle();
    expect(set).toHaveBeenLastCalledWith([{ artifact_id: 'art-1', read_at: '2026-11-09T08:59:59.999Z' }]);
    expect(card('Looks right').textContent).toContain('Unread');
    expect(card('Which threshold?').textContent).toContain('Unread');
    expect(set).toHaveBeenCalledTimes(2);
  });

  it('without the feature, neither reads nor writes read state', async () => {
    readsOn = false;
    await mount([NEW]);
    expect(commentReadAPI.list).not.toHaveBeenCalled();
    expect(set).not.toHaveBeenCalled();
    expect(container.textContent).not.toContain('Mark unread');
  });
});
