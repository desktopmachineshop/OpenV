import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { InterviewChat } from './InterviewChat';
import { publicInterviewAPI } from '../api/client';

// The public interview's dropped stream reconnects after a delay (2, 4, 8,
// 15 s ...). A reconnect still waiting when the page moves to another
// interview link, or unmounts, is cancelled: before #379 bug 203 the old
// timer fired anyway, closed the new interview's stream and reopened the old
// one, whose messages then landed in the new interview.

// The page reads its token from the route; the test moves it between renders.
const route = vi.hoisted(() => ({ token: 'tok-1' }));

vi.mock('react-router-dom', () => ({
  useParams: () => ({ token: route.token }),
}));

vi.mock('../components/ui', () => ({
  useConfirm: () => () => Promise.resolve(true),
}));

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    publicInterviewAPI: { streamUrl: (token: string) => `/public/interviews/${token}/stream` },
  })
);

const api = vi.mocked(publicInterviewAPI);

// An EventSource the test drives by hand. As with the real one, an event
// reaches both its addEventListener listeners and its on<event> property.
class MockEventSource {
  static all: MockEventSource[] = [];
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onopen: ((e: MessageEvent) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: MessageEvent) => void) | null = null;
  closed = false;

  constructor(
    public url: string,
    public init?: EventSourceInit
  ) {
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
  route.token = 'tok-1';
  api.intro.mockResolvedValue({
    data: {
      interview_name: 'Pump maintenance',
      transcript: [],
      session: { participant_name: 'Pat', status: 'active' },
    },
  } as any);
  // jsdom lays nothing out, so it has no scrollIntoView.
  (Element.prototype as any).scrollIntoView = vi.fn();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  mounted = true;
});

afterEach(() => {
  if (mounted) unmount();
  container.remove();
  vi.useRealTimers();
});

const render = async (token: string) => {
  route.token = token;
  await act(async () => {
    root.render(<InterviewChat />);
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
  if (!es) throw new Error('the page never opened a stream');
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

const ONE = '/public/interviews/tok-1/stream';
const TWO = '/public/interviews/tok-2/stream';

describe('InterviewChat reconnect across interview links (#379 bug 203)', () => {
  it('reconnects to the same interview after 2000 ms, and drops a pending reconnect when the token changes', async () => {
    await render('tok-1');
    expect(urls()).toEqual([ONE]);

    // A drop still reconnects to the same interview after 2000 ms, without credentials.
    drop();
    expect(stream().closed).toBe(true);
    await tick(1999);
    expect(urls()).toEqual([ONE]);
    await tick(1);
    expect(urls()).toEqual([ONE, ONE]);
    expect(stream().init).toEqual({ withCredentials: false });

    // It drops again, and the page moves to tok-2 while that retry waits.
    drop();
    await render('tok-2');
    expect(api.intro).toHaveBeenLastCalledWith('tok-2');
    expect(urls()).toEqual([ONE, ONE, TWO]);
    const current = stream();

    // The retry for tok-1 never fires: tok-1 is not reopened, tok-2 stays open.
    await tick(60000);
    expect(urls()).toEqual([ONE, ONE, TWO]);
    expect(current.closed).toBe(false);

    // And tok-2's own messages keep arriving.
    await act(async () => {
      current.emit('message', {
        id: 'm-2',
        session_id: 'session-2',
        role: 'assistant',
        content: 'A question in the second interview.',
        created_at: '2026-10-05T10:00:00Z',
      });
    });
    expect(container.textContent).toContain('A question in the second interview.');
  });

  it("does not show the previous interview's messages in the new one", async () => {
    await render('tok-1');
    drop();
    await render('tok-2');
    await tick(60000);

    // The server sends tok-1's messages on any tok-1 stream still open.
    await act(async () => {
      MockEventSource.all
        .filter((es) => es.url === ONE && !es.closed)
        .forEach((es) =>
          es.emit('message', {
            id: 'm-1',
            session_id: 'session-1',
            role: 'assistant',
            content: 'A question from the first interview.',
            created_at: '2026-10-05T10:00:00Z',
          })
        );
    });
    expect(container.textContent).not.toContain('A question from the first interview.');
    expect(stream().url).toBe(TWO);
    expect(stream().closed).toBe(false);
  });

  it('cancels a pending reconnect on unmount, and opens no stream after it', async () => {
    await render('tok-1');
    const before = vi.getTimerCount();
    drop();
    expect(vi.getTimerCount()).toBe(before + 1);

    unmount();
    expect(vi.getTimerCount()).toBe(before);
    await tick(60000);
    expect(urls()).toEqual([ONE]);
  });
});
