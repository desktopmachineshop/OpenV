import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { CloudRunnerCard } from './CloudRunnerCard';
import { cloudRunnerAPI } from '../../api/client';

// The card's dependencies, mocked down to what the pool indicator needs.
jest.mock('../../api/client', () => ({
  cloudRunnerAPI: { get: jest.fn(), start: jest.fn(), extend: jest.fn(), end: jest.fn() },
}));
jest.mock('../ui', () => ({
  ErrorBanner: ({ message }: any) =>
    message ? require('react').createElement('div', { role: 'alert' }, message) : null,
  useConfirm: () => () => Promise.resolve(true),
}));
jest.mock('../../hooks/useViewport', () => ({
  useViewport: () => ({ isPhone: mockIsPhone, isCompact: false }),
}));

// Factory-referenced, so it has to be named mock*.
let mockIsPhone = false;

const api = cloudRunnerAPI as jest.Mocked<typeof cloudRunnerAPI>;

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  mockIsPhone = false;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const render = async (payload: any) => {
  api.get.mockResolvedValue({ data: payload } as any);
  await act(async () => {
    root.render(<CloudRunnerCard orgId="org-1" />);
  });
  await flush();
};

const idle = (status: string) => ({
  enabled: true,
  session: null,
  pool_load: { status },
});

describe('the cloud runner pool indicator', () => {
  it.each([
    ['green', 'Runners available'],
    ['amber', 'Runners busy'],
    ['red', 'All runners in use'],
    ['unavailable', 'No runners online'],
  ])('shows %s as "%s"', async (status, label) => {
    await render(idle(status));
    expect(container.textContent).toContain(label);
  });

  // The point of the change: an account is told how loaded the pool is, not
  // how big it is or how much of it is taken.
  it('never prints how many runners there are', async () => {
    await render(idle('amber'));
    expect(container.textContent).not.toMatch(/\d+\s+of\s+\d+/);
    expect(container.textContent).not.toMatch(/free/i);
  });

  // A phone gets the same band, at reading size under the button rather than
  // as an aside beside it.
  it('shows the band on a phone too', async () => {
    mockIsPhone = true;
    await render(idle('red'));
    expect(container.textContent).toContain('All runners in use');
    expect(container.textContent).not.toMatch(/\d+\s+of\s+\d+/);
  });

  // A full pool answers 503 with the same payload shape. The member is told
  // to come back, without being told the size of the queue.
  it('explains a full pool without figures', async () => {
    api.get.mockResolvedValue({ data: idle('red') } as any);
    api.start.mockRejectedValue({
      response: { data: idle('red') },
    } as any);
    await act(async () => {
      root.render(<CloudRunnerCard orgId="org-1" />);
    });
    await flush();

    const button = container.querySelector('button') as HTMLButtonElement;
    await act(async () => {
      button.click();
    });
    await flush();

    const alert = container.querySelector('[role="alert"]');
    expect(alert?.textContent).toBe(
      'Every cloud runner is in use right now. Try again in a few minutes.'
    );
  });

  // A deployment with no pool at all is a different message from a pool that
  // is merely full, and the card has to keep telling them apart.
  it('distinguishes a deployment with no pool', async () => {
    api.get.mockResolvedValue({ data: idle('unavailable') } as any);
    api.start.mockRejectedValue({ response: { data: idle('unavailable') } } as any);
    await act(async () => {
      root.render(<CloudRunnerCard orgId="org-1" />);
    });
    await flush();

    const button = container.querySelector('button') as HTMLButtonElement;
    await act(async () => {
      button.click();
    });
    await flush();

    expect(container.querySelector('[role="alert"]')?.textContent).toBe(
      'No cloud runners are available on this deployment yet.'
    );
  });
});
