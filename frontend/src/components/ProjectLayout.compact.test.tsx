import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectLayout } from './ProjectLayout';
import { DialogProvider } from './ui';
import { useAppStore } from '../state/store';

// The compact layout (compact-layout feature key): on a wide screen the icon
// rail, whose group buttons open a flyout of their pages, and the top bar
// with the help button; on a phone the tab bar along the bottom, with More
// opening the project menu; ? and F1 toggling help on both. Without the key
// the sidebar layout is drawn as before (ProjectLayout.nav.test.tsx).

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: { get: () => ok({ id: 'p1', name: 'Pump controller', org_id: 'o1' }) },
    guidedAPI: { list: () => ok([]) },
  })
);

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
const initialWidth = window.innerWidth;

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async (path: string, opts: { compact?: boolean; width?: number } = {}) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: opts.width ?? 1280 });
  useAppStore.setState(
    {
      ...initialStore,
      features: {
        channel: 'nightly' as const,
        stable_release: '',
        preview: false,
        features: { 'compact-layout': opts.compact ?? true },
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
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/projects/:projectId" element={<ProjectLayout />}>
              <Route path="*" element={<div>page</div>} />
            </Route>
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
  localStorage.clear();
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: initialWidth });
});

const rail = () => document.querySelector('aside[aria-label="Project navigation"]') as HTMLElement;
const button = (scope: ParentNode, name: string) =>
  Array.from(scope.querySelectorAll('button')).find((b) => (b.textContent ?? '').trim() === name) as
    | HTMLButtonElement
    | undefined;
const links = (scope: ParentNode) =>
  Array.from(scope.querySelectorAll('a')).map((a) => `${(a.textContent ?? '').trim()} -> ${a.getAttribute('href')}`);
const press = async (key: string, target: EventTarget = window) => {
  await act(async () => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));
  });
};
const helpButton = () => document.querySelector('button[aria-label="Help"]') as HTMLButtonElement;

describe('compact layout on a wide screen', () => {
  it('draws the rail with one button per group and the tail as links', async () => {
    await mount('/projects/p1/requirements');
    const r = rail();
    expect(r.className).toBe('ov-rail');
    const groups = Array.from(r.querySelectorAll('nav > button')).map((b) => [
      (b.textContent ?? '').trim(),
      b.getAttribute('aria-expanded'),
      b.className.includes('is-current'),
    ]);
    expect(groups).toEqual([
      ['Define', 'false', true],
      ['Verify', 'false', false],
      ['Plan', 'false', false],
      ['Agents', 'false', false],
    ]);
    expect(links(r)).toEqual([
      'Projects -> /projects',
      'Activity -> /projects/p1/activity',
      'Settings -> /projects/p1/settings',
    ]);
    expect(r.querySelector('nav')?.getAttribute('data-help-key')).toBe('start.navigation');
  });

  it("opens a group's pages in a flyout, and closes it on Escape", async () => {
    await mount('/projects/p1/requirements');
    await act(async () => button(rail(), 'Define')!.click());
    const flyout = document.getElementById('ov-rail-flyout') as HTMLElement;
    expect(flyout.getAttribute('aria-label')).toBe('Define pages');
    expect(links(flyout)).toEqual([
      'Overview -> /projects/p1',
      'Guided Definition -> /projects/p1/guided',
      'Requirements -> /projects/p1/requirements',
      'Interviews -> /projects/p1/interviews',
    ]);
    expect(button(rail(), 'Define')!.getAttribute('aria-expanded')).toBe('true');
    await press('Escape');
    expect(document.getElementById('ov-rail-flyout')).toBeNull();
  });

  it('closes the flyout on arrival at a page', async () => {
    await mount('/projects/p1');
    await act(async () => button(rail(), 'Verify')!.click());
    const matrix = Array.from(document.querySelectorAll('#ov-rail-flyout a')).find(
      (a) => a.textContent?.trim() === 'Traceability'
    ) as HTMLAnchorElement;
    await act(async () => matrix.click());
    await flush();
    expect(document.getElementById('ov-rail-flyout')).toBeNull();
    expect(document.querySelector('.ov-topbar-page')?.textContent).toBe('Traceability');
  });

  it('names the project and the page in the top bar, and marks the search for help', async () => {
    await mount('/projects/p1/vv');
    expect(document.querySelector('.ov-topbar-project')?.textContent).toBe('Pump controller');
    expect(document.querySelector('.ov-topbar-page')?.textContent).toBe('V&V');
    expect(document.querySelector('.ov-topbar-search')?.getAttribute('data-help-key')).toBe('search');
    expect(helpButton().hasAttribute('data-help-ui')).toBe(true);
    expect(helpButton().getAttribute('aria-keyshortcuts')).toBe('? F1');
  });

  it('opens and closes help with the button, ? and F1, but not ? typed in a field', async () => {
    await mount('/projects/p1');
    const open = () => helpButton().getAttribute('aria-expanded');
    expect(open()).toBe('false');
    await act(async () => helpButton().click());
    expect(open()).toBe('true');
    await press('?');
    expect(open()).toBe('false');
    await press('F1');
    expect(open()).toBe('true');
    await press('F1');
    const field = document.createElement('input');
    container.appendChild(field);
    await press('?', field);
    expect(open()).toBe('false');
  });
});

describe('compact layout on a phone', () => {
  it('adds the tab bar, whose More opens the project menu', async () => {
    await mount('/projects/p1/vv', { width: 390 });
    const tabs = document.querySelector('nav[aria-label="Project tabs"]') as HTMLElement;
    expect(links(tabs)).toEqual([
      'Overview -> /projects/p1',
      'Reqs -> /projects/p1/requirements',
      'V&V -> /projects/p1/vv',
      'Review -> /projects/p1/review',
    ]);
    expect(tabs.querySelector('a.active')?.textContent).toBe('V&V');
    // Floating buttons at the foot of a page read this to sit above the bar.
    expect(document.documentElement.style.getPropertyValue('--ov-tabbar-height')).toBe('56px');
    const more = button(tabs, 'More')!;
    expect(more.getAttribute('aria-expanded')).toBe('false');
    await act(async () => more.click());
    expect(more.getAttribute('aria-expanded')).toBe('true');
    expect(document.querySelector('button[aria-label="Project menu"]')?.getAttribute('aria-expanded')).toBe(
      'true'
    );
  });
});

describe('the More page', () => {
  it('lists every page by its menu name, with the way back to all projects', async () => {
    await mount('/projects/p1/vv', { width: 390 });
    const tabs = document.querySelector('nav[aria-label="Project tabs"]') as HTMLElement;
    await act(async () => button(tabs, 'More')!.click());
    const aside = document.querySelector('aside[aria-label="Project navigation"]') as HTMLElement;
    expect(aside.querySelector('.ov-more-title')?.textContent).toBe('More');
    expect(aside.textContent).toContain('All projects');
    expect(aside.querySelector('a.ov-more-row.active')?.textContent).toBe('V&V');
    expect(aside.querySelector('button[aria-label="Close menu"]')).toBeTruthy();
    expect(aside.querySelector('button[aria-label^="Account menu"]')).toBeTruthy();
  });
});

describe('the phone top bar', () => {
  it('names the project small above the page being read', async () => {
    await mount('/projects/p1/vv', { width: 390 });
    expect(document.querySelector('.ov-phone-title-project')?.textContent).toBe('Pump controller');
    expect(document.querySelector('.ov-phone-title-page')?.textContent).toBe('V&V');
  });

  it('shows the project alone on a page the menu does not list', async () => {
    await mount('/projects/p1/nowhere', { width: 390 });
    expect(document.querySelector('.ov-phone-title-project')).toBeNull();
    expect(document.querySelector('.ov-phone-title-page')?.textContent).toBe('Pump controller');
  });
});

describe('without the compact-layout key', () => {
  it('keeps the sidebar layout, with no rail, tab bar or help shortcut', async () => {
    await mount('/projects/p1', { compact: false });
    expect(document.querySelector('.ov-rail')).toBeNull();
    expect(document.querySelector('.ov-topbar')).toBeNull();
    await press('?');
    expect(helpButton().getAttribute('aria-expanded')).toBe('false');
  });

  it('keeps the phone without a tab bar', async () => {
    await mount('/projects/p1', { compact: false, width: 390 });
    expect(document.querySelector('nav[aria-label="Project tabs"]')).toBeNull();
  });
});
