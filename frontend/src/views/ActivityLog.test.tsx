import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ActivityLog } from './ActivityLog';
import { eventsAPI } from '../api/client';
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
