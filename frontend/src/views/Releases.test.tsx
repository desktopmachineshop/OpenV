import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { Releases } from './Releases';
import { rangeLabel } from './ReleasesParts';
import { inheritedRange } from '../components/ArtifactReleaseRange';
import { DialogProvider } from '../components/ui';
import { releasesAPI, type Artifact, type TargetRelease } from '../api/client';

// The Releases page: the releases in order with their counts, the first
// release not yet released selected, its burn-up and its requirements with
// their ranges; and the artifact editor's inherited range.

const ok = (data: unknown) => Promise.resolve({ data });

const release = (id: string, name: string, position: number, extra: Partial<TargetRelease> = {}): TargetRelease => ({
  id,
  project_id: 'p1',
  name,
  description: '',
  target_date: null,
  status: 'planned',
  position,
  released_at: null,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  ...extra,
});

const mvp = release('mvp', 'MVP', 0, { status: 'released', target_date: '2026-10-15' });
const v1 = release('v1', 'V1.0', 1, { status: 'in_progress', target_date: '2026-12-01' });
const summary = (total: number, ready: number) => ({
  total,
  introduced: 1,
  approved: ready,
  verified: ready,
  ready,
  by_status: { approved: ready, draft: total - ready },
  by_rollup: { pass: ready, uncovered: total - ready },
});

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    releasesAPI: {
      overview: () =>
        ok({
          releases: [
            { release: mvp, summary: summary(1, 1) },
            { release: v1, summary: summary(2, 1) },
          ],
          unassigned: 1,
          unassigned_ids: ['r3'],
        }),
      progress: (_p, id) =>
        ok({
          release: id === 'v1' ? v1 : mvp,
          summary: summary(2, 1),
          entries: [
            {
              requirement_id: 'r1',
              ref: 'REQ-1',
              title: 'Answer in time',
              status: 'approved',
              rollup: 'pass',
              range: { from: 'mvp', inherited_from: 'h' },
              introduced: false,
              test_case_ids: ['tc'],
              latest_results: { tc: 'pass' },
            },
            {
              requirement_id: 'r2',
              ref: 'REQ-2',
              title: 'Export the log',
              status: 'draft',
              rollup: 'uncovered',
              range: { from: 'v1', until: 'v1' },
              introduced: true,
              test_case_ids: [],
              latest_results: {},
            },
          ],
          burn_up: [
            { date: '2026-10-01', scope: 1, verified: 0 },
            { date: '2026-10-02', scope: 2, verified: 1 },
          ],
        }),
      reorder: (_p, ids) => ok(ids),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

beforeEach(async () => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter initialEntries={['/projects/p1/releases']}>
          <Routes>
            <Route path="/projects/:projectId/releases" element={<Releases />} />
          </Routes>
        </MemoryRouter>
      </DialogProvider>
    );
  });
  await flush();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('Releases page', () => {
  it('lists the releases in order with their counts, and the requirements no release targets', () => {
    const items = Array.from(container.querySelectorAll('ol[aria-label="Releases in order"] > li'));
    expect(items.map((li) => li.querySelector('button')?.textContent)).toEqual(['MVP', 'V1.0']);
    expect(items[1].textContent).toContain('2 requirements');
    expect(items[1].textContent).toContain('50% ready');
    expect(container.textContent).toContain('1 requirement targets no release.');
  });

  it('opens the first release not yet released, with its burn-up and its requirements', () => {
    expect(vi.mocked(releasesAPI.progress)).toHaveBeenCalledWith('p1', 'v1');
    expect(container.querySelector('#release-detail')?.textContent).toBe('V1.0: progress');
    expect(container.querySelector('svg[role="img"]')?.getAttribute('aria-label')).toBe(
      'Burn-up from 2026-10-01 to 2026-10-02: 1 of 2 requirements verified'
    );
    const rows = Array.from(container.querySelectorAll('tbody tr')).map((tr) =>
      Array.from(tr.querySelectorAll('td')).map((td) => td.textContent)
    );
    expect(rows).toEqual([
      ['REQ-1', 'Answer in time', 'Approved', 'Pass', 'MVP onwards (inherited)'],
      ['REQ-2', 'Export the log', 'Draft', 'Uncovered', 'V1.0 only'],
    ]);
  });

  it('moves a release later by sending the whole order', async () => {
    const later = container.querySelector('button[aria-label="Move MVP later"]') as HTMLButtonElement;
    await act(async () => later.click());
    await flush();
    expect(vi.mocked(releasesAPI.reorder)).toHaveBeenCalledWith('p1', ['v1', 'mvp']);
  });
});

describe('release ranges', () => {
  const names = { mvp: 'MVP', v1: 'V1.0', v2: 'V2' };
  it('reads a range', () => {
    expect(rangeLabel({ from: 'mvp' }, names)).toBe('MVP onwards');
    expect(rangeLabel({ from: 'v1', until: 'v1' }, names)).toBe('V1.0 only');
    expect(rangeLabel({ from: 'mvp', until: 'v2' }, names)).toBe('MVP to V2');
  });

  it("inherits the nearest ancestor's own range, skipping a release the project does not have", () => {
    const art = (id: string, parent: string | null, attrs: Record<string, string> = {}) =>
      ({ id, ref: id.toUpperCase(), title: id, parent_id: parent, attributes: attrs }) as unknown as Artifact;
    const artifacts = [
      art('h', null, { target_release: 'mvp', target_release_until: 'gone' }),
      art('g', 'h', { target_release: 'elsewhere' }),
      art('r', 'g'),
    ];
    const got = inheritedRange('g', artifacts, [mvp, v1]);
    expect(got?.source.id).toBe('h');
    expect(got?.from).toBe('mvp');
    expect(got?.until).toBe('');
    expect(inheritedRange(null, artifacts, [mvp, v1])).toBeNull();
  });
});
