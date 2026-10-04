import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { OrgSwitcher } from './OrgSwitcher';
import { useAppStore } from '../state/store';
import { type Org } from '../api/client';

// The workspace menu is where the workspace's own pages are reached (#379
// bug 168): Workspace runs, the runs with no project, beside Workspace
// settings, once the workspace has the workspace-runs feature.

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    orgsAPI: { activate: () => Promise.resolve({ data: {} }) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();

const org = (id: string, name: string): Org =>
  ({ id, name, slug: id, type: 'company', plan: 'business', role: 'member', created_at: '' }) as Org;

let container: HTMLDivElement;
let root: Root;

const Where: React.FC = () => {
  const location = useLocation();
  return <output data-testid="where">{location.pathname + location.search}</output>;
};
const where = () => container.querySelector('[data-testid="where"]')?.textContent;

const mount = async (at: string, workspaceRuns: boolean | null) => {
  useAppStore.setState(
    {
      ...initialStore,
      activeOrgId: 'o1',
      orgs: [org('o1', 'Shop'), org('o2', 'Lab')],
      features:
        workspaceRuns === null
          ? null
          : {
              channel: workspaceRuns ? 'nightly' : 'stable',
              stable_release: '',
              preview: false,
              features: { 'workspace-runs': workspaceRuns },
            },
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[at]}>
        <OrgSwitcher />
        <Where />
      </MemoryRouter>
    );
  });
};

const click = async (el: Element) => {
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
};

const openMenu = () => click(container.querySelector('button[title="Switch workspace"]')!);
const item = (label: string) =>
  Array.from(container.querySelectorAll('[role="menu"] button')).find((b) => (b.textContent || '').trim() === label);

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

describe('the workspace menu', () => {
  it('opens Workspace runs, beside Workspace settings, once the workspace has it', async () => {
    await mount('/projects', true);
    await openMenu();
    const labels = Array.from(container.querySelectorAll('[role="menu"] button')).map((b) => (b.textContent || '').trim());
    expect(labels.indexOf('Workspace runs')).toBe(labels.indexOf('Workspace settings') - 1);
    await click(item('Workspace runs')!);
    expect(where()).toBe('/org/runs');
    expect(container.querySelector('[role="menu"]')).toBeNull();
  });

  it('has no Workspace runs without the feature, or while the gates load', async () => {
    await mount('/projects', false);
    await openMenu();
    expect(item('Workspace settings')).toBeTruthy();
    expect(item('Workspace runs')).toBeUndefined();

    act(() => root.unmount());
    act(() => {
      root = createRoot(container);
    });
    await mount('/projects', null);
    await openMenu();
    expect(item('Workspace runs')).toBeUndefined();
  });

  it("stays on Workspace runs when switching workspace, closing the other one's run", async () => {
    await mount('/org/runs?run=run-1', true);
    await openMenu();
    await click(item('Lab')!);
    expect(useAppStore.getState().activeOrgId).toBe('o2');
    expect(where()).toBe('/org/runs');
  });

  it('goes to the projects list from any other page, as before', async () => {
    await mount('/whats-new', true);
    await openMenu();
    await click(item('Lab')!);
    expect(where()).toBe('/projects');
  });
});
