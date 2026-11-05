import { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { GuidedChatPanel } from './GuidedChatPanel';
import { guidedAPI } from '../../api/client';

// A turn another page asks the floating assistant to start (#726, #728):
// sent once the transcript has loaded, in place of the empty conversation's
// kickoff, exactly once, and decorated with the page it came from.

vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    guidedAPI: { chatStreamUrl: (id: string) => `/stream/${id}` },
  })
);

class MockEventSource {
  constructor(public url: string) {}
  addEventListener() {}
  close() {}
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = MockEventSource as any;

const api = vi.mocked(guidedAPI);
let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  api.listMessages.mockResolvedValue({ data: [] } as any);
  api.kickoffChat.mockResolvedValue({ data: { status: 'launched', runner_online: true } } as any);
  api.sendMessage.mockResolvedValue({ data: { message: { id: 'u1', role: 'user', content: 'x' }, runner_online: true } } as any);
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  (Element.prototype as any).scrollTo = vi.fn();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const flush = () =>
  act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });

describe('GuidedChatPanel autoSend', () => {
  it('sends the request once the transcript is in, instead of the kickoff, and only once', async () => {
    const onAutoSent = vi.fn();
    const request = { id: 7, message: 'Drafting test cases for 1 gap: REQ-1.' };
    await act(async () => {
      root.render(
        <GuidedChatPanel sessionId="gs1" applyTarget="project" embedded autoSend={request} onAutoSent={onAutoSent} />
      );
    });
    await flush();
    expect(api.sendMessage).toHaveBeenCalledTimes(1);
    expect(api.sendMessage.mock.calls[0][1]).toBe(request.message);
    expect(api.kickoffChat).not.toHaveBeenCalled();
    expect(onAutoSent).toHaveBeenCalledWith(7);

    await act(async () => {
      root.render(
        <GuidedChatPanel sessionId="gs1" applyTarget="project" embedded autoSend={request} onAutoSent={onAutoSent} />
      );
    });
    await flush();
    expect(api.sendMessage).toHaveBeenCalledTimes(1);
  });

  it('decorates what is sent, and offers project quick actions beside the project', async () => {
    await act(async () => {
      root.render(
        <GuidedChatPanel
          sessionId="gs1"
          applyTarget="project"
          embedded
          hideHeader
          decorateMessage={(m) => `Looking at: Board\n\n${m}`}
        />
      );
    });
    await flush();
    expect(container.textContent).not.toContain('Review step');
    const draft = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Draft tests')!;
    await act(async () => draft.click());
    await flush();
    expect(String(api.sendMessage.mock.calls[0][1])).toMatch(/^Looking at: Board\n\nFind the requirements/);
  });
});
