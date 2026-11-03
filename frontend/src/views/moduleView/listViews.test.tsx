import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { Artifact, CoverageEntry, Link } from '../../api/client';
import { buildFacts } from '../../utils/artifactFacts';
import { TableView, groupBySection } from './TableView';
import { LayersView } from './LayersView';
import { LevelDrill } from './LevelDrill';

vi.mock('../../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const art = (id: string, type: string, parent: string | null, order: number, title = id): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type,
    title,
    body: '',
    sort_order: order,
    status: 'draft',
    attributes: {},
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }) as Artifact;

//  loose                      (no section)
//  hdg-1 Pumping
//    req-1
//      req-2
//    hdg-2 Start
//      req-3
//  tc-1                        verifies req-2, passes
const ARTS = [
  art('loose', 'requirement', null, 1),
  art('hdg-1', 'heading', null, 2, 'Pumping'),
  art('req-1', 'requirement', 'hdg-1', 1),
  art('req-2', 'requirement', 'req-1', 1),
  art('hdg-2', 'heading', 'hdg-1', 2, 'Start'),
  art('req-3', 'requirement', 'hdg-2', 1),
  art('tc-1', 'test-case', null, 3),
];
const LINKS: Link[] = [
  { id: 'l1', from_id: 'tc-1', to_id: 'req-2', type: 'verifies', suspect: false, attributes: {}, version: 1, created_at: '', updated_at: '' },
];
const COVERAGE: CoverageEntry[] = [
  { requirement_id: 'req-2', title: '', verification_method: 'test', verification_status: '', test_case_ids: ['tc-1'], latest_results: { 'tc-1': 'pass' }, rollup: 'pass' },
];
const facts = buildFacts(ARTS, LINKS, undefined, COVERAGE);

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

const render = (node: React.ReactElement) => act(() => root.render(node));
const buttonNamed = (text: string) =>
  Array.from(container.querySelectorAll('button')).find((b) => (b.textContent ?? '').includes(text))!;

describe('the table', () => {
  it('groups rows under the heading they sit in, in document order, with what no heading holds first', () => {
    const groups = groupBySection(ARTS);
    expect(groups.map((g) => [g.label, g.rows.map((a) => a.id)])).toEqual([
      ['', ['loose', 'tc-1']],
      ['Pumping', ['req-1', 'req-2']],
      ['Pumping · Start', ['req-3']],
    ]);
    // Nothing but headings: they are the rows.
    expect(groupBySection([ARTS[1], ARTS[4]])[0].rows.map((a) => a.id)).toEqual(['hdg-1', 'hdg-2']);
  });

  it('draws a row per artifact, selects on a click and folds a section', () => {
    const onSelect = vi.fn();
    render(
      <TableView
        artifacts={ARTS}
        allArtifacts={ARTS}
        total={ARTS.length}
        selectedId="req-2"
        onSelect={onSelect}
        facts={facts}
        readOnly={false}
        canPaste={false}
        onReorder={() => {}}
        onContextMenuAction={() => {}}
        phone={false}
      />
    );
    const rows = () => Array.from(container.querySelectorAll('[role="rowgroup"] [role="row"]'));
    expect(rows()).toHaveLength(5);
    expect(container.querySelector('[role="row"][aria-selected="true"]')?.textContent).toContain('REQ-2');
    expect(container.textContent).toContain('5 of 7 · grouped by section');

    act(() => (rows()[0] as HTMLElement).click());
    expect(onSelect).toHaveBeenCalledWith('loose');

    act(() => buttonNamed('Pumping').click());
    expect(rows()).toHaveLength(3);

    // The row's actions menu offers what the tree's does.
    act(() => (container.querySelector('button[aria-label="Actions for loose"]') as HTMLElement).click());
    expect(Array.from(container.querySelectorAll('[role="menuitem"]')).map((b) => b.textContent)).toContain('Move to…');
  });
});

describe('the layers', () => {
  it('opens a column per level of the selected chain, and the tests that verify it', () => {
    const onSelect = vi.fn();
    render(<LayersView artifacts={ARTS} selectedId="req-2" onSelect={onSelect} facts={facts} />);
    const columns = Array.from(container.querySelectorAll('section')).map((s) => s.getAttribute('aria-label'));
    // Roots, under hdg-1, under req-1, then verification.
    expect(columns).toHaveLength(4);
    expect(columns[3]).toBe('Verification');
    expect(container.querySelectorAll('button[aria-pressed="true"]')).toHaveLength(3);
    expect(container.querySelector('nav[aria-label="Selected chain"]')?.textContent).toBe('HDG-1→REQ-1→REQ-2');
    expect(container.textContent).toContain('Verifies REQ-2');
    act(() => buttonNamed('Start').click());
    expect(onSelect).toHaveBeenCalledWith('hdg-2');
  });
});

describe('the drill-down by level', () => {
  it('goes down a level on a parent, opens a leaf, and climbs back up', () => {
    const onOpen = vi.fn();
    const onBack = vi.fn();
    render(<LevelDrill artifacts={ARTS} facts={facts} onOpen={onOpen} onBack={onBack} />);
    expect(container.textContent).toContain('Top level');

    act(() => buttonNamed('Pumping').click());
    expect(container.textContent).toContain('Heading · level 1 of 3');
    act(() => buttonNamed('req-1').click());
    expect(container.textContent).toContain('Traces up to');
    expect(container.textContent).toContain('1/1 verified below');

    act(() => buttonNamed('req-2').click());
    expect(onOpen).toHaveBeenCalledWith('req-2');

    act(() => (container.querySelector('button[aria-label="Up one level"]') as HTMLElement).click());
    act(() => (container.querySelector('button[aria-label="Up one level"]') as HTMLElement).click());
    act(() => (container.querySelector('button[aria-label="Back to the list"]') as HTMLElement).click());
    expect(onBack).toHaveBeenCalled();
  });
});
