import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { DialogProvider } from '../components/ui';
import { mockApi } from '../test/mockApi';
import { VVDashboard } from './VVDashboard';

// REQ-132: wherever a person chooses between baselines, each one reads with
// its name, its date and who captured it.

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    baselineAPI: {
      list: () =>
        Promise.resolve({
          data: [
            {
              id: 'b1',
              project_id: 'p1',
              name: 'Design freeze',
              created_at: '2026-03-15T10:00:00Z',
              created_by_name: 'Dana Reyes',
            },
          ],
        }),
    },
    artifactAPI: { list: () => Promise.resolve({ data: [] }) },
    vvAPI: {
      coverage: () => Promise.resolve({ data: { entries: [], summary: {} } }),
      gaps: () => Promise.resolve({ data: {} }),
      listRuns: () => Promise.resolve({ data: [] }),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

it('names who captured each baseline in the picker', async () => {
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter initialEntries={['/projects/p1/vv']}>
          <Routes>
            <Route path="/projects/:projectId/vv" element={<VVDashboard />} />
          </Routes>
        </MemoryRouter>
      </DialogProvider>
    );
  });
  const option = container.querySelector('option[value="b1"]');
  expect(option?.textContent).toContain('Design freeze');
  expect(option?.textContent).toContain('Dana Reyes');
});
