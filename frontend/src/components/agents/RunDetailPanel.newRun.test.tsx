import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { RunDetailPanel } from './RunDetailPanel';
import { agentRunsAPI } from '../../api/client';

// The run detail panel moving to another run (the run tree, a retry, the
// next row of the table) while it stays mounted: the stream, its retries,
// the polling and the settle catch-up are the old run's and stop, and the
// new run starts afresh, its first stream from seq 0. RunDetailPanel.test.tsx
// pins one run's stream; this pins the hand-over, which X15b splits between
// the panel's log effect and hooks/useEventStream (the panel resets the seq
// before the hook opens the new stream).

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    agentRunsAPI: { streamUrl: (id: string, afterSeq = 0) => `/stream/${id}?after_seq=${afterSeq}` },
  })
);

const api = vi.mocked(agentRunsAPI);

// An EventSource the test drives by hand, as in RunDetailPanel.test.tsx.
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

const run = (id: string, status = 'running') => ({
  id,
  agent_id: 'agent-1',
  agent_name: 'Analyst',
  status,
  prompt: 'Summarise the open requirements',
  tokens_in: 0,
  tokens_out: 0,
});

const logEntry = (runId: string, seq: number, text: string) => ({
  run_id: runId,
  seq,
  kind: 'text',
  payload: { text },
  created_at: '2026-10-05T10:00:00Z',
});

let container: HTMLDivElement;
let root: Root;
let mounted = false;

beforeEach(() => {
  vi.clearAllMocks();
  MockEventSource.all = [];
  api.get.mockImplementation(((id: string) => Promise.resolve({ data: run(id) })) as any);
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
  if (mounted) {
    act(() => {
      root.unmount();
    });
    mounted = false;
  }
  container.remove();
  vi.useRealTimers();
});

const show = async (runId: string) => {
  await act(async () => {
    root.render(<RunDetailPanel runId={runId} onSelectRun={() => undefined} onClose={() => undefined} />);
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
};

/** How many times the panel read a run's log. */
const logReads = (runId: string) => api.logs.mock.calls.filter(([id]) => id === runId).length;

describe('RunDetailPanel moving to another run', () => {
  it("closes the old run's stream and opens the new run's from seq 0, with its log afresh", async () => {
    await show('run-1');
    await emit('log', logEntry('run-1', 7, 'seventh line'));
    expect(container.textContent).toContain('seventh line');
    const old = stream();

    await show('run-2');
    expect(old.closed).toBe(true);
    expect(MockEventSource.all).toHaveLength(2);
    expect(stream().url).toBe('/stream/run-2?after_seq=0');
    expect(stream().init).toEqual({ withCredentials: true });
    expect(Object.keys(stream().listeners).sort()).toEqual(['log', 'partial', 'status']);
    expect(api.get).toHaveBeenLastCalledWith('run-2');
    expect(api.tree).toHaveBeenLastCalledWith('run-2');
    expect(container.textContent).not.toContain('seventh line');
    expect(container.textContent).toContain('No log output yet');
  });

  it("cancels the old run's pending retry and gives the new run the whole retry budget", async () => {
    vi.useFakeTimers();
    await show('run-1');
    await drop();
    await reconnectsAfter(1000);
    await drop();
    // run-1's second retry is pending when the panel moves on.
    await show('run-2');
    expect(MockEventSource.all).toHaveLength(3);
    expect(stream().url).toBe('/stream/run-2?after_seq=0');
    await tick(60000);
    expect(MockEventSource.all).toHaveLength(3);

    await drop();
    await reconnectsAfter(1000);
    expect(stream().url).toBe('/stream/run-2?after_seq=0');
  });

  it("stops polling the old run, and does not poll the new one while its stream is up", async () => {
    vi.useFakeTimers();
    await show('run-1');
    for (const delay of [1000, 2000, 4000]) {
      await drop();
      await tick(delay);
    }
    await drop();
    await tick(3000);
    // run-1 is polled: 3 catch-ups and 2 polls so far.
    expect(logReads('run-1')).toBe(5);

    await show('run-2');
    await tick(30000);
    expect(logReads('run-1')).toBe(5);
    expect(logReads('run-2')).toBe(0);
    expect(api.get.mock.calls.filter(([id]) => id === 'run-1')).toHaveLength(3);
  });

  it("cancels the old run's settle catch-up", async () => {
    vi.useFakeTimers();
    await show('run-1');
    api.get.mockImplementation(((id: string) => Promise.resolve({ data: run(id, 'succeeded') })) as any);
    await emit('status', { run_id: 'run-1', status: 'succeeded' });
    expect(logReads('run-1')).toBe(1);

    api.get.mockImplementation(((id: string) => Promise.resolve({ data: run(id) })) as any);
    await show('run-2');
    await tick(10000);
    expect(logReads('run-1')).toBe(1);
    expect(logReads('run-2')).toBe(0);
  });
});
