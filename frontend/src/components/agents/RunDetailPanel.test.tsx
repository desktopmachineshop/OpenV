import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { RunDetailPanel } from './RunDetailPanel';
import { agentRunsAPI } from '../../api/client';

// The run detail panel's live tail (refactor plan S6, invariant I9; the guard
// X15b re-runs when it moves this stream into hooks/useEventStream): the
// panel opens the run's stream from the last seq it has, and reads the three
// events SSEHub sends on a run's stream, in the shapes it sends them
// (internal/api/sse.go): `log` carries one log entry, `partial` {run_id,
// text} and `status` {run_id, status}. Below, the reconnect policy the same
// move turns into a hook parameter (plan Q21).

vi.mock('../../api/client', () => ({
  agentRunsAPI: {
    get: vi.fn(),
    tree: vi.fn(),
    logs: vi.fn(),
    cancel: vi.fn(),
    retry: vi.fn(),
    streamUrl: (id: string, afterSeq = 0) => `/stream/${id}?after_seq=${afterSeq}`,
  },
}));

const api = vi.mocked(agentRunsAPI);

// A minimal EventSource the test drives by hand. Like the real one, an event
// reaches both its addEventListener listeners and its on<event> property, so
// the test does not fix which of the two the panel (or X15b's hook) uses.
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
  /** Whether anything receives an event of this type. */
  handles(type: string) {
    return typeof (this as any)[`on${type}`] === 'function' || (this.listeners[type] || []).length > 0;
  }
}

/**
 * EventSource's connection events: the panel handles `error` (a drop, or the
 * server's own error event), and a hook may also handle `open`; either may be
 * attached as a listener or a property, so neither counts among the server
 * events the stream is opened for.
 */
const CONNECTION_EVENTS = ['open', 'error'];
const serverListeners = (es: MockEventSource) =>
  Object.keys(es.listeners)
    .filter((type) => !CONNECTION_EVENTS.includes(type))
    .sort();

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = MockEventSource as any;

const RUN = {
  id: 'run-1',
  agent_id: 'agent-1',
  agent_name: 'Analyst',
  status: 'running',
  prompt: 'Summarise the open requirements',
  tokens_in: 0,
  tokens_out: 0,
};

const logEntry = (seq: number, text: string) => ({
  run_id: 'run-1',
  seq,
  kind: 'text',
  payload: { text },
  created_at: '2026-09-26T10:00:00Z',
});

let container: HTMLDivElement;
let root: Root;
let mounted = false;

beforeEach(() => {
  vi.clearAllMocks();
  MockEventSource.all = [];
  api.get.mockResolvedValue({ data: { ...RUN } } as any);
  api.tree.mockResolvedValue({ data: [] } as any);
  api.logs.mockResolvedValue({ data: [] } as any);
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

const unmount = () => {
  act(() => {
    root.unmount();
  });
  mounted = false;
};

const render = async () => {
  await act(async () => {
    root.render(<RunDetailPanel runId="run-1" onSelectRun={() => undefined} onClose={() => undefined} />);
  });
};

const stream = (): MockEventSource => {
  const es = MockEventSource.all[MockEventSource.all.length - 1];
  if (!es) throw new Error('the panel never opened a stream');
  return es;
};

const emit = async (type: string, data?: unknown) => {
  await act(async () => {
    stream().emit(type, data);
  });
};

/** The stream drops: EventSource fires `error`. */
const drop = () => emit('error');

describe('RunDetailPanel live stream', () => {
  it('opens the run stream with credentials, from seq 0, listening for log, partial and status', async () => {
    await render();
    expect(MockEventSource.all).toHaveLength(1);
    expect(stream().url).toBe('/stream/run-1?after_seq=0');
    expect(stream().init).toEqual({ withCredentials: true });
    expect(serverListeners(stream())).toEqual(['log', 'partial', 'status']);
    expect(stream().handles('error')).toBe(true);
  });

  it('appends each log event once, in seq order', async () => {
    await render();
    expect(container.textContent).toContain('No log output yet');

    await emit('log', logEntry(2, 'second line'));
    await emit('log', logEntry(1, 'first line'));
    await emit('log', logEntry(2, 'second line'));

    const text = container.textContent || '';
    expect(text).not.toContain('No log output yet');
    expect(text.split('second line')).toHaveLength(2);
    expect(text.indexOf('first line')).toBeLessThan(text.indexOf('second line'));
  });

  it('shows partial text as the answer so far, replacing it on each event', async () => {
    await render();
    expect(container.textContent).not.toContain('Output so far');

    await emit('partial', { run_id: 'run-1', text: 'The open requirem' });
    expect(container.textContent).toContain('Output so far');
    expect(container.textContent).toContain('The open requirem');

    await emit('partial', { run_id: 'run-1', text: 'The open requirements are REQ-4 and REQ-5.' });
    // Each event carries the whole text so far, so it replaces, never appends.
    expect(container.textContent).toContain('The open requirements are REQ-4 and REQ-5.');
    expect((container.textContent || '').split('The open requirem')).toHaveLength(2);
  });

  it('shows each status, and on a terminal one closes the stream and reloads the run once', async () => {
    await render();
    expect(api.get).toHaveBeenCalledTimes(1);

    await emit('status', { run_id: 'run-1', status: 'awaiting_approval' });
    expect(container.textContent).toContain('awaiting_approval');
    expect(stream().closed).toBe(false);

    api.get.mockResolvedValue({ data: { ...RUN, status: 'succeeded', final_text: 'Done.' } } as any);
    await emit('status', { run_id: 'run-1', status: 'succeeded' });
    expect(stream().closed).toBe(true);
    expect(api.get).toHaveBeenCalledTimes(2);
    expect(container.textContent).toContain('succeeded');
    expect(container.textContent).toContain('Done.');
  });

  it('after a drop, catches up and reconnects from the last seq it saw', async () => {
    vi.useFakeTimers();
    await render();
    await emit('log', logEntry(7, 'seventh line'));

    await drop();
    expect(stream().closed).toBe(true);
    expect(api.logs).toHaveBeenCalledWith('run-1', 7);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(MockEventSource.all).toHaveLength(2);
    expect(stream().url).toBe('/stream/run-1?after_seq=7');
    expect(serverListeners(stream())).toEqual(['log', 'partial', 'status']);
  });
});

// The run stream's reconnect policy, which X15b moves into
// hooks/useEventStream as a named policy (plan Q21): after a drop, a
// catch-up read of the logs and up to 3 retries at 1000 * 2^n ms, each from
// the last seq; then polling the logs and the run every 3000 ms until the run
// is terminal. Only a live event (log, partial or status) restores the retry
// budget; a stream that opens and drops again does not. A run already
// terminal is neither reconnected nor polled, and unmounting stops it all.
describe('RunDetailPanel reconnect policy', () => {
  const tick = async (ms: number) => {
    await act(async () => {
      vi.advanceTimersByTime(ms);
    });
  };

  /** A new stream opens exactly ms from now, not a millisecond sooner. */
  const reconnectsAfter = async (ms: number) => {
    const opened = MockEventSource.all.length;
    await tick(ms - 1);
    expect(MockEventSource.all).toHaveLength(opened);
    await tick(1);
    expect(MockEventSource.all).toHaveLength(opened + 1);
    expect(stream().init).toEqual({ withCredentials: true });
    expect(serverListeners(stream())).toEqual(['log', 'partial', 'status']);
  };

  it('retries after 1000, 2000 and 4000 ms, each after a catch-up, then polls every 3000 ms', async () => {
    vi.useFakeTimers();
    await render();
    for (const [i, delay] of [1000, 2000, 4000].entries()) {
      // Each stream opens before it drops: an open alone restores nothing.
      await emit('open');
      await drop();
      expect(api.logs).toHaveBeenCalledTimes(i + 1);
      expect(api.logs).toHaveBeenLastCalledWith('run-1', 0);
      await reconnectsAfter(delay);
    }
    expect(api.get).toHaveBeenCalledTimes(1);

    // The fourth drop has no retry left: poll at once, then every 3000 ms.
    await emit('open');
    await drop();
    expect(api.logs).toHaveBeenCalledTimes(4);
    expect(api.get).toHaveBeenCalledTimes(2);
    await tick(2999);
    expect(api.logs).toHaveBeenCalledTimes(4);
    await tick(1);
    expect(api.logs).toHaveBeenCalledTimes(5);
    expect(api.get).toHaveBeenCalledTimes(3);

    // A terminal status ends the polling; no stream is opened again.
    api.get.mockResolvedValue({ data: { ...RUN, status: 'succeeded' } } as any);
    await tick(3000);
    expect(api.logs).toHaveBeenCalledTimes(6);
    expect(api.get).toHaveBeenCalledTimes(4);
    await tick(30000);
    expect(api.logs).toHaveBeenCalledTimes(6);
    expect(api.get).toHaveBeenCalledTimes(4);
    expect(MockEventSource.all).toHaveLength(4);
  });

  it.each([
    ['log', logEntry(3, 'third line')],
    ['partial', { run_id: 'run-1', text: 'so far' }],
    ['status', { run_id: 'run-1', status: 'running' }],
  ])('a %s event restores the full retry budget', async (type, data) => {
    vi.useFakeTimers();
    await render();
    await drop();
    await reconnectsAfter(1000);
    await drop();
    await reconnectsAfter(2000);
    await emit(type, data);
    await drop();
    await reconnectsAfter(1000);
  });

  it('neither reconnects nor polls a run that is already terminal', async () => {
    vi.useFakeTimers();
    api.get.mockResolvedValue({ data: { ...RUN, status: 'failed' } } as any);
    await render();
    await drop();
    await tick(60000);
    expect(MockEventSource.all).toHaveLength(1);
    expect(api.logs).not.toHaveBeenCalled();
    expect(api.get).toHaveBeenCalledTimes(1);
  });

  it('closes the stream on unmount', async () => {
    await render();
    const es = stream();
    expect(es.closed).toBe(false);
    unmount();
    expect(es.closed).toBe(true);
  });

  it('opens no stream after unmount, even with a retry pending', async () => {
    vi.useFakeTimers();
    await render();
    await drop();
    unmount();
    await tick(60000);
    expect(MockEventSource.all).toHaveLength(1);
  });

  it('stops polling on unmount', async () => {
    vi.useFakeTimers();
    await render();
    for (const delay of [1000, 2000, 4000]) {
      await drop();
      await tick(delay);
    }
    await drop();
    await tick(3000);
    const logs = api.logs.mock.calls.length;
    const gets = api.get.mock.calls.length;
    unmount();
    await tick(30000);
    expect(api.logs).toHaveBeenCalledTimes(logs);
    expect(api.get).toHaveBeenCalledTimes(gets);
  });
});
