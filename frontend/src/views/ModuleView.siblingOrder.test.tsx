import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { ModuleView } from './ModuleView';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// Characterization for refactor plan step X16b (useSiblingOrdering): how the
// requirements module saves a sibling group's order, pinned before the logic
// leaves ModuleView. Each path renumbers the group 1..n and saves only the
// artifacts whose number changed (and a re-parented one), one
// artifactAPI.update per artifact, all started at once, in group order:
//
//   ▲ / ▼         handleReorderArtifact('swap'): the group in compareArtifacts
//                 order (sort_order when set, oldest first when not, ordered
//                 rows before unordered), two rows swapped
//   Move to…      handleReorderArtifact(zone): planMove, whose group is in
//                 sort_order-then-title order, the moved row placed before,
//                 after or inside the target
//   Create before / after, Paste before / after, Duplicate
//                 placeAmongSiblings: the group in sort_order order alone, a
//                 stable sort that keeps the store's order for a tie, the new
//                 artifact put beside the anchor
//
// The canned group below is drawn in a different order by each of those
// three comparators, so a path that changed comparator changes its calls:
//
//   hdg-1 Pumping
//     req-b Bravo    sort 2  created 01-03
//     req-a Alpha    sort 1  created 01-04
//     req-d Delta    sort 0  created 01-01
//     req-c Charlie  sort 0  created 01-02
//   hdg-2 Safety     (no children)
//
//   compareArtifacts   Alpha, Bravo, Delta, Charlie   (the tree's order)
//   planMove           Charlie, Delta, Alpha, Bravo
//   placeAmongSiblings Delta, Charlie, Alpha, Bravo
//
// The store is the real one; children render for real; only the API client
// is replaced, by recorders over canned answers, as in S16a.

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
  created: string
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
  attributes: {},
  version: 1,
  valid_from: created,
  valid_to: null,
  created_at: created,
  updated_at: created,
});

const LIVE = [
  art('hdg-1', 'SEC-1', null, 1, 'heading', 'Pumping', '2026-01-01T00:00:00Z'),
  art('req-b', 'REQ-B', 'hdg-1', 2, 'requirement', 'Bravo', '2026-01-03T00:00:00Z'),
  art('req-a', 'REQ-A', 'hdg-1', 1, 'requirement', 'Alpha', '2026-01-04T00:00:00Z'),
  art('req-d', 'REQ-D', 'hdg-1', 0, 'requirement', 'Delta', '2026-01-01T00:00:00Z'),
  art('req-c', 'REQ-C', 'hdg-1', 0, 'requirement', 'Charlie', '2026-01-02T00:00:00Z'),
  art('hdg-2', 'SEC-2', null, 2, 'heading', 'Safety', '2026-01-01T00:00:00Z'),
];

// What one artifactAPI.update sends for a renumbered artifact: its parent,
// type, title, body and attributes as they were, and its new place.
// A pasted copy is sent under its own title and body, named by `as`.
const update = (id: string, parent: string, order: number, as?: { title: string; body: string }) => {
  const a = LIVE.find((x) => x.id === id) ?? CREATED;
  return `artifactAPI.update(${JSON.stringify(id)}, ${JSON.stringify({
    parent_id: parent,
    type: a.type,
    title: as?.title ?? a.title,
    body: as?.body ?? a.body,
    attributes: a.attributes,
    sort_order: order,
  })})`;
};

// What the create form sends for a requirement titled Echo, under `parent`.
const createEcho = (parent: string, type: string) =>
  `artifactAPI.create(${JSON.stringify({
    project_id: 'p1',
    type,
    title: 'Echo',
    body: '',
    attributes: {},
    parent_id: parent,
    pendingLinkAdds: [],
    pendingLinkRemoves: [],
  })})`;
// Echo as the server answers it: the form left its body empty.
const ECHO = { title: 'Echo', body: '' };

// A new artifact: the server appends it to its parent's children, past the
// highest sort order there.
const CREATED = art('req-e', 'REQ-E', 'hdg-1', 5, 'requirement', 'Echo', '2026-06-01T12:00:00Z');

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'projectAPI.get': () => ({ id: 'p1', org_id: 'o1', name: 'Fuel pump', description: '', agent_auth: 'user-account', parent_project_id: '', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }),
  'artifactAPI.list': () => LIVE,
  'linkAPI.list': () => [],
  'projectAPI.linkedArtifacts': () => [],
  'baselineAPI.list': () => [],
  'qualityAPI.project': () => ({ project_id: 'p1', entries: [], summary: {} }),
  'attachmentAPI.listByProject': () => [],
  'attachmentAPI.listByArtifact': () => [],
  'artifactAPI.getVersions': () => [],
  'qualityAPI.artifact': (id: string) => ({ artifact_id: id, title: '', type: 'requirement', score: 90, band: 'good', findings: [] }),
  'linkAPI.listForArtifact': () => [],
  // The server's answer to an update: the artifact with what was sent.
  'artifactAPI.update': (id: string, data: Record<string, unknown>) => ({
    ...(LIVE.find((a) => a.id === id) ?? CREATED),
    ...data,
  }),
  'artifactAPI.create': (data: { parent_id: string | null; type: string; title: string; body: string }) => ({
    ...CREATED,
    parent_id: data.parent_id,
    type: data.type,
    title: data.title,
    body: data.body,
  }),
};

const canned = (name: string, args: unknown[]) => CANNED[name]?.(...args);

// The project load every test starts from, with no artifact selected.
const LOAD = [
  'projectAPI.get("p1")',
  'artifactAPI.list("p1")',
  'linkAPI.list("p1")',
  'baselineAPI.list("p1")',
  'qualityAPI.project("p1")',
  'attachmentAPI.listByProject("p1")',
  'projectAPI.linkedArtifacts("p1")',
];

// ---- harness ---------------------------------------------------------------

const initialStore = useAppStore.getState();
const realInnerWidth = Object.getOwnPropertyDescriptor(window, 'innerWidth');
const realMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia');

let container: HTMLDivElement;
let root: Root;
let location = '';

const LocationProbe: React.FC = () => {
  const here = useLocation();
  location = `${here.pathname}${here.search}`;
  return null;
};

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/requirements']}>
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

const setValue = async (node: HTMLInputElement | HTMLSelectElement, value: string) => {
  const proto = node instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  await act(async () => {
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event(node instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
  await flush();
};

const button = (label: string) => {
  const found = container.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`);
  expect(found, `button "${label}"`).toBeTruthy();
  return found!;
};

// A row's context menu, opened as a right click on its ref.
const menu = async (ref: string, item: string) => {
  await act(async () => {
    byText('code', ref).dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 10, clientY: 10 }));
  });
  await flush();
  await click(byText('button', item));
};

// The tree's rows top to bottom, by ref: the document pane's codes are not
// the tree's.
const tree = () =>
  Array.from(container.querySelectorAll('code'))
    .filter((node) => !node.closest('[aria-label="Artifact document"]'))
    .map((node) => node.textContent);

// The create form, filled with a title and submitted.
const createTitled = async (title: string) => {
  await setValue(container.querySelector<HTMLInputElement>('input[placeholder="Enter artifact title"]')!, title);
  await click(byText('button', 'Create'));
};

const alertText = () => container.querySelector('[role="alert"]')?.textContent ?? '';

beforeEach(async () => {
  recorder.calls = [];
  recorder.respond = canned;
  location = '';
  // Under 1200px the notes column starts auto-hidden, so ChatterPanel and its
  // loads stay out of the calls.
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 1100 });
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
  window.localStorage.clear();
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      features: { channel: 'nightly', stable_release: '', preview: false, features: {} },
    },
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  await mount();
  expect(recorder.calls).toEqual(LOAD);
  expect(tree()).toEqual(['SEC-1', 'REQ-A', 'REQ-B', 'REQ-D', 'REQ-C', 'SEC-2']);
  recorder.calls = [];
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
  window.localStorage.clear();
  for (const [key, real] of [
    ['innerWidth', realInnerWidth],
    ['matchMedia', realMatchMedia],
  ] as const) {
    if (real) Object.defineProperty(window, key, real);
    else delete (window as any)[key];
  }
});

describe('ModuleView sibling ordering (X16b characterization)', () => {
  describe('▲ and ▼ swap two rows in the tree order', () => {
    it('renumbers the whole group, unordered rows included', async () => {
      await click(button('Move Alpha down'));

      // Alpha, Bravo, Delta, Charlie with the first two swapped.
      expect(recorder.calls).toEqual([
        update('req-b', 'hdg-1', 1),
        update('req-a', 'hdg-1', 2),
        update('req-d', 'hdg-1', 3),
        update('req-c', 'hdg-1', 4),
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-B', 'REQ-A', 'REQ-D', 'REQ-C', 'SEC-2']);
      expect(location).toBe('/projects/p1/requirements');
      expect(alertText()).toBe('');
    });

    it('saves only the rows whose number changed', async () => {
      await click(button('Move Delta up'));

      // Alpha keeps 1, so it is not sent.
      expect(recorder.calls).toEqual([
        update('req-d', 'hdg-1', 2),
        update('req-b', 'hdg-1', 3),
        update('req-c', 'hdg-1', 4),
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-A', 'REQ-D', 'REQ-B', 'REQ-C', 'SEC-2']);
    });

    it('keeps the tree as it was and says so when a save fails', async () => {
      recorder.respond = (name, args) => (name === 'artifactAPI.update' ? undefined : canned(name, args));
      await click(button('Move Alpha down'));

      // Every update is started before any answer comes back.
      expect(recorder.calls).toEqual([
        update('req-b', 'hdg-1', 1),
        update('req-a', 'hdg-1', 2),
        update('req-d', 'hdg-1', 3),
        update('req-c', 'hdg-1', 4),
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-A', 'REQ-B', 'REQ-D', 'REQ-C', 'SEC-2']);
      expect(alertText()).toContain('Failed to move artifacts: no canned response for artifactAPI.update');
    });
  });

  describe('Move to… places a row in sort-order-then-title order', () => {
    const moveTo = async (title: string, targetId: string, zone: 'before' | 'after' | 'child') => {
      await click(button(`Actions for ${title}`));
      await click(byText('button', '↕️ Move to…'));
      await setValue(container.querySelector<HTMLSelectElement>('#move-target')!, targetId);
      await click(container.querySelector(`input[name="move-zone"][value="${zone}"]`)!);
      await click(byText('button', 'Move'));
    };

    it('after a sibling', async () => {
      await moveTo('Alpha', 'req-b', 'after');

      // Charlie, Delta, Bravo, then Alpha.
      expect(recorder.calls).toEqual([
        update('req-c', 'hdg-1', 1),
        update('req-d', 'hdg-1', 2),
        update('req-b', 'hdg-1', 3),
        update('req-a', 'hdg-1', 4),
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-C', 'REQ-D', 'REQ-B', 'REQ-A', 'SEC-2']);
    });

    it('before a sibling', async () => {
      await moveTo('Bravo', 'req-d', 'before');

      // Charlie, then Bravo before Delta, then Alpha. Bravo keeps 2, so it
      // is not sent.
      expect(recorder.calls).toEqual([
        update('req-c', 'hdg-1', 1),
        update('req-d', 'hdg-1', 3),
        update('req-a', 'hdg-1', 4),
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-C', 'REQ-B', 'REQ-D', 'REQ-A', 'SEC-2']);
    });

    it('inside another parent, saving the moved row even where its number stays', async () => {
      await moveTo('Alpha', 'hdg-2', 'child');

      // Alpha is Safety's only child at 1, the number it had: it is sent for
      // its new parent. The group it left is not renumbered, and draws
      // Bravo, the one numbered row left, before the unnumbered ones.
      expect(recorder.calls).toEqual([update('req-a', 'hdg-2', 1)]);
      expect(tree()).toEqual(['SEC-1', 'REQ-B', 'REQ-D', 'REQ-C', 'SEC-2', 'REQ-A']);
    });
  });

  describe('a new artifact goes beside the row it was made from, in sort-order order', () => {
    it('Create before', async () => {
      await menu('REQ-B', '➕ Create before');
      recorder.calls = [];
      await createTitled('Echo');

      // Delta, Charlie (the store's order for their tie), Alpha, Echo, Bravo.
      expect(recorder.calls).toEqual([
        createEcho('hdg-1', 'requirement'),
        update('req-d', 'hdg-1', 1),
        update('req-c', 'hdg-1', 2),
        update('req-a', 'hdg-1', 3),
        update('req-e', 'hdg-1', 4, ECHO),
        update('req-b', 'hdg-1', 5),
        'qualityAPI.project("p1")',
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-D', 'REQ-C', 'REQ-A', 'REQ-E', 'REQ-B', 'SEC-2']);
      // The form closes.
      expect(container.querySelector('input[placeholder="Enter artifact title"]')).toBeNull();
    });

    it('Create after leaves the new artifact where the server put it when that is its place', async () => {
      await menu('REQ-B', '➕ Create after');
      recorder.calls = [];
      await createTitled('Echo');

      // Echo is fifth and already 5, so it is not sent.
      expect(recorder.calls).toEqual([
        createEcho('hdg-1', 'requirement'),
        update('req-d', 'hdg-1', 1),
        update('req-c', 'hdg-1', 2),
        update('req-a', 'hdg-1', 3),
        update('req-b', 'hdg-1', 4),
        'qualityAPI.project("p1")',
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-D', 'REQ-C', 'REQ-A', 'REQ-B', 'REQ-E', 'SEC-2']);
    });

    it('Create new child renumbers nothing', async () => {
      await menu('SEC-2', '➕ Create new child');
      recorder.calls = [];
      await createTitled('Echo');

      // A child takes its parent's type in the form.
      expect(recorder.calls).toEqual([createEcho('hdg-2', 'heading'), 'qualityAPI.project("p1")']);
      expect(tree()).toEqual(['SEC-1', 'REQ-A', 'REQ-B', 'REQ-D', 'REQ-C', 'SEC-2', 'REQ-E']);
    });

    it('a form opened by hand forgets the placement a context menu asked for', async () => {
      await menu('REQ-B', '➕ Create before');
      await click(byText('button', 'Cancel'));
      await click(byText('button', '+ New Artifact'));
      recorder.calls = [];
      await createTitled('Echo');

      // The form opened by hand has no parent and the default type.
      expect(recorder.calls).toHaveLength(2);
      expect(recorder.calls[0]).toMatch(/^artifactAPI\.create\(\{"project_id":"p1",.*"title":"Echo"/);
      expect(recorder.calls[1]).toBe('qualityAPI.project("p1")');
    });

    it('a placement that fails to save leaves the form open and says so', async () => {
      await menu('REQ-B', '➕ Create before');
      recorder.respond = (name, args) => (name === 'artifactAPI.update' ? undefined : canned(name, args));
      recorder.calls = [];
      await createTitled('Echo');

      expect(recorder.calls).toEqual([
        createEcho('hdg-1', 'requirement'),
        update('req-d', 'hdg-1', 1),
        update('req-c', 'hdg-1', 2),
        update('req-a', 'hdg-1', 3),
        update('req-e', 'hdg-1', 4, ECHO),
        update('req-b', 'hdg-1', 5),
      ]);
      expect(alertText()).toContain('Failed to create artifact: no canned response for artifactAPI.update');
      expect(container.querySelector('input[placeholder="Enter artifact title"]')).not.toBeNull();
      // The store took the new artifact when the create answered, at the 5
      // the server gave it, so the tree draws it after Bravo and before the
      // unnumbered rows.
      expect(tree()).toEqual(['SEC-1', 'REQ-A', 'REQ-B', 'REQ-E', 'REQ-D', 'REQ-C', 'SEC-2']);
    });

    it('Paste before', async () => {
      await menu('REQ-C', '📋 Copy');
      await menu('REQ-B', '📥 Paste before');

      expect(recorder.calls).toEqual([
        'artifactAPI.create({"project_id":"p1","parent_id":"hdg-1","type":"requirement","title":"Charlie (copy)","body":"Charlie.","attributes":{},"copied_from":"req-c"})',
        update('req-d', 'hdg-1', 1),
        update('req-c', 'hdg-1', 2),
        update('req-a', 'hdg-1', 3),
        update('req-e', 'hdg-1', 4, { title: 'Charlie (copy)', body: 'Charlie.' }),
        update('req-b', 'hdg-1', 5),
        'qualityAPI.project("p1")',
        // The copy is selected, and its document loads.
        'qualityAPI.artifact("req-e")',
        'linkAPI.listForArtifact("req-e")',
        'attachmentAPI.listByArtifact("req-e")',
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-D', 'REQ-C', 'REQ-A', 'REQ-E', 'REQ-B', 'SEC-2']);
      expect(location).toBe('/projects/p1/requirements?artifact=req-e');
    });

    it('Duplicate puts the copy right after the original', async () => {
      await menu('REQ-D', '🧬 Duplicate');

      expect(recorder.calls).toEqual([
        'artifactAPI.create({"project_id":"p1","parent_id":"hdg-1","type":"requirement","title":"Delta (copy)","body":"Delta.","attributes":{},"copied_from":"req-d"})',
        update('req-d', 'hdg-1', 1),
        update('req-e', 'hdg-1', 2, { title: 'Delta (copy)', body: 'Delta.' }),
        update('req-c', 'hdg-1', 3),
        update('req-a', 'hdg-1', 4),
        update('req-b', 'hdg-1', 5),
        'qualityAPI.project("p1")',
        'qualityAPI.artifact("req-e")',
        'linkAPI.listForArtifact("req-e")',
        'attachmentAPI.listByArtifact("req-e")',
      ]);
      expect(tree()).toEqual(['SEC-1', 'REQ-D', 'REQ-E', 'REQ-C', 'REQ-A', 'REQ-B', 'SEC-2']);
      expect(location).toBe('/projects/p1/requirements?artifact=req-e');
    });
  });
});
