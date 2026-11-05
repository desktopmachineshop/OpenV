import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { Artifact } from '../../api/client';
import { buildFacts } from '../../utils/artifactFacts';
import { useAppStore } from '../../state/store';
import { buildListSchema, listAccessors } from './listSearch';
import { useListView } from './useListView';
import { TableView } from './TableView';

let chipSearchOn = true;
vi.mock('../../hooks/useFeature', () => ({ useFeature: () => chipSearchOn }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const art = (id: string, type: string, parent: string | null, order: number, extra: Partial<Artifact> = {}): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type,
    title: id,
    body: '',
    sort_order: order,
    status: 'draft',
    attributes: {},
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  }) as Artifact;

//  hdg-1 Pumping
//    req-1   in review, Dana, must
//    req-2   draft, should, colour=red
//  tc-1      test case, Dana
const ARTS = [
  art('hdg-1', 'heading', null, 1, { title: 'Pumping', doc_number: '1' }),
  art('req-1', 'requirement', 'hdg-1', 1, { status: 'in_review', attributes: { owner: 'Dana', priority: 'must' } }),
  art('req-2', 'requirement', 'hdg-1', 2, { attributes: { priority: 'should', colour: 'red' } }),
  art('tc-1', 'test-case', null, 2, { attributes: { owner: 'dana' } }),
];
const facts = buildFacts(ARTS, [], undefined, []);

describe('the list search schema', () => {
  it('offers the facets the issue names, from the artifacts in view', () => {
    const schema = buildListSchema(ARTS);
    const field = (k: string) => schema.fields.find((f) => f.key === k)!;
    expect(field('type').options!.map((o) => o.value)).toEqual(['heading', 'requirement', 'test-case']);
    expect(field('priority').options!.map((o) => o.value)).toEqual(['must', 'should']);
    expect(field('owner').options!.map((o) => o.value)).toEqual(['dana', 'Dana']);
    expect(field('parent').options).toEqual([{ value: 'hdg-1', label: '1 Pumping' }]);
    expect(schema.quickFilters!.map((q) => q.key)).toEqual(['mine', 'in_review', 'failing', 'suspect']);
    expect(schema.groupBy!.map((g) => g.key)).toEqual(['status', 'type', 'priority', 'owner', 'parent']);
    expect(schema.customGroups).toEqual([{ key: 'attr:colour', label: 'colour' }]);
  });

  it('reads Mine against the member, any case, and a row heading as its section', () => {
    const acc = listAccessors(ARTS, facts, ['DANA', 'dana@example.com']);
    expect(ARTS.filter((a) => acc.quickMatch!(a, 'mine')).map((a) => a.id)).toEqual(['req-1', 'tc-1']);
    expect(acc.fieldValue(ARTS[2], 'parent')).toBe('hdg-1');
    expect(acc.fieldValue(ARTS[2], 'attr:colour')).toBe('red');
    expect(acc.textMatch!(ARTS[1], 'req-1')).toBe(true);
  });
});

describe('useListView with the chip search', () => {
  let container: HTMLDivElement;
  let root: Root;
  let out: ReturnType<typeof useListView>;
  const Probe: React.FC<{ params: string }> = ({ params }) => {
    out = useListView({
      searched: ARTS,
      artifacts: ARTS,
      links: [],
      quality: undefined,
      coverage: [],
      searchParams: new URLSearchParams(params),
      setSearchParams: vi.fn(),
    });
    return null;
  };

  beforeEach(() => {
    chipSearchOn = true;
    useAppStore.setState({ currentUser: { id: 'u', name: 'Dana', email: 'dana@example.com' } as any });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    useAppStore.setState({ currentUser: null });
  });

  it('applies the chips in the URL and groups the rows for the table', () => {
    act(() => root.render(<Probe params="f=type:requirement|test-case&g=owner" />));
    expect(out.filteredArtifacts.map((a) => a.id)).toEqual(['req-1', 'req-2', 'tc-1']);
    expect(out.typeCounts).toEqual([
      { type: 'requirement', count: 2 },
      { type: 'test-case', count: 1 },
    ]);
    expect(out.tableGroups!.map((g) => [g.label, g.rows.map((a) => a.id)])).toEqual([
      ['dana', ['tc-1']],
      ['Dana', ['req-1']],
      ['No owner', ['req-2']],
    ]);
    expect(out.groupedBy).toBe('Owner');
    expect(out.chipQuickCounts.mine).toBe(2);

    act(() => root.render(<Probe params="qf=mine&qf=in_review" />));
    expect(out.filteredArtifacts.map((a) => a.id)).toEqual(['req-1']);
    expect(out.tableGroups).toBeUndefined();
  });

  it('leaves the URL chips alone without the feature', () => {
    chipSearchOn = false;
    act(() => root.render(<Probe params="f=type:requirement&g=owner" />));
    expect(out.filteredArtifacts).toHaveLength(4);
    expect(out.tableGroups).toBeUndefined();
  });

  it('draws the table under the chosen groups', () => {
    act(() => root.render(<Probe params="g=status" />));
    act(() =>
      root.render(
        <TableView
          artifacts={out.filteredArtifacts}
          allArtifacts={ARTS}
          total={ARTS.length}
          selectedId={null}
          onSelect={() => {}}
          facts={facts}
          readOnly
          canPaste={false}
          onReorder={() => {}}
          onContextMenuAction={() => {}}
          phone={false}
          groups={out.tableGroups}
          groupedBy={out.groupedBy}
        />
      )
    );
    const headers = Array.from(container.querySelectorAll('[role="rowgroup"] > button')).map((b) => b.textContent);
    expect(headers).toEqual(['Draft2', 'In review1']);
    expect(container.textContent).toContain('grouped by status');
  });
});
