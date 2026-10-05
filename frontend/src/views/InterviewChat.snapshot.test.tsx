import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { InterviewChat, InterviewChatRoute } from './InterviewChat';
import { DialogProvider } from '../components/ui';

// Refactor plan step S16j (invariants I9 and I21): the public interview page
// at /interview/:token as it behaves today, before X15d moves its
// EventSource onto useEventStream. X15d re-runs it first and must leave every
// file snapshot and the ordered calls identical. The stream is driven only
// from outside, through a fake EventSource, and read only through the DOM.
//
// Each mode below is one test, and the snapshots it writes:
//
//   loading      loading: the intro still on its way. Then an interview with
//                no name is called "Interview", and the stream opens only
//                once the intro is in; unmounted while loading, the page
//                never opens one.
//   error        error: an expired invite (404), and no token at all,
//                without a call, read the same. busy: a rate-limited intro
//                (429) says to wait, in the server's words; no answer at all
//                (a network error) asks to check the connection (#379 bug
//                215).
//   name         name: a first visit (no session yet) asks a name, with the
//                stream already open; Start is disabled until the name is
//                more than spaces. fresh: the name taken (trimmed, no call),
//                the empty chat and its hint. The first answer carries the
//                name.
//   transcript   transcript: a resumed session, the interviewer's markdown,
//                the participant's literal words, a system note; the replay
//                the stream sends on connect adds nothing; unmount closes it.
//   send         sending: an answer on its way, Send disabled, the
//                interviewer thinking, and a second send ignored; the
//                answer's broadcast arrives before the call's answer.
//                typing: the answer confirmed, the composer cleared, still
//                thinking. partial: `assistant_partial`, the reply so far,
//                in place of the thinking line, then replaced by the next;
//                malformed frames are ignored. reply: the whole reply as a
//                `message` event, shown once. Then answers sent with
//                Ctrl+Enter and Cmd+Enter (Enter alone, or with Shift, sends
//                nothing, nor do spaces).
//   send-error   send-error: the server's own words for a refused answer (the
//                rate limit); then a network error's fallback; the next send
//                clears it as it starts.
//   end          end-confirm: End interview asks first; Cancel leaves the
//                chat. done: confirmed, finish, the stream closed, the
//                thank-you page; the same page when finish fails.
//   ended        ended: a session the intro already reports completed (or
//                finished) shows the thank-you page and opens no stream.
//   reconnect    reconnect: a dropped stream asks the intro again (an
//                interview ended elsewhere refuses its stream, #379 bug 216)
//                and reopens after 2, 4, 8, 15, 15 ... s, an open restarting
//                the count; a reopened stream's replay adds nothing and its
//                new message shows. A reconnect pending at unmount or at the
//                end of the interview opens nothing.
//   phone        phone: at 390px with a coarse pointer the page is the same
//                markup as on a desktop, and the confirmation fills the width.
//
// Each snapshot is container.innerHTML, one tag per line so a diff reads,
// then the value of every form control and the focused element (innerHTML
// carries neither), written to __snapshots__/InterviewChat.<name>.html. Each
// mode asserts its ordered calls, and the last test writes every mode's calls
// with their arguments to __snapshots__/InterviewChat.api.json. The calls are
// the API client's, the stream's opening (with its init) and closing, and
// every timer of a second or more the page sets (only its reconnect sets
// any). The router and the dialog provider are real, mounted as App.tsx
// mounts the page; only the API client (and the browser's EventSource) is
// replaced, by a small in-memory server.
//
// Regenerate only for a deliberate change to what the page shows or calls,
// never in a refactor pull request:
//   npx vitest run src/views/InterviewChat.snapshot.test.tsx -u

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
const TOKEN = 'tok-1';
const clone = <V,>(value: V): V => JSON.parse(JSON.stringify(value));
// A time `seconds` after T, written as the server writes it (RFC 3339).
const at = (seconds: number) => new Date(Date.parse(T) + seconds * 1000).toISOString().replace('.000Z', 'Z');

type Row = Record<string, any>;
type Role = 'assistant' | 'participant' | 'system';

const message = (id: string, role: Role, content: string, seconds = 0): Row => ({
  id,
  session_id: 's-1',
  role,
  content,
  created_at: at(seconds),
});

const SESSION = (over: Row = {}): Row => ({
  id: 's-1',
  interview_id: 'iv-1',
  invite_id: 'inv-1',
  participant_name: 'Pat',
  status: 'active',
  summary: '',
  started_at: at(-600),
  ...over,
});

const OPENING = (): Row =>
  message(
    'm-1',
    'assistant',
    "Hi Pat! I'd like to learn how your team looks after the **coolant pumps**.\nWhat does a normal week look like?",
    -540
  );

// A resumed conversation: the interviewer writes markdown (bold, a numbered
// list, code, a link); the participant's words are literal, asterisks and
// tags and line breaks included; a system note closes it, as the server
// writes one when no interviewer could be started.
const TRANSCRIPT = (): Row[] => [
  OPENING(),
  message('m-2', 'participant', 'We check them every Monday.\nSometimes *twice* when the line runs hot. <b>Not bold.</b>', -480),
  message(
    'm-3',
    'assistant',
    'Thanks. Two follow-ups:\n\n1. Who signs the check off?\n2. Where is `PUMP-7` logged?\n\nThe [checklist](https://example.com/pumps) may help.',
    -420
  ),
  message('m-4', 'participant', 'The shift lead, in the paper log.', -360),
  message('m-5', 'system', 'The interviewer is unavailable right now. Your answer was saved — please check back shortly.', -359),
];

const RATE_LIMIT = "You're sending messages a little too quickly. Please wait a moment and try again.";
const BUSY = 'Too many requests from your network. Please wait a moment and reload the page.';
const OFFLINE = "We couldn't reach the server. Check your connection and reload the page.";

// An error as axios rejects with it: the server's JSON body on .response.
const httpError = (status: number, error: string) =>
  Object.assign(new Error(`Request failed with status code ${status}`), { response: { status, data: { error } } });

// The in-memory server, as internal/api/public_interview_handlers.go and
// internal/domain/interviews answer: the intro is read-only and sends the
// invite's session (none on a first visit, and then no transcript); a
// message or a stream connect starts a session when there is none, and a
// message fills in a blank participant name; finish completes the session
// and answers 204 with no body. The intro sends whatever session the test
// holds, as the real one sends the invite's active session or else its
// latest (#379 bug 214), so the `ended` mode's completed session is what a
// link reopened after its interview ended reads. The real server refuses a
// message or a stream once the interview has ended (409, #379 bug 216); no
// mode here sends one.
let server: {
  invite: 'open' | 'expired' | 'busy' | 'offline';
  name: string;
  session: Row | null;
  transcript: Row[];
  hold: Promise<void> | null;
  refuse: unknown;
  finishFails: boolean;
};

const startOrResume = (participantName: string): Row => {
  if (!server.session) {
    server.session = SESSION({ participant_name: participantName });
  } else if (participantName.trim() && !String(server.session.participant_name).trim()) {
    server.session.participant_name = participantName.trim();
  }
  return server.session;
};

const held = (body: unknown) => (server.hold ? server.hold.then(() => body) : body);

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'publicInterviewAPI.intro': () => {
    if (server.invite === 'expired') return Promise.reject(httpError(404, 'invite has expired'));
    if (server.invite === 'busy') return Promise.reject(httpError(429, BUSY));
    // No answer at all: axios rejects with no response.
    if (server.invite === 'offline') return Promise.reject(new Error('Network Error'));
    return held({
      interview_name: server.name,
      session: server.session ? clone(server.session) : null,
      transcript: server.session ? clone(server.transcript) : null,
    });
  },
  'publicInterviewAPI.sendMessage': (_token: string, content: string, participantName?: string) => {
    if (server.refuse) {
      const refusal = server.refuse;
      server.refuse = null;
      return Promise.reject(refusal);
    }
    const session = startOrResume(participantName || '');
    const sent = message(`m-${server.transcript.length + 1}`, 'participant', content);
    server.transcript.push(sent);
    return held({ session: clone(session), message: clone(sent) });
  },
  'publicInterviewAPI.finish': () => {
    if (server.finishFails) return Promise.reject(httpError(404, 'invite not found'));
    if (server.session) Object.assign(server.session, { status: 'completed', ended_at: T });
    return '';
  },
};

recorder.respond = (name, args) => CANNED[name]?.(...args);

const deferred = () => {
  let release = () => {};
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { promise, release };
};

// The browser's EventSource. Opening and closing one are logged with the API
// calls (the URL without its origin, which follows the environment); as with
// the real one, an event reaches its addEventListener listeners and its
// on<event> property.
const path = (url: string) => url.replace(/^[a-z]+:\/\/[^/]+/, '');

class FakeEventSource {
  static all: FakeEventSource[] = [];
  listeners: Record<string, ((event: MessageEvent) => void)[]> = {};
  onopen: ((event: MessageEvent) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: MessageEvent) => void) | null = null;
  closed = false;

  constructor(
    public url: string,
    init?: EventSourceInit
  ) {
    recorder.calls.push({ name: 'EventSource', json: JSON.stringify([path(url), init]) });
    FakeEventSource.all.push(this);
  }

  addEventListener(type: string, fn: (event: MessageEvent) => void) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  }

  close() {
    recorder.calls.push({ name: 'EventSource.close', json: JSON.stringify([path(this.url)]) });
    this.closed = true;
  }

  emit(type: string, data?: string) {
    const event = { type, data } as MessageEvent;
    const handler = (this as any)[`on${type}`];
    if (typeof handler === 'function') handler(event);
    (this.listeners[type] || []).forEach((fn) => fn(event));
  }
}

// ---- harness ---------------------------------------------------------------

const COARSE = '(hover: none) and (pointer: coarse)';
const realInnerWidth = Object.getOwnPropertyDescriptor(window, 'innerWidth');
const realMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia');
const realEventSource = (globalThis as any).EventSource;
const realScrollIntoView = (Element.prototype as any).scrollIntoView;
const realSetTimeout = window.setTimeout.bind(window);

let container: HTMLDivElement;
let root: Root;
let mounted = false;
// The timers of a second or more the page has set and the test not yet run.
let timers: (() => void)[] = [];

const setViewport = (width: number, coarse: boolean) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: width });
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: (query: string) => ({
      matches: coarse && query === COARSE,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
};

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => realSetTimeout(resolve, 0));
  });
};

// Mounted as App.tsx mounts it: inside the dialog provider, on its route.
// The bare /interview route is not App's; it reaches the page's own guard
// for a missing token.
const mount = async (entry = `/interview/${TOKEN}`) => {
  if (mounted) unmount();
  act(() => {
    root = createRoot(container);
  });
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[entry]}>
        <DialogProvider>
          <Routes>
            <Route path="/interview/:token" element={<InterviewChatRoute />} />
            <Route path="/interview" element={<InterviewChat />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  mounted = true;
  await flush();
};

const unmount = () => {
  act(() => root.unmount());
  mounted = false;
};

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

// React tracks a control's value itself, so the value goes through the
// native setter and then the event React listens for.
const setValue = async (node: HTMLInputElement | HTMLTextAreaElement, value: string) => {
  const proto = node instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  await act(async () => {
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
};

const press = async (node: Element, init: KeyboardEventInit) => {
  await act(async () => {
    node.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }));
  });
  await flush();
};

const byText = <E extends Element = HTMLElement>(scope: Element, selector: string, text: string): E => {
  const found = Array.from(scope.querySelectorAll<E>(selector)).find((node) => (node.textContent ?? '').trim() === text);
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

// The page's words, the composer's left out: React mirrors a textarea's
// value into its text as well.
const text = () => {
  const copy = container.cloneNode(true) as HTMLElement;
  copy.querySelectorAll('textarea').forEach((node) => node.remove());
  return copy.textContent ?? '';
};
const count = (words: string) => text().split(words).length - 1;
const composer = () => container.querySelector('textarea')!;
// The composer's button reads Send, or Sending… while an answer is on its way.
const sendLabel = () =>
  Array.from(container.querySelectorAll('button')).some((b) => b.textContent === 'Sending…') ? 'Sending…' : 'Send';
const sendButton = () => byText<HTMLButtonElement>(container, 'button', sendLabel());
const nameInput = () => container.querySelector<HTMLInputElement>('input[placeholder="Your name"]')!;
const startButton = () => byText<HTMLButtonElement>(container, 'button', 'Start interview');
const endButton = () => byText(container.querySelector('header')!, 'button', 'End interview');
const confirmation = () => container.querySelector<HTMLElement>('[role="alertdialog"]');
const partial = () => container.querySelector<HTMLElement>('[data-testid="assistant-partial"]');
const THINKING = 'The interviewer is thinking…';
const HINT = 'Say hello to get started — the interviewer will guide the conversation.';
const FALLBACK = 'Message failed to send — please try again.';

// The one open stream, and the server's side of it.
const stream = () => {
  const open = FakeEventSource.all.filter((es) => !es.closed);
  expect(open.length, 'open streams').toBe(1);
  return open[0];
};
const emit = async (type: string, data?: unknown) => {
  const es = stream();
  await act(async () => {
    es.emit(type, data === undefined || typeof data === 'string' ? data : JSON.stringify(data));
  });
  await flush();
};
// The server's side of a connect: it starts a session when there is none,
// opens, and replays the whole transcript as message events.
const serve = async () => {
  startOrResume('');
  const es = stream();
  await act(async () => {
    es.emit('open');
    for (const m of server.transcript) es.emit('message', JSON.stringify(m));
  });
  await flush();
};
// A message the server stores and sends on the open stream.
const say = async (role: Role, content: string) => {
  const m = message(`m-${server.transcript.length + 1}`, role, content);
  server.transcript.push(m);
  await emit('message', m);
  return m;
};
// The next pending long timer runs.
const fire = async () => {
  const run = timers.shift();
  expect(run, 'a pending timer').toBeTruthy();
  await act(async () => run!());
  await flush();
};

const describeNode = (node: Element | null) => {
  if (!node || node === document.body) return 'body';
  const label = node.getAttribute('placeholder') ?? (node.textContent ?? '').trim();
  return `${node.tagName.toLowerCase()} ${JSON.stringify(label)}`;
};

// innerHTML with a line break put between adjacent tags ("><"), then the
// value of every form control in document order (React sets it as a
// property, which innerHTML does not carry) and the focused element.
const snapshot = (name: string) => {
  const controls = Array.from(container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input, textarea')).map(
    (node, i) => `<!-- control ${i + 1}: ${node.tagName.toLowerCase()} = ${JSON.stringify(node.value)} -->`
  );
  return expect(
    [
      container.innerHTML.replace(/></g, '>\n<'),
      ...controls,
      `<!-- focus: ${describeNode(document.activeElement)} -->`,
    ].join('\n') + '\n'
  ).toMatchFileSnapshot(`./__snapshots__/InterviewChat.${name}.html`);
};

// The call list in brief, as `name(args)`; keep() files it under its mode
// for InterviewChat.api.json.
const brief = () =>
  recorder.calls.map(({ name, json }) => {
    const args = JSON.parse(json) as unknown[];
    return `${name}(${args.map((a) => JSON.stringify(a)).join(', ')})`;
  });
const log: Record<string, { call: string; args: unknown }[]> = {};
const keep = (mode: string) => {
  log[mode] = recorder.calls.map(({ name, json }) => ({ call: name, args: JSON.parse(json) }));
};

const INTRO = `publicInterviewAPI.intro(${JSON.stringify(TOKEN)})`;
const STREAM_URL = `/api/v1/public/interviews/${TOKEN}/stream`;
// The public page opens its stream without credentials.
const STREAM = `EventSource(${JSON.stringify(STREAM_URL)}, {"withCredentials":false})`;
const CLOSE = `EventSource.close(${JSON.stringify(STREAM_URL)})`;
const SEND = (content: string, participantName = 'Pat') =>
  `publicInterviewAPI.sendMessage(${[TOKEN, content, participantName].map((a) => JSON.stringify(a)).join(', ')})`;
const FINISH = `publicInterviewAPI.finish(${JSON.stringify(TOKEN)})`;
const TIMER = (ms: number) => `setTimeout(${ms})`;
// What every mount of a live session makes: the intro, then the stream.
const MOUNT = [INTRO, STREAM];

beforeEach(() => {
  recorder.calls = [];
  server = {
    invite: 'open',
    name: 'Pump maintenance',
    session: SESSION(),
    transcript: [OPENING()],
    hold: null,
    refuse: null,
    finishFails: false,
  };
  FakeEventSource.all = [];
  (globalThis as any).EventSource = FakeEventSource;
  // jsdom lays nothing out, so it has no scrollIntoView.
  (Element.prototype as any).scrollIntoView = () => {};
  // A timer of a second or more is logged and held for fire(); shorter ones
  // (the harness's own flush among them) run as usual.
  timers = [];
  vi.spyOn(window, 'setTimeout').mockImplementation(((handler: () => void, delay?: number, ...rest: unknown[]) => {
    if ((delay ?? 0) < 1000) return realSetTimeout(handler, delay, ...rest);
    recorder.calls.push({ name: 'setTimeout', json: JSON.stringify([delay]) });
    timers.push(handler);
    return 0;
  }) as any);
  // The page prints no date today; were one to appear, it would print as
  // ISO text on every machine. The clock is frozen; timers stay real.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(T));
  vi.spyOn(Date.prototype, 'toLocaleString').mockImplementation(function (this: Date) {
    return this.toISOString();
  });
  vi.spyOn(Date.prototype, 'toLocaleDateString').mockImplementation(function (this: Date) {
    return this.toISOString().slice(0, 10);
  });
  vi.spyOn(Date.prototype, 'toLocaleTimeString').mockImplementation(function (this: Date) {
    return this.toISOString().slice(11, 19);
  });
  setViewport(1280, false);
  container = document.createElement('div');
  document.body.appendChild(container);
});

afterEach(() => {
  if (mounted) unmount();
  container.remove();
  vi.restoreAllMocks();
  vi.useRealTimers();
  (globalThis as any).EventSource = realEventSource;
  (Element.prototype as any).scrollIntoView = realScrollIntoView;
  for (const [key, real] of [
    ['innerWidth', realInnerWidth],
    ['matchMedia', realMatchMedia],
  ] as const) {
    if (real) Object.defineProperty(window, key, real);
    else delete (window as any)[key];
  }
});

const MODES = [
  'loading',
  'error',
  'name',
  'transcript',
  'send',
  'send-error',
  'end',
  'ended',
  'reconnect',
  'phone',
];

describe('InterviewChat characterization (S16j)', () => {
  it('loading: the intro on its way, then the stream', async () => {
    server.name = '';
    const gate = deferred();
    server.hold = gate.promise;
    await mount();
    expect(text()).toBe('Loading interview…');
    expect(FakeEventSource.all).toHaveLength(0);
    expect(brief()).toEqual([INTRO]);
    await snapshot('loading');

    gate.release();
    server.hold = null;
    await flush();
    // An interview with no name is called "Interview".
    expect(container.querySelector('header')!.textContent).toBe('InterviewInterviewing PatEnd interview');
    expect(brief()).toEqual(MOUNT);

    // Unmounted before its intro is in, the page never opens a stream.
    const second = deferred();
    server.hold = second.promise;
    await mount();
    expect(text()).toBe('Loading interview…');
    unmount();
    second.release();
    await flush();
    expect(FakeEventSource.all).toHaveLength(1);

    expect(brief()).toEqual([...MOUNT, CLOSE, INTRO]);
    keep('loading');
  });

  it('error: an expired invite, a rate-limited or unanswered intro, and no token at all', async () => {
    server.invite = 'expired';
    await mount();
    expect(text()).toBe(
      "🔗This link isn't working" +
        'The interview invite may have expired or been revoked. Please ask the person who sent it to you for a new link.'
    );
    expect(FakeEventSource.all).toHaveLength(0);
    await snapshot('error');
    const page = container.innerHTML;

    // A rate-limited intro says to wait, in the server's words: the link is
    // fine (#379 bug 215).
    server.invite = 'busy';
    await mount();
    expect(text()).toBe("⏳The interview didn't load" + BUSY);
    expect(FakeEventSource.all).toHaveLength(0);
    await snapshot('busy');

    // No answer at all: the page's own words, on the same page.
    server.invite = 'offline';
    await mount();
    expect(text()).toBe("⏳The interview didn't load" + OFFLINE);
    expect(FakeEventSource.all).toHaveLength(0);

    // With no token the page asks nothing.
    server.invite = 'open';
    await mount('/interview');
    expect(container.innerHTML).toBe(page);
    expect(FakeEventSource.all).toHaveLength(0);

    expect(brief()).toEqual([INTRO, INTRO, INTRO]);
    keep('error');
  });

  it('name: a first visit asks a name, then the empty chat', async () => {
    server.session = null;
    server.transcript = [];
    await mount();
    expect(container.querySelector('h2')!.textContent).toBe('Pump maintenance');
    expect(startButton().disabled).toBe(true);
    // The stream opens with the prompt, and its connect starts the session.
    expect(brief()).toEqual(MOUNT);
    await serve();
    expect(server.session!.participant_name).toBe('');
    await snapshot('name');

    await setValue(nameInput(), '   ');
    expect(startButton().disabled).toBe(true);
    await setValue(nameInput(), '  Pat Lee  ');
    expect(startButton().disabled).toBe(false);
    await click(startButton());
    expect(container.querySelector('header')!.textContent).toBe('Pump maintenanceInterviewing Pat LeeEnd interview');
    expect(count(HINT)).toBe(1);
    expect(sendButton().disabled).toBe(true);
    // Taking the name calls nothing.
    expect(brief()).toEqual(MOUNT);
    await snapshot('fresh');

    // The first answer carries the name, which the server fills in.
    await setValue(composer(), 'Hello!');
    await click(sendButton());
    expect(count(HINT)).toBe(0);
    expect(count('Hello!')).toBe(1);
    expect(count(THINKING)).toBe(1);
    expect(server.session!.participant_name).toBe('Pat Lee');

    expect(brief()).toEqual([...MOUNT, SEND('Hello!', 'Pat Lee')]);
    keep('name');
  });

  it('transcript: a resumed session, its replay, and unmount', async () => {
    server.transcript = TRANSCRIPT();
    await mount();
    expect(container.querySelector('header')!.textContent).toBe('Pump maintenanceInterviewing PatEnd interview');
    const page = container.innerHTML;
    // The connect replays the transcript, which adds nothing.
    await serve();
    expect(container.innerHTML).toBe(page);
    expect(count('The shift lead, in the paper log.')).toBe(1);
    // The interviewer's markdown renders; the participant's words do not.
    expect(container.querySelector('strong')!.textContent).toBe('coolant pumps');
    expect(container.querySelector('code')!.textContent).toBe('PUMP-7');
    expect(container.querySelector('a')!.getAttribute('target')).toBe('_blank');
    expect(count('Sometimes *twice* when the line runs hot. <b>Not bold.</b>')).toBe(1);
    expect(count(HINT)).toBe(0);
    expect(count(THINKING)).toBe(0);
    await snapshot('transcript');

    unmount();
    expect(FakeEventSource.all.map((es) => es.closed)).toEqual([true]);
    expect(brief()).toEqual([...MOUNT, CLOSE]);
    keep('transcript');
  });

  it('send: sending, typing, the partial reply, the reply, and the keys', async () => {
    await mount();
    await serve();
    expect(sendButton().disabled).toBe(true);
    // Spaces are no answer, whichever way they are sent.
    await setValue(composer(), '   ');
    expect(sendButton().disabled).toBe(true);
    await press(composer(), { key: 'Enter', ctrlKey: true });
    // Enter alone, or with Shift, is a new line.
    await setValue(composer(), 'We check them every Monday.');
    expect(sendButton().disabled).toBe(false);
    await press(composer(), { key: 'Enter' });
    await press(composer(), { key: 'Enter', shiftKey: true });
    expect(brief()).toEqual(MOUNT);

    const gate = deferred();
    server.hold = gate.promise;
    await click(sendButton());
    expect(sendLabel()).toBe('Sending…');
    expect(sendButton().disabled).toBe(true);
    expect(composer().value).toBe('We check them every Monday.');
    expect(count(THINKING)).toBe(1);
    // A second send while one is on its way does nothing.
    await press(composer(), { key: 'Enter', ctrlKey: true });
    await snapshot('sending');

    // The server broadcasts the answer before it answers the call.
    await emit('message', server.transcript[1]);
    expect(count('We check them every Monday.')).toBe(1);
    gate.release();
    server.hold = null;
    await flush();
    expect(sendLabel()).toBe('Send');
    expect(composer().value).toBe('');
    expect(count('We check them every Monday.')).toBe(1);
    expect(count(THINKING)).toBe(1);
    await snapshot('typing');

    await emit('assistant_partial', { run_id: 'run-1', text: 'Thanks, Pat. **Who' });
    expect(count(THINKING)).toBe(0);
    expect(partial()!.textContent).toBe('Thanks, Pat. **Who');
    await snapshot('partial');

    // Each event carries the whole text so far: it replaces, never appends.
    await emit('assistant_partial', { run_id: 'run-1', text: 'Thanks, Pat. **Who** signs the check' });
    expect(partial()!.textContent).toBe('Thanks, Pat. Who signs the check');
    const streaming = container.innerHTML;
    await emit('assistant_partial', 'not json');
    await emit('assistant_partial', { run_id: 'run-1', text: '' });
    await emit('assistant_partial', { run_id: 'run-1' });
    expect(container.innerHTML).toBe(streaming);

    // The whole reply replaces the partial, once however often it comes.
    const reply = await say('assistant', 'Thanks, Pat. **Who** signs the check off?');
    expect(partial()).toBeNull();
    await emit('message', reply);
    const replied = container.innerHTML;
    await emit('message', 'not json');
    await emit('message', { content: 'A message with no id.' });
    expect(container.innerHTML).toBe(replied);
    expect(count('Thanks, Pat. Who signs the check off?')).toBe(1);
    expect(count(THINKING)).toBe(0);
    await snapshot('reply');

    // Ctrl+Enter and Cmd+Enter send; the call's own answer shows the message.
    await setValue(composer(), 'The shift lead.');
    await press(composer(), { key: 'Enter', ctrlKey: true });
    expect(count('The shift lead.')).toBe(1);
    expect(composer().value).toBe('');
    await setValue(composer(), 'In the paper log.');
    await press(composer(), { key: 'Enter', metaKey: true });
    expect(count('In the paper log.')).toBe(1);
    expect(count(THINKING)).toBe(1);

    expect(brief()).toEqual([
      ...MOUNT,
      SEND('We check them every Monday.'),
      SEND('The shift lead.'),
      SEND('In the paper log.'),
    ]);
    keep('send');
  });

  it('send-error: the server’s words, the fallback, and a retry', async () => {
    await mount();
    await serve();
    await setValue(composer(), 'We check them every Monday.');

    server.refuse = httpError(429, RATE_LIMIT);
    await click(sendButton());
    expect(count(RATE_LIMIT)).toBe(1);
    expect(count(THINKING)).toBe(0);
    expect(composer().value).toBe('We check them every Monday.');
    expect(sendButton().disabled).toBe(false);
    await snapshot('send-error');

    // No words from the server: the page's own.
    server.refuse = new Error('Network Error');
    await click(sendButton());
    expect(count(RATE_LIMIT)).toBe(0);
    expect(count(FALLBACK)).toBe(1);

    // The next send clears it as it starts.
    const gate = deferred();
    server.hold = gate.promise;
    await click(sendButton());
    expect(count(FALLBACK)).toBe(0);
    gate.release();
    server.hold = null;
    await flush();
    expect(count('We check them every Monday.')).toBe(1);
    expect(count(THINKING)).toBe(1);

    expect(brief()).toEqual([
      ...MOUNT,
      SEND('We check them every Monday.'),
      SEND('We check them every Monday.'),
      SEND('We check them every Monday.'),
    ]);
    keep('send-error');
  });

  it('end: asked, cancelled, confirmed; finish failing ends it too', async () => {
    server.transcript = TRANSCRIPT();
    await mount();
    await serve();
    await click(endButton());
    expect(confirmation()!.textContent).toBe(
      'End interview' + 'End the interview? You will not be able to continue afterwards.' + 'Cancel' + 'End interview'
    );
    await snapshot('end-confirm');

    await click(byText(confirmation()!, 'button', 'Cancel'));
    expect(confirmation()).toBeNull();
    expect(container.querySelector('header')).not.toBeNull();
    expect(brief()).toEqual(MOUNT);

    await click(endButton());
    await click(byText(confirmation()!, 'button', 'End interview'));
    expect(confirmation()).toBeNull();
    expect(text()).toBe('🙏Thank you!Your feedback has been recorded. You can close this page now.');
    expect(FakeEventSource.all.map((es) => es.closed)).toEqual([true]);
    expect(server.session!.status).toBe('completed');
    expect(brief()).toEqual([...MOUNT, FINISH, CLOSE]);
    await snapshot('done');
    const done = container.innerHTML;

    // The link may already be closed: a refused finish ends the page too.
    server.session = SESSION();
    server.finishFails = true;
    await mount();
    await serve();
    await click(endButton());
    await click(byText(confirmation()!, 'button', 'End interview'));
    expect(container.innerHTML).toBe(done);
    expect(FakeEventSource.all.map((es) => es.closed)).toEqual([true, true]);

    expect(brief()).toEqual([...MOUNT, FINISH, CLOSE, ...MOUNT, FINISH, CLOSE]);
    keep('end');
  });

  it('ended: a session already completed, or finished', async () => {
    server.session = SESSION({ status: 'completed', ended_at: at(-60) });
    server.transcript = TRANSCRIPT();
    await mount();
    expect(text()).toBe('🙏Thank you!Your feedback has been recorded. You can close this page now.');
    expect(FakeEventSource.all).toHaveLength(0);
    await snapshot('ended');
    const page = container.innerHTML;

    server.session = SESSION({ status: 'finished', ended_at: at(-60) });
    await mount();
    expect(container.innerHTML).toBe(page);
    expect(FakeEventSource.all).toHaveLength(0);

    expect(brief()).toEqual([INTRO, INTRO]);
    keep('ended');
  });

  it('reconnect: the delays, a reopened stream, and a retry left pending', async () => {
    server.transcript = TRANSCRIPT();
    await mount();
    await serve();
    const page = container.innerHTML;

    // A drop closes the stream, shows nothing, and retries after 2 s.
    await emit('error');
    expect(FakeEventSource.all.filter((es) => !es.closed)).toHaveLength(0);
    expect(container.innerHTML).toBe(page);
    await fire();
    // A drop before it opens doubles the delay.
    await emit('error');
    await fire();
    // Its replay adds nothing.
    await serve();
    expect(container.innerHTML).toBe(page);
    // An open restarts the count: 2, 4, 8, then 15 s for ever.
    for (let i = 0; i < 5; i++) {
      await emit('error');
      await fire();
    }
    await serve();
    expect(container.innerHTML).toBe(page);
    await say('assistant', 'Sorry, I lost you for a moment. Where were we?');
    expect(count('Sorry, I lost you for a moment. Where were we?')).toBe(1);
    await snapshot('reconnect');

    // A retry pending at unmount opens nothing.
    await emit('error');
    unmount();
    await fire();
    expect(FakeEventSource.all.filter((es) => !es.closed)).toHaveLength(0);

    // Nor does one pending when the interview ends.
    await mount();
    await serve();
    await emit('error');
    await click(endButton());
    await click(byText(confirmation()!, 'button', 'End interview'));
    expect(text()).toBe('🙏Thank you!Your feedback has been recorded. You can close this page now.');
    await fire();
    expect(FakeEventSource.all.filter((es) => !es.closed)).toHaveLength(0);
    expect(timers).toHaveLength(0);

    // Each drop asks the intro again before its delay (#379 bug 216).
    expect(brief()).toEqual([
      ...MOUNT,
      CLOSE,
      INTRO,
      TIMER(2000),
      STREAM,
      CLOSE,
      INTRO,
      TIMER(4000),
      STREAM,
      CLOSE,
      INTRO,
      TIMER(2000),
      STREAM,
      CLOSE,
      INTRO,
      TIMER(4000),
      STREAM,
      CLOSE,
      INTRO,
      TIMER(8000),
      STREAM,
      CLOSE,
      INTRO,
      TIMER(15000),
      STREAM,
      CLOSE,
      INTRO,
      TIMER(15000),
      STREAM,
      CLOSE,
      INTRO,
      TIMER(2000),
      ...MOUNT,
      CLOSE,
      INTRO,
      TIMER(2000),
      FINISH,
    ]);
    keep('reconnect');
  });

  it('phone: the same page, the confirmation across the screen', async () => {
    server.transcript = TRANSCRIPT();
    await mount();
    await serve();
    const desktop = container.innerHTML;

    setViewport(390, true);
    await mount();
    await serve();
    expect(container.innerHTML).toBe(desktop);
    await click(endButton());
    expect(confirmation()!.style.width).toBe('100%');
    await snapshot('phone');
    await click(byText(confirmation()!, 'button', 'Cancel'));
    expect(confirmation()).toBeNull();

    expect(brief()).toEqual([...MOUNT, CLOSE, ...MOUNT]);
    keep('phone');
  });

  // Every call with its arguments, by mode. Each mode above files its calls
  // once it passes, so this one needs the whole file run, and passing, first.
  it('makes the calls of every mode', async () => {
    expect(Object.keys(log), 'the modes that passed: every one above must, in a run of the whole file').toEqual(MODES);
    await expect(JSON.stringify(log, null, 2) + '\n').toMatchFileSnapshot('./__snapshots__/InterviewChat.api.json');
  });
});
