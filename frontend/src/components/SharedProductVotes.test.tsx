import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { sharedProductsAPI } from '../api/client';
import { RandomProduct } from '../utils/randomProduct';
import { TopSharedProducts, voteDisabledReason } from './SharedProductVotes';

// The two pieces of the vote UI that have rules of their own: which products
// can be voted for (and what to say about the ones that cannot), and a
// leaderboard whose fetches race each other when the filter is switched.
jest.mock('../api/client', () => ({
  sharedProductsAPI: { list: jest.fn(), vote: jest.fn(), unvote: jest.fn() },
}));

const api = sharedProductsAPI as jest.Mocked<typeof sharedProductsAPI>;

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const product = (name: string, extra: Partial<RandomProduct> = {}): RandomProduct => ({
  category: 'kitchen appliance',
  name,
  description: `A coffee tin that recognises Kevin and locks.`,
  vision: `${name} becomes the reason the bean jar survives a Tuesday.`,
  problem: 'Beans vanish overnight and nobody admits to owning the grinder.',
  targetUsers: 'office workers whose beans keep leaving with Kevin',
  ...extra,
});

const payload = (id: string, name: string, votes: number, votesWeek: number) => ({
  id,
  category: 'kitchen appliance',
  name,
  description: `${name} recognises Kevin and locks.`,
  vision: `${name} becomes the reason the bean jar survives a Tuesday.`,
  problem: 'Beans vanish overnight and nobody admits to owning the grinder.',
  target_users: 'office workers whose beans keep leaving with Kevin',
  votes,
  votes_week: votesWeek,
  voted: false,
});

describe('voteDisabledReason', () => {
  it('lets a product from the shared pool be voted for', () => {
    expect(voteDisabledReason(product('Kevinproof', { sharedId: 'p1' }), [])).toBe('');
  });

  it('calls a built-in concept what it is, rather than a local invention', () => {
    // Nothing was invented in this browser, so an unshared roll is a built-in
    // — which is most rolls, and the case that used to be mislabelled.
    const reason = voteDisabledReason(product('Kevinproof'), []);

    expect(reason).toContain('built-in example');
    expect(reason).not.toContain('Kept in this browser');
  });

  it('says an invention is kept in this browser when it never reached the pool', () => {
    const mine = product('Gnomecast');
    const reason = voteDisabledReason(mine, [mine]);

    expect(reason).toContain('Kept in this browser');
    expect(reason).not.toContain('built-in example');
  });

  it('still votes for an invention once it has reached the pool', () => {
    const mine = product('Gnomecast', { sharedId: 'p9' });
    expect(voteDisabledReason(mine, [mine])).toBe('');
  });
});

describe('TopSharedProducts', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    jest.clearAllMocks();
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

  const flush = async () => {
    await act(async () => {
      await Promise.resolve();
    });
  };

  it('ignores a slow response that a newer filter has superseded', async () => {
    // Two reads in flight at once, answered in the wrong order: the all-time
    // list is asked for first but replies last. Switching the filter must not
    // leave all-time rows sitting under "this week".
    const deferred = <T,>() => {
      let resolve!: (value: T) => void;
      const promise = new Promise<T>((r) => {
        resolve = r;
      });
      return { promise, resolve };
    };
    const allTime = deferred<any>();
    const thisWeek = deferred<any>();
    api.list.mockImplementation((params?: { sort?: string; limit?: number }) =>
      (params?.sort === 'top_week' ? thisWeek.promise : allTime.promise) as any
    );

    await act(async () => {
      root.render(<TopSharedProducts sort="top" onUse={jest.fn()} />);
    });
    // Switch filters before the first read has answered.
    await act(async () => {
      root.render(<TopSharedProducts sort="top_week" onUse={jest.fn()} />);
    });

    // The newer request answers first...
    await act(async () => {
      thisWeek.resolve({ data: [payload('p2', 'Crustodian', 3, 3)] });
    });
    await flush();
    expect(container.textContent).toContain('Crustodian');

    // ...and the stale one lands afterwards, and must be dropped.
    await act(async () => {
      allTime.resolve({ data: [payload('p1', 'Kevinproof', 9, 0)] });
    });
    await flush();

    expect(container.textContent).toContain('Crustodian');
    expect(container.textContent).not.toContain('Kevinproof');
    expect(
      container.querySelector('[aria-label="Top 5 products this week"]')
    ).toBeTruthy();
  });

  it('drops a superseded failure rather than showing it against the new filter', async () => {
    const allTime = { promise: null as any, reject: (_: any) => {} };
    allTime.promise = new Promise((_, reject) => {
      allTime.reject = reject;
    });
    api.list.mockImplementation((params?: { sort?: string; limit?: number }) =>
      (params?.sort === 'top_week'
        ? Promise.resolve({ data: [payload('p2', 'Crustodian', 3, 3)] })
        : allTime.promise) as any
    );

    await act(async () => {
      root.render(<TopSharedProducts sort="top" onUse={jest.fn()} />);
    });
    await act(async () => {
      root.render(<TopSharedProducts sort="top_week" onUse={jest.fn()} />);
    });
    await flush();

    await act(async () => {
      allTime.reject(new Error('too slow'));
    });
    await flush();

    expect(container.textContent).toContain('Crustodian');
    expect(container.textContent).not.toContain('Could not load the standings');
  });
});
