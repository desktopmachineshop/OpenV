import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { AgentsPage } from './AgentsPage';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';
import { agentsAPI, type AgentDef, type Org } from '../api/client';

// The Agents page as a hierarchy of cards (issue 733): with the
// agent-hierarchy feature, the lead on top and a column per group, each card
// with its numbers over 7 days read in one call; a card opens the agent's
// runs; Edit arranges who reports to whom. Without the feature, the list.

const ok = (data: unknown) => Promise.resolve({ data });

const agent = (id: string, name: string, extra: Partial<AgentDef> = {}): AgentDef =>
  ({
    id,
    slug: id,
    name,
    description: `${name} does its part.`,
    provider: 'claude-code',
    model: '',
    effort: '',
    allowed_tools: ['mcp__openv__*'],
    write_mode: 'proposal',
    repo_access: false,
    max_turns: 50,
    timeout_seconds: 600,
    system_prompt: '',
    file_path: '',
    ...extra,
  }) as AgentDef;

const agents = [
  agent('chief', 'Chief of Staff'),
  agent('vv', 'V&V Engineer', { parent_agent_id: 'chief', reports_to: 'chief', group_name: 'Verification' }),
  agent('author', 'Test Case Author', { parent_agent_id: 'vv', reports_to: 'vv', group_name: 'Verification' }),
  agent('dev', 'Developer', { parent_agent_id: 'chief', reports_to: 'chief', group_name: 'Engineering', sort_order: 1 }),
];

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    agentsAPI: {
      list: () => ok(agents),
      stats: () =>
        ok({
          days: 7,
          since: '',
          agents: [
            { agent_id: 'vv', open: 2, done: 4, cost_usd: 1.5, pending_proposals: 3 },
            { agent_id: 'author', open: 1, done: 1, cost_usd: 0.5, pending_proposals: 0 },
          ],
        }),
      arrange: (placements) => ok(agents.map((a) => (a.slug === placements[0].slug ? { ...a, group_name: 'QA' } : a))),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;
let location = '';

const Where: React.FC = () => {
  location = useLocation().pathname + useLocation().search;
  return null;
};

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const org = (role: 'admin' | 'member'): Org =>
  ({ id: 'o1', name: 'Shop', slug: 'shop', type: 'company', plan: 'business', role, created_at: '' }) as Org;

const mount = async (feature: boolean, role: 'admin' | 'member' = 'admin') => {
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      activeOrgId: 'o1',
      orgs: [org(role)],
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'agent-hierarchy': feature } },
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/agents']}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
            <Route path="*" element={null} />
          </Routes>
          <Where />
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

const card = (slug: string) => document.body.querySelector(`[data-agent="${slug}"]`) as HTMLElement;
const groups = () =>
  Array.from(document.body.querySelectorAll('.agent-group')).map((g) => g.getAttribute('aria-label'));

beforeEach(() => {
  vi.mocked(agentsAPI.stats).mockClear();
  vi.mocked(agentsAPI.arrange).mockClear();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

describe('AgentsPage hierarchy', () => {
  it('draws the lead on top, a column per group and the reports nested, with each card’s numbers', async () => {
    await mount(true);
    expect(document.body.textContent).toContain('Live numbers over the last 7 days');
    expect(vi.mocked(agentsAPI.stats).mock.calls).toEqual([['p1']]);
    expect(document.body.querySelector('.agent-top [data-agent="chief"]')).not.toBeNull();
    expect(groups()).toEqual(['Group Verification', 'Group Engineering']);
    // The author is nested under its lead, on the group's guide line.
    expect(card('vv').parentElement!.querySelector('.agent-tree-children [data-agent="author"]')).not.toBeNull();
    expect(card('vv').textContent).toContain('2 open');
    expect(card('vv').textContent).toContain('3 to approve');
    // The group's header sums its agents.
    const header = document.body.querySelector('[aria-label="Group Verification"] .agent-group-header')!;
    expect(header.textContent).toContain('3 open');
    expect(header.textContent).toContain('$2.00');
  });

  it('opens an agent’s runs from its card, and its mission and tools on the expander', async () => {
    await mount(true);
    const expander = Array.from(card('author').querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('Mission and tools')
    )!;
    await act(async () => expander.click());
    expect(card('author').textContent).toContain('Test Case Author does its part.');
    expect(card('author').textContent).toContain('mcp__openv__*');
    await act(async () => card('author').click());
    await flush();
    expect(location).toBe('/projects/p1/agent-runs?agent=author');
  });

  it('arranges in Edit mode: a group change is saved as the agent’s place', async () => {
    await mount(true);
    const edit = Array.from(document.body.querySelectorAll('button')).find((b) => b.textContent === 'Edit')!;
    await act(async () => edit.click());
    const input = document.body.querySelector('[aria-label="Developer group"]') as HTMLInputElement;
    await act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      set.call(input, 'QA');
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new FocusEvent('focusout', { bubbles: true }));
    });
    await flush();
    expect(vi.mocked(agentsAPI.arrange).mock.calls[0][0]).toEqual([
      { slug: 'dev', reports_to: 'chief', group_name: 'QA', sort_order: 1 },
    ]);
    // A lead cannot be put under its own report.
    const reportsTo = document.body.querySelector('[aria-label="V&V Engineer reports to"]') as HTMLSelectElement;
    expect(Array.from(reportsTo.options).map((o) => o.value)).not.toContain('author');
  });

  it('offers Edit only to a workspace admin', async () => {
    await mount(true, 'member');
    expect(Array.from(document.body.querySelectorAll('button')).some((b) => b.textContent === 'Edit')).toBe(false);
  });

  it('keeps the list without the feature', async () => {
    await mount(false);
    expect(document.body.querySelector('.agent-hierarchy')).toBeNull();
    expect(vi.mocked(agentsAPI.stats)).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain('Select an agent to edit it');
  });
});
