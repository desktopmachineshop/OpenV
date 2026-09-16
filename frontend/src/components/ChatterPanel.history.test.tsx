import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ChatterPanel } from './ChatterPanel';
import { chatterAPI } from '../api/client';

vi.mock('../api/client', () => ({
  chatterAPI: { list: vi.fn(), create: vi.fn() },
  artifactAPI: { list: vi.fn() },
}));

// The assistant tab and the to-do controls are not what these tests are
// about; stubbing them keeps the panel to its history.
vi.mock('./wizard/GuidedChatPanel', () => ({ GuidedChatPanel: () => null }));
vi.mock('./wizard/assistantSession', () => ({ resolveAssistantSessionId: async () => '' }));
vi.mock('./wizard/applySuggestion', () => ({
  ASSISTANT_EDITS_FEATURE: 'assistant-project-edits',
  GATED_REASON: 'gated',
  applySuggestionsToProject: async () => [],
  isProjectEditKind: () => false,
}));
vi.mock('../hooks/useFeature', () => ({ useFeature: () => false }));
vi.mock('./NoteTodo', () => ({ NoteTodoChip: () => null, AddTodoControl: () => null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const list = vi.mocked(chatterAPI.list, { partial: true, deep: true });
const create = vi.mocked(chatterAPI.create, { partial: true, deep: true });

const entry = (id: string, message: string, isAuto: boolean) => ({
  id,
  artifact_id: 'art-1',
  message,
  is_auto_entry: isAuto,
  entry_type: isAuto ? 'version-change' : 'comment',
  author_name: isAuto ? 'System' : 'Dana',
  created_at: '2026-09-16T10:00:00Z',
  updated_at: '2026-09-16T10:00:00Z',
});

const CHANGE = entry('e1', 'Version 2 saved', true);
const COMMENT = entry('e2', 'Checked against the datasheet', false);

describe('ChatterPanel history', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    list.mockReset();
    create.mockReset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const mount = async (entries: unknown[]) => {
    list.mockResolvedValue({ data: entries } as any);
    await act(async () => {
      root.render(
        <ChatterPanel artifactId="art-1" projectId="proj-1" isOpen onToggle={() => {}} />
      );
    });
    await act(async () => {
      await Promise.resolve();
    });
  };

  const click = async (name: string) => {
    const button = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent === name
    );
    if (!button) throw new Error(`no button labelled ${name}`);
    await act(async () => {
      button.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
  };

  it('calls the tab History, not Comments', async () => {
    await mount([CHANGE, COMMENT]);
    const tabs = Array.from(container.querySelectorAll('button')).map((b) => b.textContent);
    expect(tabs).toContain('History');
  });

  it('opens on the comments, not the recorded changes', async () => {
    await mount([CHANGE, COMMENT]);
    expect(container.textContent).toContain('Checked against the datasheet');
    expect(container.textContent).not.toContain('Version 2 saved');
  });

  it('offers the filters in the order comments, changes, all', async () => {
    await mount([CHANGE, COMMENT]);
    const group = container.querySelector('[aria-label="Filter history"]') as HTMLElement;
    const labels = Array.from(group.querySelectorAll('button')).map((b) => b.textContent);
    expect(labels).toEqual(['Comments', 'Changes', 'All']);
  });

  it('narrows to changes', async () => {
    await mount([CHANGE, COMMENT]);
    await click('Changes');
    expect(container.textContent).toContain('Version 2 saved');
    expect(container.textContent).not.toContain('Checked against the datasheet');
  });

  it('widens to everything', async () => {
    await mount([CHANGE, COMMENT]);
    await click('All');
    expect(container.textContent).toContain('Version 2 saved');
    expect(container.textContent).toContain('Checked against the datasheet');
  });

  it('says why a filtered list is empty rather than showing nothing', async () => {
    await mount([CHANGE]);
    expect(container.textContent).toContain('No comments yet');
  });

  it('says so when the artifact has no history at all', async () => {
    await mount([]);
    await click('All');
    expect(container.textContent).toContain('Nothing yet');
  });

  it('does not hide a comment written while reading the changes', async () => {
    await mount([CHANGE]);
    await click('Changes');
    create.mockResolvedValue({ data: entry('e3', 'Raised with the supplier', false) } as any);

    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    const setValue = Object.getOwnPropertyDescriptor(
      window.HTMLTextAreaElement.prototype,
      'value'
    )!.set!;
    await act(async () => {
      setValue.call(textarea, 'Raised with the supplier');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await click('Add Note');
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain('Raised with the supplier');
  });
});
