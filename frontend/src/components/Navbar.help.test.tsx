import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { Navbar } from './Navbar';
import { DialogProvider } from './ui';
import { useAppStore } from '../state/store';

// The workspace pages' bar (workspace settings, workspace runs, platform
// admin, What's new) brings its own help where the compact layout is on: a ?
// beside the bell, the help panel, and ? / F1 to open it. A page that passes
// its own help (the projects list) keeps it, and without the key nothing is
// added.

vi.mock('../api/client', async (orig) => mockApi(await orig(), {}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

class SilentEventSource {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  addEventListener() {}
  removeEventListener() {}
  close() {}
}
(globalThis as any).EventSource = SilentEventSource;

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const mount = async (element: React.ReactElement, compact: boolean, path = '/org/settings') => {
  useAppStore.setState(
    {
      ...initialStore,
      features: {
        channel: 'nightly' as const,
        stable_release: '',
        preview: false,
        features: { 'compact-layout': compact },
      },
    } as never,
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter initialEntries={[path]}>{element}</MemoryRouter>
      </DialogProvider>
    );
  });
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const helpButtons = () => Array.from(document.querySelectorAll('button[aria-label="Help"]'));
const press = async (key: string) => {
  await act(async () => {
    window.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));
  });
};

describe('Navbar help on the workspace pages', () => {
  it('adds a ? button and the help panel, which ? and F1 toggle', async () => {
    await mount(<Navbar title="Workspace Settings" showWorkspaceControls />, true);
    const [button] = helpButtons();
    expect(button).toBeTruthy();
    expect(button.getAttribute('aria-keyshortcuts')).toBe('? F1');
    expect(button.hasAttribute('data-help-ui')).toBe(true);
    expect(button.getAttribute('aria-expanded')).toBe('false');
    await act(async () => (button as HTMLButtonElement).click());
    expect(button.getAttribute('aria-expanded')).toBe('true');
    expect(document.querySelector('.help-sidebar h3')?.textContent).toBe('Workspace settings');
    await press('?');
    expect(button.getAttribute('aria-expanded')).toBe('false');
    await press('F1');
    expect(button.getAttribute('aria-expanded')).toBe('true');
  });

  it('keeps the help a page passes in, without a second panel', async () => {
    const toggle = vi.fn();
    await mount(<Navbar showWorkspaceControls helpOpen={false} onHelpToggle={toggle} />, true, '/projects');
    expect(helpButtons()).toHaveLength(1);
    expect(document.querySelector('.help-sidebar')).toBeNull();
    await act(async () => (helpButtons()[0] as HTMLButtonElement).click());
    expect(toggle).toHaveBeenCalledTimes(1);
  });

  it('adds nothing without the compact-layout key', async () => {
    await mount(<Navbar title="Workspace Settings" showWorkspaceControls />, false);
    expect(helpButtons()).toHaveLength(0);
    await press('F1');
    expect(document.querySelector('.help-sidebar')).toBeNull();
  });
});
