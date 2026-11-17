import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectSwitcher } from './ProjectLayoutSwitcher';
import { Navbar } from './Navbar';
import { DialogProvider } from './ui';
import { useAppStore } from '../state/store';

// One interface: the project switcher in the top bar lists the workspace's
// projects, then All projects and New project, inside a project and out; and
// a workspace page (Projects, settings, runs, What's new, admin) draws the
// same rail and top bar as a project's pages when the compact layout is on.

const ok = (data: unknown) => Promise.resolve({ data });
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: {
      list: () =>
        ok([
          { id: 'p1', name: 'Brake-by-wire ECU', org_id: 'o1' },
          { id: 'p2', name: 'Steering angle sensor', org_id: 'o1' },
        ]),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
class SilentEventSource {
  addEventListener() {}
  removeEventListener() {}
  close() {}
}
(globalThis as any).EventSource = SilentEventSource;

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async (ui: React.ReactElement, path = '/projects', features: Record<string, boolean> = {}) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1280 });
  useAppStore.setState(
    {
      ...initialStore,
      orgs: [{ id: 'o1', name: 'Desktop Machine Shop', type: 'company', role: 'admin' }],
      activeOrgId: 'o1',
      currentUser: { id: 'u1', name: 'Sam', email: 'sam@example.com' },
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'compact-layout': true, ...features } },
    } as never,
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="*" element={ui} />
          </Routes>
        </MemoryRouter>
      </DialogProvider>
    );
  });
  await flush();
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  document.body.classList.remove('ov-has-rail');
});

const click = async (el: Element | null) => {
  if (!el) throw new Error('nothing to click');
  await act(async () => {
    (el as HTMLElement).click();
  });
  await flush();
};

describe('the project switcher', () => {
  it('lists the projects, then All projects and New project, marking the current one', async () => {
    await mount(<ProjectSwitcher current={{ id: 'p2', name: 'Steering angle sensor' } as never} />, '/projects/p2', {
      'redesign-pages': true,
    });
    const button = container.querySelector('button.ov-topbar-project')!;
    expect(button.textContent).toContain('Steering angle sensor');
    expect(button.getAttribute('aria-expanded')).toBe('false');
    await click(button);
    expect(button.getAttribute('aria-expanded')).toBe('true');
    const links = Array.from(container.querySelectorAll('#ov-project-list a'));
    expect(links.map((a) => a.textContent)).toEqual(['Brake-by-wire ECU', 'Steering angle sensor', 'All projects', '+ New project']);
    expect(links[0].getAttribute('href')).toBe('/projects/p1/home');
    expect(links[1].getAttribute('aria-current')).toBe('true');
    expect(links[2].getAttribute('href')).toBe('/projects');
  });

  it('reads All projects outside a project, and opens a project at its overview without the redesign', async () => {
    await mount(<ProjectSwitcher current={null} />);
    const button = container.querySelector('button.ov-topbar-project')!;
    expect(button.textContent).toContain('All projects');
    await click(button);
    expect(container.querySelector('#ov-project-list a')!.getAttribute('href')).toBe('/projects/p1');
  });

  it('closes on Escape', async () => {
    await mount(<ProjectSwitcher current={null} />);
    await click(container.querySelector('button.ov-topbar-project'));
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(container.querySelector('#ov-project-list')).toBeNull();
  });
});

describe('a workspace page in the compact layout', () => {
  it('draws the rail and the top bar with both switchers', async () => {
    await mount(<Navbar title="Workspace Settings" showWorkspaceControls />, '/org/settings');
    const rail = container.querySelector('aside[aria-label="Workspace navigation"]')!;
    expect(rail).not.toBeNull();
    // Home, then the Agents group and the settings; News is in the account
    // menu and beside the release in the settings, not a rail entry.
    expect(Array.from(rail.querySelectorAll('a')).map((a) => a.getAttribute('href'))).toEqual(['/projects', '/org/agents', '/org/settings']);
    expect(document.body.classList.contains('ov-has-rail')).toBe(true);
    const bar = container.querySelector('header.ov-topbar')!;
    expect(bar.querySelector('button[title="Switch workspace"]')).not.toBeNull();
    expect(bar.querySelector('button.ov-topbar-project')!.textContent).toContain('All projects');
    expect(bar.querySelector('[aria-current="page"]')!.textContent).toBe('Workspace Settings');
    expect(bar.querySelector('button[aria-label="Help"]')).not.toBeNull();
  });

  it('keeps the old bar without the compact layout', async () => {
    await mount(<Navbar title="Workspace Settings" showWorkspaceControls />, '/org/settings');
    act(() => root.unmount());
    container.remove();
    useAppStore.setState({ features: { channel: 'nightly', stable_release: '', preview: false, features: {} } } as never);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    await act(async () => {
      root.render(
        <DialogProvider>
          <MemoryRouter initialEntries={['/org/settings']}>
            <Navbar title="Workspace Settings" showWorkspaceControls />
          </MemoryRouter>
        </DialogProvider>
      );
    });
    expect(container.querySelector('aside[aria-label="Workspace navigation"]')).toBeNull();
    expect(container.querySelector('img[alt="OpenV Logo"]')).not.toBeNull();
  });
});
