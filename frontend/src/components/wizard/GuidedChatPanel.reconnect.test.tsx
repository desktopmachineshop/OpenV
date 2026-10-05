import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { GuidedChatPanel } from './GuidedChatPanel';
import { guidedAPI } from '../../api/client';

// A dropped assistant stream reconnects after a delay (2, 4, 8, 15 s ...).
// A reconnect still waiting when the panel moves to another session, or
// unmounts, is cancelled: before #379 bug 203 the old timer fired anyway,
// closed the new session's stream and reopened the old one, whose messages
// then landed in the new session's panel.

vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    guidedAPI: { chatStreamUrl: (id: string) => `/stream/${id}` },
  })
);

const api = vi.mocked(guidedAPI);

// An EventSource the test drives by hand. As with the real one, an event
// reaches both its addEventListener listeners and its on<event> property.
class MockEventSource {
  static all: MockEventSource[] = [];
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onopen: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: MessageEvent) => void) | null = null;
  closed = false;

  constructor(public url: string) {
    MockEventSource.all.push(this);
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  }
  close() {
    this.closed = true;
  }
  emit(type: string, data?: unknown) {
    const event = { type, data: data === undefined ? undefined : JSON.stringify(data) } as MessageEvent;
    const handler = (this as any)[`on${type}`];
    if (typeof handler === 'function') handler(event);
    (this.listeners[type] || []).forEach((fn) => fn(event));
  }
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = MockEventSource as any;

let container: HTMLDivElement;
let root: Root;
let mounted = false;

beforeEach(() => {
  vi.clearAllMocks();
  vi.useFakeTimers();
  MockEventSource.all = [];
  api.listMessages.mockResolvedValue({ data: [] } as any);
  api.kickoffChat.mockResolvedValue({ data: { status: 'launched', runner_online: true } } as any);
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  mounted = true;
  // jsdom has no scrollTo on elements.
  (Element.prototype as any).scrollTo = vi.fn();
});

afterEach(() => {
  if (mounted) unmount();
  container.remove();
  vi.useRealTimers();
});

const render = async (sessionId: string) => {
  await act(async () => {
    root.render(<GuidedChatPanel sessionId={sessionId} step={2} />);
  });
};

const unmount = () => {
  act(() => {
    root.unmount();
  });
  mounted = false;
};

const urls = () => MockEventSource.all.map((es) => es.url);

const stream = (): MockEventSource => {
  const es = MockEventSource.all[MockEventSource.all.length - 1];
  if (!es) throw new Error('the panel never opened a stream');
  return es;
};

/** The stream drops: EventSource fires `error`. */
const drop = () => {
  act(() => {
    stream().emit('error');
  });
};

const tick = async (ms: number) => {
  await act(async () => {
    vi.advanceTimersByTime(ms);
  });
};

describe('GuidedChatPanel reconnect across sessions (#379 bug 203)', () => {
  it("reconnects to the same session after 2000 ms, and drops a pending reconnect when the session changes", async () => {
    await render('gs-1');
    expect(urls()).toEqual(['/stream/gs-1']);

    // A drop still reconnects to the same session after 2000 ms.
    drop();
    expect(stream().closed).toBe(true);
    await tick(1999);
    expect(urls()).toEqual(['/stream/gs-1']);
    await tick(1);
    expect(urls()).toEqual(['/stream/gs-1', '/stream/gs-1']);

    // It drops again, and the panel moves to gs-2 while that retry waits.
    drop();
    await render('gs-2');
    expect(urls()).toEqual(['/stream/gs-1', '/stream/gs-1', '/stream/gs-2']);
    const current = stream();

    // The retry for gs-1 never fires: gs-1 is not reopened, gs-2 stays open.
    await tick(60000);
    expect(urls()).toEqual(['/stream/gs-1', '/stream/gs-1', '/stream/gs-2']);
    expect(current.closed).toBe(false);

    // And gs-2's own messages keep arriving.
    await act(async () => {
      current.emit('message', {
        id: 'm-2',
        session_id: 'gs-2',
        role: 'assistant',
        content: 'A reply in the second session.',
        created_at: '2026-10-05T10:00:00Z',
      });
    });
    expect(container.textContent).toContain('A reply in the second session.');
  });

  it("does not show the previous session's messages in the new one", async () => {
    await render('gs-1');
    drop();
    await render('gs-2');
    await tick(60000);

    // The server sends gs-1's messages on any gs-1 stream still open.
    await act(async () => {
      MockEventSource.all
        .filter((es) => es.url === '/stream/gs-1' && !es.closed)
        .forEach((es) =>
          es.emit('message', {
            id: 'm-1',
            session_id: 'gs-1',
            role: 'assistant',
            content: 'A reply from the first session.',
            created_at: '2026-10-05T10:00:00Z',
          })
        );
    });
    expect(container.textContent).not.toContain('A reply from the first session.');
    expect(stream().url).toBe('/stream/gs-2');
    expect(stream().closed).toBe(false);
  });

  it('cancels a pending reconnect on unmount, and opens no stream after it', async () => {
    await render('gs-1');
    const before = vi.getTimerCount();
    drop();
    expect(vi.getTimerCount()).toBe(before + 1);

    unmount();
    expect(vi.getTimerCount()).toBe(before);
    await tick(60000);
    expect(urls()).toEqual(['/stream/gs-1']);
  });
});
