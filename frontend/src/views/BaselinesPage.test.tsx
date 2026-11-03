import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { BaselinesPage } from './BaselinesPage';
import { useAppStore } from '../state/store';
import { baselineAPI } from '../api/client';
import { wordDiff } from '../utils/baselinesDiff';
import { features } from '../test/redesignFixtures';

// The Baselines page: the timeline, the newest baseline compared to the
// working copy, the change list and the text of a change word by word.

vi.mock('../api/client', async (orig) => {
  const f = await import('../test/redesignFixtures');
  const before = f.ARTIFACTS.map((a) =>
    a.id === 'req1' ? { ...a, body: 'The controller shall detect slip above 20% within 50 ms.' } : a
  ).concat([f.art('old', 'requirement', 'REQ-0', 'Manual bleed')]);
  const live = f.ARTIFACTS.map((a) =>
    a.id === 'req1' ? { ...a, body: 'The controller shall detect slip above 15% within 50 ms.', version: 3 } : a
  );
  return mockApi(await orig(), {
    baselineAPI: {
      list: () => f.ok(f.BASELINES),
      get: () => f.ok({ artifacts: before, links: [] }),
      diff: (_b, against) =>
        f.ok({
          base: { id: 'bl2', name: 'BL-2' },
          target: { id: against, name: 'Live' },
          added: [{ id: 'tc2', type: 'test-case', title: 'Detection latency' }],
          removed: [{ id: 'old', type: 'requirement', title: 'Manual bleed' }],
          modified: [
            { id: 'req1', type: 'requirement', old_title: 'Detect wheel slip', new_title: 'Detect wheel slip', title_changed: false, body_changed: true, type_changed: false, status_changed: true, parent_changed: false },
          ],
          links_added: [{ from_id: 'tc2', to_id: 'req1', type: 'verifies', from_title: 'Detection latency', to_title: 'Detect wheel slip' }],
          links_removed: [],
        }),
    },
    artifactAPI: { list: () => f.ok(live) },
    linkAPI: { list: () => f.ok(f.LINKS) },
  });
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const flush = () =>
  act(async () => {
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
  });

const render = async (on: boolean) => {
  useAppStore.setState({ features: features(on) });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/baselines']}>
        <Routes>
          <Route path="/projects/:projectId/baselines" element={<BaselinesPage />} />
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

describe('Baselines page', () => {
  it('reads nothing while the gate is off', async () => {
    vi.mocked(baselineAPI.list).mockClear();
    await render(false);
    expect(container.textContent).toContain('The Baselines page is not in your workspace');
    expect(baselineAPI.list).not.toHaveBeenCalled();
  });

  it('compares the newest baseline with the working copy', async () => {
    await render(true);
    const timeline = Array.from(container.querySelectorAll('[aria-label="Baselines"] li b')).map((b) => b.textContent);
    expect(timeline).toEqual(['Working copy', 'BL-2', 'BL-1']);
    expect(vi.mocked(baselineAPI.diff)).toHaveBeenLastCalledWith('bl2', 'live');
    expect(container.querySelector('h1')?.textContent).toBe('BL-2 → Working copy');
    const rows = Array.from(container.querySelectorAll('[role="table"][aria-label="Changes"] [role="row"]'))
      .slice(1)
      .map((r) => Array.from(r.querySelectorAll('[role="cell"]')).map((c) => c.textContent));
    expect(rows).toEqual([
      ['changed', 'REQ-1', 'Detect wheel slip', 'text, status'],
      ['added', 'TC-2', 'Detection latency', 'new'],
      ['removed', 'REQ-0', 'Manual bleed', '—'],
      ['link added', 'TC-2', 'verifies REQ-1', 'links'],
    ]);
  });

  it('shows the selected change word by word, with the links it left suspect', async () => {
    await render(true);
    const panel = container.querySelector('[aria-label="Text change in REQ-1"]');
    expect(panel?.querySelector('del')?.textContent?.trim()).toBe('20%');
    expect(panel?.querySelector('ins')?.textContent?.trim()).toBe('15%');
    expect(panel?.textContent).toContain('1 link is suspect now: TC-2.');
  });

  it('compares from another baseline when it is picked', async () => {
    await render(true);
    const bl1 = Array.from(container.querySelectorAll('[aria-label="Baselines"] button')).find((b) => b.textContent?.includes('BL-1'));
    await act(async () => (bl1 as HTMLButtonElement).click());
    await flush();
    expect(vi.mocked(baselineAPI.diff)).toHaveBeenLastCalledWith('bl1', 'live');
  });
});

describe('wordDiff', () => {
  it('keeps the common words and marks the rest', () => {
    expect(wordDiff('a b c', 'a x c')).toEqual([
      { kind: 'same', text: 'a ' },
      { kind: 'del', text: 'b' },
      { kind: 'same', text: ' ' },
      { kind: 'ins', text: 'x' },
      { kind: 'same', text: ' c' },
    ]);
  });
});
