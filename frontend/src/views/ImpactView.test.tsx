import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ImpactView } from './ImpactView';
import { useAppStore } from '../state/store';
import { vvAPI } from '../api/client';
import type { OrgFeatures } from '../api/client';

// Impact analysis: the picker pane (the requirements as a folding table
// under the chip search bar), the impact drawn with the shared tree diagram,
// the result as a table, and re-centring.

vi.mock('../api/client', async (orig) => {
  const f = await import('../test/redesignFixtures');
  const artifacts = [
    f.art('h1', 'heading', '', 'Braking'),
    f.art('req1', 'requirement', 'REQ-1', 'Detect wheel slip', { parent_id: 'h1' }),
    f.art('req2', 'requirement', 'REQ-2', 'Warn the driver', { parent_id: 'h1' }),
    f.art('des1', 'design-item', 'DES-1', 'Sensor front end'),
    f.art('tc1', 'test-case', 'TC-1', 'Slip bench test'),
    f.art('un1', 'user-need', 'UN-1', 'Stop safely'),
  ];
  const node = (id: string, type: string, via: string, path: string[]) => ({ artifact_id: id, title: '', type, via, distance: path.length - 1, path });
  return mockApi(await orig(), {
    artifactAPI: { list: () => f.ok(artifacts) },
    vvAPI: {
      impact: (_p: string, id: string) =>
        f.ok({
          project_id: 'p1',
          artifact_id: id,
          direction: 'both',
          upstream: [{ type: 'user-need', count: 1, nodes: [node('un1', 'user-need', 'derives-from', [id, 'un1'])] }],
          downstream: [
            { type: 'design-item', count: 1, nodes: [node('des1', 'design-item', 'satisfies', [id, 'des1'])] },
            { type: 'test-case', count: 1, nodes: [node('tc1', 'test-case', 'verifies', [id, 'des1', 'tc1'])] },
          ],
          total: 3,
        }),
    },
  });
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
let location = '';

const Where: React.FC = () => {
  const l = useLocation();
  location = l.pathname + l.search;
  return null;
};

const flush = () =>
  act(async () => {
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
  });

const render = async (on: boolean, url = '/projects/p1/impact') => {
  useAppStore.setState({
    features: { channel: 'nightly', stable_release: '', preview: false, features: { 'impact-picker': on } } as OrgFeatures,
  });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[url]}>
        <Where />
        <Routes>
          <Route path="/projects/:projectId/impact" element={<ImpactView />} />
          <Route path="/projects/:projectId/requirements" element={<p>Requirements page</p>} />
        </Routes>
      </MemoryRouter>
    );
  });
  await flush();
};

const pickerRows = () =>
  Array.from(container.querySelectorAll('[aria-label="Requirements to pick from"] [role="treeitem"]')).map((r) => r.textContent || '');

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState({ features: null });
});

describe('Impact analysis', () => {
  it('keeps the dropdown while the picker is off', async () => {
    await render(false);
    expect(container.querySelector('select')).not.toBeNull();
    expect(container.querySelector('[aria-label="Pick a requirement"]')).toBeNull();
  });

  it('picks the requirement from the folding table, under its heading', async () => {
    await render(true);
    expect(container.querySelector('select')).toBeNull();
    const rows = pickerRows();
    expect(rows[0]).toContain('Braking');
    expect(rows.some((r) => r.includes('REQ-1') && r.includes('Detect wheel slip'))).toBe(true);
    // Only requirements, and the headings above them.
    expect(rows.some((r) => r.includes('DES-1'))).toBe(false);
    const req2 = Array.from(container.querySelectorAll<HTMLElement>('[aria-label="Requirements to pick from"] [role="treeitem"]')).find((r) =>
      r.textContent?.includes('REQ-2')
    )!;
    await act(async () => req2.click());
    await flush();
    expect(location).toBe('/projects/p1/impact?artifact=req2');
    expect(vi.mocked(vvAPI.impact)).toHaveBeenLastCalledWith('p1', 'req2', 'both');
  });

  it('narrows the table with the search bar', async () => {
    await render(true);
    const input = container.querySelector<HTMLInputElement>('input[aria-label="Search the requirements"]')!;
    await act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      set.call(input, 'warn');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await flush();
    const rows = pickerRows();
    expect(rows.some((r) => r.includes('REQ-2'))).toBe(true);
    expect(rows.some((r) => r.includes('REQ-1'))).toBe(false);
  });

  it('draws the impact as the two-sided tree, with the link key', async () => {
    await render(true, '/projects/p1/impact?artifact=req1');
    const tree = container.querySelector('[role="tree"][aria-label="Impact tree"]')!;
    const labels = Array.from(tree.querySelectorAll('[role="treeitem"]')).map((t) => t.getAttribute('aria-label'));
    expect(labels[0]).toContain('REQ-1, Detect wheel slip');
    expect(labels.some((l) => l?.includes('UN-1') && l.includes('upstream, derives-from, 1 hop'))).toBe(true);
    expect(labels.some((l) => l?.includes('TC-1') && l.includes('downstream, verifies, 2 hops'))).toBe(true);
    const key = Array.from(container.querySelectorAll('[aria-label="Link key"] li')).map((l) => l.textContent);
    expect(key).toEqual(['derives-from', 'satisfies', 'verifies']);
    expect(container.querySelector('[role="status"]')?.textContent).toContain('3 affected artifacts: 1 upstream, 2 downstream');
    // The picked row is highlighted in the picker.
    const picked = container.querySelector('[aria-label="Requirements to pick from"] [aria-selected="true"]');
    expect(picked?.textContent).toContain('REQ-1');
  });

  it('opens a box, and re-centres the impact on another', async () => {
    await render(true, '/projects/p1/impact?artifact=req1');
    const centre = container.querySelector<HTMLButtonElement>('button[aria-label="Centre the impact on DES-1"]')!;
    await act(async () => centre.click());
    await flush();
    expect(location).toBe('/projects/p1/impact?artifact=des1');
    const box = Array.from(container.querySelectorAll<HTMLElement>('[aria-label="Impact tree"] [role="treeitem"]')).find((t) =>
      t.getAttribute('aria-label')?.includes('UN-1')
    )!;
    await act(async () => box.click());
    await flush();
    expect(location).toBe('/projects/p1/requirements?artifact=un1');
  });

  it('shows the same result as a table, and folds the picker away', async () => {
    await render(true, '/projects/p1/impact?artifact=req1');
    const tableButton = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Table')!;
    await act(async () => tableButton.click());
    const rows = Array.from(container.querySelectorAll('[role="table"][aria-label="Impact"] [role="row"]')).slice(1);
    expect(rows.map((r) => r.querySelectorAll('[role="cell"]')[3]?.textContent)).toEqual(['UN-1', 'DES-1', 'TC-1']);
    expect(rows[2].textContent).toContain('via DES-1');
    const hide = Array.from(container.querySelectorAll('button')).find((b) => b.textContent?.includes('Hide list'))!;
    await act(async () => hide.click());
    expect(container.querySelector('[aria-label="Pick a requirement"]')).toBeNull();
    const show = Array.from(container.querySelectorAll('button')).find((b) => b.textContent?.includes('Show list'))!;
    await act(async () => show.click());
    expect(container.querySelector('[aria-label="Pick a requirement"]')).not.toBeNull();
  });
});
