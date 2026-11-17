import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectHome } from './ProjectHome';
import ProjectStatus from './ProjectHomeStatus';
import { useAppStore } from '../state/store';
import { DialogProvider } from '../components/ui';

// The refreshed interface (ui-refresh) has one page for what the project is
// and where it stands. Where it stands is this column of the Overview: the
// figures, what needs the team with the agent proposals among its lines, and
// the recent activity. Summary's own address leads to the Overview.

vi.mock('../api/client', async (orig) => {
  const f = await import('../test/redesignFixtures');
  return mockApi(await orig(), {
    projectAPI: {
      get: () => f.ok({ id: 'p1', name: 'Brake ECU' }),
      list: () => f.ok([{ id: 'p1', name: 'Brake ECU' }, { id: 'p2', name: 'Steering sensor' }]),
    },
    artifactAPI: {
      list: () => f.ok(f.ARTIFACTS),
      listPage: () => f.ok([], { 'x-total-count': '131' }),
    },
    vvAPI: {
      coverage: () => f.ok(f.COVERAGE),
      listRuns: () => f.ok([f.RUN]),
      listResults: () => f.ok(f.RESULTS.slice(0, 1)),
    },
    baselineAPI: { list: () => f.ok(f.BASELINES) },
    membersAPI: { list: () => f.ok([{}, {}, {}]) },
    reviewAPI: {
      get: () =>
        f.ok({
          in_review_artifacts: [f.ARTIFACTS[0]],
          suspect_links: [
            { id: 'l3', type: 'verifies', from_id: 'tc2', from_title: 'Detection latency', from_type: 'test-case', to_id: 'req1', to_title: 'Detect wheel slip', to_type: 'requirement', updated_at: '2026-10-20T00:00:00Z' },
          ],
        }),
    },
    eventsAPI: {
      list: (params) =>
        params.event_type === 'chatter.created'
          ? f.ok([
              { id: 'c1', event_type: 'chatter.created', actor: 'user:u2', actor_name: 'Priya', payload: { artifact_id: 'req2', entry_type: 'comment', message: 'Which threshold?' }, created_at: '2026-10-21T08:00:00Z' },
            ])
          : f.ok([
              { id: 'e1', event_type: 'artifact.status_changed', actor: 'user:u1', actor_name: 'Alex', entity_name: 'REQ-9', payload: { to: 'approved' }, created_at: '2026-10-21T09:58:00Z' },
            ]),
    },
    proposalsAPI: {
      list: () => f.ok([{ id: 'pr1', run_id: 'ar1', project_id: 'p1', op: 'create_artifact', payload: { title: 'Ice track test' }, status: 'pending', review_note: '', created_at: '' }]),
    },
  });
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const gates = (refresh: boolean) =>
  useAppStore.setState({
    features: {
      channel: 'nightly' as const,
      stable_release: '',
      preview: false,
      features: { 'redesign-pages': true, 'compact-layout': true, 'ui-refresh': refresh },
    },
  } as never);

const mount = async (ui: React.ReactElement, path: string) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter initialEntries={[path]}>{ui}</MemoryRouter>
      </DialogProvider>
    );
  });
  await act(async () => {
    for (let i = 0; i < 8; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('where the project stands, on the Overview', () => {
  it('shows the figures as tiles, then what needs the team and the recent activity', async () => {
    gates(true);
    await mount(<ProjectStatus projectId="p1" />, '/projects/p1');
    const tiles = Array.from(container.querySelectorAll('[data-help-key="home.figures"] a')).map((a) => a.getAttribute('href'));
    expect(tiles).toEqual(['/projects/p1/matrix', '/projects/p1/vv', '/projects/p1/tests', '/projects/p1/review']);
    const headings = Array.from(container.querySelectorAll('section h2, section h3, section [id^="home-"]')).map((h) => (h.textContent ?? '').trim());
    expect(headings.join(' | ')).toContain('Needs you');
    expect(headings.join(' | ')).toContain('Recent activity');
    // No list of projects and no separate proposals band: Home has the first, Needs you the second.
    expect(container.querySelector('[data-help-key="home.projects"]')).toBeNull();
    expect(container.querySelector('[data-help-key="home.proposals"]')).toBeNull();
  });

  it('lists an agent proposal as a line of Needs you that opens the runs', async () => {
    gates(true);
    await mount(<ProjectStatus projectId="p1" />, '/projects/p1');
    const row = Array.from(container.querySelectorAll('li')).find((li) => (li.textContent ?? '').includes('Ice track test'));
    expect(row?.textContent).toContain('proposal');
    expect(row?.textContent).toContain('create artifact');
    expect(Array.from(row?.querySelectorAll('a') ?? []).map((a) => a.getAttribute('href'))).toContain('/projects/p1/agent-runs');
  });

  it('words a suspect link\'s two actions', async () => {
    gates(true);
    await mount(<ProjectStatus projectId="p1" />, '/projects/p1');
    const labels = Array.from(container.querySelectorAll('button')).map((b) => (b.textContent ?? '').trim());
    expect(labels).toEqual(expect.arrayContaining(['Still valid', 'Remove']));
  });
});

describe("Summary's address", () => {
  const routes = (
    <Routes>
      <Route path="/projects/:projectId" element={<div>overview page</div>} />
      <Route path="/projects/:projectId/home" element={<ProjectHome />} />
    </Routes>
  );

  it('leads to the Overview in the refreshed interface', async () => {
    gates(true);
    await mount(routes, '/projects/p1/home');
    expect(container.textContent).toBe('overview page');
  });

  it('is the Home page as it was without the key', async () => {
    gates(false);
    await mount(routes, '/projects/p1/home');
    expect(container.textContent).not.toBe('overview page');
    expect(container.querySelector('[data-help-pane="home"]')).not.toBeNull();
  });
});
