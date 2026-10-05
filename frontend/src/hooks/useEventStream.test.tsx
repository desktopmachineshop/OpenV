import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { SSE_EVENT } from '../sseEvents';
import {
  BackoffReconnect,
  EventStreamListeners,
  EventStreamOptions,
  RECONNECT,
  useEventStream,
} from './useEventStream';

// useEventStream against the four EventSource loops it replaces (refactor
// plan X15, quirk Q21). Each describe mirrors one caller's inline code as it
// stands and is named after it: the URL it opens, the events it listens for
// (by SSE_EVENT), its reconnect delays, after_seq on a reconnect,
// withCredentials, and what unmounting closes. X15b-X15e move each caller
// onto the hook; that caller's own stream test is the guard there.

// An EventSource the test drives by hand. As with the real one, an event
// reaches both its addEventListener listeners and its on<event> property.
class FakeEventSource {
  static all: FakeEventSource[] = [];
  /** Makes the next construction throw, as a malformed URL does. */
  static failNext = false;
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onopen: ((e: MessageEvent) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: MessageEvent) => void) | null = null;
  closed = false;

  constructor(
    public url: string,
    public init?: EventSourceInit
  ) {
    if (FakeEventSource.failNext) {
      FakeEventSource.failNext = false;
      throw new SyntaxError('The URL is invalid.');
    }
    FakeEventSource.all.push(this);
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
(globalThis as any).EventSource = FakeEventSource as any;

interface ProbeProps {
  url: string | null;
  listeners: EventStreamListeners;
  options: EventStreamOptions;
}

const Probe: React.FC<ProbeProps> = ({ url, listeners, options }) => {
  useEventStream(url, listeners, options);
  return null;
};

let container: HTMLDivElement;
let root: Root;
let mounted = false;

beforeEach(() => {
  FakeEventSource.all = [];
  FakeEventSource.failNext = false;
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

const render = (props: ProbeProps) => {
  act(() => {
    root.render(<Probe {...props} />);
  });
};

const unmount = () => {
  act(() => {
    root.unmount();
  });
  mounted = false;
};

const opened = () => FakeEventSource.all.length;

const stream = (): FakeEventSource => {
  const es = FakeEventSource.all[FakeEventSource.all.length - 1];
  if (!es) throw new Error('the hook never opened a stream');
  return es;
};

const emit = (type: string, data?: unknown) => {
  act(() => {
    stream().emit(type, data);
  });
};

/** The stream drops: EventSource fires `error`. */
const drop = () => emit('error');

const tick = (ms: number) => {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
};

/** A new stream opens exactly ms from now, not a millisecond sooner. */
const reconnectsAfter = (ms: number) => {
  const before = opened();
  tick(ms - 1);
  expect(opened()).toBe(before);
  tick(1);
  expect(opened()).toBe(before + 1);
};

const listenedFor = (es: FakeEventSource) => Object.keys(es.listeners).sort();

// RunDetailPanel.tsx:281-358 (the budget and connectStream), :362-368
// (cleanup); its constants at :15-16. The panel keeps its own catch-up, polling
// and terminal-status bookkeeping and hands the hook three callbacks.
describe('useEventStream for RunDetailPanel (backoff and after_seq)', () => {
  const TERMINAL = ['succeeded', 'failed', 'timed_out', 'cancelled'];

  const setup = (status = 'running') => {
    const lastSeq = { current: 0 };
    const runStatus = { current: status };
    const calls: string[] = [];
    const received: string[] = [];
    const listeners: EventStreamListeners = {
      [SSE_EVENT.log]: (e) => {
        received.push(`log ${e.data}`);
        lastSeq.current = JSON.parse(e.data).seq;
      },
      [SSE_EVENT.partial]: (e) => received.push(`partial ${e.data}`),
      [SSE_EVENT.status]: (e, es) => {
        received.push(`status ${e.data}`);
        runStatus.current = JSON.parse(e.data).status;
        if (TERMINAL.includes(runStatus.current)) es.close();
      },
    };
    const reconnect: BackoffReconnect = {
      ...RECONNECT.backoff,
      onRetry: () => calls.push(`catch up from ${lastSeq.current}`),
      onGiveUp: () => calls.push('poll'),
      isDone: () => TERMINAL.includes(runStatus.current),
    };
    const props: ProbeProps = {
      url: '/stream/run-1',
      listeners,
      options: {
        withCredentials: true,
        reconnect,
        resumeUrl: () => `/stream/run-1?after_seq=${lastSeq.current}`,
      },
    };
    render(props);
    return { lastSeq, runStatus, calls, received, props };
  };

  it('keeps the panel timings: 3 retries from 1000 ms', () => {
    expect(RECONNECT.backoff).toEqual({ kind: 'backoff', baseDelayMs: 1000, maxAttempts: 3 });
  });

  it('opens the run stream from seq 0 with credentials, listening for log, partial and status', () => {
    setup();
    expect(opened()).toBe(1);
    expect(stream().url).toBe('/stream/run-1?after_seq=0');
    expect(stream().init).toEqual({ withCredentials: true });
    expect(listenedFor(stream())).toEqual([SSE_EVENT.log, SSE_EVENT.partial, SSE_EVENT.status].sort());
    expect(stream().onerror).toBeInstanceOf(Function);
    // The panel sets no onopen: an open restores nothing.
    expect(stream().onopen).toBeNull();
  });

  it('hands each event to its listener', () => {
    const { received } = setup();
    emit(SSE_EVENT.log, { seq: 1 });
    emit(SSE_EVENT.partial, { run_id: 'run-1', text: 'so far' });
    emit(SSE_EVENT.status, { run_id: 'run-1', status: 'running' });
    expect(received).toEqual([
      'log {"seq":1}',
      'partial {"run_id":"run-1","text":"so far"}',
      'status {"run_id":"run-1","status":"running"}',
    ]);
  });

  it('retries after 1000, 2000 and 4000 ms, each after a catch-up, then gives up for polling', () => {
    vi.useFakeTimers();
    const { calls } = setup();
    for (const [i, delay] of [1000, 2000, 4000].entries()) {
      // Each stream opens before it drops: an open alone restores nothing.
      emit('open');
      const dropped = stream();
      drop();
      expect(dropped.closed).toBe(true);
      // The catch-up runs at the drop, before the delay.
      expect(calls).toHaveLength(i + 1);
      expect(calls[i]).toBe('catch up from 0');
      reconnectsAfter(delay);
      expect(stream().init).toEqual({ withCredentials: true });
      expect(listenedFor(stream())).toEqual([SSE_EVENT.log, SSE_EVENT.partial, SSE_EVENT.status].sort());
    }
    emit('open');
    drop();
    expect(calls).toEqual(['catch up from 0', 'catch up from 0', 'catch up from 0', 'poll']);
    tick(60000);
    expect(opened()).toBe(4);
  });

  it('reconnects from the last seq the caller read (after_seq)', () => {
    vi.useFakeTimers();
    const { calls } = setup();
    emit(SSE_EVENT.log, { seq: 7 });
    drop();
    expect(calls).toEqual(['catch up from 7']);
    reconnectsAfter(1000);
    expect(stream().url).toBe('/stream/run-1?after_seq=7');
  });

  it.each([
    [SSE_EVENT.log, { seq: 3 }],
    [SSE_EVENT.partial, { run_id: 'run-1', text: 'so far' }],
    [SSE_EVENT.status, { run_id: 'run-1', status: 'running' }],
  ])('a %s event restores the full retry budget', (type, data) => {
    vi.useFakeTimers();
    setup();
    drop();
    reconnectsAfter(1000);
    drop();
    reconnectsAfter(2000);
    emit(type, data);
    drop();
    reconnectsAfter(1000);
  });

  it('neither retries nor gives up on a drop once the run is terminal', () => {
    vi.useFakeTimers();
    const { calls } = setup('failed');
    drop();
    expect(stream().closed).toBe(true);
    tick(60000);
    expect(opened()).toBe(1);
    expect(calls).toEqual([]);
  });

  it('lets a listener close the stream on a terminal status, and opens no other', () => {
    vi.useFakeTimers();
    const { calls } = setup();
    emit(SSE_EVENT.status, { run_id: 'run-1', status: 'succeeded' });
    expect(stream().closed).toBe(true);
    tick(60000);
    expect(opened()).toBe(1);
    expect(calls).toEqual([]);
  });

  it('gives up at once when the EventSource cannot be made', () => {
    FakeEventSource.failNext = true;
    const { calls } = setup();
    expect(opened()).toBe(0);
    expect(calls).toEqual(['poll']);
  });

  it('gives up when a retry cannot make its EventSource', () => {
    vi.useFakeTimers();
    const { calls } = setup();
    drop();
    FakeEventSource.failNext = true;
    tick(1000);
    expect(opened()).toBe(1);
    expect(calls).toEqual(['catch up from 0', 'poll']);
  });

  it('closes the stream on unmount', () => {
    setup();
    const es = stream();
    expect(es.closed).toBe(false);
    unmount();
    expect(es.closed).toBe(true);
  });

  it('opens no stream after unmount, even with a retry pending', () => {
    vi.useFakeTimers();
    setup();
    drop();
    unmount();
    tick(60000);
    expect(opened()).toBe(1);
  });

  it('starts a new run with a full budget, closing the old stream', () => {
    vi.useFakeTimers();
    const { props } = setup();
    drop();
    reconnectsAfter(1000);
    drop();
    reconnectsAfter(2000);
    const old = stream();
    render({ ...props, url: '/stream/run-2', options: { ...props.options, resumeUrl: () => '/stream/run-2?after_seq=0' } });
    expect(old.closed).toBe(true);
    expect(opened()).toBe(4);
    expect(stream().url).toBe('/stream/run-2?after_seq=0');
    drop();
    reconnectsAfter(1000);
  });
});

// GuidedChatPanel.tsx:384-426 (connectStream), :461-468 (cleanup). The panel
// opens the stream once the transcript has loaded, so url stays null until
// then.
describe('useEventStream for GuidedChatPanel (a capped exponent for ever)', () => {
  const setup = (url: string | null = '/guided/gs-1/chat/stream') => {
    const received: string[] = [];
    const props: ProbeProps = {
      url,
      listeners: {
        [SSE_EVENT.message]: (e) => received.push(`message ${e.data}`),
        [SSE_EVENT.assistant_partial]: (e) => received.push(`assistant_partial ${e.data}`),
      },
      options: { withCredentials: true, reconnect: RECONNECT.cappedExponent },
    };
    render(props);
    return { received, props };
  };

  it('keeps the panel timings: base 1000 ms, exponent capped at 6, delay capped at 15000 ms', () => {
    expect(RECONNECT.cappedExponent).toEqual({
      kind: 'cappedExponent',
      baseDelayMs: 1000,
      maxExponent: 6,
      maxDelayMs: 15000,
    });
  });

  it('opens nothing while the url is null (the transcript is loading)', () => {
    const { props } = setup(null);
    expect(opened()).toBe(0);
    render({ ...props, url: '/guided/gs-1/chat/stream' });
    expect(opened()).toBe(1);
  });

  it('opens the session stream with credentials, listening for message and assistant_partial', () => {
    setup();
    expect(opened()).toBe(1);
    expect(stream().url).toBe('/guided/gs-1/chat/stream');
    expect(stream().init).toEqual({ withCredentials: true });
    expect(listenedFor(stream())).toEqual([SSE_EVENT.assistant_partial, SSE_EVENT.message].sort());
    expect(stream().onopen).toBeInstanceOf(Function);
    expect(stream().onerror).toBeInstanceOf(Function);
  });

  it('hands each event to its listener', () => {
    const { received } = setup();
    emit(SSE_EVENT.assistant_partial, { run_id: 'run-1', text: 'Half' });
    emit(SSE_EVENT.message, { id: 'm-1' });
    expect(received).toEqual(['assistant_partial {"run_id":"run-1","text":"Half"}', 'message {"id":"m-1"}']);
  });

  it('retries for ever after 2000, 4000, 8000 ms and then every 15000 ms, closing each dropped stream', () => {
    vi.useFakeTimers();
    setup();
    for (const delay of [2000, 4000, 8000, 15000, 15000, 15000, 15000, 15000]) {
      const dropped = stream();
      drop();
      expect(dropped.closed).toBe(true);
      reconnectsAfter(delay);
      expect(stream().url).toBe('/guided/gs-1/chat/stream');
      expect(stream().init).toEqual({ withCredentials: true });
      expect(listenedFor(stream())).toEqual([SSE_EVENT.assistant_partial, SSE_EVENT.message].sort());
    }
  });

  it('starts over at 2000 ms after an open; an event alone does not', () => {
    vi.useFakeTimers();
    setup();
    drop();
    reconnectsAfter(2000);
    emit(SSE_EVENT.message, { id: 'm-1' });
    emit(SSE_EVENT.assistant_partial, { run_id: 'run-1', text: 'Half' });
    drop();
    reconnectsAfter(4000);
    emit('open');
    drop();
    reconnectsAfter(2000);
  });

  it('carries the exponent over to a new session until an open (the panel keeps retryRef across sessions)', () => {
    vi.useFakeTimers();
    const { props } = setup();
    drop();
    reconnectsAfter(2000);
    drop();
    reconnectsAfter(4000);
    const old = stream();
    render({ ...props, url: '/guided/gs-2/chat/stream' });
    expect(old.closed).toBe(true);
    expect(stream().url).toBe('/guided/gs-2/chat/stream');
    drop();
    reconnectsAfter(8000);
    emit('open');
    drop();
    reconnectsAfter(2000);
  });

  it('closes the stream on unmount', () => {
    setup();
    const es = stream();
    unmount();
    expect(es.closed).toBe(true);
  });

  it('opens no stream after unmount, even with a retry pending', () => {
    vi.useFakeTimers();
    setup();
    drop();
    unmount();
    tick(60000);
    expect(opened()).toBe(1);
  });
});

// InterviewChat.tsx:46-86 (connectStream), :117-124 (cleanup): the
// GuidedChatPanel loop, on the public stream, without credentials.
describe('useEventStream for InterviewChat (the capped exponent, without credentials)', () => {
  const setup = () => {
    const received: string[] = [];
    render({
      url: '/public/interviews/tok-1/stream',
      listeners: {
        [SSE_EVENT.message]: (e) => received.push(`message ${e.data}`),
        [SSE_EVENT.assistant_partial]: (e) => received.push(`assistant_partial ${e.data}`),
      },
      options: { withCredentials: false, reconnect: RECONNECT.cappedExponent },
    });
    return { received };
  };

  it("opens the interview's stream without credentials, listening for message and assistant_partial", () => {
    const { received } = setup();
    expect(opened()).toBe(1);
    expect(stream().url).toBe('/public/interviews/tok-1/stream');
    expect(stream().init).toEqual({ withCredentials: false });
    expect(listenedFor(stream())).toEqual([SSE_EVENT.assistant_partial, SSE_EVENT.message].sort());
    emit(SSE_EVENT.message, { id: 'm-1' });
    expect(received).toEqual(['message {"id":"m-1"}']);
  });

  it('reconnects without credentials after 2000, 4000, 8000 and 15000 ms, and after 2000 ms once opened', () => {
    vi.useFakeTimers();
    setup();
    for (const delay of [2000, 4000, 8000, 15000, 15000]) {
      drop();
      reconnectsAfter(delay);
      expect(stream().url).toBe('/public/interviews/tok-1/stream');
      expect(stream().init).toEqual({ withCredentials: false });
    }
    emit('open');
    drop();
    reconnectsAfter(2000);
  });

  it('closes the stream on unmount and opens none after it', () => {
    vi.useFakeTimers();
    setup();
    drop();
    reconnectsAfter(2000);
    const es = stream();
    drop();
    unmount();
    expect(es.closed).toBe(true);
    tick(60000);
    expect(opened()).toBe(2);
  });
});

// NotificationBell.tsx:116-132: one stream for the life of the bell, with no
// error handler, so the browser's own reconnect brings it back.
describe('useEventStream for NotificationBell (the browser reconnects)', () => {
  const setup = () => {
    const received: string[] = [];
    const props: ProbeProps = {
      url: '/notifications/stream',
      listeners: { [SSE_EVENT.notification]: (e) => received.push(`notification ${e.data}`) },
      options: { withCredentials: true, reconnect: RECONNECT.browser },
    };
    render(props);
    return { received, props };
  };

  it('opens the notification stream with credentials, listening for notification and nothing else', () => {
    const { received } = setup();
    expect(opened()).toBe(1);
    expect(stream().url).toBe('/notifications/stream');
    expect(stream().init).toEqual({ withCredentials: true });
    expect(listenedFor(stream())).toEqual([SSE_EVENT.notification]);
    expect(stream().onerror).toBeNull();
    expect(stream().onopen).toBeNull();
    expect(stream().onmessage).toBeNull();
    emit(SSE_EVENT.notification, { id: 'n-1' });
    expect(received).toEqual(['notification {"id":"n-1"}']);
  });

  it('leaves a drop to the browser: it neither closes the stream nor opens another', () => {
    vi.useFakeTimers();
    setup();
    drop();
    tick(60000);
    expect(opened()).toBe(1);
    expect(stream().closed).toBe(false);
  });

  it('keeps one stream across renders, each event reaching the newest listener', () => {
    const { props } = setup();
    const newer: string[] = [];
    render({ ...props, listeners: { [SSE_EVENT.notification]: (e) => newer.push(e.data) } });
    expect(opened()).toBe(1);
    emit(SSE_EVENT.notification, { id: 'n-2' });
    expect(newer).toEqual(['{"id":"n-2"}']);
  });

  it('closes the stream on unmount', () => {
    setup();
    const es = stream();
    unmount();
    expect(es.closed).toBe(true);
  });
});
