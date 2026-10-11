import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { GuidedWizard } from './GuidedWizard';
import { DialogProvider } from '../components/ui';

// Refactor plan step X18c (invariants I21; fe-suite-org-1, fe-suite-org-2):
// what the guided wizard renders where S16b's walk (GuidedWizard.snapshot) and
// the answers test do not look, before X18c moves the step bodies, the
// landing and committed screens and the phone's assistant sheet out of
// GuidedWizard.tsx into views/guidedWizard/. The walk pins each step as a
// first visit on a desktop; what it leaves out is pinned here, each as a file
// snapshot of container.innerHTML (one tag per line, then the form controls'
// values, which innerHTML does not carry) under
// __snapshots__/GuidedWizard.states.<name>.html:
//
//   screens    the loading text; the landing of a project whose definition is
//              committed (Modify / Start over), while it opens, with a start
//              that fails and with a list that fails to load; the committed
//              screen before its baseline, while it is made and when that
//              fails.
//   step 2     entries whose drafts exist: a green dot, locked fields, no
//              remove button.
//   step 3     locked needs; the "Needs without a persona" card (reassigned,
//              removed); no persona to assign to.
//   step 4     locked requirements; the "Requirements without a user need"
//              card (reattached, removed); no user need to attach to.
//   step 5, 6  categories open by their entries or closed by hand, one and
//              two entries in their counts, locked entries, a row clicked and
//              its checkbox clicked, an entry edited and removed.
//   step 7     no candidate; candidates whose stubs exist (locked, ticked,
//              "stub created") beside those still to make.
//   step 8     the drafts loading; none; by type; Discard (the draft goes, or
//              the call fails and says so); Back; the commit button while it
//              commits and when the commit fails.
//   phone      the shell, the closed sheet with its button, the open sheet
//              (its overlay, the close button), the sheet above the keyboard,
//              and the overlay, button and close button opening and closing it.
//
// The assistant panel is a stub that shows the props the wizard gives it, so
// the sheet around it is pinned without the panel's own markup (the walk pins
// that). The API client is a recorder over a small in-memory server.
//
// Regenerate only for a deliberate change to what the wizard shows, never in
// a refactor pull request:
//   npx vitest run src/views/GuidedWizard.states.test.tsx -u

const recorder = vi.hoisted(() => ({
  calls: [] as { name: string; json: string }[],
  respond: (_name: string, _args: unknown[]): unknown => undefined,
  compact: false,
  visual: { height: 800, keyboardInset: 0 },
  // The latest props the wizard gave the assistant panel.
  panel: null as null | Record<string, any>,
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

vi.mock('../components/wizard/GuidedChatPanel', async () => {
  const { forwardRef, useImperativeHandle } = await import('react');
  return {
    GuidedChatPanel: forwardRef((props: Record<string, any>, ref) => {
      recorder.panel = props;
      useImperativeHandle(ref, () => ({ nudge: () => {} }));
      return (
        <div
          data-assistant=""
          data-embedded={String(!!props.embedded)}
          data-session={props.sessionId}
          data-step={String(props.step)}
          data-applied={Object.keys(props.applied).join(',')}
        />
      );
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

vi.mock('../hooks/useVisualViewport', () => ({ useVisualViewport: () => recorder.visual }));

vi.mock('../state/store', () => ({ useAppStore: (sel: any) => sel({ projectId: 'p1' }) }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// ---- the server ------------------------------------------------------------

const T = '2026-06-01T12:00:00Z';

const deferred = () => {
  let resolve!: (value?: unknown) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
};

let server: {
  sessions: any[];
  drafts: any[];
  // A call that waits (a promise) or fails (a rejection) instead of answering.
  hold: Record<string, () => unknown>;
};

const session = (current_step: number, answers: Record<string, any>, status = 'in-progress') => ({
  id: status === 'committed' ? 'gs-0' : 'gs-1',
  project_id: 'p1',
  status,
  current_step,
  answers,
  draft_artifact_ids: [],
  created_at: T,
  updated_at: T,
});

const artifact = (id: string, type: string, title: string, status = 'draft') => ({
  id,
  project_id: 'p1',
  type,
  title,
  body: '',
  parent_id: null,
  sort_order: 1,
  attributes: { status },
  created_at: T,
  updated_at: T,
});

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'guidedAPI.list': () => JSON.parse(JSON.stringify(server.sessions)),
  'guidedAPI.start': () => ({ ...session(1, {}), id: 'gs-2' }),
  'guidedAPI.saveStep': (id: string, step: number, answers: Record<string, any>) => ({
    ...session(step, JSON.parse(JSON.stringify(answers))),
    id,
  }),
  'guidedAPI.commit': (id: string) => ({ ...session(8, {}, 'committed'), id }),
  'productProfileAPI.get': () => ({ project_id: 'p1', vision: '', problem_statement: '', target_users: '' }),
  'artifactAPI.list': () => JSON.parse(JSON.stringify(server.drafts)),
  'artifactAPI.delete': () => ({}),
  'baselineAPI.create': () => ({ id: 'b-1' }),
};

recorder.respond = (name, args) => (server.hold[name] ? server.hold[name]() : CANNED[name]?.(...args));

// ---- a project's entries -------------------------------------------------------

const personas = [
  { id: 'p-1', name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe', pains: 'No overview', artifact_id: 'art-p1' },
  { id: 'p-2', name: 'Pat the Planner', role: 'Maintenance planner', goals: 'Plan downtime', pains: 'Surprise breakdowns' },
  { id: 'p-3', name: '  ', role: '', goals: '', pains: '' },
];

const needs = [
  { id: 'n-1', persona_id: 'p-1', capability: 'a list of live spindles', outcome: 'I can clear the floor', artifact_id: 'art-n1' },
  { id: 'n-2', persona_id: 'p-1', capability: 'a lock-out reminder', outcome: '' },
  // The persona is gone (n-3), unnamed (n-4) or never given (n-5).
  { id: 'n-3', persona_id: 'p-9', capability: 'a service calendar', outcome: 'downtime is planned', artifact_id: 'art-n3' },
  { id: 'n-4', persona_id: 'p-3', capability: 'to know who is on shift', outcome: '' },
  { id: 'n-5', persona_id: '', capability: '', outcome: '' },
];

const requirements = [
  { id: 'r-1', need_id: 'n-1', text: 'The system shall list live spindles', fit_criterion: 'Listed within 1 s', verification_method: 'test', artifact_id: 'art-r1' },
  { id: 'r-2', need_id: 'n-1', text: 'The system shall warn before restart', fit_criterion: '', verification_method: 'demonstration' },
  // The need is gone (r-3), has no capability (r-4) or was never given (r-5).
  { id: 'r-3', need_id: 'n-9', text: 'The system shall log every start', fit_criterion: '', verification_method: 'test', artifact_id: 'art-r3' },
  { id: 'r-4', need_id: 'n-5', text: '', fit_criterion: '', verification_method: 'inspection' },
  { id: 'r-5', need_id: '', text: 'The system shall list who is on shift', fit_criterion: 'Matches the roster', verification_method: 'analysis' },
];

const nfrs = [
  { id: 'f-1', category: 'Performance', text: 'The system shall answer within 1 s', fit_criterion: '', verification_method: 'test', artifact_id: 'art-f1' },
  { id: 'f-2', category: 'Performance', text: 'The system shall hold 50 users', fit_criterion: '', verification_method: 'analysis' },
  { id: 'f-3', category: 'Security', text: 'The system shall require a badge', fit_criterion: '', verification_method: 'test', artifact_id: 'art-f3' },
];

const hazards = [
  { id: 'h-1', category: 'Safety', hazard: 'A spindle restarts', harm: 'Crushed fingers', severity: 'critical', artifact_id: 'art-h1' },
  { id: 'h-2', category: 'Safety', hazard: 'A guard is left open', harm: 'Cuts', severity: 'serious' },
  { id: 'h-3', category: 'Security', hazard: 'A spoofed command', harm: 'Injury', severity: 'minor', artifact_id: 'art-h3' },
];

const entries = {
  step_1: { vision: 'Safe floors', problem_statement: 'Live spindles', target_users: 'Machinists' },
  step_2: { personas },
  step_3: { needs },
  step_4: { requirements },
  step_5: { nfrs },
  step_6: { hazards },
};

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

// A project with a session open at a step, with these answers.
const openAt = async (step: number, answers: Record<string, any> = entries) => {
  server.sessions = [session(step, answers)];
  await mount();
};

// innerHTML with a line break between adjacent tags, then the value of every
// form control in document order.
const snapshot = (name: string) => {
  const controls = Array.from(
    container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>('input, textarea, select')
  ).map((node, i) =>
    node instanceof HTMLInputElement && node.type === 'checkbox'
      ? `<!-- control ${i + 1}: checkbox ${node.checked ? 'checked' : 'unchecked'}${node.disabled ? ' disabled' : ''} -->`
      : `<!-- control ${i + 1}: ${node.tagName.toLowerCase()} = ${JSON.stringify(node.value)}${node.disabled ? ' disabled' : ''} -->`
  );
  return expect([container.innerHTML.replace(/></g, '>\n<'), ...controls].join('\n') + '\n').toMatchFileSnapshot(
    `./__snapshots__/GuidedWizard.states.${name}.html`
  );
};

const all = <E extends Element>(selector: string) => Array.from(container.querySelectorAll<E>(selector));

const byText = <E extends Element>(selector: string, text: string): E => {
  const found = all<E>(selector).find((node) => (node.textContent ?? '').trim() === text);
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

const press = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

const click = (label: string) => press(byText('button', label));

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

const names = () => recorder.calls.map((c) => c.name);

beforeEach(() => {
  recorder.calls = [];
  recorder.compact = false;
  recorder.panel = null;
  recorder.visual = { height: 800, keyboardInset: 0 };
  server = { sessions: [], drafts: [], hold: {} };
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

describe('GuidedWizard screens (X18c characterization)', () => {
  it('shows its loading text until the sessions arrive', async () => {
    const gate = deferred();
    server.hold['guidedAPI.list'] = () => gate.promise;
    await mount();
    await snapshot('loading');
    await act(async () => {
      gate.resolve([]);
    });
  });

  it('offers to modify or start over from a committed definition, and says when either fails', async () => {
    server.sessions = [session(8, entries, 'committed')];
    await mount();
    await snapshot('landing-committed');

    // Opening the definition: the buttons wait.
    const gate = deferred();
    server.hold['guidedAPI.start'] = () => gate.promise;
    await click('Modify guided definition');
    await snapshot('landing-opening');
    await act(async () => {
      gate.reject(new Error('the project is archived'));
    });
    await flush();
    await snapshot('landing-opening-failed');

    // Start over fails: the banner stays until dismissed.
    server.hold['guidedAPI.start'] = () => Promise.reject(new Error('limit reached'));
    await click('Start over from scratch');
    await snapshot('landing-start-failed');
    await press(container.querySelector('button[aria-label="Dismiss error"]')!);
    expect(container.querySelector('[role="alert"]')).toBeNull();
    await snapshot('landing-dismissed');
  });

  it('starts from the plain landing while it waits, and says when the sessions did not load', async () => {
    server.hold['guidedAPI.list'] = () => Promise.reject(new Error('offline'));
    await mount();
    await snapshot('landing-load-failed');

    const gate = deferred();
    server.hold['guidedAPI.start'] = () => gate.promise;
    await click('Start guided definition');
    await snapshot('landing-starting');
    await act(async () => {
      gate.resolve({ ...session(1, {}), id: 'gs-2' });
    });
    await flush();
    expect(container.querySelector('[data-assistant]')?.getAttribute('data-session')).toBe('gs-2');
  });

  it('shows the committed screen before its baseline, while it is made and when it fails', async () => {
    await openAt(8);
    const gate = deferred();
    server.hold['guidedAPI.commit'] = () => gate.promise;
    await click('Commit definition');
    await snapshot('step-8-committing');
    await act(async () => {
      gate.resolve({ ...session(8, {}, 'committed'), id: 'gs-1' });
    });
    await flush();
    await snapshot('committed-before-baseline');

    const baseline = deferred();
    server.hold['baselineAPI.create'] = () => baseline.promise;
    await click('Create baseline');
    await snapshot('committed-creating');
    await act(async () => {
      baseline.reject(new Error('a baseline needs a requirement'));
    });
    await flush();
    await snapshot('committed-baseline-failed');
    expect(names().filter((n) => n === 'baselineAPI.create')).toHaveLength(1);

    delete server.hold['baselineAPI.create'];
    await click('Create baseline');
    expect(container.textContent).toContain('Baseline "Initial requirements" created');
    expect(names().filter((n) => n === 'baselineAPI.create')).toHaveLength(2);
  });

  it('says when the commit fails, and keeps the step', async () => {
    await openAt(8);
    server.hold['guidedAPI.commit'] = () => Promise.reject(new Error('a draft is locked'));
    await click('Commit definition');
    await snapshot('step-8-commit-failed');
  });
});

describe('GuidedWizard entry steps (X18c characterization)', () => {
  it('step 1 takes the vision, the problem and the target users', async () => {
    await openAt(1);
    const [vision, problem, users] = all<HTMLTextAreaElement>('textarea');
    await setValue(vision, 'Floors that report their state');
    await setValue(problem, 'Nobody knows which spindles are live');
    await setValue(users, 'Shop leads');
    expect(recorder.panel!.getState().step_1).toEqual({
      vision: 'Floors that report their state',
      problem_statement: 'Nobody knows which spindles are live',
      target_users: 'Shop leads',
    });
    await snapshot('step-1-edited');
  });

  it('step 2 locks the personas that have drafts', async () => {
    await openAt(2);
    await snapshot('step-2-locked');
    // A locked persona has no remove button; the open ones do.
    expect(all('button[title="Remove"]')).toHaveLength(2);
    await press(all('button[title="Remove"]')[0]);
    await snapshot('step-2-removed');
  });

  it('step 3 sets the needs of the named personas apart from those without one', async () => {
    await openAt(3);
    await snapshot('step-3-orphans');
    // The open need under Sam: its capability and outcome edited, then removed.
    await setValue(all<HTMLInputElement>('input[placeholder="capability"]')[1], 'a lock-out checklist');
    await setValue(all<HTMLInputElement>('input[placeholder="outcome"]')[1], 'repairs start safely');
    await snapshot('step-3-edited');
    await press(all('button[title="Remove need"]')[0]);
    // An orphan assigned to a persona moves under it (even a locked one); the
    // empty choice assigns nothing; one removed is gone.
    expect(all('select')).toHaveLength(3);
    await setValue(all<HTMLSelectElement>('select')[0], 'p-2');
    await setValue(all<HTMLSelectElement>('select')[0], '');
    expect(recorder.panel!.getState().step_3.needs.map((n: any) => [n.id, n.persona_id])).toEqual([
      ['n-1', 'p-1'],
      ['n-3', 'p-2'],
      ['n-4', 'p-3'],
      ['n-5', ''],
    ]);
    await press(all('button[title="Remove need"]').slice(-1)[0]);
    await snapshot('step-3-reassigned');
    await click('+ Add need for Pat the Planner');
    await snapshot('step-3-added');
  });

  it('step 3 has no persona to assign a need to when none is named', async () => {
    await openAt(3, { ...entries, step_2: { personas: [personas[2]] } });
    await snapshot('step-3-no-personas');
  });

  it('step 4 sets the requirements of the needs apart from those without one', async () => {
    await openAt(4);
    await snapshot('step-4-orphans');
    await setValue(all<HTMLInputElement>('input[placeholder="The system shall …"]')[1], 'The system shall confirm restarts');
    await setValue(all<HTMLTextAreaElement>('textarea[placeholder="Fit criterion — how do we know it is met?"]')[1], 'Confirmed twice');
    await setValue(all<HTMLSelectElement>('select')[1], 'analysis');
    await snapshot('step-4-edited');
    await press(all('button[title="Remove requirement"]')[0]);
    // The orphans: one attached to a need (even a locked one), the empty
    // choice attaches nothing, one removed.
    const attach = () => all<HTMLSelectElement>('select').filter((s) => s.options[0]?.textContent === 'Attach to need…');
    expect(attach()).toHaveLength(3);
    await setValue(attach()[0], 'n-2');
    await setValue(attach()[0], '');
    expect(recorder.panel!.getState().step_4.requirements.map((r: any) => [r.id, r.need_id])).toEqual([
      ['r-1', 'n-1'],
      ['r-3', 'n-2'],
      ['r-4', 'n-5'],
      ['r-5', ''],
    ]);
    await press(all('button[title="Remove requirement"]').slice(-1)[0]);
    await click('+ Add requirement');
    await snapshot('step-4-reattached');
  });

  it('step 4 has no need to attach a requirement to when none has a capability', async () => {
    await openAt(4, { ...entries, step_3: { needs: [needs[4]] } });
    await snapshot('step-4-no-needs');
  });

  it('step 5 opens a category by its entries, or by hand, and counts them', async () => {
    await openAt(5);
    await snapshot('step-5-locked');
    // The row closes Performance and its checkbox opens it again; the
    // checkbox opens Reliability, its row closes it, the checkbox opens it.
    await press(byText('span', 'Performance'));
    await snapshot('step-5-closed');
    await press(all<HTMLInputElement>('input[type="checkbox"]')[0]);
    expect(all<HTMLInputElement>('input[type="checkbox"]')[0].checked).toBe(true);
    await press(all<HTMLInputElement>('input[type="checkbox"]')[1]);
    await press(byText('span', 'Reliability'));
    expect(all<HTMLInputElement>('input[type="checkbox"]')[1].checked).toBe(false);
    await press(all<HTMLInputElement>('input[type="checkbox"]')[1]);
    await click('+ Add reliability requirement');
    await setValue(all<HTMLInputElement>('input[placeholder="The system shall … (reliability)"]')[0], 'The system shall restart unattended');
    await setValue(all<HTMLSelectElement>('select')[2], 'inspection');
    await press(all('button[title="Remove"]')[0]);
    await snapshot('step-5-edited');
  });

  it('step 6 opens a category by its entries, or by hand, and counts them', async () => {
    await openAt(6);
    await snapshot('step-6-locked');
    await press(byText('span', 'Safety'));
    await snapshot('step-6-closed');
    await press(all<HTMLInputElement>('input[type="checkbox"]')[0]);
    await press(all<HTMLInputElement>('input[type="checkbox"]')[1]);
    await click('+ Add technical hazard');
    await setValue(all<HTMLInputElement>('input[placeholder="Technical hazard (what could go wrong)"]')[0], 'Firmware loses power mid-write');
    await setValue(all<HTMLInputElement>('input[placeholder="Potential harm / impact"]')[2], 'Corrupt tool offsets');
    await setValue(all<HTMLSelectElement>('select')[2], 'minor');
    await press(all('button[title="Remove"]')[0]);
    await click('+ Add technical hazard');
    await snapshot('step-6-edited');
  });
});

describe('GuidedWizard verification and review steps (X18c characterization)', () => {
  it('step 7 has nothing to stub without a requirement to test', async () => {
    await openAt(7, { ...entries, step_4: { requirements: [requirements[1]] }, step_5: { nfrs: [nfrs[1]] } });
    await snapshot('step-7-empty');
  });

  it('step 7 shows the stubs that exist beside those still to make', async () => {
    await openAt(7, {
      ...entries,
      step_7: { selected: { 'art-r1': false, 'art-f3': false }, created: { 'art-r1': 'art-s1' } },
    });
    await snapshot('step-7-created');
    // A created stub cannot be unticked; the others can.
    const boxes = all<HTMLInputElement>('input[type="checkbox"]');
    expect(boxes.map((b) => b.disabled)).toEqual([true, false, false, false]);
    await press(boxes[1]);
    await press(boxes[2]);
    await press(boxes[3]);
    await snapshot('step-7-ticked');
  });

  it('step 8 shows its drafts loading, then none', async () => {
    const gate = deferred();
    server.hold['artifactAPI.list'] = () => gate.promise;
    await openAt(8);
    await snapshot('step-8-loading');
    await act(async () => {
      gate.resolve([]);
    });
    await flush();
    await snapshot('step-8-none');
  });

  it('step 8 lists the drafts by type and counts them on its button', async () => {
    server.drafts = [
      artifact('a-1', 'requirement', 'The system shall warn'),
      artifact('a-2', 'requirement', 'The system shall log'),
      artifact('a-3', 'persona', 'Sam the Shop Lead'),
      artifact('a-4', 'requirement', 'Already approved', 'approved'),
    ];
    await openAt(8);
    await snapshot('step-8-drafts');
    await click('Commit 3 artifacts');
    expect(names()).toContain('guidedAPI.commit');
  });

  it('step 8 drops a discarded draft, keeps one it could not discard, and goes back', async () => {
    server.drafts = [artifact('a-1', 'requirement', 'The system shall warn'), artifact('a-3', 'persona', 'Sam the Shop Lead')];
    await openAt(8);
    // By type, in order: the persona, then the requirement.
    await press(all('button').filter((b) => b.textContent === 'Discard')[1]);
    expect(recorder.calls.filter((c) => c.name === 'artifactAPI.delete').map((c) => c.json)).toEqual(['["a-1"]']);
    await snapshot('step-8-discarded');
    server.hold['artifactAPI.delete'] = () => Promise.reject(new Error('it is linked'));
    await press(all('button').filter((b) => b.textContent === 'Discard')[0]);
    await snapshot('step-8-discard-failed');
    await click('← Back');
    await snapshot('step-7-after-back');
  });
});

describe('GuidedWizard on a phone (X18c characterization)', () => {
  it('keeps the assistant in a closed sheet behind a button, and opens and closes it', async () => {
    recorder.compact = true;
    await openAt(1);
    await snapshot('compact-closed');

    await click('💬 Assistant');
    await snapshot('compact-open');

    // The overlay closes it, the button opens it again, the close button closes it.
    await press(container.querySelector('div[style*="z-index: 949"]')!);
    expect(container.querySelector('[role="dialog"]')!.getAttribute('aria-hidden')).toBe('true');
    await click('💬 Assistant');
    await click('✕');
    await snapshot('compact-closed-again');
  });

  it('sits the sheet above the keyboard', async () => {
    recorder.compact = true;
    recorder.visual = { height: 420, keyboardInset: 300 };
    await openAt(6);
    await click('💬 Assistant');
    await snapshot('compact-keyboard');
  });

  it('shows the landing as on a desktop', async () => {
    recorder.compact = true;
    await mount();
    await snapshot('compact-landing');
  });
});
