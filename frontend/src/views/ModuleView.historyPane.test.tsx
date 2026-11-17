import { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ModuleView } from './ModuleView';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';
import { useAssistantStore } from '../components/wizard/assistantStore';
import { chatterAPI } from '../api/client';

// The requirements page with the floating assistant (#725, #726): the
// artifact's History is a section of its pane, the Notes column (its strip,
// its handle, its panel) is gone, and the page tells the floating assistant
// which artifact is open. With the gate closed the Notes column stays.

const art = (id: string, ref: string, parent: string | null, order: number, type: string, title: string) => ({
  id,
  ref,
  project_id: 'p1',
  parent_id: parent,
  type,
  title,
  body: `${title}.`,
  sort_order: order,
  status: 'draft',
  attributes: {},
  version: 1,
  valid_from: '2026-01-02T08:00:00Z',
  valid_to: null,
  created_at: '2026-01-02T08:00:00Z',
  updated_at: '2026-01-02T08:00:00Z',
});

const LIVE = [
  art('hdg-1', 'SEC-1', null, 1, 'heading', 'Pumping'),
  art('req-1', 'REQ-1', 'hdg-1', 1, 'requirement', 'The pump shall start'),
];

const ok = (data: unknown) => Promise.resolve({ data, headers: {} });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: {
      get: () => ok({ id: 'p1', org_id: 'o1', name: 'Fuel pump', parent_project_id: '' }),
      linkedArtifacts: () => ok([]),
    },
    artifactAPI: { list: () => ok(LIVE), getVersions: () => ok([]) },
    linkAPI: { list: () => ok([]), listForArtifact: () => ok([]) },
    baselineAPI: { list: () => ok([]) },
    qualityAPI: {
      project: () => ok({ project_id: 'p1', entries: [], summary: {} }),
      artifact: (id: string) => ok({ artifact_id: id, title: '', type: 'requirement', score: 90, band: 'good', findings: [] }),
    },
    attachmentAPI: { listByProject: () => ok([]), listByArtifact: () => ok([]) },
    chatterAPI: {
      list: () => ok([{ id: 'c1', artifact_id: 'req-1', message: 'Looks testable', author_name: 'Ada', is_auto_entry: false, created_at: '2026-01-03T08:00:00Z' }]),
    },
  })
);

vi.mock('../components/ChatterPanel', async () => {
  const { createElement } = await import('react');
  return { ChatterPanel: () => createElement('section', { 'data-probe': 'chatter' }) };
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 6; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async (gate: boolean, entry = '/projects/p1/requirements?artifact=req-1') => {
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'floating-assistant': gate } },
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[entry]}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/requirements" element={<ModuleView />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

beforeEach(() => {
  window.localStorage.clear();
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 1800 });
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

const history = () => container.querySelector('section[aria-label="History"]');

describe('ModuleView with History in the pane', () => {
  it('shows the History section in the artifact pane and no Notes column', async () => {
    await mount(true);
    expect(history()).not.toBeNull();
    expect(history()!.getAttribute('data-help-key')).toBe('requirements.history');
    expect(history()!.textContent).toContain('Looks testable');
    expect(vi.mocked(chatterAPI.list)).toHaveBeenCalledWith('req-1');
    expect(container.querySelector('[data-probe="chatter"]')).toBeNull();
    expect(container.querySelector('button.panel-edge-strip')).toBeNull();
  });

  it('tells the floating assistant which artifact is open', async () => {
    await mount(true);
    expect(useAssistantStore.getState().page.context).toEqual({ label: 'REQ-1 The pump shall start', artifactId: 'req-1' });
  });

  it('keeps the Notes column, and no History section, with the gate closed', async () => {
    await mount(false);
    expect(history()).toBeNull();
    expect(container.querySelector('[data-probe="chatter"]')).not.toBeNull();
  });
});
