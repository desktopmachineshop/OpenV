import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ModuleView } from './ModuleView';
import { artifactAPI } from '../api/client';
import { DialogProvider } from '../components/ui';

// Stepping through the document, wired up: the ‹ / › controls, J and K, and the
// guards that keep a bare letter from firing when it was meant as a letter.

vi.mock('../api/client', () => ({
  artifactAPI: { list: vi.fn(), getVersions: vi.fn() },
  linkAPI: { list: vi.fn(), listForArtifact: vi.fn() },
  attachmentAPI: {
    listByProject: vi.fn(),
    listByArtifact: vi.fn(),
    getDownloadUrl: (id: string) => `/dl/${id}`,
  },
  baselineAPI: { list: vi.fn() },
  qualityAPI: { project: vi.fn(), artifact: vi.fn() },
  projectAPI: { get: vi.fn(), linkedArtifacts: vi.fn(), parties: vi.fn() },
  agentsAPI: {},
  membersAPI: { list: vi.fn().mockResolvedValue({ data: [] }) },
  metaAPI: {
    attributeDefinitions: vi.fn().mockResolvedValue({ data: [] }),
    artifactTypes: vi.fn().mockResolvedValue({ data: [] }),
    linkTypes: vi.fn().mockResolvedValue({ data: [] }),
  },
}));

let phone = false;
vi.mock('../hooks/useViewport', () => ({
  useViewport: () =>
    phone
      ? { width: 390, cls: 'phone', isPhone: true, isCompact: true, coarsePointer: true }
      : { width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false },
}));

let steppingOn = true;
vi.mock('../hooks/useFeature', () => ({
  useFeature: (key: string) => (key === 'artifact-stepping' ? steppingOn : true),
}));

// A stand-in store that behaves like the real one in the way this suite
// depends on: a write re-renders whoever read it. Without that the selection
// the ?artifact= param sets would never reach the component.
let selectedArtifactId: string | null = null;
let storeArtifacts: any[] = [];
const listeners = new Set<() => void>();
const notify = () => listeners.forEach((listener) => listener());
// Stable identities, like the real store's. Fresh closures every render would
// change the deps of the component's own callbacks, which reloads the artifacts,
// which writes to the store, which renders again — for ever.
const setArtifacts = (next: any[]) => {
  storeArtifacts = next;
  notify();
};
const setSelectedArtifactId = (id: string | null) => {
  selectedArtifactId = id;
  notify();
};
const noop = () => {};
const storeState = () => ({
  projectId: 'p1',
  artifacts: storeArtifacts,
  setArtifacts,
  addArtifact: noop,
  updateArtifact: noop,
  removeArtifact: noop,
  addLink: noop,
  selectedArtifactId,
  setSelectedArtifactId,
});
// ModuleView reads the store both with a selector and without one.
vi.mock('../state/store', () => ({
  useAppStore: (selector?: (s: any) => unknown) => {
    const [, bump] = React.useState(0);
    React.useEffect(() => {
      const listener = () => bump((n) => n + 1);
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    }, []);
    return selector ? selector(storeState()) : storeState();
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const art = (id: string, parent: string | null, order: number, type = 'requirement') => ({
  id,
  ref: id.toUpperCase(),
  project_id: 'p1',
  parent_id: parent,
  type,
  title: `Title ${id}`,
  body: '',
  sort_order: order,
  status: 'draft',
  attributes: {},
  version: 1,
  valid_from: '',
  valid_to: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
});

//  hdg-1 / req-1 / req-2  — document order: hdg-1, req-1, req-2
const ARTIFACTS = [art('hdg-1', null, 1, 'heading'), art('req-1', 'hdg-1', 1), art('req-2', 'hdg-1', 2)];

const list = vi.mocked(artifactAPI.list, { partial: true, deep: true });
const getVersions = vi.mocked(artifactAPI.getVersions, { partial: true, deep: true });

describe('ModuleView artifact stepping', () => {
  let container: HTMLDivElement;
  let root: Root;

  const mount = async () => {
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
  };

  const button = (name: string) =>
    container.querySelector<HTMLButtonElement>(`button[aria-label="${name}"]`);
  // The tree column has an h3 of its own, so take them all: the artifact's
  // title is whichever one names it.
  const headings = () =>
    Array.from(container.querySelectorAll('h3'))
      .map((node) => node.textContent ?? '')
      .join(' | ');
  const count = () => {
    const live = container.querySelector('[aria-live="polite"]');
    return live?.textContent ?? '';
  };
  // jsdom has no TouchEvent constructor, so the touch lists are put straight
  // onto a plain Event — which is where React's synthetic event reads them.
  const touchEvent = (type: string, x: number, y: number, target: Element) => {
    const event: any = new Event(type, { bubbles: true, cancelable: true });
    const touch = { clientX: x, clientY: y, identifier: 0, target };
    event.touches = type === 'touchend' ? [] : [touch];
    event.changedTouches = [touch];
    return event as Event;
  };

  const pane = () => container.querySelector('[aria-label="Artifact document"]')!;

  const swipe = async (dx: number, dy: number, from?: Element) => {
    const target = from ?? pane();
    const startX = 195;
    const startY = 300;
    // Dispatched on the element the finger actually landed on, so the guard
    // sees the real target and the event still bubbles to the pane's handler.
    await act(async () => {
      target.dispatchEvent(touchEvent('touchstart', startX, startY, target));
      target.dispatchEvent(touchEvent('touchmove', startX + dx / 2, startY + dy / 2, target));
      target.dispatchEvent(touchEvent('touchend', startX + dx, startY + dy, target));
    });
  };

  const press = async (key: string, over: Partial<KeyboardEventInit> = {}, target?: Element) => {
    await act(async () => {
      (target ?? window).dispatchEvent(
        new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...over })
      );
    });
  };

  beforeEach(async () => {
    steppingOn = true;
    phone = false;
    selectedArtifactId = null;
    storeArtifacts = [];
    listeners.clear();
    [list, getVersions].forEach((m) => m.mockReset());
    list.mockResolvedValue({ data: ARTIFACTS } as any);
    getVersions.mockResolvedValue({ data: [] } as any);
    const { linkAPI, attachmentAPI, baselineAPI, qualityAPI, projectAPI } = await import(
      '../api/client'
    );
    vi.mocked(linkAPI.list).mockResolvedValue({ data: [] } as any);
    vi.mocked(linkAPI.listForArtifact).mockResolvedValue({ data: [] } as any);
    vi.mocked(attachmentAPI.listByProject).mockResolvedValue({ data: [] } as any);
    vi.mocked(attachmentAPI.listByArtifact).mockResolvedValue({ data: [] } as any);
    vi.mocked(baselineAPI.list).mockResolvedValue({ data: [] } as any);
    vi.mocked(qualityAPI.project).mockResolvedValue({ data: { rows: [] } } as any);
    vi.mocked(qualityAPI.artifact).mockResolvedValue({ data: { findings: [] } } as any);
    vi.mocked(projectAPI.get).mockResolvedValue({ data: { id: 'p1', name: 'P' } } as any);
    vi.mocked(projectAPI.linkedArtifacts).mockResolvedValue({ data: [] } as any);
    vi.mocked(projectAPI.parties).mockResolvedValue({ data: [] } as any);

    container = document.createElement('div');
    document.body.appendChild(container);
    act(() => {
      root = createRoot(container);
    });
    await mount();
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('says where in the document the artifact sits', () => {
    expect(count()).toContain('2 of 3');
    expect(headings()).toContain('Title req-1');
  });

  it('steps forward and back from the controls', async () => {
    await act(async () => button('Next artifact')!.click());
    expect(count()).toContain('3 of 3');
    expect(headings()).toContain('Title req-2');
    await act(async () => button('Previous artifact')!.click());
    expect(count()).toContain('2 of 3');
    expect(headings()).toContain('Title req-1');
  });

  it('has nothing beyond either end', async () => {
    await act(async () => button('Previous artifact')!.click());
    expect(count()).toContain('1 of 3');
    expect(button('Previous artifact')!.disabled).toBe(true);
    await act(async () => button('Next artifact')!.click());
    await act(async () => button('Next artifact')!.click());
    expect(count()).toContain('3 of 3');
    expect(button('Next artifact')!.disabled).toBe(true);
  });

  it('steps with J and K', async () => {
    await press('j');
    expect(count()).toContain('3 of 3');
    await press('k');
    expect(count()).toContain('2 of 3');
  });

  it('leaves a keystroke in a field to the field', async () => {
    const search = container.querySelector('input')!;
    await press('j', {}, search);
    expect(count()).toContain('2 of 3');
  });

  it('leaves a capital J to whoever typed it', async () => {
    await press('J', { shiftKey: true });
    expect(count()).toContain('2 of 3');
  });

  it('never claims a keystroke a modifier owns', async () => {
    await press('j', { metaKey: true });
    await press('j', { ctrlKey: true });
    await press('j', { altKey: true });
    expect(count()).toContain('2 of 3');
  });

  it('stands down while something floats over the document', async () => {
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.appendChild(dialog);
    await press('j');
    expect(count()).toContain('2 of 3');
    dialog.remove();
  });

  it('stands down once the editor is open', async () => {
    const edit = Array.from(container.querySelectorAll('button')).find(
      (node) => node.textContent?.trim() === 'Edit'
    );
    expect(edit).toBeDefined();
    await act(async () => edit!.click());
    await press('j');
    // The stepper is not drawn over the editor, and the key changed nothing
    // behind it: a half-written requirement is not something to navigate off.
    expect(button('Next artifact')).toBeNull();
    expect(selectedArtifactId).toBe('req-1');
  });

  it('turns the page on a leftward swipe, and back on a rightward one', async () => {
    phone = true;
    await mount();
    await swipe(-140, 0);
    expect(count()).toContain('3 of 3');
    await swipe(140, 0);
    expect(count()).toContain('2 of 3');
  });

  it('leaves a thumb that was scrolling alone', async () => {
    phone = true;
    await mount();
    await swipe(0, -200);
    await swipe(-60, 200);
    expect(count()).toContain('2 of 3');
  });

  it('ignores travel too short to be meant', async () => {
    phone = true;
    await mount();
    await swipe(-30, 0);
    expect(count()).toContain('2 of 3');
  });

  it('leaves the swipe to a table that scrolls sideways', async () => {
    phone = true;
    await mount();
    const table = document.createElement('div');
    // jsdom reports every element as unscrollable, so the one fact the guard
    // reads is stated here; the walk itself is covered in the pure suite.
    Object.defineProperty(table, 'scrollWidth', { value: 800 });
    Object.defineProperty(table, 'clientWidth', { value: 300 });
    table.style.overflowX = 'auto';
    pane().appendChild(table);
    await swipe(-140, 0, table);
    expect(count()).toContain('2 of 3');
  });

  it('does not step on a pointer device, where the buttons and keys do it', async () => {
    await swipe(-140, 0);
    expect(count()).toContain('2 of 3');
  });

  it('gives a workspace without the feature no controls, and the artifact still reads', async () => {
    // A fresh mount, because the gate is read once per render tree and this
    // suite has already mounted one with the feature on.
    act(() => root.unmount());
    steppingOn = false;
    selectedArtifactId = null;
    listeners.clear();
    act(() => {
      root = createRoot(container);
    });
    await mount();
    expect(button('Next artifact')).toBeNull();
    await press('j');
    expect(headings()).toContain('Title req-1');
  });
});
