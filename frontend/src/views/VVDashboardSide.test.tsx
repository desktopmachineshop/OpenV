import { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { GapsCard } from './VVDashboardSide';
import { useAssistantStore } from '../components/wizard/assistantStore';
import type { Artifact } from '../api/client';

// The V&V dashboard's gaps card with the floating assistant (#728): "Draft
// tests" opens the assistant and starts a turn naming the gaps the card
// shows, and each row's ✦ does the same for its one gap. With the gate
// closed the card keeps its link to the Agents page.

let featureOn = true;
vi.mock('../hooks/useFeature', () => ({ useFeature: () => featureOn }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const a = (id: string, ref: string, title: string) => ({ id, ref, title }) as Artifact;
const ARTIFACTS: Record<string, Artifact> = {
  r1: a('r1', 'REQ-153', 'Brake light'),
  r2: a('r2', 'REQ-88', 'Backup and restore'),
  r3: a('r3', 'REQ-54', 'Night mode'),
};
const GROUPS = [
  { key: 'no_test', label: 'No test', why: 'no test', ids: ['r1', 'r3'] },
  { key: 'unverified', label: 'Unverified', why: 'unverified', ids: ['r2'] },
];

let container: HTMLDivElement;
let root: Root;

const render = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <GapsCard projectId="p1" groups={GROUPS} artifacts={ARTIFACTS} pendingProposals={0} isPhone={false} loading={false} />
      </MemoryRouter>
    );
  });
};

beforeEach(() => {
  featureOn = true;
  useAssistantStore.setState({ panel: 'closed', page: {}, override: null, dismissed: null, request: null });
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

describe('GapsCard and the V&V Assistant', () => {
  it('"Draft tests" opens the assistant with every gap shown', async () => {
    await render();
    const button = Array.from(container.querySelectorAll('button')).find((b) => b.textContent?.includes('Draft tests'))!;
    expect(container.querySelector('a[href="/projects/p1/agents"]')).toBeNull();
    await act(async () => button.click());
    const s = useAssistantStore.getState();
    expect(s.panel).toBe('open');
    expect(s.override).toEqual({ label: '3 V&V gaps' });
    expect(s.request!.message.split('\n')[0]).toBe('Drafting test cases for 3 gaps: REQ-153, REQ-54, REQ-88.');
    expect(s.request!.message).toContain('REQ-88 Backup and restore (unverified; artifact id r2)');
  });

  it("a row's ✦ starts the same for that one gap", async () => {
    await render();
    const ask = container.querySelector('button[aria-label="Draft tests for REQ-88 in the V&V Assistant"]') as HTMLButtonElement;
    await act(async () => ask.click());
    const s = useAssistantStore.getState();
    expect(s.override).toEqual({ label: 'REQ-88 (V&V gap)' });
    expect(s.request!.message.split('\n')[0]).toBe('Drafting test cases for 1 gap: REQ-88.');
    // The row still links to the artifact.
    expect(container.querySelector('a[href="/projects/p1/requirements?artifact=r2"]')).not.toBeNull();
  });

  it('keeps the Agents link with the gate closed', async () => {
    featureOn = false;
    await render();
    expect(container.querySelector('a[href="/projects/p1/agents"]')).not.toBeNull();
    expect(container.querySelector('.vv-gap-ask')).toBeNull();
  });
});
