import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { AgentsPage } from './AgentsPage';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';
import { agentsAPI, type AgentDef, type Org } from '../api/client';

// Archived agents (the flat team archives the agents it replaces): out of the
// hierarchy, listed under Archived, and put back by Reinstate.

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
    system_prompt: 'Prompt.',
    file_path: '',
    ...extra,
  }) as AgentDef;

const agents = [
  agent('requirements-copilot', 'V&V Assistant'),
  agent('link-auditor', 'Link Auditor', { parent_agent_id: 'requirements-copilot', reports_to: 'requirements-copilot' }),
  agent('chief-of-staff', 'Chief of Staff', { archived: true }),
];

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    agentsAPI: {
      list: () => ok(agents),
      stats: () => ok({ days: 7, since: '', agents: [] }),
      update: vi.fn(() => ok({})),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async () => {
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      activeOrgId: 'o1',
      orgs: [{ id: 'o1', name: 'Shop', slug: 'shop', type: 'company', plan: 'business', role: 'admin', created_at: '' } as Org],
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'agent-hierarchy': true } },
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/agents']}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

const button = (text: string) =>
  Array.from(document.body.querySelectorAll('button')).find((b) => b.textContent?.trim().endsWith(text)) as HTMLButtonElement;

beforeEach(() => {
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

describe('archived agents', () => {
  it('leaves them out of the hierarchy and lists them under Archived', async () => {
    await mount();
    expect(document.body.querySelector('[data-agent="requirements-copilot"]')).not.toBeNull();
    expect(document.body.querySelector('[data-agent="chief-of-staff"]')).toBeNull();
    expect(button('Archived (1)')).toBeTruthy();
  });

  it('reinstates one, keeping the rest of its definition', async () => {
    await mount();
    await act(async () => button('Archived (1)').click());
    await act(async () => button('Reinstate').click());
    await flush();
    expect(agentsAPI.update).toHaveBeenCalledWith('chief-of-staff', expect.objectContaining({ archived: false, system_prompt: 'Prompt.' }));
  });
});
