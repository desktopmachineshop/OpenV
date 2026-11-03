import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { ProjectList } from './ProjectList';
import { artifactAPI, projectAPI, templateAPI, vvAPI, workerStatusAPI } from '../api/client';
import { LAST_PROJECT_KEY } from '../appShortcuts';

// The Home design (redesign-pages): arriving in the app opens a project's
// Home, and the projects list itself is the Projects panel's rows, each with
// its size and traced and verified share. Opening the list on purpose (no
// arrival state) stays on the list.
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
let gatesLoaded = true;
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
    features: gatesLoaded ? { channel: 'nightly', features: { 'redesign-pages': redesign } } : null,
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
  gatesLoaded = true;
  storeProjects = projects;
  locationState = null;
  window.localStorage.clear();
  vi.mocked(projectAPI.list, { partial: true, deep: true }).mockResolvedValue({ data: projects });
  vi.mocked(templateAPI.list, { partial: true, deep: true }).mockResolvedValue({ data: [] });
  vi.mocked(workerStatusAPI.get, { partial: true, deep: true }).mockResolvedValue({ data: { workers: [] } });
  vi.mocked(vvAPI.coverage, { partial: true, deep: true }).mockImplementation(
    (id: string) => Promise.resolve({ data: { summary: id === 'p1' ? { total: 10, covered: 9, verified: 8 } : { total: 0 } } }) as any
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

describe('arriving in the app', () => {
  it('opens the project last opened at its Home', async () => {
    window.localStorage.setItem(LAST_PROJECT_KEY, 'p2');
    locationState = { landing: true };
    await render();
    expect(navigate).toHaveBeenCalledWith('/projects/p2/home', { replace: true });
  });

  it('opens the first project when none is remembered', async () => {
    locationState = { landing: true };
    await render();
    expect(navigate).toHaveBeenCalledWith('/projects/p1/home', { replace: true });
  });

  it('does not leave the list when a member with no projects creates one', async () => {
    storeProjects = [];
    vi.mocked(projectAPI.list, { partial: true, deep: true }).mockResolvedValue({ data: [] });
    locationState = { landing: true };
    await render();
    // The new project reaches the store; the arrival was already decided.
    storeProjects = [projects[0]];
    await render();
    expect(navigate).not.toHaveBeenCalledWith(expect.stringMatching(/\/home$/), expect.anything());
  });

  it('waits for the feature gates, which can arrive after the list', async () => {
    gatesLoaded = false;
    locationState = { landing: true };
    await render();
    expect(navigate).not.toHaveBeenCalledWith(expect.stringMatching(/\/home$/), expect.anything());
    gatesLoaded = true;
    await render();
    expect(navigate).toHaveBeenCalledWith('/projects/p1/home', { replace: true });
  });

  it('never moves a member who has started using the list', async () => {
    gatesLoaded = false;
    locationState = { landing: true };
    await render();
    // The staging smoke journey: + New Project clicked before the gates load.
    await act(async () => {
      window.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    });
    gatesLoaded = true;
    await render();
    expect(navigate).not.toHaveBeenCalledWith(expect.stringMatching(/\/home$/), expect.anything());
  });

  it('stays on the list when the list is opened on purpose', async () => {
    await render();
    expect(navigate).not.toHaveBeenCalledWith(expect.stringMatching(/\/home$/), expect.anything());
  });

  it('stays on the list without the redesign', async () => {
    redesign = false;
    locationState = { landing: true };
    await render();
    expect(navigate).not.toHaveBeenCalledWith(expect.stringMatching(/\/home$/), expect.anything());
  });
});

describe('the list as the Projects panel', () => {
  it('draws rows with each project size and traced and verified share', async () => {
    await render();
    expect(container.querySelector('.projects-grid.is-rows')).not.toBeNull();
    const rows = Array.from(container.querySelectorAll('.project-card'));
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('248 artifacts');
    expect(rows[0].textContent).toContain('Traced');
    expect(rows[0].textContent).toContain('90%');
    expect(rows[0].textContent).toContain('Verified');
    expect(rows[0].textContent).toContain('80%');
    expect(rows[1].textContent).toContain('No requirements to trace yet');
    // The actions stay on every row.
    expect(rows[0].querySelector('[title="Delete project"]')).not.toBeNull();
    expect(rows[0].querySelector('[title="Edit project"]')).not.toBeNull();
  });

  it('keeps the card grid without the redesign', async () => {
    redesign = false;
    await render();
    expect(container.querySelector('.projects-grid.is-rows')).toBeNull();
    expect(vvAPI.coverage).not.toHaveBeenCalled();
  });
});
