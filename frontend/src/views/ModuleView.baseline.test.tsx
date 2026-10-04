import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ModuleView } from './ModuleView';
import { artifactAPI } from '../api/client';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// A baseline is a frozen record: reading an artifact in it must offer nothing
// that writes to the live artifact of the same id (#379, bug 103).

const art = (id: string, ref: string, parent: string | null, type: string, title: string, version: number) => ({
  id,
  ref,
  project_id: 'p1',
  parent_id: parent,
  type,
  title,
  body: '',
  sort_order: 1,
  status: 'draft',
  attributes: {},
  version,
  valid_from: '2026-01-02T08:00:00Z',
  valid_to: null,
  created_at: '2026-01-02T08:00:00Z',
  updated_at: '2026-01-02T08:00:00Z',
});

const LIVE = [
  art('hdg-1', 'SEC-1', null, 'heading', 'Pumping', 1),
  art('req-1', 'REQ-1', 'hdg-1', 'requirement', 'The pump shall start within 2 seconds', 3),
];
// The baseline holds an older version of the same artifact.
const BASELINE = [
  art('hdg-1', 'SEC-1', null, 'heading', 'Pumping', 1),
  art('req-1', 'REQ-1', 'hdg-1', 'requirement', 'The pump shall start within 5 seconds', 2),
];

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: { get: () => ok({ id: 'p1', org_id: 'o1', name: 'Fuel pump', parent_project_id: '' }) },
    artifactAPI: {
      list: () => ok(JSON.parse(JSON.stringify(LIVE))),
      getVersions: () => ok([]),
    },
    linkAPI: {
      list: () => ok([]),
      listForArtifact: () => ok([]),
      listForArtifactVersion: () => ok([]),
    },
    baselineAPI: {
      list: () => ok([{ id: 'bl-1', project_id: 'p1', name: 'Design freeze', created_at: '2026-03-15T09:30:00Z' }]),
      get: () => ok({ project_id: 'p1', artifacts: JSON.parse(JSON.stringify(BASELINE)), links: [], attachments: [] }),
    },
    membersAPI: { list: () => ok([]) },
    metaAPI: {
      attributeDefinitions: () => ok([]),
      artifactTypes: () => ok([]),
      linkTypes: () => ok([]),
    },
  })
);

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

const choose = async (node: HTMLSelectElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
};

const byText = <T extends Element>(selector: string, text: string): T => {
  const found = Array.from(container.querySelectorAll<T>(selector)).find(
    (node) => (node.textContent ?? '').trim() === text
  );
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

// The buttons of the document pane, by their text.
const documentButtons = () =>
  Array.from(container.querySelectorAll('[aria-label="Artifact document"] button')).map((b) => (b.textContent ?? '').trim());

const WRITES = ['Edit', 'Delete', 'History', 'Submit for review', 'Return to draft', 'Approve', 'Mark superseded'];

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ ...initialStore, projectId: 'p1' }, true);
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

describe('ModuleView baseline document', () => {
  it('offers no action on the live artifact while a baseline is shown', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/projects/p1/requirements?artifact=req-1']}>
          <DialogProvider>
            <Routes>
              <Route path="/projects/:projectId/requirements" element={<ModuleView />} />
            </Routes>
          </DialogProvider>
        </MemoryRouter>
      );
    });
    await flush();

    // Live, the artifact's header carries its actions.
    expect(byText('h3', 'REQ-1The pump shall start within 2 seconds')).toBeTruthy();
    expect(documentButtons()).toEqual(expect.arrayContaining(['Edit', 'Delete', 'History', 'Submit for review']));

    // The same artifact as the baseline captured it: read only.
    await choose(container.querySelector<HTMLSelectElement>('select[title="Select baseline"]')!, 'bl-1');
    await click(byText('button', 'Expand all'));
    await click(byText('code', 'REQ-1'));
    expect(byText('h3', 'REQ-1The pump shall start within 5 seconds')).toBeTruthy();
    expect(container.textContent).toContain('Version 2');
    expect(documentButtons().filter((b) => WRITES.includes(b))).toEqual([]);
    expect(vi.mocked(artifactAPI.changeStatus)).not.toHaveBeenCalled();
    expect(vi.mocked(artifactAPI.delete)).not.toHaveBeenCalled();
  });
});
