import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { ModuleView } from './ModuleView';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// Refactor plan step S16a (invariant I21): the requirements module as it
// renders today, in its three modes, before F4 moves its filter engine out
// and F7 moves its toolbar, phone sheet, filter panel, tree pane and
// document header into views/moduleView/. F7 and X16 re-run it first; both
// must leave every file snapshot and every ordered API-call list identical.
//
//   desktop   1100px wide (window.innerWidth, which useViewport reads),
//             fine pointer. Deep-linked to ?artifact=req-1, with a search
//             term and two field filters typed into the filter panel
//             (version gt 1, the Number path; updated_at lt a date, the
//             Date.parse path; title gt "The P", the string path, which
//             compares lowercased), joined by AND. Under 1200px the notes column
//             starts auto-hidden, so ChatterPanel is not mounted.
//   phone     390px wide, coarse pointer (the matchMedia query useViewport
//             reads), so the module stacks into Tree / Document / Notes and
//             the toolbar folds into the ⋯ actions sheet, opened here. The
//             same deep link leaves the Tree pane showing.
//   baseline  desktop, with a baseline picked in the toolbar's baseline
//             select (the one way in), then a tree row clicked.
//   live      desktop, deep-linked, then another tree row clicked: the
//             API calls the click makes, with no file snapshot.
//
// Each mode writes container.innerHTML, one tag per line so a diff reads, to
// __snapshots__/ModuleView.<mode>.html, and asserts the ordered API calls the
// view and the children it mounts make, the URL it leaves and the state of
// its selects. The store is the real one; children render for real; only the
// API client is replaced, by canned answers.
//
// Regenerate only for a deliberate change to what the view shows or loads,
// never in a refactor pull request:
//   npx vitest run src/views/ModuleView.snapshot.test.tsx -u

// Every *API namespace of the client is replaced by recorders over a canned
// responder: the call is logged, in the order made, as `ns.method(args)`.
// URL builders are synchronous and do no I/O, so they keep the real code.
const recorder = vi.hoisted(() => ({
  calls: [] as string[],
  respond: (_name: string, _args: unknown[]): unknown => undefined,
}));

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  const out: Record<string, unknown> = { ...actual };
  for (const [ns, value] of Object.entries(actual)) {
    if (!ns.endsWith('API') || typeof value !== 'object' || value === null) continue;
    const wrapped: Record<string, unknown> = {};
    for (const [method, fn] of Object.entries(value as Record<string, unknown>)) {
      if (typeof fn !== 'function' || /Url$|URL$/.test(method)) {
        wrapped[method] = fn;
        continue;
      }
      wrapped[method] = (...args: unknown[]) => {
        const name = `${ns}.${method}`;
        recorder.calls.push(`${name}(${args.map((a) => JSON.stringify(a)).join(', ')})`);
        const data = recorder.respond(name, args);
        return data === undefined
          ? Promise.reject(new Error(`no canned response for ${name}`))
          : Promise.resolve(data).then((body) => ({ data: body, headers: {} }));
      };
    }
    out[ns] = wrapped;
  }
  return out;
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// ---- canned data -----------------------------------------------------------

const art = (
  id: string,
  ref: string,
  parent: string | null,
  order: number,
  type: string,
  title: string,
  version: number,
  updated: string,
  attributes: Record<string, unknown> = {}
) => ({
  id,
  ref,
  project_id: 'p1',
  parent_id: parent,
  type,
  title,
  body: `${title}.`,
  sort_order: order,
  status: 'draft',
  attributes,
  version,
  valid_from: updated,
  valid_to: null,
  created_at: '2026-01-02T08:00:00Z',
  updated_at: updated,
});

//  hdg-1 Pumping                         heading      v1
//    req-1 The pump shall start …        requirement  v3  2026-02-10  owner Acme
//    req-2 The pump shall log faults     requirement  v1  2026-01-05
//    req-3 The catalog shall list …      requirement  v2  2026-04-01
//  hdg-2 Safety                          heading      v1
//    req-4 The valve shall close …       requirement  v4  2026-01-20
//    tc-1  Verify the start time         test_case    v2  2026-02-12
//
// Desktop's filters keep "shall" AND version > 1 AND updated before
// 2026-03-01 AND a title after "the p": req-1 and req-4. Flipping any one
// comparator changes the set.
const LIVE = [
  art('hdg-1', 'SEC-1', null, 1, 'heading', 'Pumping', 1, '2026-01-02T08:00:00Z'),
  art('req-1', 'REQ-1', 'hdg-1', 1, 'requirement', 'The pump shall start within 2 seconds', 3, '2026-02-10T10:00:00Z', {
    owner: 'Acme',
  }),
  art('req-2', 'REQ-2', 'hdg-1', 2, 'requirement', 'The pump shall log faults', 1, '2026-01-05T10:00:00Z'),
  art('req-3', 'REQ-3', 'hdg-1', 3, 'requirement', 'The catalog shall list spare parts', 2, '2026-04-01T10:00:00Z'),
  art('hdg-2', 'SEC-2', null, 2, 'heading', 'Safety', 1, '2026-01-02T08:00:00Z'),
  art('req-4', 'REQ-4', 'hdg-2', 1, 'requirement', 'The valve shall close on overpressure', 4, '2026-01-20T10:00:00Z'),
  art('tc-1', 'TC-1', 'hdg-2', 2, 'test_case', 'Verify the start time', 2, '2026-02-12T10:00:00Z'),
];

// What the "Design freeze" baseline captured: an older req-1, no req-3.
const BASELINE = [
  art('hdg-1', 'SEC-1', null, 1, 'heading', 'Pumping', 1, '2026-01-02T08:00:00Z'),
  art('req-1', 'REQ-1', 'hdg-1', 1, 'requirement', 'The pump shall start within 5 seconds', 2, '2026-01-10T10:00:00Z'),
  art('req-2', 'REQ-2', 'hdg-1', 2, 'requirement', 'The pump shall log faults', 1, '2026-01-05T10:00:00Z'),
];

const link = (id: string, from: string, to: string) => ({
  id,
  from_id: from,
  to_id: to,
  type: 'verifies',
  suspect: false,
  attributes: {},
  version: 1,
  created_at: '2026-02-12T10:00:00Z',
  updated_at: '2026-02-12T10:00:00Z',
});

const LINKS = [link('lnk-1', 'tc-1', 'req-1')];

const BASELINES = [
  { id: 'bl-2', project_id: 'p1', name: 'Design freeze', created_at: '2026-03-15T09:30:00Z', created_by: 'u1', created_by_name: 'Dana Reyes' },
  { id: 'bl-1', project_id: 'p1', name: 'Kickoff', created_at: '2026-01-03T08:00:00Z' },
];

const score = (id: string, value: number, band: 'good' | 'fair' | 'poor', findings: number) => ({
  artifact_id: id,
  title: '',
  type: 'requirement',
  score: value,
  band,
  findings: Array.from({ length: findings }, (_, i) => ({
    rule: 'vague-term',
    severity: 'warning',
    message: `Finding ${i + 1}`,
    start: 0,
    end: 3,
    match: 'The',
  })),
});

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'artifactAPI.list': (projectId: string) =>
    projectId === 'p1'
      ? LIVE
      : [{ ...art('preq-1', 'SYS-1', null, 1, 'requirement', 'The vehicle shall pump fuel', 1, '2026-01-02T08:00:00Z'), project_id: 'p0' }],
  'artifactAPI.getVersions': () => [],
  'linkAPI.list': () => LINKS,
  'linkAPI.listForArtifact': (id: string) => LINKS.filter((l) => l.from_id === id || l.to_id === id),
  'linkAPI.listForArtifactVersion': () => [],
  'projectAPI.get': (id: string) =>
    id === 'p1'
      ? { id: 'p1', org_id: 'o1', name: 'Fuel pump', description: '', agent_auth: 'user-account', parent_project_id: 'p0', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }
      : { id: 'p0', org_id: 'o1', name: 'Vehicle', description: '', agent_auth: 'user-account', parent_project_id: '', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
  'projectAPI.linkedArtifacts': () => [],
  'baselineAPI.list': () => BASELINES,
  // Held until the test lets it through, so the loading pane can be read.
  'baselineAPI.get': (id: string) =>
    baselineGate.promise.then(() => ({
      exported_at: '2026-03-15T09:30:00Z',
      version: '1',
      project_id: 'p1',
      project_name: `Fuel pump (${id})`,
      project_description: '',
      artifacts: BASELINE,
      links: [],
      attachments: [],
    })),
  'qualityAPI.project': () => ({
    project_id: 'p1',
    entries: [score('req-1', 82, 'good', 1), score('req-4', 40, 'poor', 3)],
    summary: { good: 1, poor: 1 },
  }),
  'qualityAPI.artifact': (id: string) => score(id, 82, 'good', 1),
  'attachmentAPI.listByProject': () => [],
  'attachmentAPI.listByArtifact': () => [],
};

const deferred = () => {
  let release = () => {};
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { promise, release };
};
let baselineGate = deferred();

recorder.respond = (name, args) => CANNED[name]?.(...args);

// Filter presets the panel lists, as the browser would have kept them.
const PRESETS = [
  {
    name: 'Long-lived',
    data: JSON.stringify({ searchText: '', searchExact: false, filterLogic: 'and', filterRows: [{ id: 'p-1', field: 'version', value: '3', comparator: 'gt' }] }),
  },
];

// ---- harness ---------------------------------------------------------------

const initialStore = useAppStore.getState();
const COARSE = '(hover: none) and (pointer: coarse)';
const realInnerWidth = Object.getOwnPropertyDescriptor(window, 'innerWidth');
const realMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia');

let container: HTMLDivElement;
let root: Root;
let location = '';

// Records where the view leaves the URL: ?artifact= is the selection's
// shareable source of truth.
const LocationProbe: React.FC = () => {
  const here = useLocation();
  location = `${here.pathname}${here.search}`;
  return null;
};

const setViewport = (width: number, coarse: boolean) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: width });
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: (query: string) => ({
      matches: coarse && query === COARSE,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
};

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async (entry: string) => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[entry]}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/requirements" element={<ModuleView />} />
          </Routes>
        </DialogProvider>
        <LocationProbe />
      </MemoryRouter>
    );
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

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

// React tracks a control's value itself, so the value goes through the
// native setter and then the event React listens for.
const setValue = async (node: HTMLInputElement | HTMLSelectElement, value: string) => {
  const proto = node instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  await act(async () => {
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event(node instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
  await flush();
};

// The snapshot is container.innerHTML with a line break put between adjacent
// tags ("><"); text never holds that pair, because the serializer escapes it.
const snapshot = (mode: string) =>
  expect(container.innerHTML.replace(/></g, '>\n<') + '\n').toMatchFileSnapshot(
    `./__snapshots__/ModuleView.${mode}.html`
  );

// innerHTML does not carry a <select>'s current choice or a checkbox's state
// (React sets the properties), so the form controls are read beside it.
const controls = () =>
  Array.from(container.querySelectorAll<HTMLInputElement | HTMLSelectElement>('select, input[type="checkbox"]')).map(
    (node) => (node instanceof HTMLInputElement ? `checkbox:${node.checked}` : `select:${node.value}`)
  );

beforeEach(() => {
  recorder.calls = [];
  baselineGate = deferred();
  location = '';
  // Dates print through toLocale*String, which follows the machine's zone
  // and locale; ISO text keeps the snapshot the same everywhere. The clock
  // is frozen too (a new filter row's id is Date.now()); timers stay real.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-06-01T12:00:00Z'));
  vi.spyOn(Date.prototype, 'toLocaleString').mockImplementation(function (this: Date) {
    return this.toISOString();
  });
  vi.spyOn(Date.prototype, 'toLocaleDateString').mockImplementation(function (this: Date) {
    return this.toISOString().slice(0, 10);
  });
  vi.spyOn(Date.prototype, 'toLocaleTimeString').mockImplementation(function (this: Date) {
    return this.toISOString().slice(11, 19);
  });
  window.localStorage.clear();
  window.localStorage.setItem('artifactFilterPresets', JSON.stringify(PRESETS));
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      features: {
        channel: 'nightly',
        stable_release: '',
        preview: false,
        features: { 'artifact-owners': true, 'artifact-stepping': true },
      },
    },
    true
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
  useAppStore.setState(initialStore, true);
  window.localStorage.clear();
  vi.restoreAllMocks();
  vi.useRealTimers();
  for (const [key, real] of [
    ['innerWidth', realInnerWidth],
    ['matchMedia', realMatchMedia],
  ] as const) {
    if (real) Object.defineProperty(window, key, real);
    else delete (window as any)[key];
  }
});

// The load every mode starts with, in the order it is made: the
// parent-project lookup (its effect is declared above the load effect), the
// five loads of the load effect, the selection's figures once the ?artifact=
// sync has set it, the parent project and its requirements once p1 names its
// parent, and the cross-project link ends after the links.
const LOAD = [
  'projectAPI.get("p1")',
  'artifactAPI.list("p1")',
  'linkAPI.list("p1")',
  'baselineAPI.list("p1")',
  'qualityAPI.project("p1")',
  'attachmentAPI.listByProject("p1")',
];
const PARENT_AND_LINKED = [
  'projectAPI.get("p0")',
  'artifactAPI.list("p0", "requirement")',
  'projectAPI.linkedArtifacts("p1")',
];
// What the document pane's header and details load for live req-1 (v3).
const LIVE_DOCUMENT = [
  'artifactAPI.getVersions("req-1")',
  'qualityAPI.artifact("req-1")',
  'linkAPI.listForArtifact("req-1")',
];
// The same for the baseline's req-1 (v2): links come from the version.
const BASELINE_DOCUMENT = [
  'artifactAPI.getVersions("req-1")',
  'qualityAPI.artifact("req-1")',
  'linkAPI.listForArtifactVersion("req-1", 2)',
];

describe('ModuleView characterization (S16a)', () => {
  it('desktop: deep link, search and gt/lt field filters', async () => {
    setViewport(1100, false);
    await mount('/projects/p1/requirements?artifact=req-1');

    await click(container.querySelector('button[title="Toggle filters"]')!);
    const comparatorSelects = () =>
      Array.from(container.querySelectorAll<HTMLSelectElement>('select')).filter((s) =>
        Array.from(s.options).some((o) => o.value === 'gt')
      );
    const fieldSelects = () =>
      Array.from(container.querySelectorAll<HTMLSelectElement>('select')).filter((s) =>
        Array.from(s.options).some((o) => o.value === 'updated_at')
      );
    const valueInputs = () =>
      Array.from(container.querySelectorAll<HTMLInputElement>('input[placeholder="Contains..."]'));

    await setValue(fieldSelects()[0], 'version');
    await setValue(comparatorSelects()[0], 'gt');
    await setValue(valueInputs()[0], '1');
    await click(byText('button', '+ Add filter'));
    await setValue(fieldSelects()[1], 'updated_at');
    await setValue(comparatorSelects()[1], 'lt');
    await setValue(valueInputs()[1], '2026-03-01');
    // A new row's id is Date.now(), so the frozen clock moves on first: two
    // rows added in one millisecond would share an id, and an edit to one
    // would land on both.
    vi.setSystemTime(Date.now() + 1000);
    await click(byText('button', '+ Add filter'));
    await setValue(fieldSelects()[2], 'title');
    await setValue(comparatorSelects()[2], 'gt');
    await setValue(valueInputs()[2], 'The P');
    await setValue(container.querySelector<HTMLInputElement>('input[placeholder="Search..."]')!, 'shall');

    expect(recorder.calls).toEqual([
      ...LOAD,
      'attachmentAPI.listByArtifact("req-1")',
      ...PARENT_AND_LINKED,
      ...LIVE_DOCUMENT,
    ]);
    expect(location).toBe('/projects/p1/requirements?artifact=req-1');
    expect(controls()).toEqual([
      'select:live',
      'checkbox:false',
      'select:and',
      'select:version',
      'select:gt',
      'select:updated_at',
      'select:lt',
      'select:title',
      'select:gt',
      'select:',
    ]);
    await snapshot('desktop');
  });

  it('phone: stacked panes and the actions sheet', async () => {
    setViewport(390, true);
    await mount('/projects/p1/requirements?artifact=req-1');
    await click(container.querySelector('button[aria-label="Requirements actions"]')!);

    expect(recorder.calls).toEqual([
      ...LOAD,
      'attachmentAPI.listByArtifact("req-1")',
      ...PARENT_AND_LINKED,
      ...LIVE_DOCUMENT,
    ]);
    expect(location).toBe('/projects/p1/requirements?artifact=req-1');
    expect(controls()).toEqual(['select:live']);
    await snapshot('phone');
  });

  it('baseline: picked in the toolbar, then a row selected', async () => {
    setViewport(1100, false);
    await mount('/projects/p1/requirements');
    await setValue(container.querySelector<HTMLSelectElement>('select[title="Select baseline"]')!, 'bl-2');
    // While the export is on its way the tree pane says so.
    expect(container.querySelector('[role="status"]')?.textContent).toBe(
      'Loading baseline…A baseline holds the whole project, so this can take a moment.'
    );
    baselineGate.release();
    await flush();
    // Nothing is selected yet, so the document pane holds its placeholder.
    expect(container.querySelector('[aria-label="Artifact document"]')?.textContent).toBe(
      'No Artifact SelectedSelect an artifact from the list to view details.'
    );
    // The baseline's tree arrives collapsed (the live one did not: its first
    // render had no artifacts to collapse), so open it to reach a row.
    await click(byText('button', 'Expand all'));
    await click(byText('code', 'REQ-1'));

    expect(recorder.calls).toEqual([
      ...LOAD,
      ...PARENT_AND_LINKED,
      'baselineAPI.get("bl-2")',
      // A row click loads the document once: the click writes ?artifact=
      // and the selection follows the URL (#379, bug 102; it loaded twice,
      // the selection set, cleared by the sync effect and set again).
      ...BASELINE_DOCUMENT,
    ]);
    expect(location).toBe('/projects/p1/requirements?artifact=req-1');
    expect(controls()).toEqual(['select:bl-2']);
    await snapshot('baseline');
  });

  // The live tree, the same way (#379, bug 102): one click, one load of the
  // document and its figures. It used to load req-4, then req-1 again, then
  // req-4. No file snapshot; the desktop mode has one.
  it('live: a tree row click loads the document once', async () => {
    setViewport(1100, false);
    await mount('/projects/p1/requirements?artifact=req-1');
    recorder.calls = [];
    await click(byText('code', 'REQ-4'));

    expect(recorder.calls).toEqual([
      'artifactAPI.getVersions("req-4")',
      'qualityAPI.artifact("req-4")',
      'linkAPI.listForArtifact("req-4")',
      'attachmentAPI.listByArtifact("req-4")',
    ]);
    expect(location).toBe('/projects/p1/requirements?artifact=req-4');
  });
});
