import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { GuidedWizard } from './GuidedWizard';
import { artifactAPI, guidedAPI } from '../api/client';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// An assistant card that changes the project, applied from the wizard: once
// the artifact is created the card says so, the wizard records it, and the
// card offers no second add (#379, bug 109), not even to a second click
// that lands before the first one's answer (bug 119).

const T = '2026-06-01T12:00:00Z';

const session = {
  id: 'gs-1',
  project_id: 'p1',
  status: 'in-progress',
  current_step: 8,
  answers: {},
  draft_artifact_ids: [],
  created_at: T,
  updated_at: T,
};

const HEADING = {
  id: 'art-1',
  ref: 'SEC-9',
  project_id: 'p1',
  parent_id: null,
  type: 'heading',
  title: 'Design',
  body: '',
  sort_order: 70,
  status: 'approved',
  attributes: { status: 'approved' },
  version: 1,
  created_at: T,
  updated_at: T,
};

const CARD = { kind: 'artifact', type: 'design-item', title: 'Interlock wiring', body: 'Per drawing E-12.', parent: 'SEC-9' };

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    guidedAPI: {
      list: () => ok([JSON.parse(JSON.stringify(session))]),
      listMessages: () =>
        ok([
          {
            id: 'm-1',
            session_id: 'gs-1',
            role: 'assistant',
            content: ['The interlock wiring belongs in the design section:', '```openv-suggestion\n' + JSON.stringify(CARD) + '\n```'].join('\n\n'),
            created_at: T,
          },
        ]),
      saveStep: (id: string, step: number, answers: Record<string, any>) => ok({ ...session, id, current_step: step, answers }),
    },
    artifactAPI: {
      list: () => ok([HEADING]),
      create: (payload: Record<string, any>) => ok({ ...HEADING, ...payload, id: 'art-2', ref: 'DI-1', status: 'draft' }),
    },
  })
);

vi.mock('../hooks/useFeature', () => ({ useFeature: (key: string) => key === 'assistant-project-edits' }));

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

// The assistant's stream; this test's reply is already in the transcript.
class FakeEventSource {
  addEventListener() {}
  close() {}
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
const realEventSource = (globalThis as any).EventSource;
const realScrollTo = (Element.prototype as any).scrollTo;

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const buttons = (text: string) =>
  Array.from(container.querySelectorAll('button')).filter((b) => (b.textContent ?? '').trim() === text);

beforeEach(() => {
  vi.clearAllMocks();
  (globalThis as any).EventSource = FakeEventSource;
  (Element.prototype as any).scrollTo = () => {};
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
  (globalThis as any).EventSource = realEventSource;
  (Element.prototype as any).scrollTo = realScrollTo;
});

const mount = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/guided']}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/guided" element={<GuidedWizard />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

describe('GuidedWizard project card', () => {
  it('reports an applied project card as added, records it and offers no second add', async () => {
    await mount();

    const add = buttons('+ Add to project');
    expect(add).toHaveLength(1);
    await act(async () => {
      add[0].click();
    });
    await flush();

    expect(vi.mocked(artifactAPI.create)).toHaveBeenCalledTimes(1);
    expect(container.textContent).not.toContain('could not be applied');
    expect(container.textContent).toContain('✓ Added to project');
    expect(buttons('+ Add to project')).toEqual([]);

    // Recorded in the session, so a reload does not offer the card again.
    const saves = vi.mocked(guidedAPI.saveStep).mock.calls;
    expect(saves).toHaveLength(1);
    expect(saves[0][2].copilot_applied).toEqual(['m-1:1']);
  });

  // Bug 119: the card was marked added only once the first click's answer
  // came back, so a double-click created the artifact twice.
  it('creates the artifact once for a double-click', async () => {
    await mount();

    const add = buttons('+ Add to project')[0];
    await act(async () => {
      add.click();
      add.click();
    });
    await flush();

    expect(vi.mocked(artifactAPI.create)).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain('✓ Added to project');
    expect(buttons('+ Add to project')).toEqual([]);
    expect(vi.mocked(guidedAPI.saveStep).mock.calls.map((c) => c[2].copilot_applied)).toEqual([['m-1:1']]);
  });
});
