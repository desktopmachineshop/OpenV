import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { Artifact } from '../api/client';
import { ArtifactList } from './ArtifactList';

// The tree without its stacked ▲ ▼ buttons: moving an artifact among its
// siblings is Alt+↑ / Alt+↓ on a focused row, or Move up / Move down in the
// row's menu; and the depth control opens the tree to a level.

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const art = (id: string, parent: string | null, order: number): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type: parent ? 'requirement' : 'heading',
    title: `Title ${id}`,
    body: '',
    sort_order: order,
    attributes: {},
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: '',
    updated_at: '',
  }) as Artifact;

//  hdg-1
//    req-1
//      req-3
//    req-2
const TREE = [art('hdg-1', null, 1), art('req-1', 'hdg-1', 1), art('req-3', 'req-1', 1), art('req-2', 'hdg-1', 2)];

let container: HTMLDivElement;
let root: Root;
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

const render = (props: Partial<React.ComponentProps<typeof ArtifactList>> = {}) =>
  act(() =>
    root.render(
      <ArtifactList artifacts={TREE} allArtifacts={TREE} onSelect={() => {}} onReorder={() => {}} defaultCollapsed {...props} />
    )
  );
const rows = () => Array.from(container.querySelectorAll<HTMLElement>('[role="treeitem"]'));
const shown = () => rows().map((r) => r.querySelector('span[title^="Stable reference"]')?.textContent);
const key = (node: HTMLElement, init: KeyboardEventInit) =>
  act(() => {
    node.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, ...init }));
  });

describe('ArtifactList keys and depth', () => {
  it('moves a row among its siblings with Alt+arrows, and opens and shuts with the arrows', () => {
    const onReorder = vi.fn();
    const onSelect = vi.fn();
    render({ onReorder, onSelect, depthTrigger: { depth: Infinity, token: 1 } });
    expect(shown()).toEqual(['HDG-1', 'REQ-1', 'REQ-3', 'REQ-2']);

    key(rows()[3], { key: 'ArrowUp', altKey: true });
    expect(onReorder).toHaveBeenCalledWith('req-2', 'req-1', 'swap');
    key(rows()[1], { key: 'ArrowUp', altKey: true });
    expect(onReorder).toHaveBeenCalledTimes(1);

    key(rows()[1], { key: 'ArrowLeft' });
    expect(shown()).toEqual(['HDG-1', 'REQ-1', 'REQ-2']);
    key(rows()[1], { key: 'ArrowRight' });
    expect(shown()).toEqual(['HDG-1', 'REQ-1', 'REQ-3', 'REQ-2']);
    key(rows()[2], { key: 'Enter' });
    expect(onSelect).toHaveBeenCalledWith('req-3');
  });

  it('opens the tree to the depth picked', () => {
    render({ depthTrigger: { depth: 2, token: 1 } });
    expect(shown()).toEqual(['HDG-1', 'REQ-1', 'REQ-2']);
    render({ depthTrigger: { depth: 1, token: 2 } });
    expect(shown()).toEqual(['HDG-1']);
    render({ depthTrigger: { depth: Infinity, token: 3 } });
    expect(shown()).toEqual(['HDG-1', 'REQ-1', 'REQ-3', 'REQ-2']);
  });

  it('keeps Move up and Move down in the row menu', () => {
    const onReorder = vi.fn();
    render({ onReorder, depthTrigger: { depth: Infinity, token: 1 } });
    act(() => (container.querySelector('button[aria-label="Actions for Title req-1"]') as HTMLElement).click());
    expect((container.querySelector('button[aria-label="Move Title req-1 up"]') as HTMLButtonElement).disabled).toBe(true);
    act(() => (container.querySelector('button[aria-label="Move Title req-1 down"]') as HTMLElement).click());
    expect(onReorder).toHaveBeenCalledWith('req-1', 'req-2', 'swap');
  });
});
