import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { GuidedWizard } from './GuidedWizard';

// Refactor plan step X18b (invariants I16 and I21): the guided wizard's
// session, on the paths S16b's walk and the answers tests do not take, before
// X18b moves its state and effects into useGuidedSession. The walk pins the
// happy path (Start, Next, Skip, Back, Commit, Create baseline, Modify) and
// what is saved; these pin the rest of the session's life:
//
//   - which session loads (the newest in progress, spelled either way), the
//     step it resumes at (clamped to 1..8, which is also how far the stepper
//     lets you go back), the newest committed one remembered for the start
//     page, the project it reads (the route's, else the store's, else none),
//     the loading screen, a failed load, a load that a newer project makes
//     stale, and the session a project switch leaves in place;
//   - Start: the busy label, the profile whose answer fills step 1 only when
//     the new session has none, its failure ignored, the error cleared on the
//     next try, and Start over from scratch beside a committed definition;
//   - Modify guided definition: its two calls and their failure, the busy
//     label, the session the seed's answer becomes, all eight steps open;
//   - the saved step never regresses on Next, Skip or an applied card, the
//     save's answer replaces the session, a failed Next, Skip or card says so
//     and leaves the step, the error clears on the next try, the busy labels;
//   - Abandon: the question asked, what a No and a failure leave, the start
//     page after a Yes, the progress reset;
//   - Commit and Create baseline failing, and Modify after a commit.
//
// Everything runs against the unchanged component: only the API client, the
// assistant panel (a probe that keeps its props and logs its nudges), the
// confirm dialog, the viewport and the store are replaced. The calls are
// logged in the order made.

const h = vi.hoisted(() => {
  const state = {
    calls: [] as string[],
    raw: [] as { name: string; args: any[] }[],
    impl: {} as Record<string, (...args: any[]) => unknown>,
    // The latest props the wizard gave the assistant panel.
    panel: null as null | Record<string, any>,
    compact: false,
    storeProject: 'store-p' as string | null,
    confirms: [] as any[],
    confirmAnswer: true,
  };
  const show = (a: unknown) =>
    Array.isArray(a) ? `[${a.length}]` : a !== null && typeof a === 'object' ? '{…}' : JSON.stringify(a);
  const method =
    (name: string) =>
    (...args: any[]) => {
      state.calls.push(`${name}(${args.map(show).join(', ')})`);
      state.raw.push({ name, args });
      const fn = state.impl[name];
      return fn ? fn(...args) : Promise.reject(new Error(`no canned response for ${name}`));
    };
  return { state, method };
});
const S = h.state;

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    guidedAPI: {
      list: h.method('guidedAPI.list'),
      start: h.method('guidedAPI.start'),
      saveStep: h.method('guidedAPI.saveStep'),
      abandon: h.method('guidedAPI.abandon'),
      commit: h.method('guidedAPI.commit'),
      materializeDrafts: h.method('guidedAPI.materializeDrafts'),
    },
    productProfileAPI: {
      get: h.method('productProfileAPI.get'),
      update: h.method('productProfileAPI.update'),
    },
    artifactAPI: { list: h.method('artifactAPI.list') },
    baselineAPI: { create: h.method('baselineAPI.create') },
  })
);

// The assistant: its props are kept to read the step and session it is given
// and to apply a card through the wizard's own handler; its nudges join the
// call list, so their order against the saves is visible.
vi.mock('../components/wizard/GuidedChatPanel', async () => {
  const { forwardRef, useImperativeHandle } = await import('react');
  return {
    GuidedChatPanel: forwardRef((props: Record<string, any>, ref) => {
      h.state.panel = props;
      useImperativeHandle(ref, () => ({
        nudge: (step: number, event: string) => {
          h.state.calls.push(`assistant.nudge(${step}, ${JSON.stringify(event)})`);
        },
      }));
      return null;
    }),
  };
});

vi.mock('../components/ui', async () => {
  const actual = await vi.importActual<any>('../components/ui');
  return {
    ...actual,
    useConfirm: () => async (options: unknown) => {
      h.state.confirms.push(options);
      h.state.calls.push('confirm');
      return h.state.confirmAnswer;
    },
  };
});

vi.mock('../hooks/useFeature', () => ({ useFeature: () => false }));

vi.mock('../hooks/useViewport', () => ({
  useViewport: () =>
    h.state.compact
      ? { width: 390, cls: 'phone', isPhone: true, isCompact: true, coarsePointer: true }
      : { width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false },
}));

vi.mock('../state/store', () => ({ useAppStore: (sel: any) => sel({ projectId: h.state.storeProject }) }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// ---- canned data and harness -------------------------------------------------

const T = '2026-06-01T12:00:00Z';

const session = (over: Record<string, any> = {}) => ({
  id: 'gs-1',
  project_id: 'p1',
  status: 'in-progress',
  current_step: 1,
  answers: {} as Record<string, any>,
  draft_artifact_ids: [] as string[],
  created_at: T,
  updated_at: T,
  ...over,
});

const framing = (vision: string) => ({ step_1: { vision, problem_statement: '', target_users: '' } });

const ok = (data: unknown) => Promise.resolve({ data });
const fail = (message: string) => Promise.reject(new Error(message));

const deferred = <V,>() => {
  let resolve!: (value: V) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<V>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
};

let container: HTMLDivElement;
let root: Root;
let navigate: (to: string) => void;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const Probe = () => {
  navigate = useNavigate();
  return null;
};

const mount = async (path = '/projects/p1/guided') => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[path]}>
        <Probe />
        <Routes>
          <Route path="/projects/:projectId/guided" element={<GuidedWizard />} />
          <Route path="/bare" element={<GuidedWizard />} />
        </Routes>
      </MemoryRouter>
    );
  });
  await flush();
};

const go = async (to: string) => {
  await act(async () => {
    navigate(to);
  });
  await flush();
};

const findButton = (label: string) =>
  Array.from(container.querySelectorAll('button')).find((b) => (b.textContent ?? '').trim() === label);

const click = async (label: string) => {
  const button = findButton(label);
  expect(button, `button "${label}"`).toBeTruthy();
  await act(async () => {
    button!.click();
  });
  await flush();
};

const settle = async (run: () => void) => {
  await act(async () => {
    run();
  });
  await flush();
};

const text = () => container.textContent ?? '';
const step = () => S.panel!.step as number;
// How many steps the stepper lets you jump to (compact width only).
const reached = () => container.querySelectorAll('select[aria-label="Go to step"] option:not([disabled])').length;
const saves = () => S.raw.filter((c) => c.name === 'guidedAPI.saveStep').map((c) => c.args as [string, number, Record<string, any>]);
const textareaValues = () => Array.from(container.querySelectorAll('textarea')).map((t) => t.value);

const nudge = (n: number, event: string) => `assistant.nudge(${n}, ${JSON.stringify(event)})`;

const echoSave = (id: string, n: number, answers: Record<string, any>) =>
  ok(session({ id, current_step: n, answers: JSON.parse(JSON.stringify(answers)) }));

beforeEach(() => {
  S.calls = [];
  S.raw = [];
  S.panel = null;
  S.compact = false;
  S.storeProject = 'store-p';
  S.confirms = [];
  S.confirmAnswer = true;
  S.impl = {
    'guidedAPI.saveStep': echoSave,
    'artifactAPI.list': () => ok([]),
    'productProfileAPI.get': () => ok({ project_id: 'p1', vision: '', problem_statement: '', target_users: '' }),
  };
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

// ---- loading ---------------------------------------------------------------

describe('GuidedWizard session: loading (X18b characterization)', () => {
  it('shows the loading screen until the sessions arrive, then the start page; the route names the project', async () => {
    const list = deferred<unknown>();
    S.impl['guidedAPI.list'] = () => list.promise;
    await mount();
    expect(text()).toContain('Loading guided definition…');
    expect(findButton('Start guided definition')).toBeUndefined();
    expect(S.calls).toEqual(['guidedAPI.list("p1")']);

    await settle(() => list.resolve({ data: [] }));
    expect(text()).not.toContain('Loading guided definition…');
    expect(findButton('Start guided definition')).toBeTruthy();
    expect(findButton('Start guided definition')!.disabled).toBe(false);
    expect(text()).not.toContain('Start over from scratch');
    expect(S.calls).toEqual(['guidedAPI.list("p1")']);
  });

  it("reads the store's project when the route names none, and nothing when there is none", async () => {
    S.impl['guidedAPI.list'] = () => ok([]);
    await mount('/bare');
    expect(S.calls).toEqual(['guidedAPI.list("store-p")']);
    expect(findButton('Start guided definition')).toBeTruthy();

    act(() => root.unmount());
    root = createRoot(container);
    S.calls = [];
    S.storeProject = null;
    await mount('/bare');
    expect(S.calls).toEqual([]);
    expect(text()).toContain('Loading guided definition…');
  });

  it('shows a failed load on the start page, which then still starts a session', async () => {
    S.impl['guidedAPI.list'] = () => fail('boom');
    await mount();
    expect(text()).not.toContain('Loading guided definition…');
    expect(text()).toContain('Failed to load guided sessions: boom');
    expect(findButton('Start guided definition')!.disabled).toBe(false);

    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-7' }));
    await click('Start guided definition');
    expect(S.panel!.sessionId).toBe('gs-7');
    expect(text()).not.toContain('Failed to load guided sessions');
  });

  it.each([
    [0, 1],
    [undefined, 1],
    [-4, 1],
    [1, 1],
    [3, 3],
    [8, 8],
    [9, 8],
    [99, 8],
  ])('resumes a session saved at step %s at step %s, with that many steps open', async (saved, shown) => {
    S.compact = true;
    S.impl['guidedAPI.list'] = () => ok([session({ current_step: saved })]);
    await mount();
    expect(step()).toBe(shown);
    expect(reached()).toBe(shown);
    expect(S.panel!.sessionId).toBe('gs-1');
    expect(S.calls).toEqual(['guidedAPI.list("p1")', ...(shown === 8 ? ['artifactAPI.list("p1")'] : [])]);
  });

  it('resumes the newest session in progress, spelled either way, and remembers the newest committed one', async () => {
    S.impl['guidedAPI.list'] = () =>
      ok([
        session({ id: 'gs-5', status: 'committed', current_step: 8, answers: framing('newest committed') }),
        session({ id: 'gs-4', status: 'in_progress', current_step: 2, answers: framing('newest open') }),
        session({ id: 'gs-3', status: 'in-progress', current_step: 5, answers: framing('older open') }),
        session({ id: 'gs-2', status: 'abandoned', current_step: 3 }),
        session({ id: 'gs-1', status: 'committed', current_step: 8, answers: framing('older committed') }),
      ]);
    await mount();
    expect(S.panel!.sessionId).toBe('gs-4');
    expect(step()).toBe(2);
    expect(S.calls).toEqual(['guidedAPI.list("p1")']);

    // The committed one is not shown, but it is the one Modify reopens once
    // the open one is abandoned.
    S.impl['guidedAPI.abandon'] = () => ok({});
    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-6' }));
    await click('Abandon session');
    await click('Modify guided definition');
    const [seed] = saves();
    expect(seed[0]).toBe('gs-6');
    expect(seed[2].step_1.vision).toBe('newest committed');
  });

  it('keeps an open older session in view when the list has only a committed one before it', async () => {
    S.impl['guidedAPI.list'] = () =>
      ok([session({ id: 'gs-2', status: 'committed', current_step: 8 }), session({ id: 'gs-1', current_step: 4 })]);
    await mount();
    expect(S.panel!.sessionId).toBe('gs-1');
    expect(step()).toBe(4);
  });

  it('shows Modify and Start over beside a committed definition, and neither without one', async () => {
    S.impl['guidedAPI.list'] = () => ok([session({ status: 'committed', current_step: 8 })]);
    await mount();
    expect(S.panel).toBeNull();
    expect(findButton('Modify guided definition')!.disabled).toBe(false);
    expect(findButton('Start over from scratch')!.disabled).toBe(false);
    expect(findButton('Start guided definition')).toBeUndefined();
    expect(text()).toContain('A guided definition has already been completed for this project.');
  });
});

// ---- the project changes ---------------------------------------------------

describe('GuidedWizard session: the project changes (X18b characterization)', () => {
  it('loads the new project, shows the loading screen meanwhile, and swaps the session when it has one', async () => {
    const second = deferred<unknown>();
    S.impl['guidedAPI.list'] = (projectId: string) =>
      projectId === 'p2' ? second.promise : ok([session({ id: 'gs-1', current_step: 2 })]);
    await mount();
    expect(S.panel!.sessionId).toBe('gs-1');

    await go('/projects/p2/guided');
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.list("p2")']);
    expect(text()).toContain('Loading guided definition…');

    S.compact = true;
    await settle(() => second.resolve({ data: [session({ id: 'gs-9', project_id: 'p2', current_step: 4 })] }));
    expect(text()).not.toContain('Loading guided definition…');
    expect(S.panel!.sessionId).toBe('gs-9');
    expect(step()).toBe(4);
  });

  it('leaves the session in view when the new project has no session in progress, as it does today', async () => {
    S.impl['guidedAPI.list'] = (projectId: string) =>
      ok(projectId === 'p2' ? [] : [session({ id: 'gs-1', current_step: 3 })]);
    await mount();
    await go('/projects/p2/guided');
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.list("p2")']);
    expect(S.panel!.sessionId).toBe('gs-1');
    expect(step()).toBe(3);
    expect(text()).not.toContain('Loading guided definition…');
  });

  it('ignores the answer of a project the page has left', async () => {
    const first = deferred<unknown>();
    S.impl['guidedAPI.list'] = (projectId: string) =>
      projectId === 'p1' ? first.promise : ok([session({ id: 'gs-9', project_id: 'p2', current_step: 4 })]);
    await mount();
    expect(text()).toContain('Loading guided definition…');

    await go('/projects/p2/guided');
    expect(S.panel!.sessionId).toBe('gs-9');
    expect(step()).toBe(4);

    await settle(() => first.resolve({ data: [session({ id: 'gs-1', current_step: 2 })] }));
    expect(S.panel!.sessionId).toBe('gs-9');
    expect(step()).toBe(4);

    // The same for an answer that is an error, which also does not end the
    // loading of the project the page is on.
    const stale = deferred<unknown>();
    S.impl['guidedAPI.list'] = (projectId: string) => (projectId === 'p3' ? stale.promise : ok([]));
    await go('/projects/p3/guided');
    await go('/projects/p4/guided');
    await settle(() => stale.reject(new Error('late')));
    expect(text()).not.toContain('Failed to load guided sessions');
  });

  it('keeps the loading screen while the new project loads, whatever the one left answers', async () => {
    const first = deferred<unknown>();
    const second = deferred<unknown>();
    S.impl['guidedAPI.list'] = (projectId: string) => (projectId === 'p1' ? first.promise : second.promise);
    await mount();
    await go('/projects/p2/guided');
    await settle(() => first.resolve({ data: [] }));
    expect(text()).toContain('Loading guided definition…');
    expect(findButton('Start guided definition')).toBeUndefined();

    await settle(() => second.resolve({ data: [] }));
    expect(text()).not.toContain('Loading guided definition…');
    expect(findButton('Start guided definition')).toBeTruthy();
  });

  it('drops the error of a failed load when the next project loads', async () => {
    S.impl['guidedAPI.list'] = (projectId: string) => (projectId === 'p1' ? fail('boom') : ok([]));
    await mount();
    expect(text()).toContain('Failed to load guided sessions: boom');

    await go('/projects/p2/guided');
    expect(text()).not.toContain('Failed to load guided sessions');
    expect(findButton('Start guided definition')).toBeTruthy();
  });
});

// ---- Start -----------------------------------------------------------------

describe('GuidedWizard session: Start (X18b characterization)', () => {
  beforeEach(() => {
    S.impl['guidedAPI.list'] = () => ok([]);
  });

  it('says it is starting, shows a failure and re-enables, and a retry clears the error', async () => {
    const start = deferred<unknown>();
    S.impl['guidedAPI.start'] = () => start.promise;
    await mount();
    await click('Start guided definition');
    expect(findButton('Starting…')!.disabled).toBe(true);
    expect(findButton('Start guided definition')).toBeUndefined();

    await settle(() => start.reject(new Error('nope')));
    expect(text()).toContain('Failed to start session: nope');
    expect(findButton('Start guided definition')!.disabled).toBe(false);
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")']);

    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-2' }));
    await click('Start guided definition');
    expect(S.panel!.sessionId).toBe('gs-2');
    expect(step()).toBe(1);
    expect(text()).not.toContain('Failed to start session');
    expect(S.calls.slice(2)).toEqual(['guidedAPI.start("p1")', 'productProfileAPI.get("p1")']);
  });

  it('fills step 1 from the product profile, but only when the new session has no step 1 (the profile is read either way)', async () => {
    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-2' }));
    S.impl['productProfileAPI.get'] = () =>
      ok({ project_id: 'p1', vision: 'From profile', problem_statement: 'Profile problem', target_users: 'Profile users' });
    await mount();
    await click('Start guided definition');
    expect(textareaValues()).toEqual(['From profile', 'Profile problem', 'Profile users']);

    act(() => root.unmount());
    root = createRoot(container);
    S.calls = [];
    S.impl['guidedAPI.start'] = () =>
      ok(session({ id: 'gs-3', answers: { step_1: { vision: 'Seeded', problem_statement: 'Seeded problem', target_users: '' } } }));
    await mount();
    await click('Start guided definition');
    // The profile is read all the same; its answer is only not used.
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")', 'productProfileAPI.get("p1")']);
    expect(textareaValues()).toEqual(['Seeded', 'Seeded problem', '']);
  });

  it('starts with an empty step 1 and no error when the profile cannot be read', async () => {
    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-2' }));
    S.impl['productProfileAPI.get'] = () => fail('no profile');
    await mount();
    await click('Start guided definition');
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")', 'productProfileAPI.get("p1")']);
    expect(S.panel!.sessionId).toBe('gs-2');
    expect(textareaValues()).toEqual(['', '', '']);
    expect(text()).not.toContain('no profile');
    expect(text()).not.toContain('Failed');
  });

  it('starts over from scratch with a new session and no seed, from step 1 with one step open', async () => {
    S.compact = true;
    S.impl['guidedAPI.list'] = () => ok([session({ id: 'gs-1', status: 'committed', current_step: 8, answers: framing('Kept') })]);
    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-2' }));
    await mount();
    await click('Start over from scratch');
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")', 'productProfileAPI.get("p1")']);
    expect(S.panel!.sessionId).toBe('gs-2');
    expect(step()).toBe(1);
    expect(reached()).toBe(1);
    expect(textareaValues()).toEqual(['', '', '']);
  });
});

// ---- Modify ----------------------------------------------------------------

describe('GuidedWizard session: Modify guided definition (X18b characterization)', () => {
  const committed = () => session({ id: 'gs-1', status: 'committed', current_step: 8, answers: framing('Committed vision') });

  beforeEach(() => {
    S.impl['guidedAPI.list'] = () => ok([committed()]);
  });

  it('shows a failed start, leaves the start page as it was, and tries again', async () => {
    S.impl['guidedAPI.start'] = () => fail('no room');
    await mount();
    await click('Modify guided definition');
    expect(text()).toContain('Failed to reopen definition: no room');
    expect(S.panel).toBeNull();
    expect(findButton('Modify guided definition')!.disabled).toBe(false);
    expect(findButton('Start over from scratch')).toBeTruthy();
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")']);
  });

  it('shows a failed seed save the same way, with the new session left behind unopened', async () => {
    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-2' }));
    S.impl['guidedAPI.saveStep'] = () => fail('seed refused');
    await mount();
    await click('Modify guided definition');
    expect(text()).toContain('Failed to reopen definition: seed refused');
    expect(S.panel).toBeNull();
    expect(findButton('Modify guided definition')!.disabled).toBe(false);
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")', 'guidedAPI.saveStep("gs-2", 1, {…})']);
  });

  it('says it is opening, then shows the session the seed save returned, at step 1 with all eight steps open', async () => {
    S.compact = true;
    const seed = deferred<unknown>();
    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-2' }));
    S.impl['guidedAPI.saveStep'] = () => seed.promise;
    await mount();
    await click('Modify guided definition');
    expect(findButton('Opening…')!.disabled).toBe(true);

    await settle(() =>
      seed.resolve({ data: session({ id: 'gs-2b', current_step: 1, answers: framing('Returned by the seed save') }) })
    );
    expect(S.panel!.sessionId).toBe('gs-2b');
    expect(step()).toBe(1);
    expect(reached()).toBe(8);
    // The form reads the answers the save returned, not the ones it sent.
    expect(textareaValues()).toEqual(['Returned by the seed save', '', '']);
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")', 'guidedAPI.saveStep("gs-2", 1, {…})']);
  });

  it('clears an earlier error when it succeeds', async () => {
    S.impl['guidedAPI.start'] = () => fail('first try');
    await mount();
    await click('Modify guided definition');
    expect(text()).toContain('Failed to reopen definition: first try');

    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-2' }));
    await click('Modify guided definition');
    expect(S.panel!.sessionId).toBe('gs-2');
    expect(text()).not.toContain('Failed to reopen definition');
  });
});

// ---- saving progress -------------------------------------------------------

describe('GuidedWizard session: saving progress (X18b characterization)', () => {
  const resume = async (savedStep: number, answers: Record<string, any> = {}) => {
    S.compact = true;
    S.impl['guidedAPI.list'] = () => ok([session({ current_step: savedStep, answers })]);
    await mount();
  };

  it('never saves an earlier step than the session holds on Next, and moves on one step all the same', async () => {
    await resume(5);
    await click('← Back');
    await click('← Back');
    expect(step()).toBe(3);
    expect(S.calls).toEqual(['guidedAPI.list("p1")']);

    await click('Next →');
    expect(S.calls.slice(1)).toEqual([
      nudge(4, 'saved step 3 ("User needs") and moved on to step 4 ("Requirements")'),
      'guidedAPI.saveStep("gs-1", 5, {…})',
    ]);
    expect(saves()[0][1]).toBe(5);
    expect(step()).toBe(4);
    expect(reached()).toBe(5);
  });

  it('never saves an earlier step than the session holds on Skip either', async () => {
    await resume(8);
    await click('← Back');
    await click('← Back');
    expect(step()).toBe(6);
    await click('Skip');
    expect(S.calls.slice(-2)).toEqual([
      nudge(7, 'skipped step 6 ("Hazards") and moved on to step 7 ("Verification stubs")'),
      'guidedAPI.saveStep("gs-1", 8, {…})',
    ]);
    expect(step()).toBe(7);
    expect(reached()).toBe(8);
  });

  it("takes the session the save returns as the one it now holds, and saves against that one's step", async () => {
    await resume(2);
    // The server says the session is at step 7, whatever was sent.
    S.impl['guidedAPI.saveStep'] = (id: string, _n: number, answers: Record<string, any>) =>
      ok(session({ id, current_step: 7, answers: JSON.parse(JSON.stringify(answers)) }));
    await click('Next →');
    await click('Next →');
    expect(saves().map(([, n]) => n)).toEqual([3, 7]);
    expect(step()).toBe(4);
  });

  it('opens the next step as far as it goes, and a step back leaves it open', async () => {
    await resume(2);
    expect(reached()).toBe(2);
    await click('Next →');
    expect(reached()).toBe(3);
    await click('← Back');
    expect(step()).toBe(2);
    expect(reached()).toBe(3);
    expect(S.calls.filter((c) => c.startsWith('guidedAPI.saveStep'))).toHaveLength(1);
  });

  it('says it is saving, with the abandon button off, and shows a failure without moving on', async () => {
    await resume(2);
    const save = deferred<unknown>();
    S.impl['guidedAPI.saveStep'] = () => save.promise;
    await click('Next →');
    expect(findButton('Saving…')!.disabled).toBe(true);
    expect(findButton('Abandon session')!.disabled).toBe(true);

    await settle(() => save.reject(new Error('disk full')));
    expect(text()).toContain('Failed to save step: disk full');
    expect(step()).toBe(2);
    expect(findButton('Next →')!.disabled).toBe(false);
    expect(findButton('Abandon session')!.disabled).toBe(false);
    // The assistant was nudged before the save that failed.
    expect(S.calls).toEqual([
      'guidedAPI.list("p1")',
      nudge(3, 'saved step 2 ("Personas") and moved on to step 3 ("User needs")'),
      'guidedAPI.saveStep("gs-1", 3, {…})',
    ]);
  });

  it('clears the error on the next Next, and the next Skip', async () => {
    await resume(6);
    S.impl['guidedAPI.saveStep'] = () => fail('first');
    await click('Skip');
    expect(text()).toContain('Failed to skip step: first');
    expect(step()).toBe(6);
    expect(findButton('Skip')!.disabled).toBe(false);

    S.impl['guidedAPI.saveStep'] = echoSave;
    await click('Skip');
    expect(text()).not.toContain('Failed to skip step');
    expect(step()).toBe(7);

    S.impl['guidedAPI.saveStep'] = () => fail('second');
    await click('Next →');
    expect(text()).toContain('Failed to save step: second');
    expect(step()).toBe(7);

    S.impl['guidedAPI.saveStep'] = echoSave;
    await click('Next →');
    expect(text()).not.toContain('Failed to save step');
    expect(step()).toBe(8);
  });

  it('goes back and jumps to a step reached without calling the API, and not to one beyond', async () => {
    S.impl['guidedAPI.list'] = () => ok([session({ current_step: 4 })]);
    await mount();
    expect(findButton('← Back')!.disabled).toBe(false);
    const row = (label: string) =>
      Array.from(container.querySelectorAll('div')).find((d) => d.children.length === 0 && d.textContent === label)!
        .parentElement!;
    await act(async () => row('Personas').click());
    expect(step()).toBe(2);
    await act(async () => row('Hazards').click());
    expect(step()).toBe(2);
    await act(async () => row('Requirements').click());
    expect(step()).toBe(4);
    await click('← Back');
    expect(step()).toBe(3);
    await click('← Back');
    await click('← Back');
    expect(step()).toBe(1);
    expect(findButton('← Back')!.disabled).toBe(true);
    expect(S.calls).toEqual(['guidedAPI.list("p1")']);
  });
});

// ---- an applied card -------------------------------------------------------

describe('GuidedWizard session: an applied card (X18b characterization)', () => {
  const card = (key: string, text: string) => ({ suggestion: { kind: 'framing', field: 'vision', text }, key });

  const apply = async (...items: ReturnType<typeof card>[]) => {
    let results: unknown;
    await act(async () => {
      results = await S.panel!.onApplySuggestions(items);
    });
    await flush();
    return results;
  };

  const resumeEarlier = async () => {
    S.compact = true;
    S.impl['guidedAPI.list'] = () => ok([session({ current_step: 6 })]);
    await mount();
    await click('← Back');
    await click('← Back');
    await click('← Back');
    expect(step()).toBe(3);
  };

  it('saves at the session\'s step when applied on an earlier one, and a second apply of it saves nothing', async () => {
    await resumeEarlier();
    S.calls = [];
    expect(await apply(card('m-1:1', 'Applied vision'))).toEqual([null]);
    expect(S.calls).toEqual(['guidedAPI.saveStep("gs-1", 6, {…})']);
    expect(saves()[0][2].copilot_applied).toEqual(['m-1:1']);
    expect(saves()[0][2].step_1.vision).toBe('Applied vision');
    expect(step()).toBe(3);
    expect(S.panel!.applied).toEqual({ 'm-1:1': true });

    expect(await apply(card('m-1:1', 'Applied vision'))).toEqual([null]);
    expect(S.calls).toEqual(['guidedAPI.saveStep("gs-1", 6, {…})']);
  });

  it('says a failed save may not survive a reload, and keeps the card applied in the form', async () => {
    await resumeEarlier();
    S.impl['guidedAPI.saveStep'] = () => fail('offline');
    expect(await apply(card('m-1:1', 'Applied vision'))).toEqual([null]);
    expect(text()).toContain(
      'Suggestion applied in the wizard, but saving the session failed — it may not survive a reload.'
    );
    expect(text()).not.toContain('offline');
    expect(S.panel!.applied).toEqual({ 'm-1:1': true });
    expect(S.panel!.getState().step_1.vision).toBe('Applied vision');
    expect(step()).toBe(3);
  });

  it("takes the session its save returns as the one it holds", async () => {
    await resumeEarlier();
    S.impl['guidedAPI.saveStep'] = (id: string, _n: number, answers: Record<string, any>) =>
      ok(session({ id, current_step: 7, answers: JSON.parse(JSON.stringify(answers)) }));
    await apply(card('m-1:1', 'Applied vision'));
    await click('Next →');
    expect(saves().map(([, n]) => n)).toEqual([6, 7]);
  });
});

// ---- Abandon ---------------------------------------------------------------

describe('GuidedWizard session: Abandon (X18b characterization)', () => {
  const resume = async (extra: ReturnType<typeof session>[] = []) => {
    S.compact = true;
    S.impl['guidedAPI.list'] = () => ok([session({ id: 'gs-3', current_step: 5 }), ...extra]);
    await mount();
  };

  it('asks first, and a No leaves the session as it was', async () => {
    await resume();
    S.confirmAnswer = false;
    await click('Abandon session');
    expect(S.confirms).toEqual([
      {
        title: 'Abandon guided session',
        message:
          'Abandon this guided session? Draft artifacts already created are kept, but unsaved entries are discarded.',
        confirmLabel: 'Abandon',
        danger: true,
      },
    ]);
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'confirm']);
    expect(S.panel!.sessionId).toBe('gs-3');
    expect(step()).toBe(5);
    expect(findButton('Abandon session')!.disabled).toBe(false);
  });

  it('drops the session on a Yes and returns to the start page, from which Start begins at step 1', async () => {
    await resume();
    S.impl['guidedAPI.abandon'] = () => ok({});
    await click('Abandon session');
    expect(S.calls).toEqual(['guidedAPI.list("p1")', 'confirm', 'guidedAPI.abandon("gs-3")']);
    expect(findButton('Start guided definition')!.disabled).toBe(false);
    expect(text()).not.toContain('Failed');
    expect(findButton('Abandon session')).toBeUndefined();

    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-4' }));
    await click('Start guided definition');
    expect(S.panel!.sessionId).toBe('gs-4');
    expect(step()).toBe(1);
    expect(reached()).toBe(1);
  });

  it('keeps the session on a failure, says so, and clears it on a success', async () => {
    await resume();
    S.impl['guidedAPI.abandon'] = () => fail('locked');
    await click('Abandon session');
    expect(text()).toContain('Failed to abandon session: locked');
    expect(S.panel!.sessionId).toBe('gs-3');
    expect(step()).toBe(5);
    expect(findButton('Abandon session')!.disabled).toBe(false);

    S.impl['guidedAPI.abandon'] = () => ok({});
    await click('Abandon session');
    expect(findButton('Start guided definition')).toBeTruthy();
    expect(text()).not.toContain('Failed to abandon session');
  });
});

// ---- Commit, Create baseline and Modify after a commit -----------------------

describe('GuidedWizard session: after the review (X18b characterization)', () => {
  const resumeAtReview = async () => {
    S.compact = true;
    S.impl['guidedAPI.list'] = () =>
      ok([session({ id: 'gs-8', current_step: 8, answers: framing('Held vision') })]);
    await mount();
    expect(step()).toBe(8);
  };

  it('says it is committing, shows a failure on the review, and a retry reaches the committed page', async () => {
    await resumeAtReview();
    const commit = deferred<unknown>();
    S.impl['guidedAPI.commit'] = () => commit.promise;
    await click('Commit definition');
    expect(findButton('Committing…')!.disabled).toBe(true);

    await settle(() => commit.reject(new Error('conflict')));
    expect(text()).toContain('Failed to commit: conflict');
    expect(text()).not.toContain('Requirements committed');
    expect(findButton('Commit definition')!.disabled).toBe(false);
    expect(step()).toBe(8);

    S.impl['guidedAPI.commit'] = () => ok(session({ id: 'gs-8', status: 'committed' }));
    await click('Commit definition');
    expect(text()).toContain('Requirements committed');
    expect(text()).not.toContain('Failed to commit');
    expect(S.calls.filter((c) => c.startsWith('guidedAPI.commit'))).toEqual(['guidedAPI.commit("gs-8")', 'guidedAPI.commit("gs-8")']);
  });

  it('shows a baseline that failed, and then the one that was made', async () => {
    await resumeAtReview();
    S.impl['guidedAPI.commit'] = () => ok(session({ id: 'gs-8', status: 'committed' }));
    S.impl['baselineAPI.create'] = () => fail('name taken');
    await click('Commit definition');
    await click('Create baseline');
    expect(text()).toContain('Failed to create baseline: name taken');
    expect(findButton('Create baseline')!.disabled).toBe(false);
    expect(S.calls.slice(-1)).toEqual(['baselineAPI.create("p1", "Initial requirements")']);

    S.impl['baselineAPI.create'] = () => ok({});
    await click('Create baseline');
    expect(text()).toContain('✓ Baseline "Initial requirements" created');
    expect(text()).not.toContain('Failed to create baseline');
  });

  it('reopens the committed definition from the session it held, not the one the commit returned', async () => {
    await resumeAtReview();
    S.impl['guidedAPI.commit'] = () => ok(session({ id: 'gs-8', status: 'committed', answers: framing('Returned by commit') }));
    await click('Commit definition');
    S.impl['guidedAPI.start'] = () => fail('first try');
    await click('Modify guided definition');
    // Back on the start page, with the failure and both ways in.
    expect(text()).toContain('Failed to reopen definition: first try');
    expect(S.panel!.sessionId).toBe('gs-8');
    expect(findButton('Modify guided definition')).toBeTruthy();
    expect(findButton('Start over from scratch')).toBeTruthy();

    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-9' }));
    await click('Modify guided definition');
    const [seed] = saves();
    expect(seed[0]).toBe('gs-9');
    expect(seed[1]).toBe(1);
    expect(seed[2].step_1.vision).toBe('Held vision');
    expect(step()).toBe(1);
    expect(reached()).toBe(8);
    expect(text()).not.toContain('Failed to reopen definition');
  });

  it('starts over from the start page a failed Modify leaves, at step 1 with one step open', async () => {
    await resumeAtReview();
    S.impl['guidedAPI.commit'] = () => ok(session({ id: 'gs-8', status: 'committed' }));
    await click('Commit definition');
    S.impl['guidedAPI.start'] = () => fail('first try');
    await click('Modify guided definition');
    expect(step()).toBe(8);

    S.impl['guidedAPI.start'] = () => ok(session({ id: 'gs-9' }));
    await click('Start over from scratch');
    expect(S.panel!.sessionId).toBe('gs-9');
    expect(step()).toBe(1);
    expect(reached()).toBe(1);
    expect(S.raw.filter((c) => c.name === 'guidedAPI.saveStep')).toEqual([]);
  });
});
