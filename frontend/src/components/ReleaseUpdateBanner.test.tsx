import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ReleaseUpdateBanner, RELEASE_POLL_MS } from './ReleaseUpdateBanner';
import { releaseAPI } from '../api/client';

jest.mock('../api/client', () => ({
  releaseAPI: { current: jest.fn() },
}));

jest.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const current = releaseAPI.current as jest.Mock;

const answer = (version: string) =>
  Promise.resolve({ data: { version, date: version, notes: [], markdown: '', history: '' } });

describe('ReleaseUpdateBanner', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    jest.useFakeTimers();
    current.mockReset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    jest.useRealTimers();
  });

  const mount = async () => {
    await act(async () => {
      root.render(<ReleaseUpdateBanner />);
    });
  };

  it('stays hidden while the release behind the tab is the one it loaded with', async () => {
    current.mockImplementation(() => answer('2026-09-12'));
    await mount();
    await act(async () => {
      jest.advanceTimersByTime(RELEASE_POLL_MS);
    });
    expect(current).toHaveBeenCalledTimes(2);
    expect(container.textContent).toBe('');
  });

  it('offers a reload once a poll reports a newer release', async () => {
    current.mockImplementationOnce(() => answer('2026-09-12')).mockImplementation(() => answer('2026-09-13'));
    await mount();
    expect(container.textContent).toBe('');
    await act(async () => {
      jest.advanceTimersByTime(RELEASE_POLL_MS);
    });
    expect(container.textContent).toContain('OpenV has been updated (2026-09-13)');
    expect(container.querySelector('a')?.getAttribute('href')).toBe('/whats-new');
    expect(container.querySelector('button')?.textContent).toBe('Reload');
  });

  it('checks again when the tab comes back into view', async () => {
    current.mockImplementation(() => answer('2026-09-12'));
    await mount();
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
    });
    expect(current).toHaveBeenCalledTimes(2);
  });
});
