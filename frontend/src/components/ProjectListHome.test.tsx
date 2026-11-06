import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { ProjectListHome } from './ProjectListHome';
import type { WorkspaceHomeData, WorkspaceTodo } from './ProjectListHomeLoad';

// The workspace Home fitted to the window (REQ-244): its panels fill the
// height they are given and scroll their own rows, and Needs you lists every
// item rather than the first eight. The window sizing itself is CSS
// (ProjectList.css, .is-fit) that jsdom does not lay out; the phone audit's
// desktop profiles render it.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const todo: WorkspaceTodo[] = Array.from({ length: 12 }, (_, i) => ({
  key: `approve-a${i}`,
  kind: 'approve',
  ref: `REQ-${i + 1}`,
  title: `Requirement ${i + 1}`,
  meta: 'In review · 2h',
  action: 'Review',
  to: `/projects/p1/requirements?artifact=a${i}`,
  open: `/projects/p1/requirements?artifact=a${i}`,
  project: 'Brake-by-wire ECU',
  projectId: 'p1',
}));

const data: WorkspaceHomeData = {
  todo,
  activity: [],
  figures: {},
  proposals: [],
  totals: { artifacts: 10, traced: 50, verified: 40, failing: 0 },
};

const render = async (fit: boolean) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter>
        <ProjectListHome workspace="Acme" projects={1} data={data} phone={false} fit={fit} />
      </MemoryRouter>
    );
  });
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const needs = () => container.querySelector<HTMLElement>('section[aria-labelledby="home-needs"]')!;

describe('ProjectListHome fitted to the window', () => {
  it('lists the first eight items when the page scrolls as a whole', async () => {
    await render(false);
    expect(needs().querySelectorAll('li')).toHaveLength(8);
    expect(needs().textContent).toContain('And 4 more in the projects below.');
    expect(needs().style.overflow).toBe('');
  });

  it('lists every item and scrolls each panel on its own when fitted', async () => {
    await render(true);
    expect(needs().querySelectorAll('li')).toHaveLength(12);
    expect(needs().textContent).not.toContain('more in the projects below');
    const activity = container.querySelector<HTMLElement>('section[aria-labelledby="home-activity"]')!;
    for (const panel of [needs(), activity]) {
      expect(panel.style.display).toBe('flex');
      expect(panel.style.overflow).toBe('hidden');
      // The body under the header is the scroller.
      expect((panel.children[1] as HTMLElement).style.overflowY).toBe('auto');
    }
  });
});
