import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ChatterPanel } from './ChatterPanel';
import {
  artifactAPI,
  attachmentAPI,
  chatterAPI,
  linkAPI,
  membersAPI,
  workItemsAPI,
} from '../api/client';

vi.mock('../api/client', () => ({
  chatterAPI: { list: vi.fn(), create: vi.fn() },
  artifactAPI: { list: vi.fn() },
  attachmentAPI: { listByProject: vi.fn() },
  linkAPI: { listForArtifact: vi.fn() },
  membersAPI: { list: vi.fn() },
  workItemsAPI: { create: vi.fn() },
}));

vi.mock('./wizard/GuidedChatPanel', () => ({ GuidedChatPanel: () => null }));
vi.mock('./wizard/assistantSession', () => ({ resolveAssistantSessionId: async () => '' }));
vi.mock('./wizard/applySuggestion', () => ({
  ASSISTANT_EDITS_FEATURE: 'assistant-project-edits',
  GATED_REASON: 'gated',
  applySuggestionsToProject: async () => [],
  isProjectEditKind: () => false,
}));
vi.mock('./NoteTodo', async () => {
  const actual = await vi.importActual<any>('./NoteTodo');
  return { ...actual, NoteTodoChip: () => null, AddTodoControl: () => null };
});

let taggingOn = true;
vi.mock('../hooks/useFeature', () => ({ useFeature: (key: string) => key === 'note-tagging' && taggingOn }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const list = vi.mocked(chatterAPI.list, { partial: true, deep: true });
const create = vi.mocked(chatterAPI.create, { partial: true, deep: true });
const listMembers = vi.mocked(membersAPI.list, { partial: true, deep: true });
const listArtifacts = vi.mocked(artifactAPI.list, { partial: true, deep: true });
const listLinks = vi.mocked(linkAPI.listForArtifact, { partial: true, deep: true });
const listAttachments = vi.mocked(attachmentAPI.listByProject, { partial: true, deep: true });
const createWorkItem = vi.mocked(workItemsAPI.create, { partial: true, deep: true });

const MEMBERS = [
  { project_id: 'p1', user_id: 'u1', role: 'editor', user_name: 'Dana Okoro', user_email: 'dana@example.com' },
  { project_id: 'p1', user_id: 'u2', role: 'editor', user_name: 'Sam Reeve', user_email: 'sam@example.com' },
];
const ARTIFACTS = [
  { id: 'art-1', ref: 'REQ-12', title: 'Seal integrity', type: 'requirement' },
  { id: 'art-2', ref: 'REQ-99', title: 'Hydraulic schedule', type: 'requirement' },
];

describe('ChatterPanel tagging', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    taggingOn = true;
    [list, create, listMembers, listArtifacts, listLinks, listAttachments, createWorkItem].forEach(
      (m) => m.mockReset()
    );
    list.mockResolvedValue({ data: [] } as any);
    listMembers.mockResolvedValue({ data: MEMBERS } as any);
    listArtifacts.mockResolvedValue({ data: ARTIFACTS } as any);
    listLinks.mockResolvedValue({ data: [] } as any);
    listAttachments.mockResolvedValue({ data: [] } as any);
    createWorkItem.mockResolvedValue({ data: { id: 'w1' } } as any);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const mount = async () => {
    await act(async () => {
      root.render(
        <ChatterPanel artifactId="art-1" projectId="p1" isOpen onToggle={() => {}} />
      );
    });
    await act(async () => {
      await Promise.resolve();
    });
  };

  const setValue = Object.getOwnPropertyDescriptor(
    window.HTMLTextAreaElement.prototype,
    'value'
  )!.set!;

  const type = async (text: string) => {
    const area = container.querySelector('textarea') as HTMLTextAreaElement;
    await act(async () => {
      setValue.call(area, text);
      area.dispatchEvent(new Event('input', { bubbles: true }));
    });
    // The menus fetch their contents the first time they open.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    return area;
  };

  const menu = (label: string) =>
    container.querySelector(`[aria-label="${label}"]`) as HTMLElement | null;

  const click = async (name: string) => {
    const button = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent === name
    )!;
    await act(async () => {
      button.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
  };

  it('offers the project members on @', async () => {
    await mount();
    await type('ask @');
    expect(menu('People suggestions')?.textContent).toContain('@danaokoro');
    expect(menu('People suggestions')?.textContent).toContain('Sam Reeve');
  });

  it('narrows the people as the name is typed', async () => {
    await mount();
    await type('ask @sam');
    const text = menu('People suggestions')!.textContent || '';
    expect(text).toContain('Sam Reeve');
    expect(text).not.toContain('Dana Okoro');
  });

  it('says a doubled @@ will raise a to-do', async () => {
    await mount();
    await type('@@dana');
    expect(menu('People suggestions')?.textContent).toContain('raises a to-do');
  });

  it('writes the handle the server will resolve', async () => {
    await mount();
    const area = await type('ask @dan');
    const row = menu('People suggestions')!.querySelector('[role="option"]') as HTMLElement;
    await act(async () => {
      row.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
    });
    expect(area.value).toBe('ask @danaokoro ');
  });

  it('offers references on #', async () => {
    await mount();
    await type('see ##REQ');
    expect(menu('Reference suggestions')?.textContent).toContain('REQ-99');
  });

  it('raises a to-do for the person a note names with @@', async () => {
    create.mockResolvedValue({
      data: { id: 'e1', artifact_id: 'art-1', message: '@@samreeve please check the seal' },
    } as any);
    await mount();
    await type('@@samreeve please check the seal');
    await click('Add Note');
    await act(async () => {
      await Promise.resolve();
    });
    expect(createWorkItem).toHaveBeenCalledTimes(1);
    const [projectId, payload] = createWorkItem.mock.calls[0] as any[];
    expect(projectId).toBe('p1');
    expect(payload).toMatchObject({
      assignee_id: 'u2',
      artifact_ids: ['art-1'],
      source_chatter_id: 'e1',
      title: '@@samreeve please check the seal',
    });
  });

  it('raises no to-do for a plain @ mention', async () => {
    create.mockResolvedValue({
      data: { id: 'e1', artifact_id: 'art-1', message: 'thanks @samreeve' },
    } as any);
    await mount();
    await type('thanks @samreeve');
    await click('Add Note');
    await act(async () => {
      await Promise.resolve();
    });
    expect(createWorkItem).not.toHaveBeenCalled();
  });

  it('keeps the note when the to-do cannot be raised', async () => {
    create.mockResolvedValue({
      data: { id: 'e1', artifact_id: 'art-1', message: '@@samreeve look' },
    } as any);
    createWorkItem.mockRejectedValue(new Error('board is full'));
    await mount();
    await type('@@samreeve look');
    await click('Add Note');
    await act(async () => {
      await Promise.resolve();
    });
    expect(create).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain('The note was posted');
  });

  it('offers nothing to a workspace without the feature', async () => {
    taggingOn = false;
    await mount();
    await type('ask @');
    expect(menu('People suggestions')).toBeNull();
    expect(listMembers).not.toHaveBeenCalled();
  });
});
