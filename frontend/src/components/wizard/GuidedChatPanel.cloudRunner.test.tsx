import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { GuidedChatPanel } from './GuidedChatPanel';
import { guidedAPI } from '../../api/client';

// With no runner online, the server may lease a cloud runner for the turn
// (runner_starting): the panel then says a runner is starting rather than
// that nothing is connected, until the reply arrives.

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


const stream = (): MockEventSource => MockEventSource.all[MockEventSource.all.length - 1];

describe('GuidedChatPanel while a cloud runner starts for the turn', () => {
  it('shows the starting notice, not the not-connected one, until the reply arrives', async () => {
    api.kickoffChat.mockResolvedValue({
      data: { status: 'launched', runner_online: false, runner_starting: true },
    } as any);
    await render('gs-1');
    expect(container.querySelector('[data-testid="cloud-runner-starting"]')).not.toBeNull();
    expect(container.textContent).not.toContain('not connected');

    act(() => {
      stream().emit('message', { id: 'm1', role: 'assistant', content: 'Hello', created_at: '2026-10-16T00:00:00Z' });
    });
    expect(container.querySelector('[data-testid="cloud-runner-starting"]')).toBeNull();
  });

  it('still says nothing is connected when no runner is starting', async () => {
    api.kickoffChat.mockResolvedValue({ data: { status: 'launched', runner_online: false } } as any);
    await render('gs-1');
    expect(container.querySelector('[data-testid="cloud-runner-starting"]')).toBeNull();
    expect(container.textContent).toContain('not connected');
  });
});
