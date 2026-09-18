import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { ReviewQueue } from './ReviewQueue';
import { linkAPI, reviewAPI } from '../api/client';

vi.mock('../api/client', () => ({
  reviewAPI: { get: vi.fn(), startRound: vi.fn() },
  linkAPI: { confirm: vi.fn() },
}));

// The page reads the active project from the store when the route has none.
vi.mock('../state/store', () => ({ useAppStore: (sel: any) => sel({ projectId: 'p1' }) }));

let roundOn = true;
vi.mock('../hooks/useFeature', () => ({
  useFeature: (key: string) => key === 'project-review-round' && roundOn,
}));

// The round asks before it runs; the answer is the test's to give.
let confirmAnswer = true;
vi.mock('../components/ui', async () => {
  const actual = await vi.importActual<any>('../components/ui');
  return { ...actual, useConfirm: () => async () => confirmAnswer };
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const get = vi.mocked(reviewAPI.get, { partial: true, deep: true });
const startRound = vi.mocked(reviewAPI.startRound, { partial: true, deep: true });

describe('ReviewQueue project review round', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    roundOn = true;
    confirmAnswer = true;
    get.mockReset();
    startRound.mockReset();
    vi.mocked(linkAPI.confirm, { partial: true, deep: true }).mockReset();
    get.mockResolvedValue({ data: { suspect_links: [], in_review_artifacts: [] } } as any);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const mount = async () => {
    await act(async () => {
      root.render(
        <MemoryRouter>
          <ReviewQueue />
        </MemoryRouter>,
      );
    });
    await act(async () => {
      await Promise.resolve();
    });
  };

  const roundButton = () =>
    Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('Send project for review'),
    );

  it('starts a round and reports what it did', async () => {
    startRound.mockResolvedValue({
      data: {
        moved: [{ id: 'a1' }, { id: 'a2' }],
        already_in_review: 1,
        approved: 7,
        superseded: 0,
        out_of_scope: 3,
        types: ['requirement'],
      },
    } as any);
    await mount();

    const button = roundButton();
    expect(button).toBeTruthy();
    await act(async () => {
      button!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(startRound).toHaveBeenCalledWith('p1');
    // The summary has to say what stayed approved, or "send everything for
    // review" reads like it discarded sign-offs people already gave.
    expect(container.textContent).toContain('Sent 2 artifacts for review');
    expect(container.textContent).toContain('7 stayed approved');
    // The queue is reloaded so the newly submitted artifacts appear.
    expect(get).toHaveBeenCalledTimes(2);
  });

  it('says so when a re-run finds nothing new', async () => {
    startRound.mockResolvedValue({
      data: { moved: [], already_in_review: 4, approved: 9, superseded: 0, out_of_scope: 0, types: [] },
    } as any);
    await mount();

    await act(async () => {
      roundButton()!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain('Nothing new to review');
    expect(container.textContent).toContain('9 approved and unchanged');
  });

  it('does nothing when the confirmation is declined', async () => {
    confirmAnswer = false;
    await mount();

    await act(async () => {
      roundButton()!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(startRound).not.toHaveBeenCalled();
  });

  it('hides the action for a workspace the feature has not reached', async () => {
    roundOn = false;
    await mount();
    expect(roundButton()).toBeUndefined();
  });

  it('surfaces a refusal instead of claiming the round ran', async () => {
    startRound.mockRejectedValue({ response: { status: 403, data: { error: 'editor role required' } } });
    await mount();

    await act(async () => {
      roundButton()!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain('editor role required');
    expect(container.textContent).not.toContain('Sent');
  });
});
