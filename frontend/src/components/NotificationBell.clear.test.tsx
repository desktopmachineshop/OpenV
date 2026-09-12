import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { NotificationBell } from './NotificationBell';
import { notificationsAPI } from '../api/client';

// Clearing the notification list. The confirmation is the part under test:
// the call deletes read and unread alike and cannot be undone, so a stray
// click must not reach the server.
jest.mock('../api/client', () => ({
  notificationsAPI: {
    list: jest.fn(),
    markRead: jest.fn(),
    markAllRead: jest.fn(),
    clearAll: jest.fn(),
    streamUrl: () => 'http://localhost/api/v1/notifications/stream',
  },
}));

jest.mock('react-router-dom', () => ({
  useNavigate: () => jest.fn(),
  useSearchParams: () => [new URLSearchParams(), () => {}],
}));

jest.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ isPhone: false, isCompact: false }),
}));

// The dialog provider needs a host; the confirm answer is what each test sets.
// jest.mock factories are hoisted above the file, so anything they close over
// has to be mock-prefixed for Jest to allow the access.
let mockConfirmAnswer = true;
let mockConfirmOptions: any = null;
jest.mock('./ui', () => ({
  useConfirm: () => (opts: any) => {
    mockConfirmOptions = opts;
    return Promise.resolve(mockConfirmAnswer);
  },
}));

// EventSource is not in jsdom, and the bell opens one on mount.
class FakeEventSource {
  onmessage: ((e: MessageEvent) => void) | null = null;
  addEventListener() {}
  close() {}
}
(globalThis as any).EventSource = FakeEventSource;
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const api = notificationsAPI as jest.Mocked<typeof notificationsAPI>;

const notification = (id: string, read: boolean) => ({
  id,
  user_id: 'u-1',
  type: 'run_failed',
  title: `Notification ${id}`,
  body: 'something happened',
  read,
  created_at: new Date().toISOString(),
  data: {},
});

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  jest.clearAllMocks();
  mockConfirmAnswer = true;
  mockConfirmOptions = null;
  api.list.mockResolvedValue({
    data: { notifications: [notification('n-1', false), notification('n-2', true)], unread_count: 1 },
  } as any);
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
    await Promise.resolve();
  });
};

const render = async () => {
  await act(async () => {
    root.render(<NotificationBell />);
  });
  await flush();
};

const button = (label: string): HTMLButtonElement | undefined =>
  Array.from(container.querySelectorAll('button')).find(
    (b) => (b.textContent || '').trim() === label
  ) as HTMLButtonElement | undefined;

const click = async (el: HTMLElement) => {
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

// Open the panel by clicking the bell itself.
const openPanel = async () => {
  const bell = container.querySelector('button[aria-label^="Notifications"]') as HTMLButtonElement;
  await click(bell);
};

describe('clearing the notification list', () => {
  it('asks first, then empties the list and zeroes the badge', async () => {
    api.clearAll.mockResolvedValue({ data: { deleted: 2, unread_count: 0 } } as any);
    await render();
    await openPanel();

    const clear = button('Clear all');
    expect(clear).toBeTruthy();
    await click(clear!);

    // The member was warned, and told what they were about to lose.
    expect(mockConfirmOptions).toBeTruthy();
    expect(mockConfirmOptions.danger).toBe(true);
    expect(mockConfirmOptions.message).toContain('2');
    expect(mockConfirmOptions.message).toContain('1 of them unread');
    expect(mockConfirmOptions.message).toContain('cannot be undone');

    expect(api.clearAll).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain("You're all caught up.");
    // With nothing left there is nothing to clear, so the action goes away.
    expect(button('Clear all')).toBeFalsy();
  });

  it('does nothing at all when the confirmation is declined', async () => {
    mockConfirmAnswer = false;
    await render();
    await openPanel();
    await click(button('Clear all')!);

    expect(api.clearAll).not.toHaveBeenCalled();
    expect(container.textContent).toContain('Notification n-1');
  });

  it('keeps the list on screen when the server refuses', async () => {
    api.clearAll.mockRejectedValue(new Error('nope'));
    await render();
    await openPanel();
    await click(button('Clear all')!);

    expect(api.clearAll).toHaveBeenCalledTimes(1);
    // Not emptied optimistically: what is shown still matches the server.
    expect(container.textContent).toContain('Notification n-1');
  });
});
