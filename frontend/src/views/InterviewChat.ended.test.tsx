import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { InterviewChat } from './InterviewChat';
import { publicInterviewAPI } from '../api/client';

// An interview that ends while its page is open, from another tab or
// device (#379 bug 216). The server refuses the ended link's answers and its
// stream, 409, rather than start a new session on the same invite. A
// refused answer takes the page to its thank-you. A refused stream cannot
// be read as such (an EventSource sees no status), so at each drop the page
// asks the intro again before the retry: a completed session takes it to
// its thank-you and cancels the retry; an open one, or no answer, lets the
// retry go ahead.

vi.mock('react-router-dom', () => ({
  useParams: () => ({ token: 'tok-1' }),
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

// An EventSource the test drives by hand.
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
    MockEventSource.all.push(this);
  }
  addEventListener() {}
  close() {
    this.closed = true;
  }
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = MockEventSource as any;

const ENDED = 'This interview has ended — thank you.';
const THANKS = '🙏Thank you!Your feedback has been recorded. You can close this page now.';

const intro = (status: string) => ({
  data: {
    interview_name: 'Pump maintenance',
    transcript: status === 'active' ? [] : null,
    session: { participant_name: 'Pat', status },
  },
});

// An error as axios rejects with it: the server's JSON body on .response.
const httpError = (status: number, error: string) =>
  Object.assign(new Error(`Request failed with status code ${status}`), { response: { status, data: { error } } });

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  vi.useFakeTimers();
  MockEventSource.all = [];
  api.intro.mockResolvedValue(intro('active') as any);
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
    root.render(<InterviewChat />);
  });
};

const open = () => MockEventSource.all.filter((es) => !es.closed);

/** The open stream drops: EventSource fires `error`. */
const drop = async () => {
  const [es] = open();
  expect(es, 'an open stream').toBeTruthy();
  await act(async () => {
    es.onerror?.({ type: 'error' } as MessageEvent);
  });
};

const tick = async (ms: number) => {
  await act(async () => {
    vi.advanceTimersByTime(ms);
  });
};

const send = async (content: string) => {
  const composer = container.querySelector('textarea')!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(composer, content);
    composer.dispatchEvent(new Event('input', { bubbles: true }));
  });
  const button = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Send')!;
  await act(async () => {
    button.click();
  });
};

describe('InterviewChat once its interview has ended elsewhere (#379 bug 216)', () => {
  it('takes an answer the server refuses as ended (409) to the thank-you page, closing the stream', async () => {
    await render();
    expect(open()).toHaveLength(1);
    api.sendMessage.mockRejectedValue(httpError(409, ENDED));

    await send('One more thing: metric units.');
    expect(api.sendMessage).toHaveBeenCalledWith('tok-1', 'One more thing: metric units.', 'Pat');
    expect(container.textContent).toBe(THANKS);
    expect(open()).toHaveLength(0);
    await tick(60000);
    expect(MockEventSource.all).toHaveLength(1);
  });

  it('asks the intro again at a drop; a completed session shows the thank-you page and cancels the retry', async () => {
    await render();
    expect(api.intro).toHaveBeenCalledTimes(1);
    api.intro.mockResolvedValue(intro('completed') as any);

    await drop();
    expect(api.intro).toHaveBeenCalledTimes(2);
    expect(api.intro).toHaveBeenLastCalledWith('tok-1');
    expect(container.textContent).toBe(THANKS);
    await tick(60000);
    expect(MockEventSource.all).toHaveLength(1);
    expect(open()).toHaveLength(0);
    expect(api.intro).toHaveBeenCalledTimes(2);
  });

  it('retries as before when the intro asked again finds the session open, or does not answer', async () => {
    await render();
    const page = container.innerHTML;

    await drop();
    expect(api.intro).toHaveBeenCalledTimes(2);
    expect(container.innerHTML).toBe(page);
    await tick(2000);
    expect(open()).toHaveLength(1);

    api.intro.mockRejectedValue(new Error('Network Error'));
    await drop();
    expect(api.intro).toHaveBeenCalledTimes(3);
    expect(container.innerHTML).toBe(page);
    await tick(4000);
    expect(open()).toHaveLength(1);
    expect(MockEventSource.all).toHaveLength(3);
  });
});
