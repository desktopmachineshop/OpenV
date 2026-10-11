import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ModuleView } from './ModuleView';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// Characterization for refactor plan step X16b (useColumnResize and
// usePanelMode): the requirements module's two resizable columns and the
// notes column's three modes, pinned before that logic leaves ModuleView.
//
//   column resize  The tree column is drawn at its saved width
//                  (openv-leftColumnWidth, else 400), clamped so the document
//                  keeps 420px beside the 260px of sidebar and a pinned notes
//                  column; the notes column at openv-rightColumnWidth, else
//                  320. A drag on a handle moves the column by the pointer's
//                  travel from where it was pressed, from the width drawn,
//                  clamped to 200–800 (tree) or 250–600 (notes), and the width
//                  is saved only on release, as a string.
//   panel mode     The notes column is pinned, auto-hidden or hidden
//                  (openv-panel-mode-artifact-notes; unsaved, auto-hide under
//                  1200px and pinned from there). The button inside the panel
//                  cycles the mode and saves it; the edge strip opens an
//                  auto-hidden panel on hover and reveals any unpinned panel
//                  on click, until a click away, Escape or the strip again.
//
// ChatterPanel is replaced by a probe that shows what the module passes it
// (its labels, its artifact, its toggle) and counts its mounts, which its key
// decides: the real one loads notes and an assistant session, which are not
// what this pins. The store is the real one; the API client answers with
// canned data, and records every call so the tests can show that resizing
// and changing modes call nothing.

const recorder = vi.hoisted(() => ({
  calls: [] as string[],
  respond: (_name: string, _args: unknown[]): unknown => undefined,
}));

const chatter = vi.hoisted(() => ({ mounts: 0 }));

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

vi.mock('../components/ChatterPanel', async () => {
  const { createElement, useEffect } = await import('react');
  return {
    ChatterPanel: (props: {
      artifactId?: string;
      projectId?: string;
      isOpen: boolean;
      onToggle: () => void;
      modeLabel?: string;
      nextModeLabel?: string;
    }) => {
      useEffect(() => {
        chatter.mounts += 1;
      }, []);
      return createElement(
        'section',
        {
          'data-probe': 'chatter',
          'data-artifact': props.artifactId ?? '',
          'data-project': props.projectId ?? '',
          'data-open': String(props.isOpen),
        },
        createElement('button', { type: 'button', onClick: props.onToggle }, `${props.modeLabel} → ${props.nextModeLabel}`)
      );
    },
  };
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// ---- canned data -----------------------------------------------------------

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
  art('req-2', 'REQ-2', 'hdg-1', 2, 'requirement', 'The pump shall stop'),
];

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
};

const canned = (name: string, args: unknown[]) => CANNED[name]?.(...args);

// ---- harness ---------------------------------------------------------------

const initialStore = useAppStore.getState();
const COARSE = '(hover: none) and (pointer: coarse)';
const realInnerWidth = Object.getOwnPropertyDescriptor(window, 'innerWidth');
const realMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia');

let container: HTMLDivElement;
let root: Root;

const setViewport = (width: number, coarse = false) => {
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

const mount = async (entry = '/projects/p1/requirements') => {
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
  // What resizing and changing modes call is measured from here.
  recorder.calls = [];
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

const dispatch = async (target: EventTarget, event: Event) => {
  await act(async () => {
    target.dispatchEvent(event);
  });
  return event;
};

const mouse = (type: string, clientX = 0, relatedTarget: EventTarget | null = null) =>
  new MouseEvent(type, { bubbles: true, cancelable: true, clientX, relatedTarget });

// React fires onMouseEnter and onMouseLeave from mouseover and mouseout, with
// the element the pointer came from or went to as the related target.
const enter = (node: Element, from: Element = document.body) => dispatch(node, mouse('mouseover', 0, from));
const leave = (node: Element, to: Element = document.body) => dispatch(node, mouse('mouseout', 0, to));

// ---- what the page draws ---------------------------------------------------

// The tree column: the box the "+ New Artifact" button sits in.
const treeColumn = () => byText<HTMLButtonElement>('button', '+ New Artifact').parentElement as HTMLDivElement;
const treeWidth = () => treeColumn().style.width;
// The resize handles, left to right: the tree's, then the notes column's.
const handles = () =>
  Array.from(container.querySelectorAll<HTMLDivElement>('div')).filter((node) => node.style.cursor === 'col-resize');
const probe = () => container.querySelector<HTMLElement>('[data-probe="chatter"]');
// The box around the notes panel, which carries its width and placement.
const notesColumn = () => probe()!.parentElement as HTMLDivElement;
const modeButton = () => probe()!.querySelector('button')!;
const strip = () => container.querySelector<HTMLButtonElement>('button.panel-edge-strip');
const backdrop = () =>
  Array.from(container.querySelectorAll<HTMLDivElement>('div')).find((node) => node.style.zIndex === '899') ?? null;
const saved = (key: string) => window.localStorage.getItem(key);

const LEFT = 'openv-leftColumnWidth';
const RIGHT = 'openv-rightColumnWidth';
const MODE = 'openv-panel-mode-artifact-notes';

beforeEach(() => {
  recorder.calls = [];
  recorder.respond = canned;
  chatter.mounts = 0;
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
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
  window.localStorage.clear();
  document.body.style.cursor = '';
  document.body.style.userSelect = '';
  for (const [key, real] of [
    ['innerWidth', realInnerWidth],
    ['matchMedia', realMatchMedia],
  ] as const) {
    if (real) Object.defineProperty(window, key, real);
    else delete (window as any)[key];
  }
});

describe('ModuleView column resize (X16b characterization)', () => {
  it('draws 400 and 320 when nothing is saved', async () => {
    setViewport(1600);
    await mount();

    expect(treeWidth()).toBe('400px');
    expect(notesColumn().style.width).toBe('320px');
    expect(notesColumn().style.minWidth).toBe('250px');
    expect(notesColumn().style.maxWidth).toBe('600px');
    expect(handles()).toHaveLength(2);
    expect(saved(LEFT)).toBeNull();
    expect(saved(RIGHT)).toBeNull();
  });

  it('draws the saved widths, read as integers', async () => {
    window.localStorage.setItem(LEFT, '480');
    window.localStorage.setItem(RIGHT, '300.9');
    setViewport(1600);
    await mount();

    expect(treeWidth()).toBe('480px');
    expect(notesColumn().style.width).toBe('300px');
  });

  it('narrows the tree to leave the document 420px, but never below 200, and keeps the saved width', async () => {
    window.localStorage.setItem(LEFT, '700');
    // 1300 - 260 (sidebar) - 330 (pinned notes and handle) - 420 = 290.
    setViewport(1300);
    await mount();
    expect(treeWidth()).toBe('290px');
    expect(saved(LEFT)).toBe('700');

    act(() => root.unmount());
    act(() => {
      root = createRoot(container);
    });
    // 1200 - 260 - 330 - 420 = 190, under the floor.
    setViewport(1200);
    await mount();
    expect(treeWidth()).toBe('200px');
  });

  it('drags the tree column by the pointer, clamped to 200–800, and saves the width on release', async () => {
    setViewport(1600);
    await mount();
    const [left] = handles();

    const down = await dispatch(left, mouse('mousedown', 500));
    expect(down.defaultPrevented).toBe(true);
    expect(left.style.backgroundColor).toBe('var(--accent)');
    expect(document.body.style.cursor).toBe('col-resize');
    expect(document.body.style.userSelect).toBe('none');

    await dispatch(document, mouse('mousemove', 560));
    expect(treeWidth()).toBe('460px');
    // Nothing is saved while the pointer is down.
    expect(saved(LEFT)).toBeNull();

    await dispatch(document, mouse('mousemove', 50));
    expect(treeWidth()).toBe('200px');

    // 800 at most; drawn at the 590 the window leaves the tree.
    await dispatch(document, mouse('mousemove', 1500));
    expect(treeWidth()).toBe('590px');

    await dispatch(document, mouse('mouseup', 1500));
    expect(saved(LEFT)).toBe('800');
    expect(saved(RIGHT)).toBeNull();
    expect(left.style.backgroundColor).toBe('transparent');
    expect(document.body.style.cursor).toBe('');
    expect(document.body.style.userSelect).toBe('');

    // Released, the pointer moves nothing.
    await dispatch(document, mouse('mousemove', 600));
    expect(treeWidth()).toBe('590px');
    expect(recorder.calls).toEqual([]);
  });

  it('drags the notes column the other way, clamped to 250–600', async () => {
    setViewport(1600);
    await mount();
    const [, right] = handles();

    await dispatch(right, mouse('mousedown', 1000));
    expect(right.style.backgroundColor).toBe('var(--accent)');

    await dispatch(document, mouse('mousemove', 900));
    expect(notesColumn().style.width).toBe('420px');
    expect(treeWidth()).toBe('400px');

    await dispatch(document, mouse('mousemove', 1400));
    expect(notesColumn().style.width).toBe('250px');

    // 600 at most, and the tree yields to keep the document its 420px:
    // 1600 - 260 - 610 - 420 = 310.
    await dispatch(document, mouse('mousemove', 0));
    expect(notesColumn().style.width).toBe('600px');
    expect(treeWidth()).toBe('310px');
    expect(saved(RIGHT)).toBeNull();

    await dispatch(document, mouse('mouseup', 0));
    expect(saved(RIGHT)).toBe('600');
    expect(saved(LEFT)).toBeNull();
    expect(recorder.calls).toEqual([]);
  });

  it('starts a tree drag from the width drawn, not the width saved', async () => {
    window.localStorage.setItem(LEFT, '700');
    setViewport(1300);
    await mount();
    expect(treeWidth()).toBe('290px');

    await dispatch(handles()[0], mouse('mousedown', 500));
    await dispatch(document, mouse('mousemove', 450));
    expect(treeWidth()).toBe('240px');
    await dispatch(document, mouse('mouseup', 450));
    expect(saved(LEFT)).toBe('240');
  });

  it('saves the width it last knew when released without a move', async () => {
    window.localStorage.setItem(LEFT, '700');
    setViewport(1300);
    await mount();

    // The tree is drawn at 290, but a press and release with no move saves
    // the 700 it read at mount.
    await dispatch(handles()[0], mouse('mousedown', 500));
    await dispatch(document, mouse('mouseup', 500));
    expect(saved(LEFT)).toBe('700');

    await dispatch(handles()[1], mouse('mousedown', 1000));
    await dispatch(document, mouse('mouseup', 1000));
    expect(saved(RIGHT)).toBe('320');

    // After a drag it is the dragged width.
    await dispatch(handles()[0], mouse('mousedown', 500));
    await dispatch(document, mouse('mousemove', 480));
    await dispatch(document, mouse('mouseup', 480));
    expect(saved(LEFT)).toBe('270');
    await dispatch(handles()[0], mouse('mousedown', 500));
    await dispatch(document, mouse('mouseup', 500));
    expect(saved(LEFT)).toBe('270');
  });

  it('lights a handle under the pointer, but not while a drag is on', async () => {
    setViewport(1600);
    await mount();
    const [left, right] = handles();

    await enter(left);
    expect(left.style.backgroundColor).toBe('var(--neutral-soft)');
    await leave(left);
    expect(left.style.backgroundColor).toBe('transparent');

    await dispatch(left, mouse('mousedown', 500));
    await enter(right);
    expect(right.style.backgroundColor).toBe('transparent');
    await dispatch(document, mouse('mouseup', 500));
  });

  it('has no notes handle unless the notes are pinned, and no handles stacked', async () => {
    setViewport(1100);
    await mount();
    expect(handles()).toHaveLength(1);

    act(() => root.unmount());
    act(() => {
      root = createRoot(container);
    });
    setViewport(390, true);
    await mount();
    expect(handles()).toHaveLength(0);
  });
});

describe('ModuleView notes panel mode (X16b characterization)', () => {
  it('starts pinned from 1200px', async () => {
    setViewport(1200);
    await mount();

    expect(probe()).not.toBeNull();
    expect(modeButton().textContent).toBe('Pinned → Auto-hide');
    expect(probe()!.dataset.open).toBe('true');
    expect(probe()!.dataset.project).toBe('p1');
    expect(strip()).toBeNull();
    expect(notesColumn().style.position).toBe('');
    expect(saved(MODE)).toBeNull();
  });

  it('starts auto-hidden under 1200px, behind the edge strip', async () => {
    setViewport(1199);
    await mount();

    expect(probe()).toBeNull();
    expect(strip()!.getAttribute('aria-label')).toBe('Show the notes panel');
    expect(strip()!.getAttribute('aria-expanded')).toBe('false');
    expect(strip()!.title).toBe('Show the notes panel (Auto-hide) — the button inside the panel changes the mode');
    expect(strip()!.style.width).toBe('24px');
    expect(strip()!.textContent).toBe('‹');
    expect(saved(MODE)).toBeNull();
  });

  it('starts in a saved mode whatever the width, and in the width\'s mode when the saved one is unknown', async () => {
    window.localStorage.setItem(MODE, 'hidden');
    setViewport(1600);
    await mount();
    expect(probe()).toBeNull();
    expect(strip()!.title).toBe('Show the notes panel (Hidden) — the button inside the panel changes the mode');

    act(() => root.unmount());
    act(() => {
      root = createRoot(container);
    });
    window.localStorage.setItem(MODE, 'pinned');
    setViewport(1100);
    await mount();
    expect(modeButton().textContent).toBe('Pinned → Auto-hide');

    act(() => root.unmount());
    act(() => {
      root = createRoot(container);
    });
    window.localStorage.setItem(MODE, 'sideways');
    setViewport(1100);
    await mount();
    expect(probe()).toBeNull();
    expect(strip()!.title).toBe('Show the notes panel (Auto-hide) — the button inside the panel changes the mode');
    expect(saved(MODE)).toBe('sideways');
  });

  it('cycles pinned, auto-hide, hidden and back, saving each, and keeps the panel up while choosing', async () => {
    window.localStorage.setItem(LEFT, '600');
    setViewport(1600);
    await mount();
    // Pinned, the notes take 330px: 1600 - 260 - 330 - 420 = 590.
    expect(treeWidth()).toBe('590px');
    expect(chatter.mounts).toBe(1);

    await click(modeButton());
    expect(saved(MODE)).toBe('autohide');
    // Revealed: still on screen, floating over the document beside the strip.
    expect(modeButton().textContent).toBe('Auto-hide → Hidden');
    expect(notesColumn().style.position).toBe('fixed');
    expect(notesColumn().style.right).toBe('24px');
    expect(notesColumn().style.zIndex).toBe('900');
    expect(notesColumn().style.width).toBe('320px');
    expect(strip()!.getAttribute('aria-label')).toBe('Hide the notes panel');
    expect(strip()!.getAttribute('aria-expanded')).toBe('true');
    expect(strip()!.textContent).toBe('›');
    expect(backdrop()).not.toBeNull();
    expect(handles()).toHaveLength(1);
    // Unpinned, the notes take no width from the tree.
    expect(treeWidth()).toBe('600px');

    await click(modeButton());
    expect(saved(MODE)).toBe('hidden');
    expect(modeButton().textContent).toBe('Hidden → Pinned');
    expect(backdrop()).not.toBeNull();

    await click(modeButton());
    expect(saved(MODE)).toBe('pinned');
    expect(modeButton().textContent).toBe('Pinned → Auto-hide');
    expect(notesColumn().style.position).toBe('');
    expect(strip()).toBeNull();
    expect(backdrop()).toBeNull();
    expect(handles()).toHaveLength(2);
    expect(treeWidth()).toBe('590px');

    // The panel stayed mounted through the cycle, and nothing was called.
    expect(chatter.mounts).toBe(1);
    expect(recorder.calls).toEqual([]);
  });

  it('opens an auto-hidden panel while the pointer is over the strip or the panel', async () => {
    setViewport(1100);
    await mount();

    await enter(strip()!);
    expect(probe()).not.toBeNull();
    expect(strip()!.getAttribute('aria-expanded')).toBe('true');
    // Hovering is not revealing: there is nothing to click away from.
    expect(backdrop()).toBeNull();

    // From the strip onto the panel it stays open; off the panel it closes.
    const panel = notesColumn();
    await leave(strip()!, panel);
    expect(probe()).not.toBeNull();
    await leave(panel);
    expect(probe()).toBeNull();

    await enter(strip()!);
    await leave(strip()!);
    expect(probe()).toBeNull();
    expect(saved(MODE)).toBeNull();
    expect(recorder.calls).toEqual([]);
  });

  it('leaves a hidden panel shut on hover; the strip reveals it until a click away, Escape or the strip', async () => {
    window.localStorage.setItem(MODE, 'hidden');
    setViewport(1600);
    await mount();

    await enter(strip()!);
    expect(probe()).toBeNull();

    await click(strip()!);
    expect(probe()).not.toBeNull();
    expect(backdrop()).not.toBeNull();
    await leave(strip()!);
    expect(probe()).not.toBeNull();
    await click(backdrop()!);
    expect(probe()).toBeNull();

    await click(strip()!);
    expect(probe()).not.toBeNull();
    await dispatch(window, new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    expect(probe()).not.toBeNull();
    await dispatch(window, new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(probe()).toBeNull();

    await click(strip()!);
    expect(probe()).not.toBeNull();
    await click(strip()!);
    expect(probe()).toBeNull();
    expect(saved(MODE)).toBe('hidden');
  });

  it('does not open on hover where the pointer is a finger, and widens the strip', async () => {
    window.localStorage.setItem(MODE, 'autohide');
    setViewport(1600, true);
    await mount();

    expect(strip()!.style.width).toBe('32px');
    await enter(strip()!);
    expect(probe()).toBeNull();
    await click(strip()!);
    expect(probe()).not.toBeNull();
    expect(notesColumn().style.right).toBe('32px');
  });

  it('stacked, the notes are a pane whose button goes back to the document and saves nothing', async () => {
    setViewport(390, true);
    await mount();
    expect(probe()).toBeNull();
    expect(strip()).toBeNull();

    await click(byText('button', 'Notes'));
    expect(modeButton().textContent).toBe('Notes → Document');
    expect(notesColumn().style.flex).toBe('1 1 0%');
    expect(notesColumn().style.position).toBe('');

    await click(modeButton());
    expect(probe()).toBeNull();
    expect(byText('button', 'Document').getAttribute('aria-selected')).toBe('true');
    expect(saved(MODE)).toBeNull();
  });

  it('remounts the panel for another artifact, and keeps it for the same one', async () => {
    setViewport(1600);
    await mount('/projects/p1/requirements?artifact=req-1');
    // Twice: first with no artifact, before the ?artifact= sync selects it.
    expect(probe()!.dataset.artifact).toBe('req-1');
    expect(chatter.mounts).toBe(2);

    await click(byText('code', 'REQ-2'));
    expect(probe()!.dataset.artifact).toBe('req-2');
    expect(chatter.mounts).toBe(3);

    await click(byText('code', 'REQ-2'));
    expect(chatter.mounts).toBe(3);
  });
});
