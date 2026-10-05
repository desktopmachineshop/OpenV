import React, { act, useState } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { CopilotSuggestion, GuidedChatPanel, GuidedChatPanelHandle } from './GuidedChatPanel';

// Refactor plan step S16i (invariant I21): the V&V Assistant's chat panel as
// it behaves today, in both of its homes, before X15c moves its EventSource
// onto hooks/useEventStream and F8 gives its two modes a props-only first
// pass. Both re-run it first and must leave every file snapshot, every ordered
// call list and the stream's reconnect delays identical.
//
//   wizard          beside the guided wizard (GuidedWizard's desktop column:
//                   a step, the wizard's answers, the applied-card keys it
//                   persists, its apply handler).
//     empty         the transcript loaded empty, the stream open and the
//                   kickoff asked for but not yet answered.
//     thinking      the kickoff answered "launched".
//     partial       assistant_partial events: each carries the whole text so
//                   far and replaces the bubble; an empty, a text-less and a
//                   malformed one are ignored; half a suggestion block is
//                   prose, not a card.
//     reply         the final `message` replaces the bubble: prose and a card
//                   of every wizard kind (a framing field that has text is
//                   replaced, one that has none applied, a "replaces" naming an
//                   entry id shown by its title), two malformed blocks shown
//                   raw, an unknown kind, a project edit; Apply all. The same
//                   id again, a message with no id and a malformed one are
//                   ignored.
//     applied       one card added, one refused (its reason shown), then
//                   Apply all with two refused. A nudge from the wizard
//                   follows (calls only).
//   transcript      a transcript of assistant (markdown), user (line breaks
//                   kept), assistant with cards, one already applied, and
//                   system messages: no kickoff.
//     sent          a message typed and sent: echoed, composer cleared,
//                   thinking. Then one sent with Ctrl+Enter, trimmed, and an
//                   assistant reply on the stream.
//     send-failed   a quick action whose send fails.
//   runner-offline  the kickoff says no runner is online: the connect notice,
//                   kept by a send that says so too, gone when a reply lands.
//   error           the transcript fails to load: no stream, no kickoff. A
//                   send then clears the error.
//   notes           beside an artifact (ChatterPanel's notes column):
//                   embedded, the notes subtitle and quick actions, the
//                   artifact id in every turn, no step and no wizard state;
//                   project cards of every kind.
//     notes-applied Apply all, every card in its done state.
//   read-only       no apply handler: cards say to open the project, no
//                   Apply all.
//   stream-reconnect  a dropped stream is closed and reopened after 2, 4, 8 s
//                   and then every 15 s; an open resets the wait; the reopened
//                   stream delivers, a message already shown is ignored;
//                   nothing reloads; an unmount during the wait cancels the
//                   reopen (calls and waits only).
//   stream-session  the session changes: the old stream closes, the new
//                   session's transcript loads, its stream opens and, being
//                   empty, it is kicked off; an unmount closes the stream
//                   (calls only).
//
// The stream is driven the way a browser drives one: each event goes to the
// on<type> handler and to every listener of its type, so the test does not
// care which of the two the panel (or X15c's hook) uses; at most one stream is
// ever open.
//
// Each snapshot is container.innerHTML, one tag per line, followed by the form
// controls' values (innerHTML does not carry them), written to
// __snapshots__/GuidedChatPanel.<name>.html. Each mode asserts its ordered
// calls in brief; the last test writes every mode's calls with their full
// arguments (the API's, each stream opened or closed, each batch the host is
// asked to apply), collected by the mode tests in file order, to
// __snapshots__/GuidedChatPanel.api.json. The panel and
// ChatMarkdown render for real inside a real router; only the API client and
// the browser's EventSource are replaced.
//
// Regenerate only for a deliberate change to what the panel shows, sends or
// streams, never in a refactor pull request:
//   npx vitest run src/components/wizard/GuidedChatPanel.snapshot.test.tsx -u

// Every *API namespace of the client is replaced by recorders over a canned
// responder: the call is logged, in the order made, with its arguments as
// JSON at the moment of the call. URL builders keep the real code.
const recorder = vi.hoisted(() => ({
  calls: [] as { name: string; json: string }[],
  respond: (_name: string, _args: unknown[]): unknown => undefined,
}));

vi.mock('../../api/client', async (importOriginal) => {
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

type Role = 'assistant' | 'user' | 'system';
const message = (id: string, role: Role, content: string, sessionId = 'gs-1') => ({
  id,
  session_id: sessionId,
  role,
  content,
  created_at: T,
});
// A suggestion block as the assistant writes one into a reply.
const card = (s: Record<string, unknown>) => '```openv-suggestion\n' + JSON.stringify(s) + '\n```';
const reply = (id: string, ...parts: string[]) => message(id, 'assistant', parts.join('\n\n'));

// What the wizard has entered when the panel asks (getState): a vision already
// written, so its card replaces it; no target users, so that card applies;
// one persona and one need with stable ids, which a card's "replaces" names.
const WIZARD_STATE = {
  step_1: { vision: 'Every shop floor runs its machines safely.', problem_statement: '', target_users: '' },
  step_2: { personas: [{ id: 'per-1', name: 'Maya the Machinist', role: 'CNC operator', goals: '', pains: '' }] },
  step_3: { needs: [{ id: 'need-1', persona_id: 'per-1', capability: 'a live view of each spindle', outcome: 'I can clear the floor' }] },
};

// The wizard reply: prose, then a card of each wizard kind, two blocks that
// are not cards, an unknown kind and a project edit. Segments alternate with
// the blank lines between them, so the cards are segments 1, 3, 5, … and
// their keys "m-2:1", "m-2:3", ….
const WIZARD_REPLY = reply(
  'm-2',
  'Your vision statement is **vague**. Two framings:',
  card({ kind: 'framing', field: 'vision', text: 'Every machine on the floor reports its state before anyone touches it.' }),
  card({ kind: 'framing', field: 'target_users', text: 'Machinists, shop leads and maintenance planners.' }),
  card({ kind: 'persona', name: 'Sam the Shop Lead', role: 'Supervisor', goals: 'Keep the floor safe' }),
  card({ kind: 'persona', replaces: 'per-1', name: 'Maya the Machinist', role: 'Senior CNC operator' }),
  card({ kind: 'need', persona: 'Sam the Shop Lead', capability: 'a list of the live spindles', outcome: 'I can clear the floor' }),
  card({ kind: 'need', replaces: 'need-1', outcome: 'I can clear the floor before maintenance' }),
  card({ kind: 'requirement', text: 'The system shall show each live spindle within 1 s.', fit_criterion: 'Measured on the shop display' }),
  card({ kind: 'nfr', text: 'The system shall keep 90 days of spindle history.' }),
  card({ kind: 'hazard', hazard: 'A spindle restarts after a power dip', harm: 'Crushed fingers', severity: 'Medium' }),
  '```openv-suggestion\n{"kind": "nfr", "text": }\n```',
  '```openv-suggestion\n{"field": "vision"}\n```',
  card({ kind: 'risk', text: 'Coolant mist hides the lamp' }),
  card({ kind: 'edit', ref: 'REQ-4', title: 'Spindle lamp', body: 'The lamp shall be red while the spindle turns.', attributes: { status: 'review', owner: 'Sam' } }),
  'Tell me which **fits**.'
);
const SAM = 'm-2:5';
const TARGET_USERS = 'm-2:3';
const RISK = 'm-2:23';

// The project reply, beside an artifact: a card of each kind the project
// takes.
const NOTES_REPLY = reply(
  'm-2',
  'For **REQ-7**, here is what I would change:',
  card({ kind: 'framing', field: 'vision', text: 'Every machine reports its state.' }),
  card({ kind: 'persona', name: 'Pat the Planner', role: 'Maintenance planner' }),
  card({ kind: 'requirement', replaces: 'REQ-4', text: 'The system shall light the lamp within 200 ms.' }),
  card({ kind: 'artifact', type: 'test-case', title: 'Lamp latency test', parent: 'HDG-4 ', after: ' TC-2' }),
  card({ kind: 'artifact', type: 'heading', title: 'Maintenance' }),
  card({ kind: 'edit', ref: 'REQ-7', body: 'The lamp shall be red while the spindle turns.' }),
  card({ kind: 'move', ref: 'REQ-7', parent: '', before: 'REQ-2', position: 'first' }),
  card({ kind: 'move', ref: 'REQ-8', parent: 'HDG-4', after: 'REQ-7', position: 'last' })
);

const TRANSCRIPT = [
  reply('m-1', 'Welcome. Tell me about the product:\n- **who** uses it\n- what `REQ-1` should say\n\nSee [the guide](https://openv.app/manual).'),
  message('m-2', 'user', 'It watches spindles.\nAnd lights a lamp.'),
  reply(
    'm-3',
    'Two personas and a need:',
    card({ kind: 'persona', name: 'Maya the Machinist', role: 'CNC operator' }),
    card({ kind: 'persona', name: 'Sam the Shop Lead', role: 'Supervisor' }),
    card({ kind: 'need', persona: 'Maya the Machinist', capability: 'a lamp per spindle', outcome: 'I know it is live' })
  ),
  message('m-4', 'system', 'The assistant hit a technical problem answering. Try again.'),
];

const deferred = <V,>() => {
  let release: (value: V) => void = () => {};
  const promise = new Promise<V>((resolve) => {
    release = resolve;
  });
  return { promise, release };
};

// What each test's server answers. listMessages and sendMessage may answer
// with a rejection; kickoffChat may be held until the test lets it through.
let server: {
  transcripts: Record<string, unknown[] | Error>;
  kickoff: unknown;
  sends: unknown[];
  refuse: Record<string, string>;
};
let sent = 0;

const answer = (value: unknown) => (value instanceof Error ? Promise.reject(value) : value);
const CANNED: Record<string, (...args: any[]) => unknown> = {
  'guidedAPI.listMessages': (id: string) => answer(server.transcripts[id] ?? []),
  'guidedAPI.kickoffChat': () => server.kickoff,
  'guidedAPI.nudgeChat': () => ({ status: 'pending', runner_online: true }),
  // The person's message comes back saved; the next queued answer, or the
  // runner being online, goes with it.
  'guidedAPI.sendMessage': (id: string, content: string) => {
    const queued = server.sends.shift();
    if (queued instanceof Error) return Promise.reject(queued);
    return { message: message(`u-${++sent}`, 'user', content, id), runner_online: true, ...(queued as object) };
  },
};
recorder.respond = (name, args) => CANNED[name]?.(...args);

// The browser's EventSource: opening and closing one are logged with the API
// calls (the URL without its origin, which follows the environment), and the
// test dispatches events the way a browser does, to the on<type> handler and
// to every listener of that type.
class FakeEventSource {
  static all: FakeEventSource[] = [];
  readonly url: string;
  readonly withCredentials: boolean;
  readyState = 0;
  listeners: Record<string, ((event: MessageEvent) => void)[]> = {};
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;

  constructor(url: string, init?: EventSourceInit) {
    this.url = url.replace(/^[a-z]+:\/\/[^/]+/, '');
    this.withCredentials = !!init?.withCredentials;
    recorder.calls.push({ name: 'EventSource', json: JSON.stringify([this.url, init]) });
    FakeEventSource.all.push(this);
  }

  addEventListener(type: string, fn: (event: MessageEvent) => void) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  }

  removeEventListener(type: string, fn: (event: MessageEvent) => void) {
    this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== fn);
  }

  close() {
    recorder.calls.push({ name: 'EventSource.close', json: JSON.stringify([this.url]) });
    this.closed = true;
    this.readyState = 2;
  }

  dispatch(type: string, data?: string) {
    const event = { type, data } as MessageEvent;
    if (type === 'open') this.readyState = 1;
    const handler = (this as any)[`on${type}`];
    if (typeof handler === 'function') handler.call(this, event);
    (this.listeners[type] || []).slice().forEach((fn) => fn(event));
  }
}

// ---- harness ---------------------------------------------------------------

// Flushing waits on real macrotasks, so it works while the reconnect test
// fakes setTimeout.
const realSetTimeout = globalThis.setTimeout.bind(globalThis);
const realEventSource = (globalThis as any).EventSource;
const realScrollTo = (Element.prototype as any).scrollTo;

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => realSetTimeout(resolve, 0));
  });
};

const mount = async (element: React.ReactElement) => {
  await act(async () => {
    root.render(<MemoryRouter initialEntries={['/projects/p1/guided']}>{element}</MemoryRouter>);
  });
  await flush();
};

// The host's side of the panel, as GuidedWizard and ChatterPanel wire it: it
// owns the applied-card keys, and its apply handler (logged with the calls)
// refuses the cards server.refuse names and marks the rest applied.
const Host = ({
  sessionId = 'gs-1',
  panelRef,
  initialApplied = {},
  notes = false,
}: {
  sessionId?: string;
  panelRef?: React.Ref<GuidedChatPanelHandle>;
  initialApplied?: Record<string, boolean>;
  notes?: boolean;
}) => {
  const [applied, setApplied] = useState(initialApplied);
  const onApplySuggestions = async (items: { suggestion: CopilotSuggestion; key: string }[]) => {
    recorder.calls.push({ name: 'onApplySuggestions', json: JSON.stringify([items]) });
    const results = items.map((item) => server.refuse[item.key] ?? null);
    setApplied((prev) => ({
      ...prev,
      ...Object.fromEntries(items.filter((_, i) => results[i] === null).map((item) => [item.key, true])),
    }));
    return results;
  };
  return notes ? (
    <GuidedChatPanel
      sessionId={sessionId}
      artifactId="art-7"
      embedded
      applied={applied}
      applyTarget="project"
      onApplySuggestions={onApplySuggestions}
      subtitle="Answers about the artifact on screen — same conversation as the wizard."
    />
  ) : (
    <GuidedChatPanel
      ref={panelRef}
      sessionId={sessionId}
      step={2}
      getState={() => WIZARD_STATE}
      applied={applied}
      onApplySuggestions={onApplySuggestions}
    />
  );
};

// The one stream that is open: the panel never holds two.
const stream = (): FakeEventSource => {
  const open = FakeEventSource.all.filter((es) => !es.closed);
  expect(open.map((es) => es.url), 'open streams').toHaveLength(1);
  return open[0];
};

const emit = async (type: string, data?: unknown) => {
  const es = stream();
  await act(async () => {
    es.dispatch(type, data === undefined || typeof data === 'string' ? data : JSON.stringify(data));
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

// The apply button of the card whose title reads `title`.
const cardButton = (title: string): HTMLButtonElement => {
  const heading = byText('div', title);
  const button = heading.parentElement!.querySelector('button');
  expect(button, `the button of card "${title}"`).toBeTruthy();
  return button!;
};

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

const composer = () => container.querySelector('textarea')!;

// React tracks a control's value itself, so the value goes through the
// native setter and then the event React listens for.
const type = async (value: string) => {
  const node = composer();
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
};

const ctrlEnter = async () => {
  await act(async () => {
    composer().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }));
  });
  await flush();
};

// innerHTML with a line break put between adjacent tags ("><"), then the
// value of every form control in document order: React sets a control's
// value as a property, which innerHTML does not carry.
const snapshot = (name: string) => {
  const controls = Array.from(
    container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>('input, textarea, select')
  ).map((node, i) => `<!-- control ${i + 1}: ${node.tagName.toLowerCase()} = ${JSON.stringify(node.value)} -->`);
  return expect(
    [container.innerHTML.replace(/></g, '>\n<'), ...controls].join('\n') + '\n'
  ).toMatchFileSnapshot(`./__snapshots__/GuidedChatPanel.${name}.html`);
};

// The call list in brief: arguments that are objects or arrays are elided
// ({…}, [n]); the full arguments are in GuidedChatPanel.api.json. An undefined
// argument (the artifact id outside the notes panel) prints as null.
const brief = () =>
  recorder.calls.map(({ name, json }) => {
    const args = JSON.parse(json) as unknown[];
    const shown = args.map((a) =>
      Array.isArray(a) ? `[${a.length}]` : a !== null && typeof a === 'object' ? '{…}' : JSON.stringify(a)
    );
    return `${name}(${shown.join(', ')})`;
  });

// The card keys of each batch the host was asked to apply.
const appliedKeys = () =>
  recorder.calls
    .filter((c) => c.name === 'onApplySuggestions')
    .map((c) => (JSON.parse(c.json)[0] as { key: string }[]).map((item) => item.key));

// Every mode's calls with their full arguments, in the order the tests run;
// the last test writes them to GuidedChatPanel.api.json.
const MODES = ['wizard', 'transcript', 'runner-offline', 'error', 'notes', 'read-only', 'stream-reconnect', 'stream-session'];
const API: Record<string, { call: string; args: unknown[] }[]> = {};
const keep = (mode: string) => {
  API[mode] = recorder.calls.map(({ name, json }) => ({ call: name, args: JSON.parse(json) }));
};

const STREAM = (id: string) => `EventSource("/api/v1/guided-sessions/${id}/chat/stream", {…})`;
const CLOSE = (id: string) => `EventSource.close("/api/v1/guided-sessions/${id}/chat/stream")`;
const THINKING = 'The assistant is thinking…';
const OFFLINE = 'V&V Assistant not connected';

beforeEach(() => {
  recorder.calls = [];
  server = {
    transcripts: {},
    kickoff: { status: 'launched', runner_online: true },
    sends: [],
    refuse: {},
  };
  sent = 0;
  FakeEventSource.all = [];
  (globalThis as any).EventSource = FakeEventSource;
  // jsdom has no scrolling; the panel scrolls to its newest message.
  (Element.prototype as any).scrollTo = () => {};
  // The clock is frozen (a nudge is throttled by Date.now()); timers stay
  // real, except in the reconnect test.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(T));
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
  vi.useRealTimers();
  (globalThis as any).EventSource = realEventSource;
  (Element.prototype as any).scrollTo = realScrollTo;
});

describe('GuidedChatPanel characterization (S16i)', () => {
  it('wizard: empty, the kickoff, a reply streaming in and landing, cards applied', async () => {
    const kickoff = deferred<unknown>();
    server.kickoff = kickoff.promise.then(() => ({ status: 'launched', runner_online: true }));
    server.refuse = {
      [TARGET_USERS]: 'Target users are locked by the product profile.',
      [RISK]: 'Unknown suggestion kind "risk".',
    };
    const panel = React.createRef<GuidedChatPanelHandle>();
    await mount(<Host panelRef={panel} />);
    expect(container.textContent).toContain('The assistant will join in a moment');
    await snapshot('empty');

    kickoff.release(undefined);
    await flush();
    expect(container.textContent).toContain(THINKING);
    await snapshot('thinking');

    await emit('assistant_partial', { run_id: 'run-1', text: 'Your vision ' });
    expect(container.textContent).not.toContain(THINKING);
    await emit('assistant_partial', { run_id: 'run-1', text: '' });
    await emit('assistant_partial', { run_id: 'run-1' });
    await emit('assistant_partial', 'not json');
    expect(container.querySelector('[data-testid="assistant-partial"]')!.textContent).toBe('Your vision');
    await emit('assistant_partial', {
      run_id: 'run-1',
      text: 'Your vision statement is **vague**. Two framings:\n\n```openv-suggestion\n{"kind": "framing", "fie',
    });
    expect(container.querySelectorAll('[data-testid="assistant-partial"]')).toHaveLength(1);
    await snapshot('partial');

    await emit('message', WIZARD_REPLY);
    expect(container.querySelector('[data-testid="assistant-partial"]')).toBeNull();
    await snapshot('reply');
    const shown = container.innerHTML;
    await emit('message', { ...WIZARD_REPLY, content: 'A second copy is never shown.' });
    await emit('message', { ...WIZARD_REPLY, id: '' });
    await emit('message', '{"id": "m-9",');
    expect(container.innerHTML).toBe(shown);

    // One card, then one refused (its reason shown as is), then the rest.
    await click(cardButton('Sam the Shop Lead'));
    expect(container.textContent).toContain('✓ Added to wizard');
    await click(cardButton(JSON.stringify({ kind: 'risk', text: 'Coolant mist hides the lamp' })));
    expect(container.textContent).toContain('Unknown suggestion kind "risk".');
    await click(byText('button', 'Apply all (10)'));
    expect(container.textContent).toContain(
      '2 suggestions could not be applied — Target users are locked by the product profile.'
    );
    await snapshot('applied');

    // The wizard moves on: the nudge goes out at once and the panel thinks.
    await act(async () => {
      panel.current!.nudge(3, 'saved step 2 ("Personas") and moved on to step 3 ("User needs")');
    });
    await flush();
    expect(container.textContent).toContain(THINKING);

    expect(brief()).toEqual([
      // the transcript, then the stream, then (the transcript being empty)
      // the kickoff with the step, the wizard's answers and no artifact
      'guidedAPI.listMessages("gs-1")',
      STREAM('gs-1'),
      'guidedAPI.kickoffChat("gs-1", 2, {…}, null)',
      // Sam's card alone; the refused card alone; Apply all: the cards not
      // applied, the refused one again among them
      'onApplySuggestions([1])',
      'onApplySuggestions([1])',
      'onApplySuggestions([10])',
      'guidedAPI.nudgeChat("gs-1", 3, {…}, "saved step 2 (\\"Personas\\") and moved on to step 3 (\\"User needs\\")")',
    ]);
    expect(appliedKeys().slice(0, 2)).toEqual([[SAM], [RISK]]);
    keep('wizard');
  });

  it('transcript: messages of every role, a send, a send that fails', async () => {
    server.transcripts['gs-1'] = TRANSCRIPT;
    server.sends = [{}, {}, new Error('network down')];
    await mount(<Host initialApplied={{ 'm-3:1': true }} />);
    await snapshot('transcript');

    await type('How short can the vision be?\nOne line?');
    await click(byText('button', 'Send'));
    expect(composer().value).toBe('');
    expect(container.textContent).toContain(THINKING);
    await snapshot('sent');

    await type('  Make it one line.  ');
    await ctrlEnter();
    expect(composer().value).toBe('');
    await emit('message', reply('m-7', 'One line it is.'));
    expect(container.textContent).not.toContain(THINKING);

    await type('Kept while a quick action fails.');
    await click(byText('button', 'Gap analysis'));
    expect(container.textContent).toContain('Message failed to send — please try again.');
    await snapshot('send-failed');

    expect(brief()).toEqual([
      // a transcript that is not empty is not kicked off
      'guidedAPI.listMessages("gs-1")',
      STREAM('gs-1'),
      'guidedAPI.sendMessage("gs-1", "How short can the vision be?\\nOne line?", 2, {…}, null)',
      'guidedAPI.sendMessage("gs-1", "Make it one line.", 2, {…}, null)',
      `guidedAPI.sendMessage("gs-1", ${JSON.stringify(
        'Do a gap analysis: given everything entered so far, what is missing on this step and across the definition as a whole? Propose the most important missing entries as suggestions.'
      )}, 2, {…}, null)`,
    ]);
    keep('transcript');
  });

  it('runner-offline: the kickoff finds no runner; a reply clears the notice', async () => {
    server.kickoff = { status: 'pending', runner_online: false };
    server.sends = [{ runner_online: false }];
    await mount(<Host />);
    expect(container.textContent).toContain(OFFLINE);
    expect(container.textContent).not.toContain(THINKING);
    await snapshot('runner-offline');

    await type('Is anyone there?');
    await click(byText('button', 'Send'));
    expect(container.textContent).toContain(OFFLINE);
    expect(container.textContent).not.toContain(THINKING);

    await emit('message', reply('m-3', 'A runner connected. Hello.'));
    expect(container.textContent).not.toContain(OFFLINE);

    expect(brief()).toEqual([
      'guidedAPI.listMessages("gs-1")',
      STREAM('gs-1'),
      'guidedAPI.kickoffChat("gs-1", 2, {…}, null)',
      'guidedAPI.sendMessage("gs-1", "Is anyone there?", 2, {…}, null)',
    ]);
    keep('runner-offline');
  });

  it('error: the transcript fails to load; no stream, no kickoff', async () => {
    server.transcripts['gs-1'] = new Error('500');
    await mount(<Host />);
    expect(container.textContent).toContain('Failed to load the assistant conversation.');
    await snapshot('error');

    await type('Hello?');
    await click(byText('button', 'Send'));
    expect(container.textContent).not.toContain('Failed to load the assistant conversation.');
    expect(container.textContent).toContain('Hello?');
    expect(FakeEventSource.all).toHaveLength(0);

    expect(brief()).toEqual([
      'guidedAPI.listMessages("gs-1")',
      'guidedAPI.sendMessage("gs-1", "Hello?", 2, {…}, null)',
    ]);
    keep('error');
  });

  it('notes: beside an artifact, project cards, Apply all', async () => {
    await mount(<Host notes />);
    await emit('message', NOTES_REPLY);
    await snapshot('notes');

    await click(byText('button', 'Apply all (8)'));
    await snapshot('notes-applied');

    await click(byText('button', 'How would I verify it?'));

    expect(brief()).toEqual([
      // no step (0), no wizard state ({}), the artifact on screen
      'guidedAPI.listMessages("gs-1")',
      STREAM('gs-1'),
      'guidedAPI.kickoffChat("gs-1", 0, {…}, "art-7")',
      'onApplySuggestions([8])',
      `guidedAPI.sendMessage("gs-1", ${JSON.stringify(
        'How would I verify the artifact I am reading? Propose a verification method and the test cases that would demonstrate it, and say what evidence each would produce.'
      )}, 0, {…}, "art-7")`,
    ]);
    keep('notes');
  });

  it('read-only: no apply handler, cards without buttons', async () => {
    server.transcripts['gs-1'] = [TRANSCRIPT[2]];
    await mount(<GuidedChatPanel sessionId="gs-1" />);
    expect(container.textContent).toContain('Open the project to add this.');
    await snapshot('read-only');

    expect(brief()).toEqual(['guidedAPI.listMessages("gs-1")', STREAM('gs-1')]);
    keep('read-only');
  });

  it('stream-reconnect: a capped backoff, reset on open; unmount closes and cancels', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] });
    vi.setSystemTime(new Date(T));
    server.transcripts['gs-1'] = [TRANSCRIPT[0]];
    await mount(<Host />);
    const first = stream();
    expect([first.url, first.withCredentials]).toEqual(['/api/v1/guided-sessions/gs-1/chat/stream', true]);

    // The wait from a drop until the next stream opens.
    const reopenAfter = async () => {
      const opened = FakeEventSource.all.length;
      await emit('error');
      const from = Date.now();
      await act(async () => {
        vi.advanceTimersToNextTimer();
      });
      await flush();
      expect(FakeEventSource.all, 'a stream reopened').toHaveLength(opened + 1);
      return Date.now() - from;
    };

    await emit('open');
    const waits: number[] = [];
    for (let i = 0; i < 7; i++) waits.push(await reopenAfter());
    await emit('open');
    waits.push(await reopenAfter());
    expect(waits).toEqual([2000, 4000, 8000, 15000, 15000, 15000, 15000, 2000]);

    // The reopened stream delivers; a message already shown is not shown
    // twice.
    await emit('message', TRANSCRIPT[0]);
    await emit('message', reply('m-5', 'Back again.'));
    expect(container.textContent).toContain('Back again.');
    expect(container.textContent!.split('Welcome. Tell me about the product').length).toBe(2);

    // A drop, then an unmount before the wait is over: nothing reopens.
    await emit('error');
    act(() => root.unmount());
    await act(async () => {
      vi.advanceTimersByTime(60_000);
    });
    expect(FakeEventSource.all.filter((es) => !es.closed)).toHaveLength(0);
    expect(FakeEventSource.all).toHaveLength(9);
    act(() => {
      root = createRoot(container);
    });

    expect(brief()).toEqual([
      'guidedAPI.listMessages("gs-1")',
      // the first stream, then eight drops, each closing the stream and
      // opening the next one (and nothing else) after its wait; the last
      // drop opens nothing
      STREAM('gs-1'),
      ...Array.from({ length: 8 }, () => [CLOSE('gs-1'), STREAM('gs-1')]).flat(),
      CLOSE('gs-1'),
    ]);
    keep('stream-reconnect');
  });

  it('stream-session: a new session closes the old stream and opens its own', async () => {
    server.transcripts['gs-1'] = [TRANSCRIPT[0]];
    await mount(<Host sessionId="gs-1" />);
    expect(container.textContent).toContain('Welcome. Tell me about the product');

    await mount(<Host sessionId="gs-2" />);
    expect(container.textContent).not.toContain('Welcome. Tell me about the product');
    expect(stream().url).toBe('/api/v1/guided-sessions/gs-2/chat/stream');
    await emit('message', message('m-1', 'assistant', 'A new session.', 'gs-2'));
    expect(container.textContent).toContain('A new session.');

    act(() => root.unmount());
    expect(FakeEventSource.all.filter((es) => !es.closed)).toHaveLength(0);
    act(() => {
      root = createRoot(container);
    });

    expect(brief()).toEqual([
      'guidedAPI.listMessages("gs-1")',
      STREAM('gs-1'),
      CLOSE('gs-1'),
      'guidedAPI.listMessages("gs-2")',
      STREAM('gs-2'),
      'guidedAPI.kickoffChat("gs-2", 2, {…}, null)',
      // the unmount
      CLOSE('gs-2'),
    ]);
    keep('stream-session');
  });

  it('every call with its full arguments', async () => {
    expect(Object.keys(API), 'every mode ran, in order').toEqual(MODES);
    await expect(JSON.stringify(API, null, 2) + '\n').toMatchFileSnapshot('./__snapshots__/GuidedChatPanel.api.json');
  });
});
