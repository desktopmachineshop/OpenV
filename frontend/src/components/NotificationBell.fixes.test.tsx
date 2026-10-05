import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { NotificationBell } from './NotificationBell';
import { DialogProvider } from './ui';
import { notificationsAPI } from '../api/client';

// Four faults of the notification panel (#379, bugs 205-208), each against
// the real confirmation dialog, list answers the test releases in its own
// order, and a stream it drives by hand:
//
//   205  confirming or cancelling Clear all or Delete forever closed the
//        panel too: the dialog sits outside the bell, so the press on it
//        counted as a press outside.
//   206  Clear all counted the rows on screen (one page at most) beside the
//        server's unread total, and Delete forever counted the rows on
//        screen too, so a long history read "Clear all 30 notifications?
//        101 of them unread." and a single row "Clear all 1 notifications?".
//   207  a slow answer for a tab already left landed on the tab showing.
//   208  a frame for a row already listed was listed and counted again.

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    notificationsAPI: { streamUrl: () => 'http://localhost/api/v1/notifications/stream' },
  })
);

vi.mock('react-router-dom', () => ({
  useNavigate: () => () => {},
  useSearchParams: () => [new URLSearchParams(), () => {}],
}));

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ isPhone: false, isCompact: false }),
}));

// The browser's EventSource, driven by hand. As with the real one, an event
// reaches its addEventListener listeners.
class FakeEventSource {
  static all: FakeEventSource[] = [];
  listeners: Record<string, ((event: MessageEvent) => void)[]> = {};
  constructor(public url: string) {
    FakeEventSource.all.push(this);
  }
  addEventListener(type: string, fn: (event: MessageEvent) => void) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  }
  close() {}
  emit(type: string, data: string) {
    (this.listeners[type] || []).forEach((fn) => fn({ type, data } as MessageEvent));
  }
}
(globalThis as any).EventSource = FakeEventSource;
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const api = vi.mocked(notificationsAPI);

const notification = (id: string, over: Partial<any> = {}) => ({
  id,
  user_id: 'u-1',
  type: 'run_failed',
  title: `Notification ${id}`,
  body: 'something happened',
  read: false,
  flagged: false,
  created_at: new Date().toISOString(),
  data: {},
  entity_ref: {},
  ...over,
});

const inbox = [notification('n-1'), notification('n-2', { read: true })];
const flagged = [notification('n-9', { flagged: true })];
const cleared = [notification('n-7', { read: true, cleared_at: new Date().toISOString() })];
const page = (rows: unknown[], unread = 1, next_cursor?: string) => ({
  data: { notifications: rows, unread_count: unread, ...(next_cursor ? { next_cursor } : {}) },
});

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  FakeEventSource.all = [];
  api.list.mockImplementation((params?: any) => {
    const view = params?.view || 'inbox';
    return Promise.resolve(page(view === 'flagged' ? flagged : view === 'cleared' ? cleared : inbox)) as any;
  });
  api.clearAll.mockResolvedValue({ data: { cleared: 2, unread_count: 0 } } as any);
  api.deleteCleared.mockResolvedValue({ data: { deleted: 1, unread_count: 1 } } as any);
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

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const render = async () => {
  await act(async () => {
    root.render(
      <DialogProvider>
        <NotificationBell />
      </DialogProvider>
    );
  });
  await flush();
};

// A pointer press: mousedown, then mouseup and click, each its own task. The
// bell closes on a mousedown outside itself.
const press = async (node: Element) => {
  await act(async () => {
    node.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
  });
  await act(async () => {
    node.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
    node.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

const bell = () => container.querySelector<HTMLButtonElement>('button[title="Notifications"]')!;
const panel = () => container.querySelector<HTMLElement>('[role="dialog"][aria-label="Notifications"]');
const confirmation = () => container.querySelector<HTMLElement>('[role="alertdialog"]');
const button = (scope: Element, label: string) => {
  const found = Array.from(scope.querySelectorAll('button')).find((b) => (b.textContent || '').trim() === label);
  expect(found, `button "${label}"`).toBeTruthy();
  return found!;
};
const titles = () =>
  Array.from(panel()!.querySelectorAll('button[aria-pressed]')).map(
    (flag) => flag.parentElement!.previousElementSibling!.firstElementChild!.textContent
  );

describe('a press on the panel’s confirmation leaves the panel open (#379, bug 205)', () => {
  // The dialog's overlay covers the window: a press on it cancels.
  const overlay = () => confirmation()!.parentElement!;

  it.each([
    ['Clear all', 'Inbox', 'Cancel', ['Notification n-1', 'Notification n-2']],
    ['Clear all', 'Inbox', 'Clear all', []],
    ['Clear all', 'Inbox', 'the overlay', ['Notification n-1', 'Notification n-2']],
    ['Delete forever', 'Cleared', 'Cancel', ['Notification n-7']],
    ['Delete forever', 'Cleared', 'Delete forever', []],
    ['Delete forever', 'Cleared', 'the overlay', ['Notification n-7']],
  ])('%s on %s, answered with %s', async (action, tab, answer, rows) => {
    await render();
    await press(bell());
    if (tab !== 'Inbox') await press(button(panel()!, tab));
    await press(button(panel()!, action));
    expect(confirmation()).not.toBeNull();

    await press(answer === 'the overlay' ? overlay() : button(confirmation()!, answer));
    expect(confirmation()).toBeNull();
    expect(panel()).not.toBeNull();
    expect(titles()).toEqual(rows);

    // Once the dialog is gone, a press outside closes the panel as before.
    await press(document.body);
    expect(panel()).toBeNull();
  });
});

describe('the confirmations count what the action does (#379, bug 206)', () => {
  const longInbox = Array.from({ length: 30 }, (_, i) => notification(`u-${i + 1}`));

  it.each([
    ['a long inbox', longInbox, 101, 'Clear all notifications? 101 of them are unread.'],
    ['one unread row', [notification('n-1')], 1, 'Clear all notifications? 1 of them is unread.'],
    ['nothing unread', [notification('n-2', { read: true })], 0, 'Clear all notifications?'],
  ])('Clear all on %s', async (_name, rows, unread, question) => {
    api.list.mockResolvedValue(page(rows, unread, unread > 1 ? 'cursor' : undefined) as any);
    await render();
    await press(bell());
    await press(button(panel()!, 'Clear all'));
    expect(confirmation()!.textContent).toBe(
      'Clear notifications' +
        `${question} They move to the Cleared tab, where you can still read them.` +
        'CancelClear all'
    );
  });

  it('Delete forever', async () => {
    await render();
    await press(bell());
    await press(button(panel()!, 'Cleared'));
    await press(button(panel()!, 'Delete forever'));
    expect(confirmation()!.textContent).toBe(
      'Delete cleared notifications' +
        'Delete all cleared notifications forever? This cannot be undone.' +
        'CancelDelete forever'
    );
  });
});

describe('an answer for a tab no longer showing is dropped (#379, bug 207)', () => {
  // Each list call waits until the test answers it.
  let waiting: { params: any; answer: (body: unknown) => void }[];
  const answer = async (view: string, body: unknown, before?: string) => {
    const call = waiting.find((w) => (w.params?.view || 'inbox') === view && w.params?.before === before)!;
    expect(call, `a ${view} call waiting`).toBeTruthy();
    waiting = waiting.filter((w) => w !== call);
    call.answer(body);
    await flush();
  };

  beforeEach(() => {
    waiting = [];
    api.list.mockImplementation(
      (params?: any) => new Promise((resolve) => waiting.push({ params, answer: resolve })) as any
    );
  });

  const onCleared = async () => {
    await render();
    await answer('inbox', page(inbox));
    await press(bell());
    await answer('inbox', page(inbox));
  };

  it('a slow Flagged answer landing before the Cleared one', async () => {
    await onCleared();
    await press(button(panel()!, 'Flagged'));
    await press(button(panel()!, 'Cleared'));
    await answer('flagged', page(flagged));
    expect(titles()).toEqual([]);
    expect(panel()!.textContent).toContain('Loading…');

    await answer('cleared', page(cleared));
    expect(titles()).toEqual(['Notification n-7']);
  });

  it('a slow Flagged answer landing after the Cleared one', async () => {
    await onCleared();
    await press(button(panel()!, 'Flagged'));
    await press(button(panel()!, 'Cleared'));
    await answer('cleared', page(cleared));
    await answer('flagged', page(flagged));
    expect(titles()).toEqual(['Notification n-7']);
  });

  it('a page of older rows for a tab since left', async () => {
    await onCleared();
    await press(button(panel()!, 'Cleared'));
    await answer('cleared', page(cleared, 1, 'cursor-1'));
    await press(button(panel()!, 'Load older'));
    await press(button(panel()!, 'Inbox'));
    await answer('inbox', page(inbox));

    await answer('cleared', page([notification('older', { read: true })]), 'cursor-1');
    expect(titles()).toEqual(['Notification n-1', 'Notification n-2']);
  });
});

describe('a frame for a row already listed changes nothing (#379, bug 208)', () => {
  it('neither lists it again nor counts it again', async () => {
    const consoleError = vi.spyOn(console, 'error');
    await render();
    const emit = async (row: unknown) => {
      await act(async () => FakeEventSource.all[0].emit('notification', JSON.stringify(row)));
      await flush();
    };

    // With the panel closed: the list fetched on mount already holds n-1.
    await emit(notification('n-1'));
    expect(bell().getAttribute('aria-label')).toBe('Notifications (1 unread)');

    await press(bell());
    await emit(notification('n-1'));
    expect(titles()).toEqual(['Notification n-1', 'Notification n-2']);
    expect(bell().getAttribute('aria-label')).toBe('Notifications (1 unread)');

    // A new one is still listed on top, and counted.
    await emit(notification('n-3'));
    expect(titles()).toEqual(['Notification n-3', 'Notification n-1', 'Notification n-2']);
    expect(bell().getAttribute('aria-label')).toBe('Notifications (2 unread)');
    expect(consoleError.mock.calls.filter((c) => String(c[0]).includes('same key'))).toEqual([]);
    consoleError.mockRestore();
  });
});
