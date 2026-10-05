import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { CopilotSuggestion, GuidedChatPanel } from './GuidedChatPanel';
import { guidedAPI } from '../../api/client';

// #379 bugs 209-213 and 217 in the V&V Assistant's panel, each pinned by the
// behaviour its fix gives:
//
//   209  a link in a reply carries only its own attributes, not
//        react-markdown's `node` prop as node="[object Object]";
//   210  a transcript that fails to load still opens the session's stream
//        (with the error kept, and no kickoff);
//   211  "The assistant will join in a moment" is not shown beside the
//        not-connected notice;
//   212  a suggestion of a kind the panel does not know is shown read-only,
//        by its kind and a summary rather than its JSON, and Apply all
//        leaves it out;
//   213  a session change clears the previous session's error, thinking
//        indicator and not-connected notice;
//   217  the answer to a message sent before a session change is dropped:
//        neither the echo nor what it says about the runner reaches the
//        new session.

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

type Role = 'assistant' | 'user' | 'system';
const message = (id: string, sessionId: string, role: Role, content: string) => ({
  id,
  session_id: sessionId,
  role,
  content,
  created_at: '2026-10-05T10:00:00Z',
});
// A suggestion block as the assistant writes one into a reply.
const card = (s: Record<string, unknown>) => '```openv-suggestion\n' + JSON.stringify(s) + '\n```';

const OFFLINE = 'V&V Assistant not connected';
const THINKING = 'The assistant is thinking…';
const WAITING = 'The assistant will join in a moment';
const LOAD_FAILED = 'Failed to load the assistant conversation.';

// Each session's transcript, or the error its load fails with.
let transcripts: Record<string, unknown[] | Error> = {};

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  MockEventSource.all = [];
  transcripts = {};
  api.listMessages.mockImplementation(((id: string) => {
    const transcript = transcripts[id] ?? [];
    return transcript instanceof Error ? Promise.reject(transcript) : Promise.resolve({ data: transcript });
  }) as any);
  api.kickoffChat.mockResolvedValue({ data: { status: 'launched', runner_online: true } } as any);
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

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const show = async (element: React.ReactElement) => {
  await act(async () => {
    root.render(element);
  });
  await flush();
};

const openStreams = () => MockEventSource.all.filter((es) => !es.closed);

// What the panel shows, the composer's draft left out.
const shown = () => {
  const copy = container.cloneNode(true) as HTMLElement;
  copy.querySelectorAll('textarea').forEach((node) => node.remove());
  return copy.textContent ?? '';
};

const button = (label: string): HTMLButtonElement => {
  const found = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === label);
  if (!found) throw new Error(`no button labelled ${label}`);
  return found;
};

const click = async (node: HTMLElement) => {
  await act(async () => {
    node.click();
  });
  await flush();
};

const type = async (value: string) => {
  const node = container.querySelector('textarea')!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event('input', { bubbles: true }));
  });
};

describe('GuidedChatPanel #379 fixes', () => {
  it('renders a link in a reply with its own attributes only (bug 209)', async () => {
    transcripts['gs-1'] = [message('m-1', 'gs-1', 'assistant', 'See [the guide](https://openv.app/manual).')];
    await show(<GuidedChatPanel sessionId="gs-1" step={2} />);
    const link = container.querySelector('a[href="https://openv.app/manual"]');
    expect(link, 'the link').toBeTruthy();
    expect(link!.getAttributeNames().sort()).toEqual(['href', 'rel', 'target']);
  });

  it('opens the stream after a failed transcript load, keeping the error and kicking nothing off (bug 210)', async () => {
    transcripts['gs-1'] = new Error('500');
    await show(<GuidedChatPanel sessionId="gs-1" step={2} />);
    expect(shown()).toContain(LOAD_FAILED);
    expect(openStreams().map((es) => es.url)).toEqual(['/stream/gs-1']);
    expect(api.kickoffChat).not.toHaveBeenCalled();

    await act(async () => {
      openStreams()[0].emit('message', message('m-1', 'gs-1', 'assistant', 'A reply on the stream.'));
    });
    expect(shown()).toContain('A reply on the stream.');
    expect(shown()).toContain(LOAD_FAILED);
  });

  it('does not say the assistant will join while it says no runner is connected (bug 211)', async () => {
    api.kickoffChat.mockResolvedValue({ data: { status: 'pending', runner_online: false } } as any);
    await show(<GuidedChatPanel sessionId="gs-1" step={2} />);
    expect(shown()).toContain(OFFLINE);
    expect(shown()).not.toContain(WAITING);
  });

  it('shows a suggestion of an unknown kind read-only and leaves it out of Apply all (bug 212)', async () => {
    transcripts['gs-1'] = [
      message(
        'm-1',
        'gs-1',
        'assistant',
        [
          'Three ideas.',
          card({ kind: 'persona', name: 'Sam the Shop Lead', role: 'Supervisor' }),
          card({ kind: 'risk', text: 'Coolant mist hides the lamp' }),
          card({ kind: 'hazard', hazard: 'A spindle restarts after a power dip', harm: 'Crushed fingers' }),
        ].join('\n\n')
      ),
    ];
    const apply = vi.fn(async (items: { suggestion: CopilotSuggestion; key: string }[]) => items.map(() => null));
    await show(<GuidedChatPanel sessionId="gs-1" step={2} onApplySuggestions={apply} />);

    // Its kind and a summary, not its JSON, and nothing to click.
    expect(shown()).not.toContain('"kind"');
    const title = Array.from(container.querySelectorAll('div')).find(
      (node) => (node.textContent ?? '').trim() === 'Coolant mist hides the lamp'
    );
    expect(title, 'the unknown card by its summary').toBeTruthy();
    const risk = title!.parentElement!;
    expect(risk.textContent).toContain('risk');
    expect(risk.querySelector('button')).toBeNull();

    // Apply all counts and sends the two cards it knows.
    await click(button('Apply all (2)'));
    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply.mock.calls[0][0].map((item) => [item.key, item.suggestion.kind])).toEqual([
      ['m-1:1', 'persona'],
      ['m-1:5', 'hazard'],
    ]);
  });

  describe("a session change clears the previous session's state (bug 213)", () => {
    // The next session's transcript holds only a person's message: it is not
    // kicked off, and no assistant reply clears anything on its own.
    beforeEach(() => {
      transcripts['gs-2'] = [message('m-2', 'gs-2', 'user', 'The second session.')];
    });

    it('its load error', async () => {
      transcripts['gs-1'] = new Error('500');
      await show(<GuidedChatPanel sessionId="gs-1" step={2} />);
      expect(shown()).toContain(LOAD_FAILED);

      await show(<GuidedChatPanel sessionId="gs-2" step={2} />);
      expect(shown()).toContain('The second session.');
      expect(shown()).not.toContain(LOAD_FAILED);
    });

    it('its thinking indicator', async () => {
      await show(<GuidedChatPanel sessionId="gs-1" step={2} />);
      expect(shown()).toContain(THINKING);

      await show(<GuidedChatPanel sessionId="gs-2" step={2} />);
      expect(shown()).toContain('The second session.');
      expect(shown()).not.toContain(THINKING);
    });

    it('its not-connected notice', async () => {
      api.kickoffChat.mockResolvedValue({ data: { status: 'pending', runner_online: false } } as any);
      await show(<GuidedChatPanel sessionId="gs-1" step={2} />);
      expect(shown()).toContain(OFFLINE);

      await show(<GuidedChatPanel sessionId="gs-2" step={2} />);
      expect(shown()).toContain('The second session.');
      expect(shown()).not.toContain(OFFLINE);
    });
  });

  describe('a message sent before a session change (bug 217)', () => {
    // Neither session is empty, so neither is kicked off.
    beforeEach(() => {
      transcripts['gs-1'] = [message('m-1', 'gs-1', 'user', 'The first session.')];
      transcripts['gs-2'] = [message('m-2', 'gs-2', 'user', 'The second session.')];
    });

    // Sends "Hello?" in gs-1, moves to gs-2, then lets the send's answer in.
    const sendThenMove = async (answer: () => Promise<unknown>) => {
      let release: () => void = () => {};
      api.sendMessage.mockImplementation((() =>
        new Promise<void>((resolve) => {
          release = resolve;
        }).then(answer)) as any);
      await show(<GuidedChatPanel sessionId="gs-1" step={2} />);
      await type('Hello?');
      await click(button('Send'));
      expect(api.sendMessage).toHaveBeenCalledWith('gs-1', 'Hello?', 2, {}, undefined);

      await show(<GuidedChatPanel sessionId="gs-2" step={2} />);
      expect(shown()).toContain('The second session.');
      await act(async () => {
        release();
      });
      await flush();
    };

    it('does not land its echo, or what it says about the runner, in the new session', async () => {
      await sendThenMove(async () => ({
        data: { message: message('u-1', 'gs-1', 'user', 'Hello?'), runner_online: false },
      }));
      expect(shown()).toContain('The second session.');
      expect(shown()).not.toContain('Hello?');
      expect(shown()).not.toContain(OFFLINE);
    });

    it('does not report its failure in the new session', async () => {
      await sendThenMove(async () => {
        throw new Error('network down');
      });
      expect(shown()).toContain('The second session.');
      expect(shown()).not.toContain('Message failed to send');
    });
  });
});
