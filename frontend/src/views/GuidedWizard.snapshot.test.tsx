import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { GuidedWizard } from './GuidedWizard';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// Refactor plan step S16b (invariants I16 and I21): the guided wizard as it
// behaves today, walked through every step, before F5 moves matchEntry,
// applySuggestionToDraft, buildAnswersFrom and the artifact templates out and
// X18 turns the steps into descriptors with typed answers. Both re-run it
// first and must leave every file snapshot, the ordered API calls and the
// saved `answers` JSON identical: the answers are stored data (I16).
//
// One walk, desktop width, the assistant column beside the steps. The
// assistant's replies arrive over its stream and carry suggestion cards,
// which the walk applies; typed entries go in beside them.
//
//   landing    no session yet; Start.
//   step-1     framing pre-filled from the product profile; a vision
//              suggestion replaces it, one for an unknown field is refused;
//              target users typed. Next.
//   step-2     two personas added and one replaced by its title in any case
//              (one batch); a third typed with a trailing space, which its
//              title and its needs' sentences leave out (#379, bug 110); a
//              fourth left empty, which is saved but never materialised. Next.
//   step-3     needs from cards: one matched to its persona by name, whose
//              sentence is exactly 121 characters (cut to 117 and "…"), one
//              for an unknown persona (falls to the first) and then replaced by
//              its capability in another case, which moves it to the typed
//              persona; a persona replace by id is refused, the persona being
//              an artifact. One need typed. Next.
//   step-4     requirements of exactly 121 characters (cut) and 120 (kept),
//              each matched to its need by part of the capability, the second
//              with an unknown verification method (falls to test) and then
//              given a fit criterion by a replace naming its entry id; one with
//              no text (refused); one typed with method analysis. Next.
//   step-5     NFRs of exactly 101 characters (cut to 97 and "…") and 100
//              (kept, method analysis, so no stub), categories written in lower
//              case and unknown (falls to Performance); one replaced by its
//              text in another case, with an unknown method (kept as it was);
//              one typed under Usability. Next.
//   step-6     hazards: one 132 characters long (never cut) whose card says
//              severity "Medium", which the wizard stores as "moderate"; one
//              replaced by its title; one typed. Skip, which saves and does not
//              materialise; Back from step 7, then Next, which materialises and
//              writes no step_6_ids.
//   step-7     the requirement and NFR candidates with method test; one
//              unchecked. Skip, which saves the selection and creates no stub;
//              Back from step 8, then Next, which creates the stubs ("Verify: …",
//              never cut).
//   step-8     the drafts; a "New artifact" card (the notes panel's template,
//              cut at 120) added to the project under a heading named by ref,
//              which then reads as added and is saved as applied (#379, bug
//              109; see the call list). Commit. The notes panel's other templates, its "Medium"
//              default severity among them, are not reachable from the wizard.
//   committed  after Create baseline. Then Modify guided definition.
//   reopened   the new session seeded from the committed answers.
//
// Every Next and Skip nudges the assistant before anything is saved: the
// ordered call list pins that.
//
// Each snapshot is container.innerHTML, one tag per line, followed by the
// form controls' values (innerHTML does not carry them), written to
// __snapshots__/GuidedWizard.<name>.html. Every API call with its full
// arguments goes to __snapshots__/GuidedWizard.api.json. The store is the
// real one; children, the assistant panel included, render for real; only
// the API client (and the browser's EventSource) is replaced, by a small
// in-memory server.
//
// Regenerate only for a deliberate change to what the wizard shows, loads or
// saves, never in a refactor pull request:
//   npx vitest run src/views/GuidedWizard.snapshot.test.tsx -u

// Every *API namespace of the client is replaced by recorders over a canned
// responder: the call is logged, in the order made, with its arguments as
// JSON at the moment of the call. URL builders keep the real code.
const recorder = vi.hoisted(() => ({
  calls: [] as { name: string; json: string }[],
  respond: (_name: string, _args: unknown[]): unknown => undefined,
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

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// ---- canned data -----------------------------------------------------------

const T = '2026-06-01T12:00:00Z';
const clone = <V,>(value: V): V => JSON.parse(JSON.stringify(value));

const PROFILE = {
  project_id: 'p1',
  vision: 'Every shop floor runs its machines safely.',
  problem_statement: 'Operators cannot tell which spindles are live.',
  target_users: 'Machinists and shop leads.',
  constraints: [],
  success_metrics: [],
  settings: {},
};

// The in-memory server: sessions merge saved answers at the top level and
// take the saved step, as internal/domain/guided does; drafts become
// artifacts with status draft and origin guided-flow.
let server: {
  sessions: Record<string, any>;
  artifacts: any[];
  sessionCount: number;
};

const addArtifact = (fields: Record<string, any>) => {
  const n = server.artifacts.length + 1;
  const artifact = {
    id: `art-${n}`,
    ref: `A-${n}`,
    project_id: 'p1',
    parent_id: null,
    sort_order: 0,
    body: '',
    attributes: {},
    version: 1,
    valid_from: T,
    valid_to: null,
    created_at: T,
    updated_at: T,
    ...fields,
  };
  server.artifacts.push(artifact);
  return artifact;
};

const resetServer = () => {
  server = { sessions: {}, artifacts: [], sessionCount: 0 };
  // A heading the project already has, which the "New artifact" card names
  // by its ref, and an item under it, after which the new one sorts.
  addArtifact({ ref: 'SEC-9', type: 'heading', title: 'Design', sort_order: 70, status: 'approved', attributes: { status: 'approved' } });
  addArtifact({ ref: 'DI-1', parent_id: 'art-1', type: 'design-item', title: 'Spindle drive', sort_order: 1, status: 'approved', attributes: { status: 'approved' } });
};

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'guidedAPI.list': () => [],
  'guidedAPI.start': (projectId: string) => {
    const id = `gs-${++server.sessionCount}`;
    server.sessions[id] = {
      id,
      project_id: projectId,
      status: 'in-progress',
      current_step: 1,
      answers: {},
      draft_artifact_ids: [],
      created_at: T,
      updated_at: T,
    };
    return clone(server.sessions[id]);
  },
  'guidedAPI.saveStep': (id: string, step: number, answers: Record<string, any>) => {
    const session = server.sessions[id];
    session.answers = { ...session.answers, ...clone(answers) };
    session.current_step = step;
    return clone(session);
  },
  'guidedAPI.materializeDrafts': (id: string, drafts: any[]) => {
    const ids = drafts.map(
      (d) =>
        addArtifact({
          parent_id: d.parent_id ?? null,
          type: d.type,
          title: d.title,
          body: d.body,
          sort_order: d.sort_order ?? 0,
          status: 'draft',
          attributes: { ...(d.attributes || {}), status: 'draft', origin: 'guided-flow' },
        }).id
    );
    server.sessions[id].draft_artifact_ids.push(...ids);
    return { artifact_ids: ids };
  },
  'guidedAPI.commit': (id: string) => {
    server.sessions[id].status = 'committed';
    return clone(server.sessions[id]);
  },
  // Held until the test lets it through. The assistant's opening turn sends
  // the wizard's state, and the transcript races the profile that pre-fills
  // step 1; the gate settles the race (profile first) rather than leaving it
  // to how many promise hops each side takes.
  'guidedAPI.listMessages': () => transcriptGate.promise.then(() => []),
  'guidedAPI.kickoffChat': () => ({ status: 'launched', runner_online: true }),
  'guidedAPI.nudgeChat': () => ({ status: 'pending', runner_online: true }),
  'productProfileAPI.get': () => PROFILE,
  'productProfileAPI.update': (_projectId: string, payload: Record<string, any>) => ({ ...PROFILE, ...payload }),
  'artifactAPI.list': () => clone(server.artifacts),
  'artifactAPI.create': (payload: Record<string, any>) =>
    clone(addArtifact({ ...payload, status: payload.attributes?.status })),
  'baselineAPI.create': (projectId: string, name: string) => ({
    id: 'bl-1',
    project_id: projectId,
    name,
    created_at: T,
  }),
};

const deferred = () => {
  let release = () => {};
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { promise, release };
};
let transcriptGate = deferred();

recorder.respond = (name, args) => CANNED[name]?.(...args);

// The assistant's stream. Opening one is logged with the API calls (the URL
// without its origin, which follows the environment); the test pushes the
// assistant's replies through it.
class FakeEventSource {
  static open: FakeEventSource[] = [];
  listeners: Record<string, ((event: MessageEvent) => void)[]> = {};
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(url: string, init?: EventSourceInit) {
    recorder.calls.push({ name: 'EventSource', json: JSON.stringify([url.replace(/^[a-z]+:\/\/[^/]+/, ''), init]) });
    FakeEventSource.open.push(this);
  }

  addEventListener(type: string, fn: (event: MessageEvent) => void) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  }

  close() {
    this.closed = true;
  }
}

// A reply from the assistant: prose, then one fenced suggestion per card.
const reply = (id: string, text: string, ...suggestions: Record<string, unknown>[]) => ({
  id,
  session_id: 'gs-1',
  role: 'assistant',
  content: [text, ...suggestions.map((s) => '```openv-suggestion\n' + JSON.stringify(s) + '\n```')].join('\n\n'),
  created_at: T,
});

// Strings sized to the truncation boundaries: the need sentence and the
// requirement title are cut above 120 characters, an NFR title above 100, the
// notes panel's artifact title above 120; persona, hazard and stub titles
// never are.
const NEED_CAPABILITY = 'a live view of each spindle that is running';
const NEED_OUTCOME = 'I can clear the floor before maintenance';
const NEED_SENTENCE_121 = `As Sam the Shop Lead, I need ${NEED_CAPABILITY} so that ${NEED_OUTCOME}`;
const REQ_121 =
  'The system shall stop the spindle and lock the guard within one second of an operator pressing any emergency stop button.';
const REQ_120 =
  'The system shall record which badge started each job and show it on the machine panel until the job is cleared by a lead';
const NFR_101 =
  'The system shall show any spindle state change on all connected screens within 500 ms of it happening';
const NFR_100 =
  'The system shall keep every spindle state change for ninety days without slowing down the live view.';
const HAZARD_132 =
  'The spindle restarts on its own after a power dip while an operator is changing the tool, because the start command is still latched';
const ARTIFACT_121 =
  'Interlock wiring between the emergency stop chain, the guard door switch and the spindle drive enable inputs of a machine';

// ---- harness ---------------------------------------------------------------

const initialStore = useAppStore.getState();
const realInnerWidth = Object.getOwnPropertyDescriptor(window, 'innerWidth');
const realEventSource = (globalThis as any).EventSource;
const realScrollTo = (Element.prototype as any).scrollTo;

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
            {/* Nested as in App.tsx, so the relative "Go to Requirements"
                link resolves as it does there. */}
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

const byText = <E extends Element>(selector: string, text: string): E => {
  const found = Array.from(container.querySelectorAll<E>(selector)).find(
    (node) => (node.textContent ?? '').trim() === text
  );
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

const lastByText = (selector: string, text: string): Element => {
  const found = Array.from(container.querySelectorAll(selector)).filter((node) => (node.textContent ?? '').trim() === text);
  expect(found.length, `${selector} "${text}"`).toBeGreaterThan(0);
  return found[found.length - 1];
};

const last = <E extends Element>(selector: string): E => {
  const all = container.querySelectorAll<E>(selector);
  expect(all.length, selector).toBeGreaterThan(0);
  return all[all.length - 1];
};

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

// React tracks a control's value itself, so the value goes through the
// native setter and then the event React listens for.
const setValue = async (node: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement, value: string) => {
  const proto =
    node instanceof HTMLSelectElement
      ? HTMLSelectElement.prototype
      : node instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype;
  await act(async () => {
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event(node instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
  await flush();
};

// The assistant's reply arrives on the open stream.
const say = async (message: ReturnType<typeof reply>) => {
  const stream = FakeEventSource.open.filter((es) => !es.closed).pop()!;
  await act(async () => {
    (stream.listeners.message || []).forEach((fn) => fn({ data: JSON.stringify(message) } as MessageEvent));
  });
  await flush();
};

// Next and Skip nudge the assistant, which sends at most one nudge a second
// and holds a later one back on a timer; the frozen clock moves on first so
// every nudge goes out at once, where the call order can see it.
const advance = async (label: 'Next →' | 'Skip' | '← Back') => {
  vi.setSystemTime(Date.now() + 2000);
  await click(byText('button', label));
};

// innerHTML with a line break put between adjacent tags ("><"), then the
// value of every form control in document order: React sets a control's
// value and checked state as properties, which innerHTML does not carry.
const snapshot = (name: string) => {
  const controls = Array.from(
    container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>('input, textarea, select')
  ).map((node, i) =>
    node instanceof HTMLInputElement && node.type === 'checkbox'
      ? `<!-- control ${i + 1}: checkbox ${node.checked ? 'checked' : 'unchecked'} -->`
      : `<!-- control ${i + 1}: ${node.tagName.toLowerCase()} = ${JSON.stringify(node.value)} -->`
  );
  return expect(
    [container.innerHTML.replace(/></g, '>\n<'), ...controls].join('\n') + '\n'
  ).toMatchFileSnapshot(`./__snapshots__/GuidedWizard.${name}.html`);
};

// The call list in brief: arguments that are objects or arrays are elided
// ({…}, [n]); the full arguments are in GuidedWizard.api.json.
const brief = () =>
  recorder.calls.map(({ name, json }) => {
    const args = JSON.parse(json) as unknown[];
    const shown = args.map((a) =>
      Array.isArray(a) ? `[${a.length}]` : a !== null && typeof a === 'object' ? '{…}' : JSON.stringify(a)
    );
    return `${name}(${shown.join(', ')})`;
  });

const savedAnswers = (sessionId: string) =>
  recorder.calls
    .filter((c) => c.name === 'guidedAPI.saveStep')
    .map((c) => JSON.parse(c.json) as [string, number, Record<string, any>])
    .filter(([id]) => id === sessionId)
    .map(([, step, answers]) => ({ step, answers }));

beforeEach(() => {
  recorder.calls = [];
  resetServer();
  transcriptGate = deferred();
  FakeEventSource.open = [];
  (globalThis as any).EventSource = FakeEventSource;
  // jsdom has no scrolling; the assistant scrolls to its newest message.
  (Element.prototype as any).scrollTo = () => {};
  // Entry ids are crypto.randomUUID(); a counter keeps them, and the React
  // keys and saved answers that carry them, the same on every run.
  let uuid = 0;
  vi.spyOn(crypto, 'randomUUID').mockImplementation(
    () => `00000000-0000-4000-8000-${String(++uuid).padStart(12, '0')}` as `${string}-${string}-${string}-${string}-${string}`
  );
  // The clock is frozen (a nudge is throttled by Date.now()); timers stay
  // real.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(T));
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 1280 });
  window.localStorage.clear();
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      features: {
        channel: 'nightly',
        stable_release: '',
        preview: false,
        features: { 'assistant-project-edits': true },
      },
    },
    true
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
  useAppStore.setState(initialStore, true);
  window.localStorage.clear();
  vi.restoreAllMocks();
  vi.useRealTimers();
  if (realInnerWidth) Object.defineProperty(window, 'innerWidth', realInnerWidth);
  (globalThis as any).EventSource = realEventSource;
  (Element.prototype as any).scrollTo = realScrollTo;
});

describe('GuidedWizard characterization (S16b)', () => {
  // ~1.5 s alone, but past vitest's 5 s default in a busy parallel run (#379, bug 135).
  it('walks every step and saves the answers it saves today', async () => {
    expect(NEED_SENTENCE_121).toHaveLength(121);
    expect(REQ_121).toHaveLength(121);
    expect(REQ_120).toHaveLength(120);
    expect(NFR_101).toHaveLength(101);
    expect(NFR_100).toHaveLength(100);
    expect(HAZARD_132).toHaveLength(132);
    expect(ARTIFACT_121).toHaveLength(121);

    await mount();
    await snapshot('landing');
    await click(byText('button', 'Start guided definition'));
    transcriptGate.release();
    await flush();

    // ---- step 1: product framing
    await say(
      reply(
        'm-1',
        'Welcome. The vision could say what changes on the floor:',
        { kind: 'framing', field: 'vision', text: 'Every machine on the floor reports its state before anyone touches it.' },
        { kind: 'framing', field: 'mission', text: 'Zero spindle injuries.' }
      )
    );
    await click(byText('button', 'Apply all (2)'));
    await setValue(
      container.querySelector<HTMLTextAreaElement>('textarea[placeholder="Who are the primary users and buyers?"]')!,
      'Machinists, shop leads and maintenance planners.'
    );
    await snapshot('step-1');
    await advance('Next →');

    // ---- step 2: personas
    await say(
      reply(
        'm-2',
        'Two people stand out:',
        { kind: 'persona', name: 'Maya the Machinist', role: 'CNC operator', goals: 'Run jobs without surprises', pains: 'Spindles start without warning' },
        { kind: 'persona', name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe', pains: 'No overview of machine state' },
        { kind: 'persona', replaces: 'maya the machinist', name: 'Maya the Machinist', role: 'Senior CNC operator' }
      )
    );
    await click(byText('button', 'Apply all (3)'));
    await click(byText('button', '+ Add persona'));
    await setValue(last<HTMLInputElement>('input[placeholder="Persona name (e.g. Maya the Machinist)"]'), 'Pat the Planner ');
    await setValue(last<HTMLInputElement>('input[placeholder="Their job / context"]'), 'Maintenance planner');
    await setValue(last<HTMLTextAreaElement>('textarea[placeholder="What are they trying to achieve?"]'), 'Plan downtime');
    await setValue(last<HTMLTextAreaElement>('textarea[placeholder="What frustrates them today?"]'), 'Surprise breakdowns');
    await click(byText('button', '+ Add persona'));
    await snapshot('step-2');
    await advance('Next →');

    // ---- step 3: user needs
    await say(
      reply(
        'm-3',
        'Needs, one per persona to start:',
        { kind: 'need', persona: 'Sam the Shop Lead', capability: NEED_CAPABILITY, outcome: NEED_OUTCOME },
        { kind: 'need', persona: 'Nobody', capability: 'to see who started a job', outcome: '' },
        { kind: 'need', replaces: 'TO SEE WHO STARTED A JOB', persona: 'pat the planner', outcome: 'I can follow up on near misses' },
        { kind: 'persona', replaces: '00000000-0000-4000-8000-000000000002', name: 'Sam' }
      )
    );
    await click(byText('button', 'Apply all (4)'));
    await click(byText('button', '+ Add need for Pat the Planner'));
    await setValue(last<HTMLInputElement>('input[placeholder="capability"]'), 'a weekly list of machines due for service');
    await setValue(last<HTMLInputElement>('input[placeholder="outcome"]'), 'downtime is planned, not forced');
    await snapshot('step-3');
    await advance('Next →');

    // ---- step 4: requirements
    await say(
      reply(
        'm-4',
        'Requirements for the live view and the job log:',
        { kind: 'requirement', need: 'live view', text: REQ_121, fit_criterion: 'Measured from button press to spindle at rest', verification_method: 'test' },
        { kind: 'requirement', need: 'who started', text: REQ_120, verification_method: 'review' },
        { kind: 'requirement', need: 'live view', text: '' },
        // By the entry id of the second; "who started" names no need exactly,
        // so the need stays.
        { kind: 'requirement', replaces: '00000000-0000-4000-8000-000000000009', need: 'who started', fit_criterion: 'Shown until a lead clears it' }
      )
    );
    await click(byText('button', 'Apply all (4)'));
    // Under the last need, the typed one.
    await click(lastByText('button', '+ Add requirement'));
    await setValue(last<HTMLInputElement>('input[placeholder="The system shall …"]'), 'The system shall list the machines due for service each Monday');
    await setValue(last<HTMLTextAreaElement>('textarea[placeholder="Fit criterion — how do we know it is met?"]'), 'Matches the maintenance plan');
    await setValue(last<HTMLSelectElement>('select'), 'analysis');
    await snapshot('step-4');
    await advance('Next →');

    // ---- step 5: NFRs and constraints
    await say(
      reply(
        'm-5',
        'Qualities worth pinning down:',
        { kind: 'nfr', category: 'performance', text: NFR_101, fit_criterion: '95th percentile over a shift', verification_method: 'test' },
        { kind: 'nfr', category: 'Speed', text: NFR_100, verification_method: 'analysis' },
        { kind: 'nfr', category: 'security', text: 'The system shall require a badge to clear an interlock', verification_method: 'test' },
        { kind: 'nfr', replaces: 'the system shall require a badge to clear an interlock', fit_criterion: 'Checked at every interlock', verification_method: 'proof' }
      )
    );
    await click(byText('button', 'Apply all (4)'));
    await click(byText('span', 'Usability'));
    await click(byText('button', '+ Add usability requirement'));
    await setValue(
      last<HTMLInputElement>('input[placeholder="The system shall … (usability)"]'),
      'The system shall show machine state in colours readable with gloves on'
    );
    await snapshot('step-5');
    await advance('Next →');

    // ---- step 6: hazards, skipped, then saved on a second visit
    await say(
      reply(
        'm-6',
        'Two hazards to start the analysis:',
        { kind: 'hazard', category: 'safety', hazard: HAZARD_132, harm: 'Crushed or severed fingers', severity: 'Medium' },
        { kind: 'hazard', category: 'security', hazard: 'A spoofed network command restarts a stopped spindle', harm: 'Injury during maintenance', severity: 'serious' },
        { kind: 'hazard', replaces: 'a spoofed network command restarts a stopped spindle', severity: 'critical' }
      )
    );
    await click(byText('button', 'Apply all (3)'));
    await click(byText('span', 'Operational'));
    await click(byText('button', '+ Add operational hazard'));
    await setValue(last<HTMLInputElement>('input[placeholder="Operational hazard (what could go wrong)"]'), 'Coolant mist hides the spindle state lamp');
    await setValue(last<HTMLInputElement>('input[placeholder="Potential harm / impact"]'), 'Operator misreads machine state ');
    await snapshot('step-6');
    await advance('Skip');
    await advance('← Back');
    await advance('Next →');

    // ---- step 7: verification stubs, skipped, then created on a second visit
    await say(reply('m-7', 'Stub the tests you will run first.'));
    const stub = Array.from(container.querySelectorAll('label')).find((l) => (l.textContent ?? '').includes(REQ_120));
    await click(stub!.querySelector('input[type="checkbox"]')!);
    await snapshot('step-7');
    await advance('Skip');
    await advance('← Back');
    await advance('Next →');

    // ---- step 8: review and commit
    await say(
      reply('m-8', 'The interlock wiring belongs in the design section:', {
        kind: 'artifact',
        type: 'design-item',
        title: ARTIFACT_121,
        body: 'Per drawing E-12.',
        parent: 'sec-9',
      })
    );
    await click(byText('button', '+ Add to project'));
    await snapshot('step-8');
    await click(byText('button', 'Commit 37 artifacts'));

    // ---- committed, baselined, reopened
    await click(byText('button', 'Create baseline'));
    await snapshot('committed');
    await click(byText('button', 'Modify guided definition'));
    await snapshot('reopened');

    // Every call, in the order made: each Next and Skip nudges the assistant
    // first, then materialises (Next only), then saves; an applied card saves
    // at once. An undefined argument (kickoffChat's artifact id) prints as
    // null.
    const save = (session: string, step: number) => `guidedAPI.saveStep("${session}", ${step}, {…})`;
    const nudge = (step: number, event: string) => `guidedAPI.nudgeChat("gs-1", ${step}, {…}, ${JSON.stringify(event)})`;
    const drafts = (n: number) => `guidedAPI.materializeDrafts("gs-1", [${n}])`;
    const heading = drafts(1);
    const listArtifacts = 'artifactAPI.list("p1")';
    expect(brief()).toEqual([
      // landing; Start: the session, the profile that pre-fills step 1, and
      // the assistant's transcript, stream and opening turn
      'guidedAPI.list("p1")',
      'guidedAPI.start("p1")',
      'productProfileAPI.get("p1")',
      'guidedAPI.listMessages("gs-1")',
      'EventSource("/api/v1/guided-sessions/gs-1/chat/stream", {…})',
      'guidedAPI.kickoffChat("gs-1", 1, {…}, null)',
      // step 1: the framing card; Next writes the profile, then saves
      save('gs-1', 1),
      nudge(2, 'saved step 1 ("Product framing") and moved on to step 2 ("Personas")'),
      'productProfileAPI.update("p1", {…})',
      save('gs-1', 2),
      // step 2: the persona cards; Next creates the section heading, then the drafts
      save('gs-1', 2),
      nudge(3, 'saved step 2 ("Personas") and moved on to step 3 ("User needs")'),
      heading,
      drafts(3),
      save('gs-1', 3),
      // step 3
      save('gs-1', 3),
      nudge(4, 'saved step 3 ("User needs") and moved on to step 4 ("Requirements")'),
      heading,
      drafts(3),
      save('gs-1', 4),
      // step 4
      save('gs-1', 4),
      nudge(5, 'saved step 4 ("Requirements") and moved on to step 5 ("NFRs & constraints")'),
      heading,
      drafts(3),
      save('gs-1', 5),
      // step 5: the NFR heading, then one sub-heading per category, one call each
      save('gs-1', 5),
      nudge(6, 'saved step 5 ("NFRs & constraints") and moved on to step 6 ("Hazards")'),
      heading,
      heading,
      heading,
      heading,
      drafts(4),
      save('gs-1', 6),
      // step 6: Skip saves and materialises nothing; Back calls nothing; Next
      // materialises
      save('gs-1', 6),
      nudge(7, 'skipped step 6 ("Hazards") and moved on to step 7 ("Verification stubs")'),
      save('gs-1', 7),
      nudge(7, 'saved step 6 ("Hazards") and moved on to step 7 ("Verification stubs")'),
      heading,
      heading,
      heading,
      heading,
      drafts(3),
      save('gs-1', 7),
      // step 7: Skip saves the selection and creates no stub, and step 8
      // lists the drafts; Back; Next creates the stubs
      nudge(8, 'skipped step 7 ("Verification stubs") and moved on to step 8 ("Review & commit")'),
      save('gs-1', 8),
      listArtifacts,
      nudge(8, 'saved step 7 ("Verification stubs") and moved on to step 8 ("Review & commit")'),
      heading,
      heading,
      heading,
      heading,
      heading,
      drafts(4),
      save('gs-1', 8),
      listArtifacts,
      // step 8: the New artifact card reads the project, creates the artifact
      // and refreshes the drafts. The card then reads "✓ Added to project"
      // (the step-8 snapshot) and, like every applied card, is saved as
      // applied; the saved session coming back reloads the step's drafts.
      listArtifacts,
      'artifactAPI.create({…})',
      listArtifacts,
      save('gs-1', 8),
      listArtifacts,
      'guidedAPI.commit("gs-1")',
      'baselineAPI.create("p1", "Initial requirements")',
      // Modify guided definition: a new session seeded with the committed
      // answers, and a new conversation
      'guidedAPI.start("p1")',
      save('gs-2', 1),
      'guidedAPI.listMessages("gs-2")',
      'EventSource("/api/v1/guided-sessions/gs-2/chat/stream", {…})',
      'guidedAPI.kickoffChat("gs-2", 1, {…}, null)',
    ]);

    // The answers the walk's last save stored, key by key and in key order
    // (I16): step_2_ids to step_5_ids but no step_6_ids, hazards without an
    // ids key, severity "moderate" for the card that said "Medium", the
    // persona name with its trailing space, the step-8 project card among the
    // applied ones, section_ids last.
    const id = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`;
    const saves = savedAnswers('gs-1');
    const final = saves[saves.length - 1];
    expect(final.step).toBe(8);
    expect(JSON.stringify(final.answers, null, 2)).toBe(
      JSON.stringify({
        step_1: {
          vision: 'Every machine on the floor reports its state before anyone touches it.',
          problem_statement: 'Operators cannot tell which spindles are live.',
          target_users: 'Machinists, shop leads and maintenance planners.',
        },
        step_2: {
          personas: [
            { id: id(1), name: 'Maya the Machinist', role: 'Senior CNC operator', goals: 'Run jobs without surprises', pains: 'Spindles start without warning', artifact_id: 'art-4' },
            { id: id(2), name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe', pains: 'No overview of machine state', artifact_id: 'art-5' },
            { id: id(3), name: 'Pat the Planner ', role: 'Maintenance planner', goals: 'Plan downtime', pains: 'Surprise breakdowns', artifact_id: 'art-6' },
            { id: id(4), name: '', role: '', goals: '', pains: '' },
          ],
        },
        step_2_ids: ['art-4', 'art-5', 'art-6'],
        step_3: {
          needs: [
            { id: id(5), persona_id: id(2), capability: NEED_CAPABILITY, outcome: NEED_OUTCOME, artifact_id: 'art-8' },
            { id: id(6), persona_id: id(3), capability: 'to see who started a job', outcome: 'I can follow up on near misses', artifact_id: 'art-9' },
            { id: id(7), persona_id: id(3), capability: 'a weekly list of machines due for service', outcome: 'downtime is planned, not forced', artifact_id: 'art-10' },
          ],
        },
        step_3_ids: ['art-8', 'art-9', 'art-10'],
        step_4: {
          requirements: [
            { id: id(8), need_id: id(5), text: REQ_121, fit_criterion: 'Measured from button press to spindle at rest', verification_method: 'test', artifact_id: 'art-12' },
            { id: id(9), need_id: id(6), text: REQ_120, fit_criterion: 'Shown until a lead clears it', verification_method: 'test', artifact_id: 'art-13' },
            { id: id(10), need_id: id(7), text: 'The system shall list the machines due for service each Monday', fit_criterion: 'Matches the maintenance plan', verification_method: 'analysis', artifact_id: 'art-14' },
          ],
        },
        step_4_ids: ['art-12', 'art-13', 'art-14'],
        step_5: {
          nfrs: [
            { id: id(11), category: 'Performance', text: NFR_101, fit_criterion: '95th percentile over a shift', verification_method: 'test', artifact_id: 'art-19' },
            { id: id(12), category: 'Performance', text: NFR_100, fit_criterion: '', verification_method: 'analysis', artifact_id: 'art-20' },
            { id: id(13), category: 'Security', text: 'The system shall require a badge to clear an interlock', fit_criterion: 'Checked at every interlock', verification_method: 'test', artifact_id: 'art-21' },
            { id: id(14), category: 'Usability', text: 'The system shall show machine state in colours readable with gloves on', fit_criterion: '', verification_method: 'test', artifact_id: 'art-22' },
          ],
        },
        step_5_ids: ['art-19', 'art-20', 'art-21', 'art-22'],
        step_6: {
          hazards: [
            { id: id(15), category: 'Safety', hazard: HAZARD_132, harm: 'Crushed or severed fingers', severity: 'moderate', artifact_id: 'art-27' },
            { id: id(16), category: 'Security', hazard: 'A spoofed network command restarts a stopped spindle', harm: 'Injury during maintenance', severity: 'critical', artifact_id: 'art-28' },
            { id: id(17), category: 'Operational', hazard: 'Coolant mist hides the spindle state lamp', harm: 'Operator misreads machine state ', severity: 'moderate', artifact_id: 'art-29' },
          ],
        },
        step_7: {
          selected: { 'art-13': false },
          created: { 'art-12': 'art-35', 'art-19': 'art-36', 'art-21': 'art-37', 'art-22': 'art-38' },
        },
        copilot_applied: ['m-1:1', 'm-2:1', 'm-2:3', 'm-2:5', 'm-3:1', 'm-3:3', 'm-3:5', 'm-4:1', 'm-4:3', 'm-4:7', 'm-5:1', 'm-5:3', 'm-5:5', 'm-5:7', 'm-6:1', 'm-6:3', 'm-6:5', 'm-8:1'],
        section_ids: {
          personas: 'art-3',
          needs: 'art-7',
          requirements: 'art-11',
          nfrs: 'art-15',
          'nfrs:Performance': 'art-16',
          'nfrs:Security': 'art-17',
          'nfrs:Usability': 'art-18',
          hazards: 'art-23',
          'hazards:Safety': 'art-24',
          'hazards:Security': 'art-25',
          'hazards:Operational': 'art-26',
          tests: 'art-30',
          'tests:Functional': 'art-31',
          'tests:Performance': 'art-32',
          'tests:Security': 'art-33',
          'tests:Usability': 'art-34',
        },
      }, null, 2)
    );

    // The reopened session's seed: the committed answers with the entry
    // lists re-normalised and the applied-card keys reset.
    const [seed] = savedAnswers('gs-2');
    expect(seed.step).toBe(1);
    expect(Object.keys(seed.answers)).toEqual([
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
      'section_ids',
    ]);
    expect(seed.answers.copilot_applied).toEqual([]);

    // Every call with its full arguments: the drafts and their templates,
    // each save's answers, each nudge's state.
    await expect(
      JSON.stringify(
        recorder.calls.map(({ name, json }) => ({ call: name, args: JSON.parse(json) })),
        null,
        2
      ) + '\n'
    ).toMatchFileSnapshot('./__snapshots__/GuidedWizard.api.json');
  }, 20_000);
});
