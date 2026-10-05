import { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { ChatterPanel } from './ChatterPanel';
import { resolveAssistantSessionId } from './wizard/assistantSession';

// The notes panel's assistant tab follows the project it is in (#379 bug
// 218). With no artifact selected, ModuleView keys the panel 'chatter-none'
// whatever the project, and the project's routed outlet is not keyed either,
// so going from one project's requirements to another's (a notification
// about another project does exactly that) keeps this panel mounted and
// only changes its projectId.

vi.mock('../api/client', async (orig) => mockApi(await orig()));

// The assistant panel stands in as the session it was given; each project's
// conversation is "gs-<project>".
vi.mock('./wizard/GuidedChatPanel', () => ({
  GuidedChatPanel: ({ sessionId }: { sessionId: string }) =>
    require('react').createElement('div', { 'data-testid': 'assistant-session' }, sessionId),
}));
vi.mock('./wizard/assistantSession', () => ({
  resolveAssistantSessionId: vi.fn(async (projectId: string) => `gs-${projectId}`),
}));
vi.mock('./wizard/applySuggestion', () => ({
  ASSISTANT_EDITS_FEATURE: 'assistant-project-edits',
  GATED_REASON: 'gated',
  applySuggestionsToProject: async () => [],
  isProjectEditKind: () => false,
}));
vi.mock('../hooks/useFeature', () => ({ useFeature: () => false }));
vi.mock('./NoteTodo', () => ({ NoteTodoChip: () => null, AddTodoControl: () => null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const resolve = vi.mocked(resolveAssistantSessionId);

describe('ChatterPanel assistant across projects (#379 bug 218)', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    resolve.mockClear();
    container = document.createElement('div');
    document.body.appendChild(container);
    act(() => {
      root = createRoot(container);
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  // Nothing selected, so the panel opens on the assistant tab.
  const show = async (projectId: string) => {
    await act(async () => {
      root.render(<ChatterPanel projectId={projectId} isOpen onToggle={() => {}} />);
    });
    await act(async () => {
      await Promise.resolve();
    });
  };

  const session = () => container.querySelector('[data-testid="assistant-session"]')?.textContent ?? null;

  it("opens the new project's conversation when the panel moves to another project", async () => {
    await show('p1');
    expect(session()).toBe('gs-p1');

    await show('p2');
    expect(resolve.mock.calls.map(([projectId]) => projectId)).toEqual(['p1', 'p2']);
    expect(session()).toBe('gs-p2');
  });
});
