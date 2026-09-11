import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { GuidedChatPanel } from './GuidedChatPanel';
import { guidedAPI } from '../../api/client';

// CRA's Jest cannot resolve react-router v7's package exports; the panel only
// uses Link (in the "no runner" notice).
jest.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

jest.mock('../../api/client', () => ({
  guidedAPI: {
    listMessages: jest.fn(),
    kickoffChat: jest.fn(),
    nudgeChat: jest.fn(),
    sendMessage: jest.fn(),
    chatStreamUrl: (id: string) => `/stream/${id}`,
  },
}));

const api = guidedAPI as jest.Mocked<typeof guidedAPI>;

// A minimal EventSource the test drives: the panel subscribes to `message`
// and `assistant_partial`, and the test emits them by hand.
class MockEventSource {
  static last: MockEventSource | null = null;
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(public url: string) {
    MockEventSource.last = this;
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

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  jest.clearAllMocks();
  MockEventSource.last = null;
  api.listMessages.mockResolvedValue({ data: [] } as any);
  api.kickoffChat.mockResolvedValue({ data: { status: 'launched', runner_online: true } } as any);
  api.nudgeChat.mockResolvedValue({ data: { status: 'pending', runner_online: true } } as any);
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  // jsdom has no scrollTo on elements.
  (Element.prototype as any).scrollTo = jest.fn();
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

const render = async () => {
  await act(async () => {
    root.render(<GuidedChatPanel sessionId="gs-1" step={2} />);
  });
};

const stream = (): MockEventSource => {
  if (!MockEventSource.last) throw new Error('the panel never opened a stream');
  return MockEventSource.last;
};

const bubble = () => container.querySelector('[data-testid="assistant-partial"]');

describe('GuidedChatPanel streaming', () => {
  it('renders the reply as it is written and grows it in place', async () => {
    await render();
    expect(bubble()).toBeNull();

    await act(async () => {
      stream().emit('assistant_partial', { run_id: 'run-1', text: 'Your vision ' });
    });
    expect(bubble()).not.toBeNull();
    expect(bubble()!.textContent).toContain('Your vision');
    // Each event carries the whole text so far, so the bubble is replaced,
    // never appended to.
    await act(async () => {
      stream().emit('assistant_partial', { run_id: 'run-1', text: 'Your vision statement is vague.' });
    });
    expect(container.querySelectorAll('[data-testid="assistant-partial"]').length).toBe(1);
    expect(bubble()!.textContent).toContain('Your vision statement is vague.');
    // While text is arriving, the "thinking" placeholder is gone.
    expect(container.textContent).not.toContain('The assistant is thinking');
  });

  it('replaces the streaming bubble with the final message', async () => {
    await render();
    await act(async () => {
      stream().emit('assistant_partial', { run_id: 'run-1', text: 'Half an answ' });
    });
    expect(bubble()).not.toBeNull();

    await act(async () => {
      stream().emit('message', {
        id: 'm-1',
        session_id: 'gs-1',
        role: 'assistant',
        content: 'Half an answer, then the whole one.',
        created_at: '2026-09-11T10:00:00Z',
      });
    });
    expect(bubble()).toBeNull();
    expect(container.textContent).toContain('Half an answer, then the whole one.');
  });

  it('clears the streaming bubble when the turn fails', async () => {
    await render();
    await act(async () => {
      stream().emit('assistant_partial', { run_id: 'run-1', text: 'Starting to answ' });
    });
    expect(bubble()).not.toBeNull();

    await act(async () => {
      stream().emit('message', {
        id: 'm-err',
        session_id: 'gs-1',
        role: 'system',
        content: 'The copilot hit a technical problem answering.',
        created_at: '2026-09-11T10:00:01Z',
      });
    });
    expect(bubble()).toBeNull();
    expect(container.textContent).toContain('technical problem');
  });

  it('ignores empty or malformed partial events', async () => {
    await render();
    await act(async () => {
      stream().emit('assistant_partial', { run_id: 'run-1', text: '' });
      (stream().listeners['assistant_partial'] || []).forEach((fn) =>
        fn({ data: 'not json' } as MessageEvent)
      );
    });
    expect(bubble()).toBeNull();
  });
});

describe('GuidedChatPanel nudges', () => {
  it('sends at most one nudge a second and lets the server coalesce', async () => {
    const ref = React.createRef<any>();
    await act(async () => {
      root.render(<GuidedChatPanel ref={ref} sessionId="gs-1" step={2} />);
    });

    await act(async () => {
      ref.current.nudge(2, 'saved step 2');
      ref.current.nudge(3, 'saved step 3');
    });
    expect(api.nudgeChat).toHaveBeenCalledTimes(1);
    expect(api.nudgeChat).toHaveBeenCalledWith('gs-1', 2, expect.anything(), 'saved step 2');

    // A second later the next wizard action is sent; the server decides
    // whether it launches now or waits for the running turn.
    const realNow = Date.now;
    Date.now = () => realNow() + 1500;
    try {
      await act(async () => {
        ref.current.nudge(4, 'saved step 4');
      });
    } finally {
      Date.now = realNow;
    }
    expect(api.nudgeChat).toHaveBeenCalledTimes(2);
    expect(api.nudgeChat).toHaveBeenLastCalledWith('gs-1', 4, expect.anything(), 'saved step 4');
  });
});
