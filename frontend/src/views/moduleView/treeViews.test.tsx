import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { Artifact } from '../../api/client';
import { buildFacts } from '../../utils/artifactFacts';
import { ListToolbar } from './ListToolbar';
import { TreeDiagramView } from './TreeDiagramView';
import { viewFromParam } from './requirementsViews';

vi.mock('../../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const art = (id: string, type: string, parent: string | null, order: number, status = 'draft'): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type,
    title: `Title ${id}`,
    body: '',
    sort_order: order,
    status,
    attributes: {},
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }) as Artifact;

//  hdg-1
//    req-1
//      req-2         (folded at the start: the third level)
//        tc-1
//  need-1
const ARTS = [
  art('hdg-1', 'heading', null, 1),
  art('req-1', 'requirement', 'hdg-1', 1, 'approved'),
  art('req-2', 'requirement', 'req-1', 1),
  art('tc-1', 'test-case', 'req-2', 1, 'in_review'),
  art('need-1', 'user-need', null, 2),
];
const facts = buildFacts(ARTS, [], undefined, []);

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

const boxes = () => Array.from(container.querySelectorAll<HTMLElement>('[role="treeitem"]'));
const refs = () => boxes().map((b) => b.querySelector('span span:nth-child(2)')?.textContent);
const button = (name: string) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.getAttribute('aria-label') === name || b.textContent === name)!;

describe('?view= with the Table and Tree views', () => {
  it('opens the diagram for tree and for an old layers link, and the table otherwise', () => {
    expect(viewFromParam('tree', true)).toBe('tree');
    expect(viewFromParam('layers', true)).toBe('tree');
    expect(viewFromParam(null, true)).toBe('table');
    // Without them, the three views stay.
    expect(viewFromParam('layers')).toBe('layers');
  });
});

describe('the Tree diagram', () => {
  const draw = (props: Partial<React.ComponentProps<typeof TreeDiagramView>> = {}) =>
    act(() =>
      root.render(
        <TreeDiagramView artifacts={ARTS} selectedId={null} onSelect={() => {}} facts={facts} foldTrigger={{ folded: false, token: 0 }} {...props} />
      )
    );

  it('opens folded to two levels, a box per artifact with its type, ref, status and title', () => {
    draw();
    expect(container.querySelector('[role="tree"]')?.getAttribute('aria-label')).toBe('Requirements tree diagram');
    expect(refs()).toEqual(['HDG-1', 'REQ-1', 'NEED-1']);
    const req1 = boxes()[1];
    expect(req1.getAttribute('aria-label')).toBe('REQ-1, Title req-1, Requirement, Approved');
    expect(req1.getAttribute('aria-expanded')).toBe('false');
    expect(req1.textContent).toContain('REQ');
    expect(req1.textContent).toContain('approved');
    expect(req1.textContent).toContain('Title req-1');
  });

  it('opens the artifact a box is clicked on, and opens the branch a selection sits in', () => {
    const onSelect = vi.fn();
    draw({ onSelect });
    act(() => boxes()[2].click());
    expect(onSelect).toHaveBeenCalledWith('need-1');
    draw({ onSelect, selectedId: 'tc-1' });
    expect(refs()).toEqual(['HDG-1', 'REQ-1', 'REQ-2', 'TC-1', 'NEED-1']);
    expect(boxes()[3].getAttribute('aria-selected')).toBe('true');
  });

  it('folds and unfolds everything from the toolbar button', () => {
    draw({ foldTrigger: { folded: false, token: 1 } });
    expect(boxes()).toHaveLength(5);
    draw({ foldTrigger: { folded: true, token: 2 } });
    expect(refs()).toEqual(['HDG-1', 'NEED-1']);
  });

  it('colours by type or by status, with a key of what is in view', () => {
    draw();
    const key = () => Array.from(container.querySelectorAll('[aria-label="Colour key"] li')).map((li) => li.textContent);
    expect(key()).toEqual(['Heading', 'Requirement', 'Test case', 'User need']);
    expect(boxes()[1].style.background).toBe('var(--tint-blue)');
    act(() => button('Status').click());
    expect(button('Status').getAttribute('aria-pressed')).toBe('true');
    expect(key()).toEqual(['Draft', 'Approved', 'In review']);
    expect(boxes()[1].style.background).toBe('var(--tint-green)');
  });
});

describe('the toolbar with the Table and Tree views', () => {
  const toolbar = (props: Partial<React.ComponentProps<typeof ListToolbar>>) => (
    <ListToolbar
      phone={false}
      view="table"
      setView={() => {}}
      typeCounts={[]}
      total={0}
      typeTab=""
      setTypeTab={() => {}}
      quick=""
      setQuick={() => {}}
      quickCounts={{ in_review: 0, failing: 0, suspect: 0 }}
      searchText=""
      setSearchText={() => {}}
      filterSummary=""
      filtersActive={false}
      clearFilters={() => {}}
      showFilterPanel={false}
      setShowFilterPanel={() => {}}
      pickDepth={() => {}}
      {...props}
    />
  );

  it('switches Table | Tree and folds with one button instead of the depth picker', () => {
    const toggleFold = vi.fn();
    act(() => root.render(toolbar({ treeViews: true, folded: false, toggleFold })));
    const views = Array.from(container.querySelectorAll('[role="group"][aria-label="View"] button')).map((b) => b.textContent);
    expect(views).toEqual(['Table', 'Tree']);
    expect(container.textContent).not.toContain('Depth');
    act(() => button('Fold all').click());
    expect(toggleFold).toHaveBeenCalled();
    act(() => root.render(toolbar({ treeViews: true, folded: true, toggleFold, view: 'tree' })));
    expect(button('Unfold all').getAttribute('title')).toContain('open every level');
  });

  it('keeps Table, Tree and Layers with the depth picker without them', () => {
    act(() => root.render(toolbar({ view: 'tree' })));
    const views = Array.from(container.querySelectorAll('[role="group"][aria-label="View"] button')).map((b) => b.textContent);
    expect(views).toEqual(['Table', 'Tree', 'Layers']);
    expect(container.textContent).toContain('Depth');
  });

  it('gives a phone its list and outline, and folds only the outline', () => {
    act(() => root.render(toolbar({ phone: true, treeViews: true, folded: true, toggleFold: () => {} })));
    const pills = Array.from(container.querySelectorAll('[aria-label="Views and quick filters"] button')).map((b) => b.textContent);
    expect(pills.slice(0, 2)).toEqual(['List 0', 'Outline']);
    expect(pills).not.toContain('By level');
    expect(container.querySelector('button[aria-label="Unfold all"]')).toBeNull();
    act(() => root.render(toolbar({ phone: true, treeViews: true, folded: true, toggleFold: () => {}, view: 'tree' })));
    expect(container.querySelector('button[aria-label="Unfold all"]')).not.toBeNull();
  });
});
