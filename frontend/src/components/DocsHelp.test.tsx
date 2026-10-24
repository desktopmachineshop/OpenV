import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import type { HelpTopic } from '../api/client';

// The help panel's content from the user manual on OpenD: the page's topic,
// links between topics opening in the panel with Back to return, search,
// and handing back to the app's own help when the docs cannot answer.

const topic = (id: string, title: string, extra: Partial<HelpTopic> = {}): HelpTopic => ({
  topic_id: id,
  title,
  shortdesc: `${title} in short.`,
  html: `<article><p>Body of ${title}. See <a href="https://docs.example/topics/vv/gaps.html" data-help-topic="topics/vv/gaps">gaps</a>.</p></article>`,
  related: [{ topic_id: 'topics/vv/baselines', title: 'Baselines' }],
  full_url: `https://docs.example/${id}.html`,
  line: '0.23',
  ...extra,
});

const context = vi.fn();
const byId = vi.fn();
const search = vi.fn();

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    helpAPI: {
      context: (...args) => context(...args),
      topic: (...args) => byId(...args),
      search: (...args) => search(...args),
    },
  }),
);

import { DocsHelp } from './DocsHelp';

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const flush = () => act(async () => {
  await new Promise((r) => setTimeout(r, 0));
});

describe('DocsHelp', () => {
  let container: HTMLDivElement;
  let root: Root;
  const unavailable = vi.fn();

  beforeEach(() => {
    context.mockReset();
    byId.mockReset();
    search.mockReset();
    unavailable.mockReset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const render = async (path = '/projects/p1/vv') => {
    act(() => {
      root.render(<DocsHelp orgId="org-1" path={path} onUnavailable={unavailable} />);
    });
    await flush();
  };

  it('shows the topic for the page, with its docs line and full page', async () => {
    context.mockResolvedValue({ data: topic('topics/vv/index', 'V&V') });
    await render();
    expect(context).toHaveBeenCalledWith('org-1', { path: '/projects/p1/vv' });
    expect(container.querySelector('h3')?.textContent).toBe('V&V');
    expect(container.textContent).toContain('Manual for OpenV 0.23');
    expect(container.querySelector('.help-docs-footer a')?.getAttribute('href')).toBe('https://docs.example/topics/vv/index.html');
  });

  it('opens a linked topic in the panel, and Back returns', async () => {
    context.mockResolvedValue({ data: topic('topics/vv/index', 'V&V') });
    byId.mockResolvedValue({ data: topic('topics/vv/gaps', 'Gaps') });
    await render();
    const link = container.querySelector('a[data-help-topic]') as HTMLAnchorElement;
    await act(async () => {
      link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 }));
    });
    await flush();
    expect(byId).toHaveBeenCalledWith('org-1', 'topics/vv/gaps');
    expect(container.querySelector('h3')?.textContent).toBe('Gaps');
    const back = container.querySelector('.help-docs-back') as HTMLButtonElement;
    act(() => back.click());
    expect(container.querySelector('h3')?.textContent).toBe('V&V');
  });

  it('searches the manual and opens a hit', async () => {
    context.mockResolvedValue({ data: topic('topics/vv/index', 'V&V') });
    search.mockResolvedValue({ data: { query: 'gap', line: '0.23', hits: [{ topic_id: 'topics/vv/gaps', title: 'Gaps', snippet: '…gaps…' }] } });
    byId.mockResolvedValue({ data: topic('topics/vv/gaps', 'Gaps') });
    await render();
    const input = container.querySelector('input[type="search"]') as HTMLInputElement;
    act(() => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      setter.call(input, 'gap');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => {
      (container.querySelector('form') as HTMLFormElement).dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    await flush();
    expect(search).toHaveBeenCalledWith('org-1', 'gap');
    const hit = container.querySelector('.help-docs-list .help-docs-link') as HTMLButtonElement;
    expect(hit.textContent).toBe('Gaps');
    act(() => hit.click());
    await flush();
    expect(container.querySelector('h3')?.textContent).toBe('Gaps');
  });

  it('says when the manual has no topic for the page', async () => {
    context.mockRejectedValue({ response: { status: 404, data: { error: 'no help for this' } } });
    await render();
    expect(container.textContent).toContain('no topic for this page yet');
    expect(unavailable).not.toHaveBeenCalled();
  });

  it('hands back to the app’s own help when the docs cannot answer', async () => {
    context.mockRejectedValue({ response: { status: 502, data: { error: 'the help could not be loaded' } } });
    await render();
    expect(unavailable).toHaveBeenCalled();
  });
});
