import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { Artifact } from '../api/client';
import { ArtifactList } from './ArtifactList';

// Landing on an artifact from outside the tree — a deep link, a citation, the
// ‹ / › stepper, J / K, a swipe — has to show the row it landed on. What matters
// is that the path to the selection opens, that nothing else does, and that a
// section closed on purpose stays closed.

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({
    width: 1280,
    cls: 'desktop',
    isPhone: false,
    isCompact: false,
    coarsePointer: false,
  }),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const art = (id: string, parent: string | null, order: number, type = 'requirement'): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type,
    title: `Title ${id}`,
    body: '',
    sort_order: order,
    attributes: {},
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }) as Artifact;

//  hdg-1
//    req-1
//      req-2
//  hdg-2
//    req-3
const TREE = [
  art('hdg-1', null, 1, 'heading'),
  art('req-1', 'hdg-1', 1),
  art('req-2', 'req-1', 1),
  art('hdg-2', null, 2, 'heading'),
  art('req-3', 'hdg-2', 1),
];

describe('ArtifactList reveals the selected artifact', () => {
  let container: HTMLDivElement;
  let root: Root;

  const render = (artifacts: Artifact[], selectedId?: string) => {
    act(() => {
      root.render(
        <ArtifactList
          artifacts={artifacts}
          allArtifacts={artifacts}
          selectedId={selectedId}
          onSelect={() => {}}
          onReorder={() => {}}
          defaultCollapsed
        />
      );
    });
  };

  const shows = (id: string) => container.textContent?.includes(`Title ${id}`) ?? false;
  // Rows are siblings, not nested, and every row carries an actions button
  // named after its own artifact — so the nearest ancestor of that button
  // holding a collapse toggle is that row's toggle and no other's.
  const collapseButtonFor = (id: string): HTMLButtonElement | null => {
    let node = container.querySelector(`button[aria-label="Actions for Title ${id}"]`)
      ?.parentElement ?? null;
    while (node && node !== container) {
      const toggle = node.querySelector<HTMLButtonElement>(
        'button[aria-label="Collapse children"], button[aria-label="Expand children"]'
      );
      if (toggle) return toggle;
      node = node.parentElement;
    }
    return null;
  };

  beforeEach(() => {
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

  it('opens the whole path down to a selected grandchild', () => {
    render(TREE, 'req-2');
    expect(shows('hdg-1')).toBe(true);
    expect(shows('req-1')).toBe(true);
    expect(shows('req-2')).toBe(true);
  });

  it('opens only that path, leaving every other section shut', () => {
    // Given the artifacts up front, defaultCollapsed shuts every parent; the
    // reveal opens hdg-1 and req-1 for req-2 and touches nothing else.
    render(TREE, 'req-2');
    expect(shows('req-2')).toBe(true);
    expect(shows('req-3')).toBe(false);
  });

  it('does not re-open anything when the tree is refiltered', () => {
    render(TREE, 'req-2');
    expect(shows('req-3')).toBe(false);
    // A new array with the same content is what a keystroke in the search box
    // produces, and it must not disturb what is open.
    render([...TREE], 'req-2');
    expect(shows('req-3')).toBe(false);
  });

  it('opens a closed section when the selection steps into it', () => {
    render(TREE, 'req-2');
    expect(shows('req-3')).toBe(false);
    render(TREE, 'req-3');
    expect(shows('req-3')).toBe(true);
  });

  it('leaves a section the reader closed by hand closed', () => {
    render(TREE, 'req-2');
    // hdg-1 was opened by the reveal; shutting it again is a deliberate act.
    act(() => collapseButtonFor('hdg-1')!.click());
    expect(shows('req-1')).toBe(false);
    render([...TREE], 'req-2');
    expect(shows('req-1')).toBe(false);
  });

  it('reveals a selection that arrives after the artifacts do', () => {
    // The real order of events: the tree mounts while the fetch is in flight,
    // then the artifacts land, then the ?artifact= selection resolves.
    render([], undefined);
    render(TREE, undefined);
    act(() => collapseButtonFor('hdg-1')!.click());
    expect(shows('req-1')).toBe(false);
    render(TREE, 'req-2');
    expect(shows('req-2')).toBe(true);
  });

  it('finds nothing to open for a root, and does not mind no selection', () => {
    render(TREE, 'hdg-1');
    expect(shows('hdg-1')).toBe(true);
    render(TREE, undefined);
    expect(shows('hdg-1')).toBe(true);
  });

  it('shows an artifact whose parent the filter removed', () => {
    // req-2 survives a filter that dropped hdg-1 and req-1: it is drawn as a
    // root, so there is no ancestor to open and nothing to hide it.
    render([art('req-2', 'req-1', 1)], 'req-2');
    expect(shows('req-2')).toBe(true);
  });
});
