import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { OpenSourceProjects, groupByWorkspace } from './OpenSourceProjects';
import { OPENV_WORKSPACE } from './content';
import type { OpenSourceProject } from '../api/client';

// The open-source page (REQ-151) gives OpenV's own projects a section of
// their own and groups everyone else's under their workspace.

const project = (id: string, workspace: string): OpenSourceProject => ({
  project_id: id,
  name: `Project ${id}`,
  description: '',
  workspace,
  baseline_id: `b-${id}`,
  baseline: 'v1',
  snapshot_at: '2026-10-01T00:00:00Z',
  counts: { requirement: 2 },
});

const list = [
  project('a', 'Acme'),
  project('o', OPENV_WORKSPACE),
  project('b', 'Beta Labs'),
  project('c', 'Acme'),
];

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    openSourceAPI: { list: () => Promise.resolve({ data: list }) },
  })
);

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

describe('groupByWorkspace', () => {
  it('keeps the order the list first names each workspace in', () => {
    const groups = groupByWorkspace(list);
    expect(groups.map(([w, ps]) => [w, ps.map((p) => p.project_id)])).toEqual([
      ['Acme', ['a', 'c']],
      [OPENV_WORKSPACE, ['o']],
      ['Beta Labs', ['b']],
    ]);
  });
});

describe('OpenSourceProjects', () => {
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

  it('shows OpenV projects apart and the rest grouped by workspace', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter>
          <OpenSourceProjects />
        </MemoryRouter>
      );
    });
    const own = container.querySelector('#openv-projects')!;
    expect(own.textContent).toContain('Project o');
    expect(own.textContent).not.toContain('Project a');

    const published = container.querySelector('#projects')!;
    expect(published.textContent).not.toContain('Project o');
    const headings = [...published.querySelectorAll('h3')].map((h) => h.textContent);
    expect(headings).toEqual(['Acme', 'Project a', 'Project c', 'Beta Labs', 'Project b']);
  });
});
