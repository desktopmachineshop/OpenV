import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { GuidedChatPanel } from './GuidedChatPanel';
import { guidedAPI } from '../../api/client';

// The router is stubbed rather than provided: the panel only
// uses Link (in the "no runner" notice).
vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

vi.mock('../../api/client', () => ({
  guidedAPI: {
    listMessages: vi.fn(),
    kickoffChat: vi.fn(),
    nudgeChat: vi.fn(),
    sendMessage: vi.fn(),
    chatStreamUrl: (id: string) => `/stream/${id}`,
  },
}));

const api = vi.mocked(guidedAPI);

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
  vi.clearAllMocks();
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
  (Element.prototype as any).scrollTo = vi.fn();
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
  const renderWithRef = async () => {
    const ref = React.createRef<any>();
    await act(async () => {
      root.render(<GuidedChatPanel ref={ref} sessionId="gs-1" step={2} getState={() => state} />);
    });
    return ref;
  };
  // The wizard's live state, which the test moves on between saves.
  let state: Record<string, any> = {};

  beforeEach(() => {
    state = {};
  });

  it('sends at most one nudge a second and lets the server coalesce', async () => {
    const ref = await renderWithRef();

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

  // The freshest wizard state must reach the server: a nudge inside the
  // window waits for it to open instead of being thrown away.
  it('defers the newest nudge of a burst instead of dropping it', async () => {
    vi.useFakeTimers();
    try {
      const ref = await renderWithRef();

      // Opens the throttle window.
      state = { step_2: 'personas' };
      await act(async () => {
        ref.current.nudge(2, 'saved step 2');
      });
      expect(api.nudgeChat).toHaveBeenCalledTimes(1);

      // Two more saves inside that one second: exactly one request follows,
      // carrying the second save's step, event and state.
      state = { step_3: 'needs' };
      act(() => {
        ref.current.nudge(3, 'saved step 3');
      });
      state = { step_4: 'requirements' };
      act(() => {
        ref.current.nudge(4, 'saved step 4');
      });
      expect(api.nudgeChat).toHaveBeenCalledTimes(1);

      await act(async () => {
        vi.advanceTimersByTime(1000);
      });
      expect(api.nudgeChat).toHaveBeenCalledTimes(2);
      expect(api.nudgeChat).toHaveBeenLastCalledWith(
        'gs-1',
        4,
        { step_4: 'requirements' },
        'saved step 4'
      );

      // And nothing else goes out afterwards.
      await act(async () => {
        vi.advanceTimersByTime(5000);
      });
      expect(api.nudgeChat).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  // Nothing has been asked of the server yet, so a merely deferred nudge must
  // not claim the assistant is answering.
  it('does not show the thinking indicator for a nudge that was only deferred', async () => {
    api.nudgeChat.mockResolvedValue({ data: { status: 'unavailable', runner_online: true } } as any);
    vi.useFakeTimers();
    try {
      const ref = await renderWithRef();

      await act(async () => {
        ref.current.nudge(2, 'saved step 2');
      });
      // The first nudge was answered "unavailable": no reply is coming.
      expect(container.textContent).not.toContain('The assistant is thinking');

      act(() => {
        ref.current.nudge(3, 'saved step 3');
      });
      expect(container.textContent).not.toContain('The assistant is thinking');

      // Once it actually goes out, the indicator behaves as usual.
      await act(async () => {
        vi.advanceTimersByTime(1000);
      });
      expect(api.nudgeChat).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});

// Beside the project a card's button says what applying does there, and a
// card that only the project can take is not offered to the wizard at all —
// a "+ Add to wizard" button on a move would be a lie.
describe('GuidedChatPanel suggestion cards by target', () => {
  const reply = (content: string) => ({
    id: 'm-1',
    session_id: 'gs-1',
    role: 'assistant',
    content,
    created_at: '2026-09-13T10:00:00Z',
  });
  const cards =
    'Two changes.\n```openv-suggestion\n{"kind":"edit","ref":"REQ-12","body":"The system shall stop within 200 ms."}\n```\n' +
    '```openv-suggestion\n{"kind":"move","ref":"REQ-12","parent":"HDG-3","position":"first"}\n```\n' +
    '```openv-suggestion\n{"kind":"artifact","type":"test-case","title":"Estop test","parent":"HDG-4"}\n```';

  it('labels project-mode cards by what they do', async () => {
    const apply = vi.fn(async (items: any[]) => items.map(() => null));
    await act(async () => {
      root.render(<GuidedChatPanel sessionId="gs-1" applyTarget="project" onApplySuggestions={apply} />);
    });
    await act(async () => {
      stream().emit('message', reply(cards));
    });
    const buttons = Array.from(container.querySelectorAll('button')).map((b) => b.textContent);
    expect(buttons).toEqual(expect.arrayContaining(['Apply change', 'Move', '+ Add to project']));
    expect(container.textContent).toContain('REQ-12');
    expect(container.textContent).toContain('under HDG-3, first');
    expect(container.textContent).toContain('Test case: Estop test');
  });

  // The wizard sits on top of a project — a resumed definition is over
  // artifacts that already exist — so a card that changes the project is
  // offered there too, and says what it does rather than "Add to wizard".
  it('offers the wizard the project cards, labelled as project changes', async () => {
    const apply = vi.fn(async (items: any[]) => items.map(() => null));
    await act(async () => {
      root.render(<GuidedChatPanel sessionId="gs-1" step={4} onApplySuggestions={apply} />);
    });
    await act(async () => {
      stream().emit('message', reply(cards));
    });
    const buttons = Array.from(container.querySelectorAll('button')).map((b) => b.textContent);
    expect(buttons).toEqual(expect.arrayContaining(['Apply change', 'Move', '+ Add to project']));
    expect(buttons).not.toContain('+ Add to wizard');
  });

  // Applied cards flip to their done state from the `applied` map the host
  // owns, so a remount cannot re-arm a button that already wrote.
  it('shows a project card as done once its key is applied', async () => {
    await act(async () => {
      root.render(
        <GuidedChatPanel
          sessionId="gs-1"
          applyTarget="project"
          applied={{ 'm-1:1': true }}
          onApplySuggestions={async (items) => items.map(() => null)}
        />
      );
    });
    await act(async () => {
      stream().emit('message', reply(cards));
    });
    expect(container.textContent).toContain('✓ Changed');
  });
});
