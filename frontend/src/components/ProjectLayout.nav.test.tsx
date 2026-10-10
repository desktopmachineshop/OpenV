import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectLayout } from './ProjectLayout';
import { DialogProvider } from './ui';
import { useAppStore } from '../state/store';
import { guidedAPI, type GuidedSession } from '../api/client';

// The project menu, as a person sees it (refactor plan X17's characterization).
//
// The menu's list of destinations is a private constant of ProjectLayout, so
// this renders the layout and reads the menu back: which groups there are and
// in which order, each entry's label and link in order, which entries a
// feature gate or a guided definition hides, which group opens by itself, and
// which entry is marked as the page being viewed. A change in how the list is
// built (a registry in place of the literal, say) must leave every assertion
// here as it is.

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: { get: () => ok({ id: 'p1', name: 'Pump controller', org_id: 'o1' }) },
    guidedAPI: { list: () => ok([]) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// The notification bell in the sidebar opens an event stream; the menu does
// not care what arrives on it.
class SilentEventSource {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  addEventListener() {}
  removeEventListener() {}
  close() {}
}
(globalThis as any).EventSource = SilentEventSource;

const initialStore = useAppStore.getState();

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const features = (flags: Record<string, boolean> | null) =>
  flags === null
    ? null
    : { channel: 'nightly' as const, stable_release: '', preview: false, features: flags };

const mount = async (
  path: string,
  opts: { flags?: Record<string, boolean> | null; sessions?: Partial<GuidedSession>[] } = {}
) => {
  vi.mocked(guidedAPI.list).mockImplementation(() => ok(opts.sessions ?? []) as never);
  useAppStore.setState(
    { ...initialStore, features: features(opts.flags === undefined ? {} : opts.flags) } as never,
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/projects/:projectId" element={<ProjectLayout />}>
              <Route path="*" element={<div>page</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </DialogProvider>
    );
  });
  await flush();
};

const nav = (): HTMLElement => {
  const found = document.body.querySelector('nav');
  expect(found, 'the project menu').toBeTruthy();
  return found as HTMLElement;
};

interface Group {
  /** The header's label, or null for the group with no header. */
  header: string | null;
  /** Whether the header says the group is expanded; null with no header. */
  expanded: boolean | null;
  /** Each shown entry as "label -> href". */
  links: string[];
}

// The menu's groups in order. A group's entries are the NavLinks that follow
// its header inside the same block, so a collapsed group lists none.
const groups = (): Group[] => {
  const blocks = Array.from(nav().children).filter(
    (el) => el.tagName === 'DIV' && !el.getAttribute('style')?.includes('border-top')
  );
  return blocks.map((block) => {
    const button = block.querySelector(':scope > button');
    return {
      header: button ? (button.textContent ?? '').replace(/^[▾▸]/, '').trim() : null,
      expanded: button ? button.getAttribute('aria-expanded') === 'true' : null,
      links: Array.from(block.querySelectorAll(':scope > a')).map(
        (a) => `${(a.textContent ?? '').trim()} -> ${a.getAttribute('href')}`
      ),
    };
  });
};

const current = (): string[] =>
  Array.from(nav().querySelectorAll('a[aria-current="page"]')).map((a) => (a.textContent ?? '').trim());

// Open every group the way a returning reader has: the stored choice beats the
// default, so this shows the whole list whichever page is viewed.
const expandAll = () => {
  localStorage.setItem(
    'openv-nav-sections',
    JSON.stringify({ Define: true, Verify: true, Plan: true, Agents: true })
  );
};

const FULL_MENU: Group[] = [
  {
    header: 'Define',
    expanded: true,
    links: [
      'Overview -> /projects/p1',
      'Guided Definition -> /projects/p1/guided',
      'Requirements -> /projects/p1/requirements',
      'Interviews -> /projects/p1/interviews',
    ],
  },
  {
    header: 'Verify',
    expanded: true,
    links: [
      'V&V -> /projects/p1/vv',
      'Evidence -> /projects/p1/evidence',
      'Traceability -> /projects/p1/matrix',
      'Impact -> /projects/p1/impact',
      'Review Queue -> /projects/p1/review',
    ],
  },
  { header: 'Plan', expanded: true, links: ['Board -> /projects/p1/board'] },
  {
    header: 'Agents',
    expanded: true,
    links: [
      'Agents -> /projects/p1/agents',
      'Crew -> /projects/p1/crew',
      'Automations -> /projects/p1/automations',
      'Runs -> /projects/p1/agent-runs',
    ],
  },
  {
    header: null,
    expanded: null,
    links: ['Activity -> /projects/p1/activity', 'Settings -> /projects/p1/settings'],
  },
];

const withTodos = (): Group[] =>
  FULL_MENU.map((g) =>
    g.header === 'Plan'
      ? { ...g, links: ['Board -> /projects/p1/board', 'To-dos -> /projects/p1/todos'] }
      : g
  );

const withoutGuided = (): Group[] =>
  FULL_MENU.map((g) =>
    g.header === 'Define'
      ? { ...g, links: g.links.filter((l) => !l.startsWith('Guided Definition')) }
      : g
  );

beforeEach(() => {
  localStorage.clear();
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  localStorage.clear();
  useAppStore.setState(initialStore, true);
});

describe('the project menu: what it lists', () => {
  it('lists every destination in order, in four labelled groups and an unlabelled tail', async () => {
    expandAll();
    await mount('/projects/p1');
    expect(groups()).toEqual(FULL_MENU);
  });

  it('leaves the to-do list out until its feature is on for the workspace', async () => {
    expandAll();
    await mount('/projects/p1', { flags: { 'todo-list': false } });
    expect(groups()).toEqual(FULL_MENU);
  });

  it('leaves the to-do list out while the workspace features have not loaded', async () => {
    expandAll();
    await mount('/projects/p1', { flags: null });
    expect(groups()).toEqual(FULL_MENU);
  });

  it('puts the to-do list after the board once the feature is on', async () => {
    expandAll();
    await mount('/projects/p1', { flags: { 'todo-list': true } });
    expect(groups()).toEqual(withTodos());
  });

  it('is not changed by a gate that is on and that no entry names', async () => {
    expandAll();
    await mount('/projects/p1', { flags: { 'share-links': true, 'flow-down': true } });
    expect(groups()).toEqual(FULL_MENU);
  });

  it('drops Guided Definition once a guided definition has been worked on', async () => {
    expandAll();
    await mount('/projects/p1', {
      sessions: [{ id: 's1', status: 'in-progress', current_step: 3, answers: {} }],
    });
    expect(groups()).toEqual(withoutGuided());
  });

  it('drops Guided Definition for a committed session too', async () => {
    expandAll();
    await mount('/projects/p1', {
      sessions: [{ id: 's1', status: 'committed', current_step: 1, answers: {} }],
    });
    expect(groups()).toEqual(withoutGuided());
  });

  it('keeps Guided Definition when the only session is the assistant’s chat host', async () => {
    expandAll();
    await mount('/projects/p1', {
      sessions: [{ id: 's1', status: 'in-progress', current_step: 1, answers: {} }],
    });
    expect(groups()).toEqual(FULL_MENU);
  });

  it('ends with a Help link to the manual, outside every group', async () => {
    await mount('/projects/p1');
    const help = nav().querySelector(':scope > a');
    expect(help?.getAttribute('href')).toBe('/manual');
    expect(help?.getAttribute('target')).toBe('_blank');
    expect((help?.textContent ?? '').trim()).toBe('Help');
  });
});

describe('the project menu: which group is open', () => {
  it('opens only the group of the overview, and shows the tail, on the overview', async () => {
    await mount('/projects/p1');
    expect(groups()).toEqual([
      {
        header: 'Define',
        expanded: true,
        links: FULL_MENU[0].links,
      },
      { header: 'Verify', expanded: false, links: [] },
      { header: 'Plan', expanded: false, links: [] },
      { header: 'Agents', expanded: false, links: [] },
      FULL_MENU[4],
    ]);
  });

  it.each([
    ['/projects/p1/requirements', 'Define'],
    ['/projects/p1/requirements/REQ-1', 'Define'],
    ['/projects/p1/interviews', 'Define'],
    ['/projects/p1/vv', 'Verify'],
    ['/projects/p1/vv/runs/r1', 'Verify'],
    ['/projects/p1/matrix', 'Verify'],
    ['/projects/p1/impact', 'Verify'],
    ['/projects/p1/review', 'Verify'],
    ['/projects/p1/board', 'Plan'],
    ['/projects/p1/agents', 'Agents'],
    ['/projects/p1/crew', 'Agents'],
    ['/projects/p1/crew/network', 'Agents'],
    ['/projects/p1/automations', 'Agents'],
    ['/projects/p1/agent-runs', 'Agents'],
  ])('opens %s in the %s group alone', async (path, open) => {
    await mount(path);
    expect(
      groups()
        .filter((g) => g.expanded)
        .map((g) => g.header)
    ).toEqual([open]);
  });

  it('opens no labelled group on a tail page, a deleted page or a page in no group', async () => {
    for (const path of [
      '/projects/p1/activity',
      '/projects/p1/settings',
      '/projects/p1/baselines/b1/compare',
    ]) {
      await mount(path);
      expect(groups().map((g) => g.expanded)).toEqual([false, false, false, false, null]);
      await act(async () => root.unmount());
      container.remove();
    }
    // afterEach unmounts once more; mount a last one for it to take down.
    await mount('/projects/p1');
  });

  it('lets a stored choice close the group of the page being viewed', async () => {
    localStorage.setItem('openv-nav-sections', JSON.stringify({ Verify: false }));
    await mount('/projects/p1/matrix');
    expect(groups()[1]).toEqual({ header: 'Verify', expanded: false, links: [] });
  });

  it('expands and collapses a group on a click of its header, and remembers it', async () => {
    await mount('/projects/p1');
    const header = () =>
      Array.from(nav().querySelectorAll('button')).find((b) =>
        (b.textContent ?? '').includes('Plan')
      ) as HTMLButtonElement;
    expect(header().getAttribute('title')).toBe('Expand Plan');
    await act(async () => header().click());
    expect(groups()[2]).toEqual({
      header: 'Plan',
      expanded: true,
      links: ['Board -> /projects/p1/board'],
    });
    expect(header().getAttribute('title')).toBe('Collapse Plan');
    expect(JSON.parse(localStorage.getItem('openv-nav-sections') || '{}')).toEqual({ Plan: true });
  });
});

describe('the project menu: the page being viewed', () => {
  it.each([
    ['/projects/p1', 'Overview'],
    ['/projects/p1/requirements', 'Requirements'],
    ['/projects/p1/requirements/REQ-1', 'Requirements'],
    ['/projects/p1/vv', 'V&V'],
    ['/projects/p1/vv/runs/r1', 'V&V'],
    ['/projects/p1/crew/network', 'Crew'],
    ['/projects/p1/agent-runs', 'Runs'],
    ['/projects/p1/settings', 'Settings'],
  ])('marks only the entry for %s as current', async (path, label) => {
    expandAll();
    await mount(path);
    expect(current()).toEqual([label]);
  });

  it('does not mark the overview as current on a page beneath the project', async () => {
    expandAll();
    await mount('/projects/p1/board');
    expect(current()).toEqual(['Board']);
  });

  it('marks no entry on a page that has none', async () => {
    expandAll();
    await mount('/projects/p1/baselines/b1/compare');
    expect(current()).toEqual([]);
  });
});
