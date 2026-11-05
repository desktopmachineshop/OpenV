import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { ProjectList } from './ProjectList';
import { artifactAPI, eventsAPI, projectAPI, proposalsAPI, reviewAPI, templateAPI, vvAPI, workerStatusAPI } from '../api/client';

// The Home design (redesign-pages): the Projects page is the workspace's Home.
// What needs the member across every project and the workspace's activity sit
// above the project picker, whose rows carry each project's size and traced
// and verified share. Signing in lands here; nothing moves the member on.
vi.mock('../api/client', async (orig) => mockApi(await orig(), { agentsAPI: { list: () => Promise.resolve({ data: [] }) } }));

const navigate = vi.fn();
let locationState: unknown = null;
vi.mock('react-router-dom', () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => <a href={to}>{children}</a>,
  useNavigate: () => navigate,
  useSearchParams: () => [new URLSearchParams(), () => {}],
  useLocation: () => ({ pathname: '/projects', search: '', hash: '', state: locationState, key: 'default' }),
}));

const projects = [
  { id: 'p1', name: 'Brake-by-wire ECU', description: 'The ECU', created_at: '2026-09-01T00:00:00Z' },
  { id: 'p2', name: 'Steering angle sensor', description: '', created_at: '2026-09-02T00:00:00Z' },
];
let redesign = true;
let storeProjects: typeof projects = projects;
vi.mock('../state/store', () => {
  const state = () => ({
    projectId: null,
    setProjectId: vi.fn(),
    projects: storeProjects,
    setProjects: vi.fn(),
    addProject: vi.fn(),
    updateProject: vi.fn(),
    removeProject: vi.fn(),
    orgs: [{ id: 'org1', name: 'Sam Space', type: 'company' }],
    activeOrgId: 'org1',
    features: { channel: 'nightly', features: { 'redesign-pages': redesign } },
  });
  return { useAppStore: (select?: (s: ReturnType<typeof state>) => unknown) => (select ? select(state()) : state()) };
});

vi.mock('./Navbar', () => ({ Navbar: () => null }));
vi.mock('./HelpSidebar', () => ({ HelpSidebar: () => null }));
vi.mock('./DownloadWizard', () => ({ DownloadWizard: () => null }));
vi.mock('./CreateOrgModal', () => ({ CreateOrgModal: () => null }));
vi.mock('./ui', async () => ({
  ...(await vi.importActual<typeof import('./ui')>('./ui')),
  useConfirm: () => vi.fn(),
  usePrompt: () => vi.fn(),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  redesign = true;
  storeProjects = projects;
  locationState = null;
  vi.mocked(reviewAPI.get, { partial: true, deep: true }).mockImplementation(
    (id: string) =>
      Promise.resolve({
        data: {
          suspect_links: [],
          in_review_artifacts: id === 'p1' ? [{ id: 'a1', ref: 'SYS-021', title: 'Wheel-slip detection threshold', updated_at: '2026-09-01T00:00:00Z' }] : [],
        },
      }) as any
  );
  vi.mocked(vvAPI.listRuns, { partial: true, deep: true }).mockResolvedValue({ data: [] } as any);
  vi.mocked(eventsAPI.list, { partial: true, deep: true }).mockImplementation(
    (params: { event_type?: string }) =>
      Promise.resolve({
        data: params.event_type
          ? []
          : [{ id: 'e1', event_type: 'artifact.status_changed', project_id: 'p2', actor_name: 'Priya Shah', entity_name: 'SAS-4', payload: { to: 'approved' }, created_at: '2026-09-01T09:58:00Z' }],
      }) as any
  );
  vi.mocked(proposalsAPI.list, { partial: true, deep: true }).mockResolvedValue({ data: [] } as any);
  vi.mocked(projectAPI.list, { partial: true, deep: true }).mockResolvedValue({ data: projects });
  vi.mocked(templateAPI.list, { partial: true, deep: true }).mockResolvedValue({ data: [] });
  vi.mocked(workerStatusAPI.get, { partial: true, deep: true }).mockResolvedValue({ data: { workers: [] } });
  vi.mocked(vvAPI.coverage, { partial: true, deep: true }).mockImplementation(
    (id: string) => Promise.resolve({ data: { project_id: id, entries: [], summary: id === 'p1' ? { pass: 7, 'verified-manually': 1, fail: 1, uncovered: 1 } : {} } }) as any
  );
  vi.mocked(artifactAPI.listPage, { partial: true, deep: true }).mockImplementation(
    (id: string) => Promise.resolve({ data: [], headers: { 'x-total-count': id === 'p1' ? '248' : '12' } }) as any
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const render = async () => {
  await act(async () => {
    root.render(<ProjectList />);
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await Promise.resolve();
    });
  }
};

const settle = async () => {
  for (let i = 0; i < 12; i++) {
    await act(async () => {
      await Promise.resolve();
    });
  }
};

describe('the workspace Home', () => {
  it('heads with the workspace and its figures, and lists what needs the member across projects', async () => {
    await render();
    await settle();
    expect(container.querySelector('h1')?.textContent).toBe('Sam Space');
    expect(container.textContent).toContain('2 projects in this workspace');
    expect(container.textContent).toContain('260 artifacts');
    expect(container.textContent).toContain('90% traced');
    const needs = container.querySelector('section[aria-labelledby="home-needs"]')!;
    expect(needs.textContent).toContain('SYS-021');
    expect(needs.textContent).toContain('Brake-by-wire ECU · In review');
    const activity = container.querySelector('section[aria-labelledby="home-activity"]')!;
    expect(activity.textContent).toContain('Priya Shah approved SAS-4');
    expect(activity.textContent).toContain('Steering angle sensor');
  });

  it('puts the project picker below, each row with its size and traced and verified share', async () => {
    await render();
    await settle();
    expect(container.querySelector('.projects-grid.is-rows')).not.toBeNull();
    const rows = Array.from(container.querySelectorAll('.project-card'));
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('248 artifacts');
    expect(rows[0].textContent).toContain('90%');
    expect(rows[0].textContent).toContain('80%');
    expect(rows[1].textContent).toContain('No requirements to trace yet');
    expect(rows[0].querySelector('[title="Delete project"]')).not.toBeNull();
  });

  it('stays here after signing in', async () => {
    locationState = { landing: true };
    await render();
    await settle();
    expect(navigate).not.toHaveBeenCalled();
  });

  it('keeps the plain list without the redesign, and loads nothing for the Home', async () => {
    redesign = false;
    await render();
    await settle();
    expect(container.querySelector('.projects-grid.is-rows')).toBeNull();
    expect(container.querySelector('section[aria-labelledby="home-needs"]')).toBeNull();
    expect(vvAPI.coverage).not.toHaveBeenCalled();
  });
});
