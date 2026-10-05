import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useNavigate, type NavigateFunction } from 'react-router-dom';
import { mockApi } from './test/mockApi';
import App from './App';
import { publicInterviewAPI } from './api/client';

// The public interview page at /interview/:token, as App routes to it, when
// its invite link changes on a mounted page (back or forward between two
// links, or a link opened from another): the page starts afresh on the new
// link. Before #379 bug 219 it kept the previous link's interview, its
// conversation and a half-typed answer included, on show while the new
// link's intro loaded, and the answer in the composer after it.

vi.mock('./api/client', async (orig) =>
  mockApi(await orig(), {
    publicInterviewAPI: { streamUrl: (token: string) => `/public/interviews/${token}/stream` },
  })
);

const api = vi.mocked(publicInterviewAPI);

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
    MockEventSource.all.push(this);
  }
  addEventListener() {}
  close() {
    this.closed = true;
  }
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
const realEventSource = (globalThis as any).EventSource;

// Each intro the page asks for, answered when the test says.
let intros: { token: string; resolve: (res: unknown) => void }[] = [];
let navigate: NavigateFunction = () => {};
const Navigator: React.FC = () => {
  navigate = useNavigate();
  return null;
};

let container: HTMLDivElement;
let root: Root;
const realPath = window.location.pathname;

beforeEach(() => {
  vi.clearAllMocks();
  intros = [];
  MockEventSource.all = [];
  (globalThis as any).EventSource = MockEventSource;
  api.intro.mockImplementation(
    (token: string) => new Promise((resolve) => intros.push({ token, resolve })) as any
  );
  // App asks for no session on an interview page: it reads the browser's path.
  window.history.replaceState(null, '', '/interview/tok-1');
  // jsdom lays nothing out, so it has no scrollIntoView.
  (Element.prototype as any).scrollIntoView = vi.fn();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  window.history.replaceState(null, '', realPath);
  (globalThis as any).EventSource = realEventSource;
});

const settle = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const answer = async (i: number, data: unknown) => {
  await act(async () => {
    intros[i].resolve({ data });
  });
  await settle();
};

const go = async (path: string) => {
  await act(async () => {
    navigate(path);
  });
  await settle();
};

const composer = () => container.querySelector('textarea')!;
const open = () => MockEventSource.all.filter((es) => !es.closed).map((es) => es.url);

describe('App moving the interview page between invite links (#379 bug 219)', () => {
  it("shows nothing of the previous link's interview while the new link loads, nor after", async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/interview/tok-1']}>
          <Navigator />
          <App />
        </MemoryRouter>
      );
    });
    await settle();
    await answer(0, {
      interview_name: 'Pump maintenance',
      session: { participant_name: 'Pat', status: 'active' },
      transcript: [
        { id: 'm-1', session_id: 's-1', role: 'assistant', content: 'What does a normal week look like?' },
        { id: 'm-2', session_id: 's-1', role: 'participant', content: 'We check the pumps every Monday.' },
      ],
    });
    expect(container.textContent).toContain('We check the pumps every Monday.');
    expect(open()).toEqual(['/public/interviews/tok-1/stream']);
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(composer(), 'Half an answer');
      composer().dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(composer().value).toBe('Half an answer');

    await go('/interview/tok-2');
    expect(intros.map((x) => x.token)).toEqual(['tok-1', 'tok-2']);
    expect(container.textContent).toBe('Loading interview…');
    expect(open()).toEqual([]);

    await answer(1, {
      interview_name: 'Coolant audit',
      session: { participant_name: 'Lee', status: 'active' },
      transcript: [],
    });
    expect(container.querySelector('header')!.textContent).toBe('Coolant auditInterviewing LeeEnd interview');
    expect(container.textContent).not.toContain('Pump maintenance');
    expect(container.textContent).not.toContain('We check the pumps every Monday.');
    expect(composer().value).toBe('');
    expect(open()).toEqual(['/public/interviews/tok-2/stream']);
  });
});
