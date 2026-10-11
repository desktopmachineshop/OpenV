import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { GuidedWizard } from './GuidedWizard';
import { DialogProvider } from '../components/ui';

// Refactor plan step X18 (invariants I16; fe-suite-org-2, fe-suite-org-4):
// the saved `answers` of the guided wizard on the paths S16b's walk does not
// take, before X18 turns the steps into descriptors and the answers into a
// typed serializer. Like S16b, these must stay byte for byte as they are.
//
// S16b's in-memory server keeps the keys in the order the wizard sent them.
// The real one does not: a session's answers are a Go map, which encodes
// with its keys sorted, at every depth. So here the server answers as Go
// does, and a stored session carries keys the wizard does not know. What is
// pinned:
//
//   - a top-level key the wizard does not know survives every save (Next,
//     Skip, an applied card, the assistant's view of the state, a reopen);
//   - the saved keys keep the stored session's order, and the keys it lacks
//     follow, in the wizard's order;
//   - an unknown key inside step_1, step_2 or an entry does not survive: the
//     wizard rebuilds those;
//   - a persona's title is never cut (126 characters here); a need's sentence
//     of exactly 120 characters is not cut;
//   - a need whose persona has no artifact, and a requirement whose need has
//     none, are written with `links: []`; an unnamed persona reads "a user";
//   - Next on a step with nothing new materialises nothing and saves the
//     section ids held; a stored section heading is reused, not recreated;
//   - Skip on step 6 materialises nothing; no step_6_ids is ever written;
//   - every Next and Skip nudges the assistant before anything is saved.
//
// The full arguments of every call are written to
// __snapshots__/GuidedWizard.answers.api.json. Regenerate only for a
// deliberate change to what the wizard saves, never in a refactor:
//   npx vitest run src/views/GuidedWizard.answers.test.tsx -u

const recorder = vi.hoisted(() => ({
  calls: [] as { name: string; json: string }[],
  respond: (_name: string, _args: unknown[]): unknown => undefined,
  // The latest props the wizard gave the assistant panel.
  panel: null as null | Record<string, any>,
  // How often the panel mounted, and whether the viewport is a phone's.
  panelMounts: 0,
  compact: false,
}));

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  const out: Record<string, unknown> = { ...actual };
  for (const [ns, value] of Object.entries(actual)) {
    if (!ns.endsWith('API') || typeof value !== 'object' || value === null) continue;
    const wrapped: Record<string, unknown> = {};
    for (const [method, fn] of Object.entries(value as Record<string, unknown>)) {
      if (typeof fn !== 'function' || /Url$|URL$/.test(method)) {
        wrapped[method] = fn;
        continue;
      }
      wrapped[method] = (...args: unknown[]) => {
        const name = `${ns}.${method}`;
        recorder.calls.push({ name, json: JSON.stringify(args) });
        const data = recorder.respond(name, args);
        return data === undefined
          ? Promise.reject(new Error(`no canned response for ${name}`))
          : Promise.resolve(data).then((body) => ({ data: body, headers: {} }));
      };
    }
    out[ns] = wrapped;
  }
  return out;
});

// The assistant panel: its nudges join the call list, so their order against
// the saves is visible, and its props are kept to read the state it is given
// and to apply a card through the wizard's own handler.
vi.mock('../components/wizard/GuidedChatPanel', async () => {
  const { forwardRef, useEffect, useImperativeHandle } = await import('react');
  return {
    GuidedChatPanel: forwardRef((props: Record<string, any>, ref) => {
      recorder.panel = props;
      useEffect(() => {
        recorder.panelMounts++;
      }, []);
      useImperativeHandle(ref, () => ({
        nudge: (step: number, event: string) => {
          recorder.calls.push({ name: 'assistant.nudge', json: JSON.stringify([step, event]) });
        },
      }));
      return null;
    }),
  };
});

vi.mock('../hooks/useFeature', () => ({ useFeature: () => false }));

vi.mock('../hooks/useViewport', () => ({
  useViewport: () =>
    recorder.compact
      ? { width: 390, cls: 'phone', isPhone: true, isCompact: true, coarsePointer: true }
      : { width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false },
}));

vi.mock('../state/store', () => ({ useAppStore: (sel: any) => sel({ projectId: 'p1' }) }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// ---- the server ------------------------------------------------------------

const T = '2026-06-01T12:00:00Z';

// encoding/json writes a map's keys sorted, at every depth.
const goJSON = (value: any): any => {
  if (Array.isArray(value)) return value.map(goJSON);
  if (value !== null && typeof value === 'object') {
    const out: Record<string, any> = {};
    Object.keys(value)
      .sort()
      .forEach((k) => {
        out[k] = goJSON(value[k]);
      });
    return out;
  }
  return value;
};

let server: { sessions: any[]; artifacts: number };

const sessionById = (id: string) => server.sessions.find((s) => s.id === id);

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'guidedAPI.list': () => goJSON(server.sessions),
  'guidedAPI.start': (projectId: string) => {
    const session = {
      id: `gs-${server.sessions.length + 1}`,
      project_id: projectId,
      status: 'in-progress',
      current_step: 1,
      answers: {},
      draft_artifact_ids: [],
      created_at: T,
      updated_at: T,
    };
    server.sessions.unshift(session);
    return goJSON(session);
  },
  // SaveStep in internal/domain/guided: merge at the top level, take the step.
  'guidedAPI.saveStep': (id: string, step: number, answers: Record<string, any>) => {
    const session = sessionById(id);
    Object.entries(JSON.parse(JSON.stringify(answers))).forEach(([k, v]) => {
      session.answers[k] = v;
    });
    session.current_step = step;
    return goJSON(session);
  },
  'guidedAPI.materializeDrafts': (_id: string, drafts: any[]) => ({
    artifact_ids: drafts.map(() => `art-${++server.artifacts}`),
  }),
  'productProfileAPI.get': () => ({ project_id: 'p1', vision: '', problem_statement: '', target_users: '' }),
  'productProfileAPI.update': (_projectId: string, payload: Record<string, any>) => ({ project_id: 'p1', ...payload }),
  'artifactAPI.list': () => [],
};

recorder.respond = (name, args) => CANNED[name]?.(...args);

// ---- canned data -----------------------------------------------------------

const PERSONA_126 =
  'Pat the Planner, who schedules every maintenance window for the night shift across both halls and signs off each restart after';
const NEED_CAPABILITY = 'a list of every spindle that is live right now';
const NEED_OUTCOME = 'I can clear the floor before repairs';
const NEED_SENTENCE_120 = `As Sam the Shop Lead, I need ${NEED_CAPABILITY} so that ${NEED_OUTCOME}`;

// A session saved by an earlier visit, as the server stores it: keys the
// wizard does not know at the top level, inside step_1 and step_2 and inside
// an entry; a heading for the personas already made; steps 5 and 6 never
// saved.
const storedAnswers = () => ({
  zz_later: { kept: true, order: ['b', 'a'] },
  step_1: {
    vision: 'Every shop floor runs its machines safely.',
    problem_statement: 'Operators cannot tell which spindles are live.',
    target_users: 'Machinists and shop leads.',
    tagline: 'not kept: step 1 is rebuilt',
  },
  step_2: {
    note: 'not kept: step 2 is rebuilt',
    personas: [
      { id: 'p-1', name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe', pains: 'No overview', artifact_id: 'art-a', legacy_flag: true },
      { id: 'p-2', name: PERSONA_126, role: 'Maintenance planner', goals: 'Plan downtime', pains: 'Surprise breakdowns' },
      { id: 'p-3', name: '', role: '', goals: '', pains: '' },
    ],
  },
  step_2_ids: ['art-a'],
  step_3: {
    needs: [
      { id: 'n-1', persona_id: 'p-1', capability: NEED_CAPABILITY, outcome: NEED_OUTCOME },
      { id: 'n-2', persona_id: 'p-3', capability: 'to know who is on shift', outcome: '' },
      { id: 'n-3', persona_id: 'p-1', capability: '', outcome: '' },
    ],
  },
  step_4: {
    requirements: [
      { id: 'r-1', need_id: 'n-3', text: 'The system shall list who is on shift', fit_criterion: '', verification_method: 'analysis' },
    ],
  },
  step_7: { selected: {}, created: {} },
  section_ids: { personas: 'art-h' },
  legacy_note: 'written by an older client',
  copilot_applied: ['m-0:1'],
});

// ---- harness ---------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/guided']}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId">
              <Route path="guided" element={<GuidedWizard />} />
            </Route>
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

const click = async (label: string) => {
  const button = Array.from(container.querySelectorAll('button')).find((b) => (b.textContent ?? '').trim() === label);
  expect(button, `button "${label}"`).toBeTruthy();
  await act(async () => {
    button!.click();
  });
  await flush();
};

const brief = () =>
  recorder.calls.map(({ name, json }) => {
    const args = JSON.parse(json) as unknown[];
    const shown = args.map((a) =>
      Array.isArray(a) ? `[${a.length}]` : a !== null && typeof a === 'object' ? '{…}' : JSON.stringify(a)
    );
    return `${name}(${shown.join(', ')})`;
  });

const saves = () =>
  recorder.calls
    .filter((c) => c.name === 'guidedAPI.saveStep')
    .map((c) => JSON.parse(c.json) as [string, number, Record<string, any>]);

const drafts = () =>
  recorder.calls
    .filter((c) => c.name === 'guidedAPI.materializeDrafts')
    .map((c) => (JSON.parse(c.json) as [string, any[]])[1]);

const fullCalls = () =>
  JSON.stringify(
    recorder.calls.map(({ name, json }) => ({ call: name, args: JSON.parse(json) })),
    null,
    2
  ) + '\n';

beforeEach(() => {
  recorder.calls = [];
  recorder.panel = null;
  recorder.panelMounts = 0;
  recorder.compact = false;
  server = { sessions: [], artifacts: 0 };
  let uuid = 0;
  vi.spyOn(crypto, 'randomUUID').mockImplementation(
    () => `00000000-0000-4000-8000-${String(++uuid).padStart(12, '0')}` as `${string}-${string}-${string}-${string}-${string}`
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.restoreAllMocks();
});

// The first save after the session loads: the stored session's keys, in the
// server's order, then the ones it lacked, in the wizard's order.
const STORED_ORDER_KEYS = [
  'copilot_applied',
  'legacy_note',
  'section_ids',
  'step_1',
  'step_2',
  'step_2_ids',
  'step_3',
  'step_4',
  'step_7',
  'zz_later',
  'step_3_ids',
  'step_4_ids',
  'step_5',
  'step_5_ids',
  'step_6',
];

// Every later save: the keys of the session the last save returned, which
// the server sorted. Never a step_6_ids.
const SERVER_ORDER_KEYS = STORED_ORDER_KEYS.slice().sort();

describe('GuidedWizard saved answers (X18 characterization)', () => {
  it('keeps unknown keys and the stored key order through every save of a resumed session', async () => {
    expect(PERSONA_126).toHaveLength(126);
    expect(NEED_SENTENCE_120).toHaveLength(120);
    server.sessions.push({
      id: 'gs-1',
      project_id: 'p1',
      status: 'in-progress',
      current_step: 2,
      answers: storedAnswers(),
      draft_artifact_ids: [],
      created_at: T,
      updated_at: T,
    });

    await mount();
    await click('Next →'); // step 2
    await click('Next →'); // step 3
    await click('Next →'); // step 4
    await click('Next →'); // step 5: nothing to materialise
    await click('Skip'); // step 6
    await click('← Back');
    await click('Next →'); // step 6: nothing to materialise
    await click('Next →'); // step 7: no candidate with method test

    const nudge = (step: number, event: string) => `assistant.nudge(${step}, ${JSON.stringify(event)})`;
    const save = (step: number) => `guidedAPI.saveStep("gs-1", ${step}, {…})`;
    expect(brief()).toEqual([
      'guidedAPI.list("p1")',
      // step 2: the stored personas heading is reused; one persona is new
      nudge(3, 'saved step 2 ("Personas") and moved on to step 3 ("User needs")'),
      'guidedAPI.materializeDrafts("gs-1", [1])',
      save(3),
      // step 3: a heading, then two of the three needs
      nudge(4, 'saved step 3 ("User needs") and moved on to step 4 ("Requirements")'),
      'guidedAPI.materializeDrafts("gs-1", [1])',
      'guidedAPI.materializeDrafts("gs-1", [2])',
      save(4),
      nudge(5, 'saved step 4 ("Requirements") and moved on to step 5 ("NFRs & constraints")'),
      'guidedAPI.materializeDrafts("gs-1", [1])',
      'guidedAPI.materializeDrafts("gs-1", [1])',
      save(5),
      // step 5, 6 and 7 hold nothing new: no heading, no draft
      nudge(6, 'saved step 5 ("NFRs & constraints") and moved on to step 6 ("Hazards")'),
      save(6),
      nudge(7, 'skipped step 6 ("Hazards") and moved on to step 7 ("Verification stubs")'),
      save(7),
      nudge(7, 'saved step 6 ("Hazards") and moved on to step 7 ("Verification stubs")'),
      save(7),
      nudge(8, 'saved step 7 ("Verification stubs") and moved on to step 8 ("Review & commit")'),
      save(8),
      'artifactAPI.list("p1")',
    ]);

    // The drafts as written: the persona's title whole at 126 characters, the
    // need's sentence whole at 120, "a user" and no link for the need of the
    // unnamed persona, no link for the requirement whose need has no artifact.
    const [persona, needsHeading, needs, reqHeading, reqs] = drafts();
    expect(persona).toEqual([
      {
        type: 'persona',
        title: PERSONA_126,
        body: '**Role:** Maintenance planner\n\n**Goals:**\nPlan downtime\n\n**Pain points:**\nSurprise breakdowns',
        parent_id: 'art-h',
      },
    ]);
    expect(needsHeading).toEqual([{ type: 'heading', title: 'User Needs', body: '', sort_order: 20 }]);
    expect(needs).toEqual([
      {
        type: 'user-need',
        title: NEED_SENTENCE_120,
        body: NEED_SENTENCE_120,
        links: [{ type: 'relates-to', to_id: 'art-a' }],
        parent_id: 'art-2',
      },
      {
        type: 'user-need',
        title: 'As a user, I need to know who is on shift so that …',
        body: 'As a user, I need to know who is on shift so that …',
        links: [],
        parent_id: 'art-2',
      },
    ]);
    expect(reqHeading).toEqual([{ type: 'heading', title: 'Requirements', body: '', sort_order: 30 }]);
    expect(reqs).toEqual([
      {
        type: 'requirement',
        title: 'The system shall list who is on shift',
        body: 'The system shall list who is on shift',
        attributes: { verification_method: 'analysis' },
        links: [],
        parent_id: 'art-5',
      },
    ]);

    // Every save keeps the unknown top-level keys, in the order of the
    // session it was built over, and drops the unknown keys inside step_1,
    // step_2 and the entries.
    const all = saves();
    expect(all.map(([, step]) => step)).toEqual([3, 4, 5, 6, 7, 7, 8]);
    all.forEach(([, , answers], i) => {
      expect(Object.keys(answers)).toEqual(i === 0 ? STORED_ORDER_KEYS : SERVER_ORDER_KEYS);
      expect(answers.legacy_note).toBe('written by an older client');
      expect(answers.zz_later).toEqual({ kept: true, order: ['b', 'a'] });
      expect(Object.keys(answers.step_1)).toEqual(['vision', 'problem_statement', 'target_users']);
      expect(Object.keys(answers.step_2)).toEqual(['personas']);
      expect(Object.keys(answers.step_2.personas[0])).toEqual(['id', 'name', 'role', 'goals', 'pains', 'artifact_id']);
      expect(Object.keys(answers.step_6)).toEqual(['hazards']);
    });

    // The last save, byte for byte.
    const [, , last] = all[all.length - 1];
    expect(JSON.stringify(last)).toBe(
      JSON.stringify({
        copilot_applied: ['m-0:1'],
        legacy_note: 'written by an older client',
        section_ids: { personas: 'art-h', needs: 'art-2', requirements: 'art-5' },
        step_1: {
          vision: 'Every shop floor runs its machines safely.',
          problem_statement: 'Operators cannot tell which spindles are live.',
          target_users: 'Machinists and shop leads.',
        },
        step_2: {
          personas: [
            { id: 'p-1', name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe', pains: 'No overview', artifact_id: 'art-a' },
            { id: 'p-2', name: PERSONA_126, role: 'Maintenance planner', goals: 'Plan downtime', pains: 'Surprise breakdowns', artifact_id: 'art-1' },
            { id: 'p-3', name: '', role: '', goals: '', pains: '' },
          ],
        },
        step_2_ids: ['art-a', 'art-1'],
        step_3: {
          needs: [
            { id: 'n-1', persona_id: 'p-1', capability: NEED_CAPABILITY, outcome: NEED_OUTCOME, artifact_id: 'art-3' },
            { id: 'n-2', persona_id: 'p-3', capability: 'to know who is on shift', outcome: '', artifact_id: 'art-4' },
            { id: 'n-3', persona_id: 'p-1', capability: '', outcome: '' },
          ],
        },
        step_3_ids: ['art-3', 'art-4'],
        step_4: {
          requirements: [
            { id: 'r-1', need_id: 'n-3', text: 'The system shall list who is on shift', fit_criterion: '', verification_method: 'analysis', artifact_id: 'art-6' },
          ],
        },
        step_4_ids: ['art-6'],
        step_5: { nfrs: [] },
        step_5_ids: [],
        step_6: { hazards: [] },
        step_7: { selected: {}, created: {} },
        zz_later: { kept: true, order: ['b', 'a'] },
      })
    );

    // The state the assistant is given is the same answers but for
    // section_ids: only a Next writes the held section map; otherwise the
    // session's copy rides along, as the server returned it (keys sorted).
    expect(JSON.stringify(recorder.panel!.getState())).toBe(
      JSON.stringify({ ...last, section_ids: { needs: 'art-2', personas: 'art-h', requirements: 'art-5' } })
    );

    // An applied card saves at once, keeping the unknown keys and the order.
    recorder.calls = [];
    let results: unknown;
    await act(async () => {
      results = await recorder.panel!.onApplySuggestions([
        { suggestion: { kind: 'framing', field: 'vision', text: 'Every machine reports its state.' }, key: 'm-9:1' },
      ]);
    });
    await flush();
    expect(results).toEqual([null]);
    // On step 8 the session the save returns reloads the drafts, as in S16b.
    expect(brief()).toEqual(['guidedAPI.saveStep("gs-1", 8, {…})', 'artifactAPI.list("p1")']);
    const [[, , applied]] = saves();
    expect(Object.keys(applied)).toEqual(SERVER_ORDER_KEYS);
    expect(applied.copilot_applied).toEqual(['m-0:1', 'm-9:1']);
    expect(applied.step_1.vision).toBe('Every machine reports its state.');
    expect(applied.legacy_note).toBe('written by an older client');
    expect(applied.zz_later).toEqual({ kept: true, order: ['b', 'a'] });
  }, 20_000);

  it('seeds a reopened definition with the committed keys, unknown ones included, in their stored order', async () => {
    const answers: Record<string, any> = storedAnswers();
    delete answers.step_7;
    server.sessions.push({
      id: 'gs-1',
      project_id: 'p1',
      status: 'committed',
      current_step: 8,
      answers,
      draft_artifact_ids: [],
      created_at: T,
      updated_at: T,
    });

    await mount();
    await click('Modify guided definition');

    expect(brief()).toEqual(['guidedAPI.list("p1")', 'guidedAPI.start("p1")', 'guidedAPI.saveStep("gs-2", 1, {…})']);
    const [[, , seed]] = saves();
    // The committed answers in the server's order, the entry lists
    // re-normalised in place (unknown entry keys dropped), the applied cards
    // reset in place, and steps 5 and 6, which it lacked, after them. The
    // unknown keys inside step_1 and step_2 survive the seed itself.
    expect(JSON.stringify(seed)).toBe(
      JSON.stringify({
        copilot_applied: [],
        legacy_note: 'written by an older client',
        section_ids: { personas: 'art-h' },
        step_1: {
          problem_statement: 'Operators cannot tell which spindles are live.',
          tagline: 'not kept: step 1 is rebuilt',
          target_users: 'Machinists and shop leads.',
          vision: 'Every shop floor runs its machines safely.',
        },
        step_2: {
          personas: [
            { id: 'p-1', name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe', pains: 'No overview', artifact_id: 'art-a' },
            { id: 'p-2', name: PERSONA_126, role: 'Maintenance planner', goals: 'Plan downtime', pains: 'Surprise breakdowns' },
            { id: 'p-3', name: '', role: '', goals: '', pains: '' },
          ],
        },
        step_2_ids: ['art-a'],
        step_3: {
          needs: [
            { id: 'n-1', persona_id: 'p-1', capability: NEED_CAPABILITY, outcome: NEED_OUTCOME },
            { id: 'n-2', persona_id: 'p-3', capability: 'to know who is on shift', outcome: '' },
            { id: 'n-3', persona_id: 'p-1', capability: '', outcome: '' },
          ],
        },
        step_4: {
          requirements: [
            { id: 'r-1', need_id: 'n-3', text: 'The system shall list who is on shift', fit_criterion: '', verification_method: 'analysis' },
          ],
        },
        zz_later: { kept: true, order: ['b', 'a'] },
        step_5: { nfrs: [] },
        step_6: { hazards: [] },
      })
    );

    // What the reopened wizard then hands the assistant: the seed as the
    // server returned it, with the wizard's keys over it.
    const state = recorder.panel!.getState();
    expect(Object.keys(state)).toEqual([
      'copilot_applied',
      'legacy_note',
      'section_ids',
      'step_1',
      'step_2',
      'step_2_ids',
      'step_3',
      'step_4',
      'step_5',
      'step_6',
      'zz_later',
      'step_3_ids',
      'step_4_ids',
      'step_5_ids',
      'step_7',
    ]);
    expect(state.legacy_note).toBe('written by an older client');
    expect(state.zz_later).toEqual({ kept: true, order: ['b', 'a'] });
    expect(state.step_1).toEqual({
      vision: 'Every shop floor runs its machines safely.',
      problem_statement: 'Operators cannot tell which spindles are live.',
      target_users: 'Machinists and shop leads.',
    });
    expect(state.step_7).toEqual({ selected: {}, created: {} });
  });

  it('saves an empty section map when a fresh session moves past a step with nothing in it', async () => {
    await mount();
    await click('Start guided definition');
    await click('Next →'); // step 1
    await click('Next →'); // step 2, no personas

    expect(brief()).toEqual([
      'guidedAPI.list("p1")',
      'guidedAPI.start("p1")',
      'productProfileAPI.get("p1")',
      'assistant.nudge(2, "saved step 1 (\\"Product framing\\") and moved on to step 2 (\\"Personas\\")")',
      'productProfileAPI.update("p1", {…})',
      'guidedAPI.saveStep("gs-1", 2, {…})',
      'assistant.nudge(3, "saved step 2 (\\"Personas\\") and moved on to step 3 (\\"User needs\\")")',
      'guidedAPI.saveStep("gs-1", 3, {…})',
    ]);
    const [[, , first], [, , second]] = saves();
    const WIZARD_KEYS = [
      'step_1',
      'step_2',
      'step_2_ids',
      'step_3',
      'step_3_ids',
      'step_4',
      'step_4_ids',
      'step_5',
      'step_5_ids',
      'step_6',
      'step_7',
      'copilot_applied',
    ];
    // The first save is the wizard's own order (an empty session gives no
    // other); the second follows the server's, plus section_ids, held empty.
    expect(Object.keys(first)).toEqual(WIZARD_KEYS);
    expect(JSON.stringify(first)).toBe(
      JSON.stringify({
        step_1: { vision: '', problem_statement: '', target_users: '' },
        step_2: { personas: [] },
        step_2_ids: [],
        step_3: { needs: [] },
        step_3_ids: [],
        step_4: { requirements: [] },
        step_4_ids: [],
        step_5: { nfrs: [] },
        step_5_ids: [],
        step_6: { hazards: [] },
        step_7: { selected: {}, created: {} },
        copilot_applied: [],
      })
    );
    expect(Object.keys(second)).toEqual([...WIZARD_KEYS.slice().sort(), 'section_ids']);
    expect(second.section_ids).toEqual({});
  });

  it('writes every call it makes, in full', async () => {
    server.sessions.push({
      id: 'gs-1',
      project_id: 'p1',
      status: 'in-progress',
      current_step: 2,
      answers: storedAnswers(),
      draft_artifact_ids: [],
      created_at: T,
      updated_at: T,
    });
    await mount();
    for (let i = 0; i < 4; i++) await click('Next →');
    await click('Skip');
    await click('← Back');
    await click('Next →');
    await click('Next →');
    await expect(fullCalls()).toMatchFileSnapshot('./__snapshots__/GuidedWizard.answers.api.json');
  }, 20_000);

  // On a phone the assistant is a bottom sheet behind a button. Closed, it is
  // hidden but still mounted, so Next still nudges it; opening it shows the
  // same panel rather than mounting a new one.
  it('keeps the assistant mounted in the hidden sheet on a phone', async () => {
    recorder.compact = true;
    server.sessions.push({
      id: 'gs-1',
      project_id: 'p1',
      status: 'in-progress',
      current_step: 5,
      answers: {},
      draft_artifact_ids: [],
      created_at: T,
      updated_at: T,
    });
    await mount();

    const sheet = () => container.querySelector<HTMLElement>('[role="dialog"][aria-label="V&V Assistant"]')!;
    expect(sheet()).toBeTruthy();
    expect(sheet().getAttribute('aria-hidden')).toBe('true');
    expect(sheet().style.display).toBe('none');
    expect(recorder.panelMounts).toBe(1);
    expect(recorder.panel!.embedded).toBe(true);
    expect(recorder.panel!.sessionId).toBe('gs-1');
    expect(recorder.panel!.step).toBe(5);

    await click('Next →');
    expect(brief()).toEqual([
      'guidedAPI.list("p1")',
      'assistant.nudge(6, "saved step 5 (\\"NFRs & constraints\\") and moved on to step 6 (\\"Hazards\\")")',
      'guidedAPI.saveStep("gs-1", 6, {…})',
    ]);
    expect(recorder.panel!.step).toBe(6);

    await click('💬 Assistant');
    expect(sheet().getAttribute('aria-hidden')).toBe('false');
    expect(sheet().style.display).toBe('flex');
    await click('✕');
    expect(sheet().getAttribute('aria-hidden')).toBe('true');
    expect(recorder.panelMounts).toBe(1);
  });
});
