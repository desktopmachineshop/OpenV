import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ActivityLog } from './ActivityLog';
import { artifactAPI, eventsAPI } from '../api/client';
import { useAppStore } from '../state/store';

// The activity log's type filter offers every event type a project's
// activity can hold (#379, bug 83): all of the backend's but the
// workspace-level org.* ones, which carry no project.

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    eventsAPI: { list: () => Promise.resolve({ data: [], headers: {} }) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const filter = () => container.querySelector<HTMLSelectElement>('select[aria-label="Filter by event type"]')!;

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ ...initialStore, projectId: 'p1' }, true);
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
});

describe('ActivityLog type filter', () => {
  it('offers every event type a project can hold, and filters by it', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/projects/p1/activity']}>
          <Routes>
            <Route path="/projects/:projectId/activity" element={<ActivityLog />} />
          </Routes>
        </MemoryRouter>
      );
    });
    await flush();

    expect(Array.from(filter().options).map((o) => o.value)).toEqual([
      'all',
      'artifact.created',
      'artifact.updated',
      'artifact.deleted',
      'artifact.status_changed',
      'artifact.restored',
      'link.created',
      'link.updated',
      'link.deleted',
      'baseline.captured',
      'baseline.deleted',
      'project.review_round_started',
      'chatter.created',
      'testrun.recorded',
      'workitem.created',
      'workitem.moved',
      'workitem.updated',
      'agentrun.finished',
      'agentrun.successors_skipped',
      'proposal.created',
      'project.member_added',
      'project.member_role_changed',
      'project.member_removed',
    ]);

    // A status change, which the filter could not find before, is asked for
    // by its own type.
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(filter(), 'artifact.status_changed');
      filter().dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(vi.mocked(eventsAPI.list)).toHaveBeenLastCalledWith({
      project_id: 'p1',
      limit: 100,
      event_type: 'artifact.status_changed',
    });
  });
});

// #734: each event a sentence under its day, linked to what it is about, and
// opened, the detail for its kind, with the raw event behind a toggle.
describe('ActivityLog rows', () => {
  const today = new Date();
  today.setHours(0, 8, 0, 0);
  const statusChange = {
    id: '0a9647ac-e806-48d9-a8cb-5ed1f7bf6fe8',
    event_type: 'artifact.status_changed',
    project_id: 'p1',
    entity_id: '0c536833-4049-4000-8000-000000000000',
    actor: 'worker:org1',
    actor_kind: 'worker',
    actor_id: '6196dbea-0000',
    actor_name: 'Workspace runner',
    entity_kind: 'test-case',
    entity_name: 'Phone traceability matrix coverage suite',
    payload: { artifact_type: 'test-case', from: 'in_review', title: 'Phone traceability matrix coverage suite', to: 'approved', version: 4 },
    created_at: today.toISOString(),
  };
  const link = {
    id: 'e2',
    event_type: 'link.created',
    project_id: 'p1',
    entity_id: 'l1',
    actor: 'user:u1',
    actor_kind: 'user',
    actor_name: 'David',
    payload: { from_id: '0c536833-4049-4000-8000-000000000000', to_id: 'req229', link_type: 'verifies' },
    created_at: '2026-11-03T10:00:00Z',
  };

  it('reads as sentences under day headers, links the artifact, and draws a status change as chips', async () => {
    vi.mocked(eventsAPI.list).mockResolvedValue({ data: [statusChange, link], headers: {} } as any);
    vi.mocked(artifactAPI.list).mockResolvedValue({
      data: [
        { id: '0c536833-4049-4000-8000-000000000000', ref: 'TC-177', title: 'Phone traceability matrix coverage suite', type: 'test-case' },
        { id: 'req229', ref: 'REQ-229', title: 'Traceability matrix on a phone', type: 'requirement' },
      ],
    } as any);
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/projects/p1/activity']}>
          <Routes>
            <Route path="/projects/:projectId/activity" element={<ActivityLog />} />
          </Routes>
        </MemoryRouter>
      );
    });
    await flush();

    const text = container.textContent || '';
    expect(container.querySelector('h3')?.textContent).toBe('Today');
    expect(text).toContain('Workspace runner(runner) approved TC-177');
    expect(text).not.toContain('6196dbea');
    expect(text).not.toContain('artifact.status_changed');
    const first = container.querySelector('li a')!;
    expect(first.getAttribute('href')).toBe('/projects/p1/requirements?artifact=0c536833-4049-4000-8000-000000000000');
    expect(text).toContain('TC-177 Phone traceability matrix coverage suite —verifies→ REQ-229 Traceability matrix on a phone');

    const toggle = container.querySelector<HTMLButtonElement>('button[aria-label="Show details"]')!;
    await act(async () => toggle.click());
    const chips = Array.from(container.querySelectorAll('li .ov-chip')).map((c) => c.textContent);
    expect(chips).toEqual(['In review', 'Approved']);
    expect(container.textContent).toContain('version 4');
    expect(container.querySelector('pre')).toBeNull();

    const raw = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Show raw event')!;
    await act(async () => raw.click());
    expect(container.querySelector('pre')?.textContent).toContain('"to": "approved"');
  });
});
