import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useLocation, useNavigationType } from 'react-router-dom';
import { NotificationBell } from './NotificationBell';
import { DialogProvider } from './ui';
import { useAppStore } from '../state/store';

// Refactor plan step S16k (invariant I21): the notification bell as it
// renders today, before X15e moves its EventSource onto useEventStream. X15e
// re-runs it first and must leave every file snapshot and the ordered API
// calls identical. The stream is driven only from outside, through a fake
// EventSource, and read only through the DOM.
//
//   closed        mounted with nothing unread: the bell alone.
//   badge         mounted with four unread: the count on the bell.
//   cap           99 unread shows 99, and a 100th from the stream 99+; with
//                 the panel open a new arrival keeps the list at one page
//                 (30 rows), and Clear all counts the unread on the server,
//                 not the rows on screen. Cancel leaves the panel open.
//   empty         opened while the list loads, then nothing to show, in each
//                 of the three tabs.
//   inbox         opened on the seven-row inbox: every kind of row (read and
//                 unread, flagged, with and without a body) and the relative
//                 times on their boundaries.
//   mark-one      one row's Mark read; then an unread row opened (read, panel
//                 closed, navigated) and a read one (no call).
//   mark-all      Mark all read.
//   stream        a notification arriving with the panel closed, then open on
//                 the inbox; a malformed frame and a stream error change
//                 nothing; one arriving on the Flagged tab only counts; the
//                 stream closes on unmount.
//   flagged       the Flagged tab, a flag removed there (the row goes), a flag
//                 the server refuses (put back) and one it takes, in the inbox.
//   cleared       the Cleared tab paged with Load older, Delete forever asked
//                 and confirmed with the panel left open, and the tab
//                 reopened empty.
//   clear-all     Clear all asked and confirmed with the panel left open, the
//                 inbox reopened empty and the rows found under Cleared.
//   phone         the panel at 390px with a coarse pointer (fixed, full width).
//   dark          the sidebar's variant, which opens upward.
//   deep-link     ?panel=notifications opens the panel on mount and is
//                 dropped with a replace; a press inside keeps it open, one
//                 outside closes it.
//
// A press is a mousedown, then a click, as a pointer makes them: the bell
// closes on a mousedown outside itself, and the confirmation dialog is
// outside it, yet a press on the dialog leaves the panel open (#379, bug 205).
//
// Each snapshot is container.innerHTML, one tag per line so a diff reads,
// written to __snapshots__/NotificationBell.<name>.html. Each mode asserts its
// ordered API calls, and the last test writes every mode's calls with their
// arguments to __snapshots__/NotificationBell.api.json. The store, the dialog
// provider and the router are real; only the API client (and the browser's
// EventSource) is replaced, by a small in-memory server.
//
// Regenerate only for a deliberate change to what the bell shows or loads,
// never in a refactor pull request:
//   npx vitest run src/components/NotificationBell.snapshot.test.tsx -u

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
// A time `seconds` after T, written as the server writes it (RFC 3339).
const at = (seconds: number) => new Date(Date.parse(T) + seconds * 1000).toISOString().replace('.000Z', 'Z');

type Row = Record<string, any>;

const note = (id: string, type: string, title: string, seconds: number, over: Row = {}): Row => ({
  id,
  org_id: 'o1',
  user_id: 'u1',
  type,
  title,
  entity_ref: {},
  read: false,
  flagged: false,
  created_at: at(seconds),
  ...over,
});

// The inbox, newest first: four unread (n-1, n-2, n-5, n-7), two flagged
// (n-2, n-6), one with no body (n-4). The ages sit on timeAgo's boundaries;
// n-1 is 30 s ahead of the browser's clock.
const INBOX = (): Row[] => [
  note('n-1', 'run_failed', 'Run failed: nightly trace check', 30, {
    body: 'The runner exited with status 1 after 4 minutes.',
    entity_ref: { kind: 'run', project_id: 'p1', run_id: 'r1' },
  }),
  note('n-2', 'proposal_pending', '3 proposals wait for your review', -59, {
    body: 'From the run "Requirements sweep" & its follow-up.',
    flagged: true,
    entity_ref: { kind: 'proposal', proposal_id: 'prop-7', project_id: 'p1', run_id: 'r1' },
  }),
  note('n-3', 'mention', 'Dana mentioned you on REQ-12', -60, {
    body: 'Can you check the fit criterion?',
    read: true,
    entity_ref: { kind: 'artifact', project_id: 'p1', artifact_id: 'a12', chatter_id: 'c1' },
  }),
  note('n-4', 'interview_completed', 'Interview completed: Shop lead', -3599, {
    read: true,
    entity_ref: { kind: 'interview', project_id: 'p1', session_id: 's1' },
  }),
  note('n-5', 'review_requested', 'Review requested: REQ-4', -3600, {
    body: 'Sam asked you to review a requirement.',
    entity_ref: { kind: 'artifact', project_id: 'p1', artifact_id: 'a4' },
  }),
  note('n-6', 'budget_threshold', 'Workspace at 80% of its token budget', -86399, {
    body: '80% of this month’s budget is spent.',
    read: true,
    flagged: true,
    entity_ref: { kind: 'org_usage', org_id: 'o1', threshold: 80 },
  }),
  note('n-7', 'release_published', 'OpenV 0.16.0 is out', -86400, {
    body: 'See what is new.',
    entity_ref: { kind: 'release', version: '0.16.0' },
  }),
];

// The history: cleared rows are read; c-1 is flagged, so the Flagged tab
// shows it too.
const CLEARED = (): Row[] => [
  note('c-1', 'run_failed', 'Run failed: weekly export', -2 * 86400, {
    body: 'The export timed out.',
    read: true,
    flagged: true,
    cleared_at: at(-3600),
    entity_ref: { kind: 'run', project_id: 'p1', run_id: 'r0' },
  }),
  note('c-2', 'mention', 'Lee mentioned you on DI-3', -3 * 86400, {
    body: 'Is this the right supplier?',
    read: true,
    cleared_at: at(-3600),
    entity_ref: { kind: 'artifact', project_id: 'p1', artifact_id: 'a3', chatter_id: 'c2' },
  }),
  note('c-3', 'access_changed', 'You are now an admin of the workspace', -9 * 86400, {
    read: true,
    cleared_at: at(-3600),
    entity_ref: { kind: 'membership', org_id: 'o1', user_id: 'u1' },
  }),
];

// The in-memory server, as internal/persistence/postgres/notification_repository.go
// and internal/api/notification_handlers.go answer: each view newest first,
// a full page carries a keyset cursor, the unread count is of the inbox
// only, clearing marks read, flags survive a clear. pageSize stands in for
// a smaller limit than the bell's 30, so a page of the history stays short.
let server: { rows: Row[]; pageSize: number; hold: Promise<void> | null; refuseFlag: string[] };

const inView = (view: string, r: Row) =>
  view === 'flagged' ? r.flagged : view === 'cleared' ? Boolean(r.cleared_at) : !r.cleared_at;
const newestFirst = (a: Row, b: Row) =>
  a.created_at === b.created_at ? (a.id < b.id ? 1 : -1) : a.created_at < b.created_at ? 1 : -1;
const unreadCount = () => server.rows.filter((r) => !r.read && !r.cleared_at).length;

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'notificationsAPI.list': (params?: { view?: string; limit?: number; before?: string }) => {
    let rows = server.rows.filter((r) => inView(params?.view || 'inbox', r)).sort(newestFirst);
    if (params?.before) {
      const [time, id] = params.before.split('|');
      rows = rows.filter((r) => r.created_at < time || (r.created_at === time && r.id < id));
    }
    const limit = Math.min(params?.limit ?? 50, server.pageSize);
    const page = rows.slice(0, limit);
    const last = page[page.length - 1];
    const body = {
      notifications: page.length > 0 ? clone(page) : null,
      unread_count: unreadCount(),
      ...(page.length === limit ? { next_cursor: `${last.created_at}|${last.id}` } : {}),
    };
    return server.hold ? server.hold.then(() => body) : body;
  },
  'notificationsAPI.markRead': (ids: string[]) => {
    const rows = server.rows.filter((r) => ids.includes(r.id) && !r.read);
    rows.forEach((r) => (r.read = true));
    return { updated: rows.length, unread_count: unreadCount() };
  },
  'notificationsAPI.markAllRead': () => {
    const rows = server.rows.filter((r) => !r.read);
    rows.forEach((r) => (r.read = true));
    return { updated: rows.length, unread_count: unreadCount() };
  },
  'notificationsAPI.clearAll': () => {
    const rows = server.rows.filter((r) => !r.cleared_at);
    rows.forEach((r) => Object.assign(r, { cleared_at: T, read: true }));
    return { cleared: rows.length, unread_count: unreadCount() };
  },
  'notificationsAPI.deleteCleared': () => {
    const before = server.rows.length;
    server.rows = server.rows.filter((r) => !r.cleared_at);
    return { deleted: before - server.rows.length, unread_count: unreadCount() };
  },
  'notificationsAPI.setFlagged': (id: string, flagged: boolean) => {
    if (server.refuseFlag.includes(id)) return Promise.reject(new Error('refused'));
    server.rows.find((r) => r.id === id)!.flagged = flagged;
    return { flagged };
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
  static open: FakeEventSource[] = [];
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
    FakeEventSource.open.push(this);
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

const initialStore = useAppStore.getState();
const COARSE = '(hover: none) and (pointer: coarse)';
const realInnerWidth = Object.getOwnPropertyDescriptor(window, 'innerWidth');
const realMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia');
const realEventSource = (globalThis as any).EventSource;

let container: HTMLDivElement;
let root: Root;
let mounted = false;
let location = '';
let navigation = '';

// Where the bell leaves the URL, and how it got there.
const LocationProbe: React.FC = () => {
  const here = useLocation();
  const type = useNavigationType();
  location = `${here.pathname}${here.search}`;
  navigation = type;
  return null;
};

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
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async (entry = '/projects', variant?: 'dark' | 'light') => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[entry]}>
        <DialogProvider>
          <NotificationBell variant={variant} />
        </DialogProvider>
        <LocationProbe />
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

// A pointer press: mousedown, then mouseup and click, each its own task.
const press = async (node: Element) => {
  await act(async () => {
    node.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
  });
  await act(async () => {
    node.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
    (node as HTMLElement).click();
  });
  await flush();
};

const bell = () => container.querySelector<HTMLButtonElement>('button[title="Notifications"]')!;
const badge = () => bell().querySelectorAll('span')[1]?.textContent ?? null;
const panel = () => container.querySelector<HTMLElement>('[role="dialog"][aria-label="Notifications"]');
const confirmation = () => container.querySelector<HTMLElement>('[role="alertdialog"]');

const byText = (scope: Element, selector: string, text: string): HTMLElement => {
  const found = Array.from(scope.querySelectorAll<HTMLElement>(selector)).find(
    (node) => (node.textContent ?? '').trim() === text
  );
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};
const has = (selector: string, text: string) =>
  Array.from(container.querySelectorAll(selector)).some((node) => (node.textContent ?? '').trim() === text);

// The panel's rows, in order: each is the element holding a flag button.
const rowElements = () =>
  Array.from(panel()?.children ?? []).filter((child) => child.querySelector('button[aria-pressed]'));
const titles = () => rowElements().map((row) => row.firstElementChild!.firstElementChild!.textContent);
const row = (title: string) => {
  const found = rowElements().find((r) => r.firstElementChild!.firstElementChild!.textContent === title);
  expect(found, `row "${title}"`).toBeTruthy();
  return found!;
};
// The words below the tabs when a view has no rows.
const emptyText = () => panel()!.children[2].textContent;

// The one open stream, and an event on it.
const stream = () => {
  const open = FakeEventSource.open.filter((es) => !es.closed);
  expect(open.length, 'open streams').toBe(1);
  return open[0];
};
const emit = async (type: string, data?: string) => {
  await act(async () => {
    stream().emit(type, data);
  });
  await flush();
};
// A notification the server stores and then sends on the open stream.
const arrive = async (n: Row) => {
  server.rows.push(n);
  await emit('notification', JSON.stringify(n));
};

const snapshot = (name: string) =>
  expect(container.innerHTML.replace(/></g, '>\n<') + '\n').toMatchFileSnapshot(
    `./__snapshots__/NotificationBell.${name}.html`
  );

// The call list in brief, as `ns.method(args)`; keep() files it under its
// mode for NotificationBell.api.json.
const brief = () =>
  recorder.calls.map(({ name, json }) => {
    const args = JSON.parse(json) as unknown[];
    return `${name}(${args.map((a) => JSON.stringify(a)).join(', ')})`;
  });
const log: Record<string, { call: string; args: unknown }[]> = {};
const keep = (mode: string) => {
  log[mode] = recorder.calls.map(({ name, json }) => ({ call: name, args: JSON.parse(json) }));
};

// The Flagged tab of the canned rows: flagged in the inbox, and in the history.
const FLAGGED = ['3 proposals wait for your review', 'Workspace at 80% of its token budget', 'Run failed: weekly export'];

const LIST = (view = 'inbox', before?: string) =>
  `notificationsAPI.list(${JSON.stringify({ view, limit: 30, ...(before ? { before } : {}) })})`;
const STREAM = 'EventSource("/api/v1/notifications/stream", {"withCredentials":true})';
// What every mount makes: the inbox for the badge, then the stream.
const MOUNT = [LIST(), STREAM];

beforeEach(() => {
  recorder.calls = [];
  server = { rows: [...INBOX(), ...CLEARED()], pageSize: Infinity, hold: null, refuseFlag: [] };
  FakeEventSource.open = [];
  (globalThis as any).EventSource = FakeEventSource;
  location = '';
  navigation = '';
  // timeAgo reads Date.now(); timers stay real.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(T));
  setViewport(1280, false);
  useAppStore.setState(
    {
      ...initialStore,
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-runs': true } },
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
  if (mounted) unmount();
  container.remove();
  useAppStore.setState(initialStore, true);
  vi.useRealTimers();
  (globalThis as any).EventSource = realEventSource;
  for (const [key, real] of [
    ['innerWidth', realInnerWidth],
    ['matchMedia', realMatchMedia],
  ] as const) {
    if (real) Object.defineProperty(window, key, real);
    else delete (window as any)[key];
  }
});

const MODES = [
  'closed',
  'badge',
  'cap',
  'empty',
  'inbox',
  'mark-one',
  'mark-all',
  'stream',
  'flagged',
  'cleared',
  'clear-all',
  'phone',
  'dark',
  'deep-link',
];

describe('NotificationBell characterization (S16k)', () => {
  it('closed: nothing unread', async () => {
    server.rows = INBOX().filter((r) => r.read);
    await mount();
    expect(bell().getAttribute('aria-label')).toBe('Notifications');
    expect(badge()).toBeNull();
    expect(panel()).toBeNull();
    expect(brief()).toEqual(MOUNT);
    await snapshot('closed');
    keep('closed');
  });

  it('badge: four unread, closed', async () => {
    await mount();
    expect(bell().getAttribute('aria-label')).toBe('Notifications (4 unread)');
    expect(badge()).toBe('4');
    expect(panel()).toBeNull();
    expect(brief()).toEqual(MOUNT);
    await snapshot('badge');
    keep('badge');
  });

  it('cap: 99, then 99+, a page kept at 30 rows, and Clear all cancelled', async () => {
    server.rows = Array.from({ length: 99 }, (_, i) => note(`u-${i + 1}`, 'mention', `Mention ${i + 1}`, -60 * (i + 1)));
    await mount();
    expect(badge()).toBe('99');
    expect(bell().getAttribute('aria-label')).toBe('Notifications (99 unread)');
    await snapshot('badge-99');

    await arrive(note('u-100', 'mention', 'Mention 100', 0));
    expect(badge()).toBe('99+');
    expect(bell().getAttribute('aria-label')).toBe('Notifications (100 unread)');
    await snapshot('badge-99plus');

    // A full page: the newest 30, and the cursor offers more.
    await press(bell());
    expect(titles()).toHaveLength(30);
    expect(titles()[0]).toBe('Mention 100');
    expect(titles()[29]).toBe('Mention 29');
    expect(has('button', 'Load older')).toBe(true);
    // One more arrives on top, and the last row drops off the page.
    await arrive(note('u-101', 'mention', 'Mention 101', 0));
    expect(titles()).toHaveLength(30);
    expect(titles().slice(0, 2)).toEqual(['Mention 101', 'Mention 100']);
    expect(titles()[29]).toBe('Mention 28');
    expect(bell().getAttribute('aria-label')).toBe('Notifications (101 unread)');

    // Clear all counts the unread on the server, not the rows on screen
    // (#379, bug 206).
    await press(byText(panel()!, 'button', 'Clear all'));
    expect(confirmation()!.textContent).toBe(
      'Clear notifications' +
        'Clear all notifications? 101 of them are unread. They move to the Cleared tab, where you can still read them.' +
        'CancelClear all'
    );
    // The press on Cancel lands outside the bell, on its dialog, so the panel
    // stays open (#379, bug 205).
    await press(byText(confirmation()!, 'button', 'Cancel'));
    expect(confirmation()).toBeNull();
    expect(panel()).not.toBeNull();
    expect(badge()).toBe('99+');

    expect(brief()).toEqual([...MOUNT, LIST()]);
    keep('cap');
  });

  it('empty: loading, then nothing in any tab', async () => {
    server.rows = [];
    await mount();
    expect(badge()).toBeNull();
    const gate = deferred();
    server.hold = gate.promise;
    await press(bell());
    expect(emptyText()).toBe('Loading…');
    await snapshot('loading');

    gate.release();
    server.hold = null;
    await flush();
    expect(emptyText()).toBe("You're all caught up.");
    expect(has('button', 'Mark all read')).toBe(false);
    expect(has('button', 'Clear all')).toBe(false);
    await snapshot('empty');

    await press(byText(panel()!, 'button', 'Flagged'));
    expect(emptyText()).toBe('Nothing flagged. Flag a notification to keep it here.');
    await press(byText(panel()!, 'button', 'Cleared'));
    expect(emptyText()).toBe('Nothing cleared yet.');
    expect(has('button', 'Delete forever')).toBe(false);

    expect(brief()).toEqual([...MOUNT, LIST(), LIST('flagged'), LIST('cleared')]);
    keep('empty');
  });

  it('inbox: every kind of row', async () => {
    await mount();
    await press(bell());
    expect(titles()).toEqual(INBOX().map((r) => r.title));
    expect(rowElements().map((r) => r.lastElementChild!.firstElementChild!.textContent)).toEqual([
      'now',
      'now',
      '1m',
      '59m',
      '1h',
      '23h',
      '1d',
    ]);
    expect(
      Array.from(panel()!.querySelectorAll('[role="tab"]')).map((t) => `${t.textContent}:${t.getAttribute('aria-selected')}`)
    ).toEqual(['Inbox:true', 'Flagged:false', 'Cleared:false']);
    expect(has('button', 'Load older')).toBe(false);
    await snapshot('inbox');

    // The tab already showing loads nothing.
    await press(byText(panel()!, 'button', 'Inbox'));
    expect(brief()).toEqual([...MOUNT, LIST()]);
    keep('inbox');
  });

  it('mark-one: Mark read, then an unread and a read row opened', async () => {
    await mount();
    await press(bell());
    // The row React patches to read keeps the background longhands jsdom
    // expands it into (innerHTML carries the DOM's history).
    await press(row('3 proposals wait for your review').querySelector('button[title="Mark read"]')!);
    expect(badge()).toBe('3');
    expect(row('3 proposals wait for your review').querySelector('button[title="Mark read"]')).toBeNull();
    await snapshot('mark-one');

    // Opening an unread row marks it read, closes the panel and navigates.
    await press(row('Review requested: REQ-4').firstElementChild!.firstElementChild!);
    expect(panel()).toBeNull();
    expect(badge()).toBe('2');
    expect([location, navigation]).toEqual(['/projects/p1/requirements', 'PUSH']);

    // A read row only navigates.
    await press(bell());
    await press(row('Interview completed: Shop lead').firstElementChild!.firstElementChild!);
    expect(panel()).toBeNull();
    expect([location, navigation]).toEqual(['/projects/p1/interviews', 'PUSH']);

    expect(brief()).toEqual([
      ...MOUNT,
      LIST(),
      'notificationsAPI.markRead(["n-2"])',
      'notificationsAPI.markRead(["n-5"])',
      LIST(),
    ]);
    keep('mark-one');
  });

  it('mark-all: Mark all read', async () => {
    await mount();
    await press(bell());
    await press(byText(panel()!, 'button', 'Mark all read'));
    expect(badge()).toBeNull();
    expect(bell().getAttribute('aria-label')).toBe('Notifications');
    expect(has('button', 'Mark all read')).toBe(false);
    expect(has('button', 'Mark read')).toBe(false);
    expect(has('button', 'Clear all')).toBe(true);
    await snapshot('mark-all');
    expect(brief()).toEqual([...MOUNT, LIST(), 'notificationsAPI.markAllRead()']);
    keep('mark-all');
  });

  it('stream: arrivals closed, open, on another tab; noise; unmount', async () => {
    // Arrivals carry the server's clock, which runs ahead of the browser's
    // (n-1), so each is the newest row there too.
    await mount();
    await arrive(
      note('n-8', 'mention', 'Kim mentioned you on REQ-7', 40, {
        body: 'Does this cover the night shift?',
        entity_ref: { kind: 'artifact', project_id: 'p1', artifact_id: 'a7', chatter_id: 'c8' },
      })
    );
    expect(badge()).toBe('5');
    expect(panel()).toBeNull();
    await snapshot('stream-closed');

    await press(bell());
    expect(titles()[0]).toBe('Kim mentioned you on REQ-7');
    await arrive(
      note('n-9', 'run_failed', 'Run failed: hazard review', 50, {
        body: 'No runner picked the run up.',
        entity_ref: { kind: 'run', project_id: '', run_id: 'r9' },
      })
    );
    expect(badge()).toBe('6');
    expect(titles().slice(0, 3)).toEqual([
      'Run failed: hazard review',
      'Kim mentioned you on REQ-7',
      'Run failed: nightly trace check',
    ]);
    await snapshot('stream-open');

    // A frame that is not JSON, and a dropped connection, change nothing.
    const before = container.innerHTML;
    await emit('notification', 'not json');
    await emit('error');
    expect(container.innerHTML).toBe(before);
    expect(FakeEventSource.open).toHaveLength(1);

    // On the Flagged tab an arrival counts but is not listed.
    await press(byText(panel()!, 'button', 'Flagged'));
    await arrive(note('n-10', 'mention', 'Ana mentioned you on TC-2', 60));
    expect(badge()).toBe('7');
    expect(titles()).toEqual(FLAGGED);
    await press(byText(panel()!, 'button', 'Inbox'));
    expect(titles()[0]).toBe('Ana mentioned you on TC-2');

    unmount();
    expect(FakeEventSource.open.map((es) => es.closed)).toEqual([true]);
    expect(brief()).toEqual([
      ...MOUNT,
      LIST(),
      LIST('flagged'),
      LIST(),
      'EventSource.close("/api/v1/notifications/stream")',
    ]);
    keep('stream');
  });

  it('flagged: the tab, a flag removed there, one refused and one taken', async () => {
    server.refuseFlag = ['n-3'];
    await mount();
    await press(bell());
    await press(byText(panel()!, 'button', 'Flagged'));
    expect(titles()).toEqual(FLAGGED);
    // Neither bulk action belongs to this tab.
    expect(has('button', 'Mark all read')).toBe(false);
    expect(has('button', 'Clear all')).toBe(false);
    expect(has('button', 'Delete forever')).toBe(false);
    await snapshot('flagged');

    await press(row('Workspace at 80% of its token budget').querySelector('button[aria-label="Remove flag"]')!);
    expect(titles()).toEqual(['3 proposals wait for your review', 'Run failed: weekly export']);

    await press(byText(panel()!, 'button', 'Inbox'));
    const flag = (title: string) => row(title).querySelector('button[aria-pressed]')!;
    expect(flag('Workspace at 80% of its token budget').getAttribute('aria-pressed')).toBe('false');
    await press(flag('Dana mentioned you on REQ-12'));
    expect(flag('Dana mentioned you on REQ-12').getAttribute('aria-pressed')).toBe('false');
    await press(flag('Run failed: nightly trace check'));
    expect(flag('Run failed: nightly trace check').getAttribute('aria-pressed')).toBe('true');
    expect(flag('Run failed: nightly trace check').getAttribute('aria-label')).toBe('Remove flag');

    expect(brief()).toEqual([
      ...MOUNT,
      LIST(),
      LIST('flagged'),
      'notificationsAPI.setFlagged("n-6", false)',
      LIST(),
      'notificationsAPI.setFlagged("n-3", true)',
      'notificationsAPI.setFlagged("n-1", true)',
    ]);
    keep('flagged');
  });

  it('cleared: paged, then Delete forever asked and confirmed', async () => {
    await mount();
    await press(bell());
    server.pageSize = 2;
    await press(byText(panel()!, 'button', 'Cleared'));
    expect(titles()).toEqual(['Run failed: weekly export', 'Lee mentioned you on DI-3']);
    expect(has('button', 'Mark all read')).toBe(false);
    expect(has('button', 'Clear all')).toBe(false);
    await snapshot('cleared');

    await press(byText(panel()!, 'button', 'Load older'));
    expect(titles()).toEqual(['Run failed: weekly export', 'Lee mentioned you on DI-3', 'You are now an admin of the workspace']);
    expect(has('button', 'Load older')).toBe(false);

    await press(byText(panel()!, 'button', 'Delete forever'));
    expect(confirmation()!.textContent).toBe(
      'Delete cleared notifications' +
        'Delete all cleared notifications forever? This cannot be undone.' +
        'CancelDelete forever'
    );
    await snapshot('delete-confirm');

    // The press on the dialog leaves the panel open behind it (#379, bug 205).
    await press(byText(confirmation()!, 'button', 'Delete forever'));
    expect(confirmation()).toBeNull();
    expect(panel()).not.toBeNull();
    expect(badge()).toBe('4');

    // Closed by a press outside and reopened, the panel is still on the
    // Cleared tab.
    await press(document.body);
    expect(panel()).toBeNull();
    await press(bell());
    expect(panel()!.querySelector('[aria-selected="true"]')!.textContent).toBe('Cleared');
    expect(emptyText()).toBe('Nothing cleared yet.');

    expect(brief()).toEqual([
      ...MOUNT,
      LIST(),
      LIST('cleared'),
      LIST('cleared', `${at(-3 * 86400)}|c-2`),
      'notificationsAPI.deleteCleared()',
      LIST('cleared'),
    ]);
    keep('cleared');
  });

  it('clear-all: asked, confirmed, and the rows under Cleared', async () => {
    await mount();
    await press(bell());
    await press(byText(panel()!, 'button', 'Clear all'));
    expect(confirmation()!.textContent).toBe(
      'Clear notifications' +
        'Clear all notifications? 4 of them are unread. They move to the Cleared tab, where you can still read them.' +
        'CancelClear all'
    );
    await snapshot('clear-confirm');

    // The press on the dialog leaves the panel open behind it (#379, bug 205).
    await press(byText(confirmation()!, 'button', 'Clear all'));
    expect(confirmation()).toBeNull();
    expect(panel()).not.toBeNull();
    expect(badge()).toBeNull();

    // Closed by a press outside, and reopened.
    await press(document.body);
    expect(panel()).toBeNull();
    await press(bell());
    expect(emptyText()).toBe("You're all caught up.");
    await press(byText(panel()!, 'button', 'Cleared'));
    expect(titles()).toEqual([...INBOX(), ...CLEARED()].map((r) => r.title));

    expect(brief()).toEqual([...MOUNT, LIST(), 'notificationsAPI.clearAll()', LIST(), LIST('cleared')]);
    keep('clear-all');
  });

  it('phone: the panel across the screen', async () => {
    setViewport(390, true);
    await mount();
    await press(bell());
    expect(panel()!.style.position).toBe('fixed');
    await snapshot('phone');
    expect(brief()).toEqual([...MOUNT, LIST()]);
    keep('phone');
  });

  it('dark: the sidebar variant opens upward', async () => {
    await mount('/projects/p1/requirements', 'dark');
    await press(bell());
    expect(panel()!.style.bottom).toBe('calc(100% + 6px)');
    await snapshot('dark');
    expect(brief()).toEqual([...MOUNT, LIST()]);
    keep('dark');
  });

  it('deep-link: ?panel=notifications opens it once', async () => {
    await mount('/projects/p1/requirements?artifact=a12&panel=notifications');
    expect(panel()).not.toBeNull();
    expect([location, navigation]).toEqual(['/projects/p1/requirements?artifact=a12', 'REPLACE']);
    await snapshot('deep-link');

    await press(byText(panel()!, 'span', 'Notifications'));
    expect(panel()).not.toBeNull();
    await press(document.body);
    expect(panel()).toBeNull();
    await press(bell());
    expect(panel()).not.toBeNull();

    expect(brief()).toEqual([...MOUNT, LIST()]);
    keep('deep-link');
  });

  // Every call with its arguments, by mode. Each mode above files its calls
  // once it passes, so this one needs the whole file run, and passing, first.
  it('makes the calls of every mode', async () => {
    expect(Object.keys(log), 'the modes that passed: every one above must, in a run of the whole file').toEqual(MODES);
    await expect(JSON.stringify(log, null, 2) + '\n').toMatchFileSnapshot('./__snapshots__/NotificationBell.api.json');
  });
});
