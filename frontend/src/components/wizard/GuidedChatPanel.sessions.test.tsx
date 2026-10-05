import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { GuidedChatPanel } from './GuidedChatPanel';
import { guidedAPI } from '../../api/client';

// The assistant panel moving between sessions while it stays mounted, with
// each session's transcript answering only when the test says so. A
// session's stream opens once its own transcript has loaded, and an empty
// session is kicked off once its stream is open. A transcript that arrives
// after the panel has moved on opens nothing and shows nothing. Going back
// to a session before the other one has loaded waits for that session's
// transcript again. GuidedChatPanel.snapshot.test.tsx pins one session
// change with transcripts that answer at once. This file pins the waits in
// between, which X15c splits between the panel's transcript effect,
// hooks/useEventStream and the kickoff.

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

// What the panel did, in order: each transcript it asked for, each stream it
// opened or closed, each kickoff.
let calls: string[] = [];

// An EventSource the test drives by hand, which logs its opening and closing.
class MockEventSource {
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onopen: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: MessageEvent) => void) | null = null;
  closed = false;

  constructor(public url: string) {
    calls.push(`open ${url}`);
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  }
  close() {
    calls.push(`close ${this.url}`);
    this.closed = true;
  }
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = MockEventSource as any;

const message = (id: string, sessionId: string, content: string) => ({
  id,
  session_id: sessionId,
  role: 'assistant',
  content,
  created_at: '2026-10-05T10:00:00Z',
});

// Each transcript request waits here, oldest first, until the test answers it.
let loads: { sessionId: string; answer: (transcript: unknown[]) => void }[] = [];

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  calls = [];
  loads = [];
  api.listMessages.mockImplementation(((sessionId: string) => {
    calls.push(`listMessages ${sessionId}`);
    return new Promise((resolve) => {
      loads.push({ sessionId, answer: (transcript) => resolve({ data: transcript }) });
    });
  }) as any);
  api.kickoffChat.mockImplementation(((sessionId: string) => {
    calls.push(`kickoffChat ${sessionId}`);
    return Promise.resolve({ data: { status: 'launched', runner_online: true } });
  }) as any);
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

const show = async (sessionId: string) => {
  await act(async () => {
    root.render(<GuidedChatPanel sessionId={sessionId} step={2} />);
  });
};

/** Answers the oldest transcript request still waiting for this session. */
const answer = async (sessionId: string, transcript: unknown[]) => {
  const i = loads.findIndex((l) => l.sessionId === sessionId);
  if (i < 0) throw new Error(`no transcript request for ${sessionId} is waiting`);
  const [load] = loads.splice(i, 1);
  await act(async () => {
    load.answer(transcript);
  });
};

describe('GuidedChatPanel moving between sessions', () => {
  it("opens a session's stream once its transcript has loaded, then kicks off an empty one", async () => {
    await show('gs-1');
    expect(calls).toEqual(['listMessages gs-1']);
    await answer('gs-1', []);
    expect(calls).toEqual(['listMessages gs-1', 'open /stream/gs-1', 'kickoffChat gs-1']);

    await show('gs-2');
    expect(calls.slice(3)).toEqual(['close /stream/gs-1', 'listMessages gs-2']);
    await answer('gs-2', []);
    expect(calls.slice(3)).toEqual([
      'close /stream/gs-1',
      'listMessages gs-2',
      'open /stream/gs-2',
      'kickoffChat gs-2',
    ]);
  });

  it('opens nothing and shows nothing for a transcript that arrives after the panel moved on', async () => {
    await show('gs-1');
    await show('gs-2');
    expect(calls).toEqual(['listMessages gs-1', 'listMessages gs-2']);

    await answer('gs-1', [message('m-1', 'gs-1', 'From the first session.')]);
    expect(calls).toEqual(['listMessages gs-1', 'listMessages gs-2']);
    expect(container.textContent).not.toContain('From the first session.');

    await answer('gs-2', [message('m-2', 'gs-2', 'From the second session.')]);
    expect(calls).toEqual(['listMessages gs-1', 'listMessages gs-2', 'open /stream/gs-2']);
    expect(container.textContent).toContain('From the second session.');
    expect(container.textContent).not.toContain('From the first session.');
  });

  it('waits for its transcript again when it goes back to a session before the other one has loaded', async () => {
    await show('gs-1');
    await answer('gs-1', []);
    expect(calls).toEqual(['listMessages gs-1', 'open /stream/gs-1', 'kickoffChat gs-1']);

    await show('gs-2');
    await show('gs-1');
    expect(calls.slice(3)).toEqual(['close /stream/gs-1', 'listMessages gs-2', 'listMessages gs-1']);

    // The second session's transcript arrives late: nothing opens.
    await answer('gs-2', [message('m-2', 'gs-2', 'From the second session.')]);
    expect(calls.slice(3)).toEqual(['close /stream/gs-1', 'listMessages gs-2', 'listMessages gs-1']);
    expect(container.textContent).not.toContain('From the second session.');

    // The first session has a message now, so it is not kicked off again.
    await answer('gs-1', [message('m-1', 'gs-1', 'Welcome back.')]);
    expect(calls.slice(3)).toEqual([
      'close /stream/gs-1',
      'listMessages gs-2',
      'listMessages gs-1',
      'open /stream/gs-1',
    ]);
    expect(container.textContent).toContain('Welcome back.');
  });

  it('kicks off an empty session again when the panel comes back to it', async () => {
    await show('gs-1');
    await answer('gs-1', []);
    await show('gs-2');
    await answer('gs-2', [message('m-2', 'gs-2', 'From the second session.')]);
    await show('gs-1');
    expect(calls).toEqual([
      'listMessages gs-1',
      'open /stream/gs-1',
      'kickoffChat gs-1',
      'close /stream/gs-1',
      'listMessages gs-2',
      'open /stream/gs-2',
      'close /stream/gs-2',
      'listMessages gs-1',
    ]);

    await answer('gs-1', []);
    expect(calls.slice(8)).toEqual(['open /stream/gs-1', 'kickoffChat gs-1']);
  });
});
