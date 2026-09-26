import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { InterviewChat } from './InterviewChat';
import { publicInterviewAPI } from '../api/client';

// The public interview's live stream (refactor plan S6, invariant I9; the
// guard X15d re-runs when it moves this stream into hooks/useEventStream):
// the page opens the interview's stream without credentials and reads the
// two events the server sends on an interview:<id> stream
// (contracts/sse-events.json): `assistant_partial` {run_id, text}, the reply
// so far, and `message`, one whole transcript message. The test drives the
// page by event name and reads what it renders, so it does not care whether
// a listener is attached with addEventListener or an on<event> property, or
// in which module.

vi.mock('react-router-dom', () => ({
  useParams: () => ({ token: 'tok-1' }),
}));

vi.mock('../components/ui', () => ({
  useConfirm: () => () => Promise.resolve(true),
}));

vi.mock('../api/client', () => ({
  publicInterviewAPI: {
    intro: vi.fn(),
    sendMessage: vi.fn(),
    finish: vi.fn(),
    streamUrl: (token: string) => `/public/interviews/${token}/stream`,
  },
}));

const api = vi.mocked(publicInterviewAPI);

// A minimal EventSource the test drives by hand. Like the real one, an event
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
  emit(type: string, data: unknown) {
    const event = { type, data: JSON.stringify(data) } as MessageEvent;
    const handler = (this as any)[`on${type}`];
    if (typeof handler === 'function') handler(event);
    (this.listeners[type] || []).forEach((fn) => fn(event));
  }
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = MockEventSource as any;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  MockEventSource.all = [];
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
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

const render = async () => {
  await act(async () => {
    root.render(<InterviewChat />);
  });
};

const stream = (): MockEventSource => {
  const es = MockEventSource.all[MockEventSource.all.length - 1];
  if (!es) throw new Error('the page never opened a stream');
  return es;
};

const emit = async (type: string, data: unknown) => {
  await act(async () => {
    stream().emit(type, data);
  });
};

describe('InterviewChat live stream', () => {
  it("opens the interview's stream without credentials", async () => {
    await render();
    expect(api.intro).toHaveBeenCalledWith('tok-1');
    expect(MockEventSource.all).toHaveLength(1);
    expect(stream().url).toBe('/public/interviews/tok-1/stream');
    expect(stream().init).toEqual({ withCredentials: false });
  });

  it('shows assistant_partial as the reply so far, replacing it on each event', async () => {
    await render();
    expect(container.querySelector('[data-testid="assistant-partial"]')).toBeNull();

    await emit('assistant_partial', { run_id: 'run-1', text: 'Half a rep' });
    expect(container.querySelector('[data-testid="assistant-partial"]')?.textContent).toContain('Half a rep');

    await emit('assistant_partial', { run_id: 'run-1', text: 'Half a reply, then the rest.' });
    // Each event carries the whole text so far, so it replaces, never appends.
    const partial = container.querySelector('[data-testid="assistant-partial"]')?.textContent || '';
    expect(partial).toContain('Half a reply, then the rest.');
    expect(partial.split('Half a rep')).toHaveLength(2);
  });

  it('appends each message event once, and a whole reply replaces the partial', async () => {
    await render();
    await emit('assistant_partial', { run_id: 'run-1', text: 'Whole rep' });

    const reply = {
      id: 'm-1',
      session_id: 'session-1',
      role: 'assistant',
      content: 'Whole reply from the interviewer.',
      created_at: '2026-09-26T10:00:00Z',
    };
    await emit('message', reply);
    await emit('message', reply);

    const text = container.textContent || '';
    expect(text.split('Whole reply from the interviewer.')).toHaveLength(2);
    expect(container.querySelector('[data-testid="assistant-partial"]')).toBeNull();
  });
});
