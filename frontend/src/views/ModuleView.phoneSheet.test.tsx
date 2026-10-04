import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ModuleView } from './ModuleView';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// On a phone the requirements toolbar folds into the ⋯ actions sheet, and
// every item in it is a one-shot action: tapping one closes the sheet, while
// a tap on the baseline picker, which is no action, leaves it open. The
// sheet closes on a click that lands inside a <button>, so an item turned
// into a <div role="button"> would leave it open (#379, test gap 114).

const art = (id: string, ref: string, parent: string | null, type: string, title: string) => ({
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
  version: 1,
  valid_from: '2026-01-02T08:00:00Z',
  valid_to: null,
  created_at: '2026-01-02T08:00:00Z',
  updated_at: '2026-01-02T08:00:00Z',
});

const LIVE = [
  art('hdg-1', 'SEC-1', null, 'heading', 'Pumping'),
  art('req-1', 'REQ-1', 'hdg-1', 'requirement', 'The pump shall start within 2 seconds'),
];

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: { get: () => ok({ id: 'p1', org_id: 'o1', name: 'Fuel pump', parent_project_id: '' }) },
    artifactAPI: { list: () => ok(JSON.parse(JSON.stringify(LIVE))), getVersions: () => ok([]) },
    linkAPI: { list: () => ok([]), listForArtifact: () => ok([]) },
    baselineAPI: { list: () => ok([]) },
    membersAPI: { list: () => ok([]) },
    metaAPI: {
      attributeDefinitions: () => ok([]),
      artifactTypes: () => ok([]),
      linkTypes: () => ok([]),
    },
  })
);

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 390, cls: 'phone', isPhone: true, isCompact: true, coarsePointer: true }),
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

const sheet = () => container.querySelector('.action-sheet');
const opener = () => container.querySelector<HTMLButtonElement>('button[aria-label="Requirements actions"]')!;

// An item of the sheet as a person sees it: anything a screen reader calls
// a button, by its text.
const item = (text: string) => {
  const found = Array.from(sheet()!.querySelectorAll('button, [role="button"]')).find(
    (node) => (node.textContent ?? '').trim() === text
  );
  expect(found, `sheet item "${text}"`).toBeTruthy();
  return found!;
};

const openSheet = async () => {
  await click(opener());
  expect(sheet()).not.toBeNull();
  expect(opener().getAttribute('aria-expanded')).toBe('true');
};

beforeEach(async () => {
  vi.clearAllMocks();
  useAppStore.setState({ ...initialStore, projectId: 'p1' }, true);
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/requirements']}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/requirements" element={<ModuleView />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

describe('ModuleView phone actions sheet', () => {
  it.each(['↓ Download', 'Capture Baseline'])('closes when "%s" is tapped', async (text) => {
    await openSheet();
    await click(item(text));
    expect(sheet()).toBeNull();
    expect(opener().getAttribute('aria-expanded')).toBe('false');
  });

  it('stays open for a tap on the baseline picker, which is no action', async () => {
    await openSheet();
    await click(sheet()!.querySelector('select[title="Select baseline"]')!);
    expect(sheet()).not.toBeNull();
  });
});
