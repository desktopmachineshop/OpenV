import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectHome } from './ProjectHome';
import { describeEvent } from './ProjectHomeData';
import { useAppStore } from '../state/store';
import { projectAPI } from '../api/client';
import { features } from '../test/redesignFixtures';
import { DialogProvider } from '../components/ui';

// The Home page: gated by redesign-pages; its figures, what needs the team,
// the audit trail, the projects and the agent proposals, from the API alone.

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

const render = async (on: boolean) => {
  useAppStore.setState({ features: features(on) });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
      <MemoryRouter initialEntries={['/projects/p1/home']}>
        <Routes>
          <Route path="/projects/:projectId/home" element={<ProjectHome />} />
        </Routes>
      </MemoryRouter>
      </DialogProvider>
    );
  });
  await act(async () => {
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
  });
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState({ features: null });
});

describe('Home page', () => {
  it('says the page is not in the release, and reads nothing, while the gate is off', async () => {
    vi.mocked(projectAPI.get).mockClear();
    await render(false);
    expect(container.textContent).toContain('The Home page is not in your workspace');
    expect(projectAPI.get).not.toHaveBeenCalled();
  });

  it('shows the figures and everything that needs the team', async () => {
    await render(true);
    const text = container.textContent || '';
    expect(container.querySelector('h1')?.textContent).toBe('Brake ECU');
    expect(text).toContain('Last baseline BL-2');
    expect(text).toContain('3 members');
    expect(text).toMatch(/6\s*artifacts/);
    expect(text).toMatch(/50%\s*traced/);
    const kinds = Array.from(container.querySelectorAll('[aria-labelledby="home-needs"] li .ov-chip')).map((c) => c.textContent);
    expect(kinds).toEqual(['approve', 'suspect', 'test run', 'failing', 'comment']);
    expect(text).toContain('1 of 2 test cases left');
    expect(text).toContain('Priya: Which threshold?');
  });

  it('shows the audit trail, the projects and the proposals', async () => {
    await render(true);
    const text = container.textContent || '';
    expect(text).toContain('Alex approved REQ-9');
    expect(text).toContain('Steering sensor');
    expect(text).toContain('131 artifacts');
    expect(text).toContain('1 agent proposal waiting');
    expect(text).toContain('create artifact “Ice track test”');
  });
});

describe('describeEvent', () => {
  it('reads a failing result as bad and an agent as an agent', () => {
    const e = { id: 'x', event_type: 'testrun.recorded', actor: 'agent:a', actor_kind: 'agent', payload: { status: 'fail' }, created_at: '2026-10-21T09:00:00Z', entity_name: 'TC-2' };
    const got = describeEvent(e, new Date('2026-10-21T12:00:00Z'));
    expect(got.what).toBe('recorded fail on');
    expect(got.tone).toBe('bad');
    expect(got.who).toBe('Agent');
  });
});
