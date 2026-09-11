import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ProjectList } from './ProjectList';
import { sharedProductsAPI, projectAPI, templateAPI, workerStatusAPI } from '../api/client';

// The random-product mode of the new-project form: the vote control on the
// rolled card and the two Top 5 filters beside the roller.
//
// Everything the page talks to is mocked — the api module builds an axios
// client at import time, and the point here is the pool, not the projects
// list around it.
jest.mock('../api/client', () => ({
  agentRunsAPI: { get: jest.fn() },
  agentsAPI: { list: jest.fn().mockResolvedValue({ data: [] }), launchRun: jest.fn() },
  guidedAPI: { start: jest.fn(), saveStep: jest.fn() },
  projectAPI: { list: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), import: jest.fn() },
  sharedProductsAPI: { list: jest.fn(), publish: jest.fn(), report: jest.fn(), vote: jest.fn(), unvote: jest.fn() },
  templateAPI: { list: jest.fn(), create: jest.fn(), createProject: jest.fn() },
  workerStatusAPI: { get: jest.fn() },
}));

// The list reads the query string as well as navigating (the settings tabs
// open by ?tab=), so the router double has to answer both.
jest.mock('react-router-dom', () => ({
  useNavigate: () => jest.fn(),
  useSearchParams: () => [new URLSearchParams(), () => {}],
}));

jest.mock('../state/store', () => ({
  useAppStore: () => ({
    projectId: null,
    setProjectId: jest.fn(),
    projects: [],
    setProjects: jest.fn(),
    addProject: jest.fn(),
    updateProject: jest.fn(),
    removeProject: jest.fn(),
    orgs: [{ id: 'org1', name: 'Sam Space', type: 'company' }],
    activeOrgId: 'org1',
  }),
}));

// The chrome around the form is not what this test is about.
jest.mock('./Navbar', () => ({ Navbar: () => null }));
jest.mock('./HelpSidebar', () => ({ HelpSidebar: () => null }));
jest.mock('./DownloadWizard', () => ({ DownloadWizard: () => null }));
jest.mock('./CreateOrgModal', () => ({ CreateOrgModal: () => null }));

// The real SegmentedControl is the filter under test; only the dialog hooks
// (which need a provider) are stubbed.
jest.mock('./ui', () => ({
  ...jest.requireActual('./ui'),
  useConfirm: () => jest.fn(),
  usePrompt: () => jest.fn(),
}));

const api = sharedProductsAPI as jest.Mocked<typeof sharedProductsAPI>;

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const sharedProduct = (id: string, name: string, votes: number, votesWeek: number, voted = false) => ({
  id,
  category: 'kitchen appliance',
  name,
  description: `${name} recognises Kevin and locks.`,
  vision: `${name} becomes the reason the bean jar survives a Tuesday.`,
  problem: 'Beans vanish overnight and nobody admits to owning the grinder.',
  target_users: 'office workers whose beans keep leaving with Kevin',
  votes,
  votes_week: votesWeek,
  voted,
});

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  jest.clearAllMocks();
  (projectAPI.list as jest.Mock).mockResolvedValue({ data: [] });
  (templateAPI.list as jest.Mock).mockResolvedValue({ data: [] });
  (workerStatusAPI.get as jest.Mock).mockResolvedValue({ data: { workers: [] } });
  // The default read (the roller's own pool) and the leaderboards are the
  // same endpoint with different parameters.
  api.list.mockImplementation((params?: { sort?: string; limit?: number }) => {
    if (params?.sort === 'top') {
      return Promise.resolve({
        data: [sharedProduct('p1', 'Kevinproof', 7, 1), sharedProduct('p2', 'Crustodian', 3, 0)],
      }) as any;
    }
    if (params?.sort === 'top_week') return Promise.resolve({ data: [] }) as any;
    return Promise.resolve({ data: [sharedProduct('p1', 'Kevinproof', 7, 1)] }) as any;
  });
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

const render = async () => {
  await act(async () => {
    root.render(<ProjectList />);
  });
  await flush();
};

const button = (match: (text: string) => boolean): HTMLButtonElement => {
  const found = Array.from(container.querySelectorAll('button')).find((b) =>
    match((b.textContent || '').trim())
  );
  if (!found) throw new Error(`no button matching; saw: ${Array.from(container.querySelectorAll('button')).map((b) => b.textContent).join(' | ')}`);
  return found as HTMLButtonElement;
};

const click = async (el: HTMLElement) => {
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

const voteButton = () =>
  container.querySelector('[aria-label="Vote for this product"]') as HTMLButtonElement;

// Open the new-project form in random-product mode.
const openRandomMode = async () => {
  await click(button((t) => t === '+ New Project'));
  const select = container.querySelector('#mode') as HTMLSelectElement;
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!;
  await act(async () => {
    setter.call(select, 'random');
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
};

// Put a product from the shared pool on the card, which is the only kind
// that can be voted for.
const useTopProduct = async () => {
  await click(button((t) => t === 'Top 5 all time'));
  await click(button((t) => t === 'Use this'));
};

describe('random product mode', () => {
  it('votes and unvotes on a shared product, showing the counts the server returns', async () => {
    await render();
    await openRandomMode();
    await useTopProduct();

    expect(voteButton().getAttribute('aria-pressed')).toBe('false');
    expect(voteButton().textContent).toContain('7');
    expect(container.textContent).toContain('· 1 this week');

    api.vote.mockResolvedValue({ data: { votes: 8, votes_week: 2, voted: true } } as any);
    await click(voteButton());

    expect(api.vote).toHaveBeenCalledWith('p1');
    expect(voteButton().getAttribute('aria-pressed')).toBe('true');
    expect(voteButton().textContent).toContain('8');
    expect(container.textContent).toContain('· 2 this week');

    // Pressing it again takes the vote back rather than casting a second.
    api.unvote.mockResolvedValue({ data: { votes: 7, votes_week: 1, voted: false } } as any);
    await click(voteButton());

    expect(api.unvote).toHaveBeenCalledWith('p1');
    expect(voteButton().getAttribute('aria-pressed')).toBe('false');
    expect(voteButton().textContent).toContain('7');
  });

  it('cannot vote for a built-in concept, and says which kind it is', async () => {
    // Nothing is shared and nothing was invented here, so the roll lands on a
    // built-in concept: there is no row anyone else could vote for, and it is
    // not an invention that merely failed to publish.
    api.list.mockResolvedValue({ data: [] } as any);
    await render();
    await openRandomMode();

    expect(voteButton().disabled).toBe(true);
    expect(voteButton().getAttribute('title')).toContain('shared pool');
    expect(container.textContent).toContain('built-in example');
    expect(container.textContent).not.toContain('Kept in this browser only');
    expect(api.vote).not.toHaveBeenCalled();
  });

  it('switches between the roller and the two Top 5 filters', async () => {
    await render();
    await openRandomMode();

    // Random is the default and asks for no sort, so the roller behaves as
    // it always has.
    expect(api.list).toHaveBeenCalledWith();
    expect(button((t) => t === '🎲 Random').getAttribute('aria-pressed')).toBe('true');

    await click(button((t) => t === 'Top 5 all time'));
    expect(api.list).toHaveBeenCalledWith({ sort: 'top', limit: 5 });
    expect(container.textContent).toContain('Kevinproof');
    expect(container.textContent).toContain('7 votes');
    expect(container.textContent).toContain('Crustodian');

    // An empty week says so rather than looking like a failed load.
    await click(button((t) => t === 'Top 5 this week'));
    expect(api.list).toHaveBeenCalledWith({ sort: 'top_week', limit: 5 });
    expect(container.textContent).toContain('No votes yet this week');

    await click(button((t) => t === '🎲 Random'));
    expect(container.textContent).not.toContain('No votes yet this week');
    expect(button((t) => t.includes('Reroll'))).toBeTruthy();
  });

  it('applies a top product to the form with Use this', async () => {
    await render();
    await openRandomMode();
    await useTopProduct();

    const name = container.querySelector('#name') as HTMLInputElement;
    expect(name.value).toBe('Kevinproof');
    expect(container.textContent).toContain('recognises Kevin and locks');
  });
});
