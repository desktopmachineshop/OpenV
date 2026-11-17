import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { DesignItems } from './DesignItems';
import { visibleRows } from './DesignItemsTree';
import { useAppStore } from '../state/store';
import { artifactAPI } from '../api/client';
import { ARTIFACTS, features } from '../test/redesignFixtures';

// The Design page: the design items as a tree, and the selected item's
// attributes, the requirements it satisfies and the tests verifying them.

vi.mock('../api/client', async (orig) => {
  const f = await import('../test/redesignFixtures');
  return mockApi(await orig(), {
    artifactAPI: { list: () => f.ok(f.ARTIFACTS) },
    linkAPI: { list: () => f.ok(f.LINKS) },
    vvAPI: { coverage: () => f.ok(f.COVERAGE) },
  });
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const flush = () =>
  act(async () => {
    for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
  });

const render = async (on: boolean) => {
  useAppStore.setState({ features: features(on) });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/design']}>
        <Routes>
          <Route path="/projects/:projectId/design" element={<DesignItems />} />
        </Routes>
      </MemoryRouter>
    );
  });
  await flush();
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState({ features: null });
});

describe('Design page', () => {
  it('reads nothing while the gate is off', async () => {
    vi.mocked(artifactAPI.list).mockClear();
    await render(false);
    expect(container.textContent).toContain('The Design page is not in your workspace');
    expect(artifactAPI.list).not.toHaveBeenCalled();
  });

  it('draws the design items as a tree and opens the first', async () => {
    await render(true);
    const items = Array.from(container.querySelectorAll('[role="treeitem"]'));
    expect(items.map((li) => li.textContent?.replace(/[▾▸]/g, ''))).toEqual(['DES-1Sensor front end1', 'DES-2Wheel speed interface']);
    expect(container.querySelector('h1')?.textContent).toBe('Sensor front end');
    expect(container.textContent).toContain('Conditions the sensor signals.');
  });

  it("shows the selected item's attributes, what it satisfies and the tests verifying that", async () => {
    await render(true);
    const second = container.querySelectorAll('[role="treeitem"]')[1] as HTMLElement;
    await act(async () => second.click());
    expect(container.querySelector('h1')?.textContent).toBe('Wheel speed interface');
    const cells = Array.from(container.querySelectorAll('[aria-label="Attributes"] [role="cell"]')).map((c) => c.textContent);
    expect(cells).toEqual(['supplier part', 'WS-400']);
    const text = container.textContent || '';
    expect(text).toContain('satisfies · 1');
    expect(text).toContain('REQ-1Detect wheel slip');
    expect(text).toContain('Verified through · 2');
    expect(text).toContain('TC-1Slip on wet asphaltpass');
    expect(text).toContain('TC-2Detection latencyfail');
    expect(text).toContain('DES-1 Sensor front end');
  });

  it('filters the tree to the matches and the items above them', () => {
    const items = ARTIFACTS.filter((a) => a.type === 'design-item');
    expect(visibleRows(items, new Set(), 'wheel').map((r) => r.item.ref)).toEqual(['DES-1', 'DES-2']);
    expect(visibleRows(items, new Set(['des1']), '').map((r) => r.item.ref)).toEqual(['DES-1']);
  });
});
