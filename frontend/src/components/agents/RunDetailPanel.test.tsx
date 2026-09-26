import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { RunDetailPanel } from './RunDetailPanel';
import { agentRunsAPI } from '../../api/client';

// The run detail panel's live tail (refactor plan S6, invariant I9; the guard
// X15b re-runs when it moves this stream into hooks/useEventStream): the
// panel opens the run's stream from the last seq it has, and reads the three
// events SSEHub sends on a run's stream, in the shapes it sends them
// (internal/api/sse.go): `log` carries one log entry, `partial` {run_id,
// text} and `status` {run_id, status}.

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

// A minimal EventSource the test drives by hand.
class MockEventSource {
  static all: MockEventSource[] = [];
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onerror: (() => void) | null = null;
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
  emit(type: string, data: unknown) {
    (this.listeners[type] || []).forEach((fn) => fn({ data: JSON.stringify(data) } as MessageEvent));
  }
}

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
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  vi.useRealTimers();
});

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

const emit = async (type: string, data: unknown) => {
  await act(async () => {
    stream().emit(type, data);
  });
};

describe('RunDetailPanel live stream', () => {
  it('opens the run stream with credentials, from seq 0, listening for log, partial and status', async () => {
    await render();
    expect(MockEventSource.all).toHaveLength(1);
    expect(stream().url).toBe('/stream/run-1?after_seq=0');
    expect(stream().init).toEqual({ withCredentials: true });
    expect(Object.keys(stream().listeners).sort()).toEqual(['log', 'partial', 'status']);
    expect(stream().onerror).toBeInstanceOf(Function);
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

    await act(async () => {
      stream().onerror!();
    });
    expect(stream().closed).toBe(true);
    expect(api.logs).toHaveBeenCalledWith('run-1', 7);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(MockEventSource.all).toHaveLength(2);
    expect(stream().url).toBe('/stream/run-1?after_seq=7');
    expect(Object.keys(stream().listeners).sort()).toEqual(['log', 'partial', 'status']);
  });
});
