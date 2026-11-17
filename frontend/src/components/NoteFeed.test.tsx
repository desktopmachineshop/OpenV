import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { chatterAPI } from '../api/client';
import { ArtifactHistory } from './ArtifactHistory';
import { WorkItemHistory } from './kanban/WorkItemHistory';
import { filterHistory } from './NoteFeed';

// An artifact's History and a task's are one feed: the same filter, the
// same entries, and in a detail view a card of their own.
vi.mock('../api/client', async (orig) => mockApi(await orig(), {}));
vi.mock('../hooks/useFeature', () => ({ useFeature: () => false }));
vi.mock('./NoteTodo', () => ({ NoteTodoChip: () => null, AddTodoControl: () => null }));
vi.mock('./NoteComposer', () => ({ NoteComposer: () => <textarea aria-label="Add a note" />, postNote: vi.fn() }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
const render = async (node: React.ReactNode) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(node);
  });
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
};
const press = (label: string) =>
  act(async () => {
    Array.from(container.querySelectorAll('button')).find((b) => b.textContent === label)!.click();
  });
const kinds = () => Array.from(container.querySelectorAll('[data-history-entry]')).map((e) => e.getAttribute('data-history-entry'));

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const activity = [
  { id: 'a1', work_item_id: 'w1', kind: 'comment', actor: 'priya', content: 'Is the jig ready?', payload: {}, created_at: '2026-11-09T10:00:00Z' },
  { id: 'a2', work_item_id: 'w1', kind: 'moved', actor: 'sam', content: 'todo → doing', payload: {}, created_at: '2026-11-09T09:00:00Z' },
];

describe('the History feed', () => {
  it('filters by who wrote the entry', async () => {
    await render(null);
    const auto = (n: number) => n % 2 === 0;
    expect(filterHistory([1, 2, 3, 4], 'all', auto)).toEqual([1, 2, 3, 4]);
    expect(filterHistory([1, 2, 3, 4], 'changes', auto)).toEqual([2, 4]);
    expect(filterHistory([1, 2, 3, 4], 'comments', auto)).toEqual([1, 3]);
  });

  it('draws a task’s activity as History, in a card of its own in the full view, and filters it', async () => {
    const onComment = vi.fn();
    await render(<WorkItemHistory activity={activity} onComment={onComment} variant="card" />);
    const section = container.querySelector('section[aria-label="History"]')!;
    expect(section.className).toContain('card');
    expect(kinds()).toEqual(['comment', 'change']);
    await press('Comments');
    expect(kinds()).toEqual(['comment']);
    await press('Changes');
    expect(kinds()).toEqual(['change']);
    expect(section.textContent).toContain('moved');

    // A comment written while reading the changes is shown once it is saved.
    const box = container.querySelector('textarea')!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(box, ' On Friday ');
      box.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await press('Comment');
    expect(onComment).toHaveBeenCalledWith('On Friday');
    expect(kinds()).toEqual(['comment']);
  });

  it('is a section in the drawer, not a card', async () => {
    await render(<WorkItemHistory activity={[]} onComment={vi.fn()} variant="section" />);
    expect(container.querySelector('section[aria-label="History"]')!.className).not.toContain('card ');
    expect(container.textContent).toContain('No activity yet.');
  });

  it('draws an artifact’s History with the same parts, as a card where asked', async () => {
    vi.mocked(chatterAPI.list, { partial: true, deep: true }).mockResolvedValue({
      data: [
        { id: 'c1', artifact_id: 'art-1', message: 'Which threshold?', is_auto_entry: false, entry_type: 'comment', created_by: 'priya', author_name: 'priya', created_at: '2026-11-09T10:00:00Z', updated_at: '2026-11-09T10:00:00Z' },
        { id: 'c2', artifact_id: 'art-1', message: 'Status changed', is_auto_entry: true, entry_type: 'status', created_by: '', author_name: '', created_at: '2026-11-09T09:00:00Z', updated_at: '2026-11-09T09:00:00Z' },
      ],
    } as any);
    await render(<ArtifactHistory artifactId="art-1" projectId="p1" variant="card" />);
    const section = container.querySelector('section[aria-label="History"]')!;
    expect(section.getAttribute('data-history')).toBe('card');
    expect(section.className).toContain('card');
    // Comments are what an artifact's History opens on.
    expect(kinds()).toEqual(['comment']);
    await press('All');
    expect(kinds()).toEqual(['comment', 'change']);
  });
});
