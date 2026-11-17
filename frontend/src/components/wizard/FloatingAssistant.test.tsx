import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useNavigate } from 'react-router-dom';
import { mockApi } from '../../test/mockApi';
import { FloatingAssistant } from './FloatingAssistant';
import { openAssistant, useAssistantPageContext, useAssistantStore } from './assistantStore';
import { guidedAPI } from '../../api/client';
import { useAppStore } from '../../state/store';

// The floating V&V Assistant (#726): one button on every project page and the
// workspace Home, a panel that keeps its conversation across navigation,
// says what the reader is looking at, explains when it cannot run, and starts
// a turn another page asks for.

vi.mock('../../api/client', async (orig) => mockApi(await orig()));

let featureOn = true;
vi.mock('../../hooks/useFeature', () => ({ useFeature: () => featureOn }));

// The chat stands in as the props it was given, and reports a request sent.
vi.mock('./GuidedChatPanel', () => ({
  GuidedChatPanel: (p: any) => {
    const R = require('react');
    R.useEffect(() => {
      if (p.autoSend) p.onAutoSent?.(p.autoSend.id);
    }, [p.autoSend]);
    return R.createElement(
      'div',
      { 'data-testid': 'chat', 'data-session': p.sessionId, 'data-artifact': p.artifactId || '' },
      R.createElement('textarea', { 'aria-label': 'message' }),
      p.autoSend ? R.createElement('span', { 'data-testid': 'auto' }, p.autoSend.message) : null
    );
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const api = vi.mocked(guidedAPI);
let container: HTMLDivElement;
let root: Root;
let go: (path: string) => void = () => {};

const Nav: React.FC = () => {
  const navigate = useNavigate();
  go = (path) => navigate(path);
  return null;
};

const Page: React.FC<{ label?: string; artifactId?: string }> = ({ label, artifactId }) => {
  useAssistantPageContext(label ? { label, artifactId } : undefined);
  return null;
};

const render = async (path: string, page?: React.ReactNode) => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[path]}>
        <Nav />
        {page}
        <FloatingAssistant />
      </MemoryRouter>
    );
  });
  await act(async () => {
    await Promise.resolve();
  });
};

const flush = () =>
  act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });

const panel = () => container.querySelector('[data-testid="floating-assistant"]') as HTMLElement | null;
const launcher = () => container.querySelector('button[aria-label="Open the V&V Assistant"]') as HTMLButtonElement | null;
const context = () => container.querySelector('[data-testid="assistant-context"]')?.textContent ?? null;

beforeEach(() => {
  featureOn = true;
  vi.clearAllMocks();
  useAssistantStore.setState({ panel: 'closed', page: {}, override: null, dismissed: null, request: null });
  useAppStore.setState({ projects: [] });
  api.list.mockResolvedValue({ data: [{ id: 'gs1', status: 'in-progress', created_at: '2026-01-01' }] } as any);
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

describe('FloatingAssistant', () => {
  it('draws nothing while its gate is closed', async () => {
    featureOn = false;
    await render('/projects/p1/vv');
    expect(launcher()).toBeNull();
    expect(panel()).toBeNull();
  });

  it('stays off the guided wizard, which has its own assistant', async () => {
    await render('/projects/p1/guided');
    expect(launcher()).toBeNull();
  });

  it('opens from the button, names the page, and minimises on Esc back to the button', async () => {
    await render('/projects/p1/vv');
    expect(panel()!.hidden).toBe(true);
    await act(async () => launcher()!.click());
    await flush();
    expect(panel()!.hidden).toBe(false);
    expect(api.list).toHaveBeenCalledWith('p1');
    expect(container.querySelector('[data-testid="chat"]')!.getAttribute('data-session')).toBe('gs1');
    expect(context()).toContain('Looking at: V&V');

    await act(async () => {
      panel()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(panel()!.hidden).toBe(true);
    expect(launcher()).not.toBeNull();
    // Minimised, the conversation stays mounted.
    expect(container.querySelector('[data-testid="chat"]')).not.toBeNull();
  });

  it('opens on Alt+A', async () => {
    await render('/projects/p1');
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyA', key: 'a', altKey: true }));
    });
    expect(panel()!.hidden).toBe(false);
  });

  it("sends the page's artifact, and lets the reader remove the context", async () => {
    await render('/projects/p1/requirements', <Page label="REQ-73 Change impact analysis" artifactId="a73" />);
    await act(async () => openAssistant());
    await flush();
    expect(context()).toContain('Looking at: REQ-73 Change impact analysis');
    expect(container.querySelector('[data-testid="chat"]')!.getAttribute('data-artifact')).toBe('a73');

    await act(async () => {
      (container.querySelector('button[aria-label^="Stop sending"]') as HTMLButtonElement).click();
    });
    expect(context()).toBeNull();
    expect(container.querySelector('[data-testid="chat"]')!.getAttribute('data-artifact')).toBe('');
  });

  it('keeps one conversation while the reader moves between pages of the project', async () => {
    await render('/projects/p1/vv');
    await act(async () => openAssistant());
    await flush();
    await act(async () => go('/projects/p1/board'));
    await flush();
    expect(api.list).toHaveBeenCalledTimes(1);
    expect(panel()!.hidden).toBe(false);
    expect(context()).toContain('Looking at: Board');
  });

  it('starts the turn another page asked for, once', async () => {
    await render('/projects/p1/vv');
    await act(async () => openAssistant({ message: 'Drafting test cases for 1 gap: REQ-1.', context: { label: 'REQ-1 (V&V gap)' } }));
    await flush();
    expect(context()).toContain('REQ-1 (V&V gap)');
    expect(useAssistantStore.getState().request).toBeNull();
  });

  it('says why when the conversation cannot be opened', async () => {
    api.list.mockResolvedValue({ data: [] } as any);
    api.start.mockRejectedValue({ response: { status: 403, data: { error: 'forbidden' } } });
    await render('/projects/p1/vv');
    await act(async () => openAssistant());
    await flush();
    expect(panel()!.textContent).toContain('needs editor access');
  });

  it('on the workspace Home, uses the project chosen in the panel', async () => {
    useAppStore.setState({ projects: [{ id: 'p7', name: 'Rover' } as any, { id: 'p8', name: 'Lander' } as any] });
    await render('/projects');
    await act(async () => openAssistant());
    await flush();
    expect(context()).toContain('Looking at: Home');
    expect(api.list).toHaveBeenCalledWith('p7');
    const select = container.querySelector('select') as HTMLSelectElement;
    await act(async () => {
      select.value = 'p8';
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(api.list).toHaveBeenLastCalledWith('p8');
  });
});
