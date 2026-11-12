import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectLayout } from './ProjectLayout';
import { DialogProvider } from './ui';
import { useAppStore } from '../state/store';

// The refreshed shell (ui-refresh, on top of compact-layout): the same
// groups in the same order on both sizes, a row of the current group's pages
// that names the page, and on a phone a bar of Home and the groups with More
// as a page of tiles. Without the key the compact layout is drawn as before
// (ProjectLayout.compact.test.tsx).

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

const mount = async (path: string, opts: { refresh?: boolean; compact?: boolean; width?: number } = {}) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: opts.width ?? 1280 });
  useAppStore.setState(
    {
      ...initialStore,
      features: {
        channel: 'nightly' as const,
        stable_release: '',
        preview: false,
        features: { 'compact-layout': opts.compact ?? true, 'ui-refresh': opts.refresh ?? true, 'redesign-pages': true },
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
              <Route index element={<div>page</div>} />
            </Route>
            <Route path="/projects" element={<div>workspace home</div>} />
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

const text = (el: Element) => (el.textContent ?? '').trim();
const groupRow = () => document.querySelector('nav.ov-grouptabs') as HTMLElement | null;
const rowLinks = () => Array.from(groupRow()?.querySelectorAll('a') ?? []).map((a) => `${text(a)}${a.classList.contains('active') ? '*' : ''}`);
const bar = () => document.querySelector('nav[aria-label="Project tabs"]') as HTMLElement;
const barItems = () => Array.from(bar().children).map((el) => `${text(el)}${el.classList.contains('active') ? '*' : ''}`);
const click = async (el: Element | null | undefined) => {
  await act(async () => {
    (el as HTMLElement).click();
  });
  await flush();
};

describe('the refreshed shell on a wide screen', () => {
  it('orders the rail Plan, Define, Verify, Agents', async () => {
    await mount('/projects/p1/requirements');
    const groups = Array.from(document.querySelectorAll('aside.ov-rail nav > button')).map(text);
    expect(groups).toEqual(['Plan', 'Define', 'Verify', 'Agents']);
  });

  it("lists the current group's pages in a row and lights the one in view", async () => {
    await mount('/projects/p1/requirements');
    expect(groupRow()?.getAttribute('aria-label')).toBe('Define pages');
    expect(rowLinks()).toEqual(['Guided Definition', 'Requirements*', 'Interviews']);
    // The row names the page, so the top bar's crumb does not.
    expect(document.querySelector('.ov-topbar-page')).toBeNull();
  });

  it('puts Overview and Summary at the head of Plan', async () => {
    await mount('/projects/p1');
    expect(groupRow()?.getAttribute('aria-label')).toBe('Plan pages');
    expect(rowLinks().slice(0, 3)).toEqual(['Overview*', 'Summary', 'Board']);
  });

  it('draws no row for a page outside every group, and keeps its crumb', async () => {
    await mount('/projects/p1/settings');
    expect(groupRow()).toBeNull();
    expect(text(document.querySelector('.ov-topbar-page') as Element)).toBe('Settings');
  });

  it('is the compact layout as it was without the key', async () => {
    await mount('/projects/p1/requirements', { refresh: false });
    expect(groupRow()).toBeNull();
    expect(Array.from(document.querySelectorAll('aside.ov-rail nav > button')).map(text)).toEqual(['Define', 'Verify', 'Plan', 'Agents']);
  });

  it('waits for the compact layout', async () => {
    await mount('/projects/p1/requirements', { compact: false });
    expect(groupRow()).toBeNull();
    expect(document.querySelector('.ov-refresh')).toBeNull();
  });
});

describe('the refreshed shell on a phone', () => {
  it('reads Home, Plan, Define, Verify, More along the bar and lights the group in view', async () => {
    await mount('/projects/p1/requirements', { width: 390 });
    expect(barItems()).toEqual(['Home', 'Plan', 'Define*', 'Verify', 'More']);
    expect(rowLinks()).toEqual(['Guided Definition', 'Requirements*', 'Interviews']);
    // No menu button in the header: More is the one way to the menu.
    expect(document.querySelector('button[aria-label="Project menu"]')).toBeNull();
  });

  it('lights More for a page whose group has no tab', async () => {
    await mount('/projects/p1/agents', { width: 390 });
    expect(barItems()).toEqual(['Home', 'Plan', 'Define', 'Verify', 'More*']);
    expect(groupRow()?.getAttribute('aria-label')).toBe('Agents pages');
  });

  it("opens a group on its first page, then on the page last read there", async () => {
    await mount('/projects/p1/requirements', { width: 390 });
    const tab = (name: string) => Array.from(bar().querySelectorAll('a')).find((a) => text(a) === name);
    expect(tab('Verify')?.getAttribute('href')).toBe('/projects/p1/vv');
    await click(tab('Verify'));
    expect(barItems()).toEqual(['Home', 'Plan', 'Define', 'Verify*', 'More']);
    // Define returns to Requirements, which was in view, not to its first page.
    expect(tab('Define')?.getAttribute('href')).toBe('/projects/p1/requirements');
    expect(tab('Home')?.getAttribute('href')).toBe('/projects');
  });

  it('opens More as a page of tiles, group by group, and closes it on arrival', async () => {
    await mount('/projects/p1/requirements', { width: 390 });
    const more = Array.from(bar().querySelectorAll('button')).find((b) => text(b) === 'More');
    await click(more);
    const menu = document.querySelector('aside[aria-label="Project navigation"]') as HTMLElement;
    expect(Array.from(menu.querySelectorAll('section')).map((s) => s.getAttribute('aria-label'))).toEqual([
      'Plan',
      'Define',
      'Verify',
      'Agents',
      'Project',
      'You',
    ]);
    const tiles = (label: string) =>
      Array.from(menu.querySelectorAll(`section[aria-label="${label}"] .ov-more-tile`)).map(text);
    expect(tiles('Define')).toEqual(['Guided Definition', 'Requirements', 'Interviews']);
    expect(tiles('Project')).toEqual(['Activity', 'Settings', 'Help', 'Manual', 'All projects']);
    expect(barItems()).toEqual(['Home', 'Plan', 'Define', 'Verify', 'More*']);
    // The page stays mounted under the menu, hidden.
    expect((document.querySelector('main.ov-phone-main') as HTMLElement).hidden).toBe(true);
    await click(Array.from(menu.querySelectorAll('a')).find((a) => text(a) === 'Board'));
    expect(document.querySelector('aside[aria-label="Project navigation"]')).toBeNull();
    expect(barItems()).toEqual(['Home', 'Plan*', 'Define', 'Verify', 'More']);
  });
});
