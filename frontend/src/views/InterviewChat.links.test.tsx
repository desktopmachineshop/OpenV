import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { InterviewChat } from './InterviewChat';
import { publicInterviewAPI } from '../api/client';

// The public interview page stays mounted when its invite link changes (back
// or forward between two links). The old link's stream closes at once,
// before the new link's intro is asked for; the new link's stream opens only
// once its own intro is in and reports the session open, and not at all for
// a finished session or a broken link. Going back to a link before the other
// link's intro is in waits for that link's intro again, and the other's late
// answer opens nothing. The reconnect test (#379 bug 203) pins a retry
// pending across one change, with intros that answer at once, and S16j one
// link per mount; this pins the waits in between, which X15d moves onto
// useEventStream.

// The page reads its token from the route; the test moves it between renders.
const route = vi.hoisted(() => ({ token: 'tok-1' }));

vi.mock('react-router-dom', () => ({
  useParams: () => ({ token: route.token }),
}));

vi.mock('../components/ui', () => ({
  useConfirm: () => () => Promise.resolve(true),
}));

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    publicInterviewAPI: { streamUrl: (token: string) => `/public/interviews/${token}/stream` },
  })
);

const api = vi.mocked(publicInterviewAPI);

// What the page did, in order: each intro it asked for, each stream it
// opened and each close of one.
let log: string[] = [];

// An EventSource the test only watches open and close.
class MockEventSource {
  static all: MockEventSource[] = [];
  onopen: ((e: MessageEvent) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: MessageEvent) => void) | null = null;
  closed = false;

  constructor(
    public url: string,
    public init?: EventSourceInit
  ) {
    log.push(`open ${url}`);
    MockEventSource.all.push(this);
  }
  addEventListener() {}
  close() {
    log.push(`close ${this.url}`);
    this.closed = true;
  }
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = MockEventSource as any;

// Each intro the page asks for, in order, answered when the test says.
let intros: { token: string; resolve: (res: unknown) => void; reject: (err: unknown) => void }[] = [];

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  log = [];
  intros = [];
  MockEventSource.all = [];
  api.intro.mockImplementation(
    (token: string) =>
      new Promise((resolve, reject) => {
        log.push(`intro ${token}`);
        intros.push({ token, resolve, reject });
      }) as any
  );
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

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

const render = async (token: string) => {
  route.token = token;
  await act(async () => {
    root.render(<InterviewChat />);
    await settle();
  });
};

/** The i-th intro answers, with a session of the given status. */
const answer = async (i: number, status = 'active') => {
  await act(async () => {
    intros[i].resolve({
      data: {
        interview_name: `Interview ${intros[i].token}`,
        transcript: [],
        session: { participant_name: 'Pat', status },
      },
    });
    await settle();
  });
};

/** The i-th intro fails, as an expired invite does. */
const fail = async (i: number) => {
  await act(async () => {
    intros[i].reject(
      Object.assign(new Error('Request failed with status code 404'), {
        response: { status: 404, data: { error: 'invite has expired' } },
      })
    );
    await settle();
  });
};

const open = () => MockEventSource.all.filter((es) => !es.closed).map((es) => es.url);

const ONE = '/public/interviews/tok-1/stream';
const TWO = '/public/interviews/tok-2/stream';

describe('InterviewChat moving between invite links', () => {
  it("closes the old link's stream before asking the new link's intro, and opens the new one once that intro is in", async () => {
    await render('tok-1');
    expect(log).toEqual(['intro tok-1']);
    await answer(0);
    expect(log).toEqual(['intro tok-1', `open ${ONE}`]);

    await render('tok-2');
    expect(log).toEqual(['intro tok-1', `open ${ONE}`, `close ${ONE}`, 'intro tok-2']);
    expect(open()).toEqual([]);

    await answer(1);
    expect(log).toEqual(['intro tok-1', `open ${ONE}`, `close ${ONE}`, 'intro tok-2', `open ${TWO}`]);
    expect(open()).toEqual([TWO]);
    expect(MockEventSource.all.map((es) => es.init)).toEqual([{ withCredentials: false }, { withCredentials: false }]);
    expect(container.textContent).toContain('Interview tok-2');
  });

  it("going back before the other link's intro is in waits for this link's intro again", async () => {
    await render('tok-1');
    await answer(0);
    await render('tok-2');
    await render('tok-1');
    expect(log).toEqual(['intro tok-1', `open ${ONE}`, `close ${ONE}`, 'intro tok-2', 'intro tok-1']);
    expect(open()).toEqual([]);

    // The other link's intro, answering late, opens nothing.
    await answer(1);
    expect(open()).toEqual([]);
    expect(container.textContent).not.toContain('Interview tok-2');

    await answer(2);
    expect(log).toEqual(['intro tok-1', `open ${ONE}`, `close ${ONE}`, 'intro tok-2', 'intro tok-1', `open ${ONE}`]);
    expect(open()).toEqual([ONE]);
  });

  it('opens no stream for a new link whose session is over, or whose intro fails', async () => {
    await render('tok-1');
    await answer(0);

    await render('tok-2');
    await answer(1, 'completed');
    expect(container.textContent).toContain('Thank you!');
    expect(open()).toEqual([]);

    await render('tok-3');
    await fail(2);
    expect(container.textContent).toContain("This link isn't working");
    expect(open()).toEqual([]);

    expect(log).toEqual(['intro tok-1', `open ${ONE}`, `close ${ONE}`, 'intro tok-2', 'intro tok-3']);
  });
});
