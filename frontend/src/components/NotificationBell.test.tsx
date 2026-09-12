import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { NotificationBell } from './NotificationBell';
import { notificationsAPI } from '../api/client';

// The notification panel: three views, the flag that survives a clear, the
// two bulk actions, and paging through the history.
jest.mock('../api/client', () => ({
  notificationsAPI: {
    list: jest.fn(),
    markRead: jest.fn(),
    markAllRead: jest.fn(),
    clearAll: jest.fn(),
    deleteCleared: jest.fn(),
    setFlagged: jest.fn(),
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
  addEventListener() {}
  close() {}
}
(globalThis as any).EventSource = FakeEventSource;
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const api = notificationsAPI as jest.Mocked<typeof notificationsAPI>;

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

// Answer each view from its own fixture, so switching tabs is visible in what
// the panel renders rather than only in the call arguments.
const inbox = [notification('n-1'), notification('n-2', { read: true })];
const flagged = [notification('n-9', { flagged: true, title: 'Kept one' })];
const cleared = [notification('n-7', { read: true, cleared_at: new Date().toISOString() })];

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  jest.clearAllMocks();
  mockConfirmAnswer = true;
  mockConfirmOptions = null;
  api.list.mockImplementation((params?: any) => {
    const view = params?.view || 'inbox';
    const rows = view === 'flagged' ? flagged : view === 'cleared' ? cleared : inbox;
    return Promise.resolve({ data: { notifications: rows, unread_count: 1 } }) as any;
  });
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

const openPanel = async () => {
  const bell = container.querySelector('button[aria-label^="Notifications"]') as HTMLButtonElement;
  await click(bell);
};

describe('the notification views', () => {
  it('switches between inbox, flagged and cleared', async () => {
    await render();
    await openPanel();
    expect(container.textContent).toContain('Notification n-1');

    await click(button('Flagged')!);
    expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ view: 'flagged' }));
    expect(container.textContent).toContain('Kept one');
    expect(container.textContent).not.toContain('Notification n-1');

    await click(button('Cleared')!);
    expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ view: 'cleared' }));
    expect(container.textContent).toContain('Notification n-7');
  });

  it('offers each bulk action only on the view it acts on', async () => {
    await render();
    await openPanel();
    // The inbox clears; it cannot delete.
    expect(button('Clear all')).toBeTruthy();
    expect(button('Delete forever')).toBeFalsy();

    await click(button('Cleared')!);
    // The history deletes; there is nothing there to clear.
    expect(button('Delete forever')).toBeTruthy();
    expect(button('Clear all')).toBeFalsy();
  });
});

describe('clearing the inbox', () => {
  it('asks, archives, and says where the notifications went', async () => {
    api.clearAll.mockResolvedValue({ data: { cleared: 2, unread_count: 0 } } as any);
    await render();
    await openPanel();
    await click(button('Clear all')!);

    // Archiving is not destructive, so the confirmation says where they go
    // rather than warning that they are lost.
    expect(mockConfirmOptions.message).toContain('Cleared tab');
    expect(mockConfirmOptions.message).not.toContain('cannot be undone');
    expect(mockConfirmOptions.danger).toBeUndefined();

    expect(api.clearAll).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain("You're all caught up.");
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

    expect(container.textContent).toContain('Notification n-1');
  });
});

describe('deleting the cleared history', () => {
  it('warns that this one cannot be undone', async () => {
    api.deleteCleared.mockResolvedValue({ data: { deleted: 1, unread_count: 0 } } as any);
    await render();
    await openPanel();
    await click(button('Cleared')!);
    await click(button('Delete forever')!);

    expect(mockConfirmOptions.danger).toBe(true);
    expect(mockConfirmOptions.message).toContain('cannot be undone');
    expect(api.deleteCleared).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain('Nothing cleared yet.');
  });
});

describe('flagging', () => {
  it('flags a row optimistically and puts it back if the server refuses', async () => {
    api.setFlagged.mockResolvedValue({ data: { flagged: true } } as any);
    await render();
    await openPanel();

    const flag = container.querySelector(
      'button[aria-label="Flag this notification"]'
    ) as HTMLButtonElement;
    expect(flag).toBeTruthy();
    await click(flag);

    expect(api.setFlagged).toHaveBeenCalledWith('n-1', true);
    expect(
      container.querySelector('button[aria-label="Remove flag"]')
    ).toBeTruthy();

    // A refusal returns the row to how it was.
    api.setFlagged.mockRejectedValue(new Error('nope'));
    const second = Array.from(
      container.querySelectorAll('button[aria-label="Flag this notification"]')
    )[0] as HTMLButtonElement;
    await click(second);
    expect(
      container.querySelectorAll('button[aria-label="Remove flag"]').length
    ).toBe(1);
  });
});

describe('paging the history', () => {
  it('offers Load older only while the server says there is more', async () => {
    api.list.mockImplementation((params?: any) =>
      Promise.resolve({
        data: {
          notifications: params?.before ? [notification('older')] : cleared,
          unread_count: 0,
          // The first page of the cleared view has more; the second does not.
          ...(params?.before ? {} : { next_cursor: '2026-09-12T10:00:00Z|n-7' }),
        },
      }) as any
    );
    await render();
    await openPanel();
    await click(button('Cleared')!);

    const more = button('Load older');
    expect(more).toBeTruthy();
    await click(more!);

    // The cursor went back verbatim, and the older rows were appended.
    expect(api.list).toHaveBeenLastCalledWith(
      expect.objectContaining({ view: 'cleared', before: '2026-09-12T10:00:00Z|n-7' })
    );
    expect(container.textContent).toContain('Notification older');
    expect(container.textContent).toContain('Notification n-7');
    // No cursor came back, so there is nothing left to load.
    expect(button('Load older')).toBeFalsy();
  });
});
