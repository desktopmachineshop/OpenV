import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { TestsPage } from './TestsPage';
import { countSteps } from './TestsPageData';
import { useAppStore } from '../state/store';
import { vvAPI } from '../api/client';
import { features } from '../test/redesignFixtures';

// The Tests page: run cards with their counts, and the test case table with
// method, what each verifies, its last result and run, and its steps.

vi.mock('../api/client', async (orig) => {
  const f = await import('../test/redesignFixtures');
  return mockApi(await orig(), {
    artifactAPI: { list: () => f.ok(f.ARTIFACTS) },
    linkAPI: { list: () => f.ok(f.LINKS) },
    vvAPI: {
      listRuns: () => f.ok([f.RUN]),
      listResults: () => f.ok(f.RESULTS.slice(0, 1)),
      coverage: () => f.ok(f.COVERAGE),
    },
  });
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const render = async (on: boolean) => {
  useAppStore.setState({ features: features(on) });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/tests']}>
        <Routes>
          <Route path="/projects/:projectId/tests" element={<TestsPage />} />
        </Routes>
      </MemoryRouter>
    );
  });
  await act(async () => {
    for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
  });
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState({ features: null });
});

const rows = () =>
  Array.from(container.querySelectorAll('[aria-label="Test cases"] [role="row"]'))
    .slice(1)
    .map((r) => Array.from(r.querySelectorAll('[role="cell"]')).map((c) => c.textContent));

describe('Tests page', () => {
  it('reads nothing while the gate is off', async () => {
    vi.mocked(vvAPI.listRuns).mockClear();
    await render(false);
    expect(container.textContent).toContain('The Tests page is not in your workspace');
    expect(vvAPI.listRuns).not.toHaveBeenCalled();
  });

  it('shows each run with its counts out of every test case', async () => {
    await render(true);
    const card = container.querySelector('[aria-label="Recent test runs"] a');
    expect(card?.textContent).toContain('HIL regression');
    expect(card?.textContent).toContain('in progress');
    expect(card?.textContent).toContain('1 pass · 0 fail · 0 blocked · 1 to go');
  });

  it('lists the test cases with their method, what they verify, their last result and run', async () => {
    await render(true);
    expect(rows()).toEqual([
      ['TC-1', 'Slip on wet asphalt', 'physical', 'REQ-1', 'pass', 'HIL regression', '2'],
      ['TC-2', 'Detection latency', 'automated', 'REQ-1', 'fail', '—', '—'],
    ]);
    const filter = container.querySelector('button[aria-pressed]') as HTMLButtonElement;
    await act(async () => filter.click());
    expect(rows().map((r) => r[0])).toEqual(['TC-2']);
  });

  it('counts numbered steps', () => {
    expect(countSteps('Set up\n1. Apply power\n2) Wait\n 3. Read')).toBe(3);
    expect(countSteps('')).toBe(0);
  });
});
