import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { AutomationsPage } from './AutomationsPage';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';
import { automationsAPI, type Automation, type Org } from '../api/client';

// An automation's scope on the Automations page (#379 question 42). The page
// lives under a project, so every automation made there was pinned to that
// project, and one for the whole workspace, the only kind the workspace's
// membership events fire, could be made only through the API, and was not
// listed anywhere. The page now lists the whole workspace's automations
// beside the project's, marked "Whole workspace"; a workspace admin chooses
// the scope of a new automation and moves an existing one, once the
// workspace has the workspace-automations feature; anyone else sees the
// whole workspace's automations but cannot change them, as the API has it.

const ok = (data: unknown) => Promise.resolve({ data });

const automation = (id: string, name: string, extra: Partial<Automation>): Automation => ({
  id,
  name,
  agent_id: 'agent-1',
  kind: 'manual',
  enabled: true,
  prompt_template: '',
  cron_expr: '',
  catch_up: false,
  event_type: '',
  event_filter: {},
  cooldown_seconds: 60,
  max_runs_per_hour: 10,
  ...extra,
});

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    automationsAPI: {
      list: () =>
        ok([
          automation('au-p1', 'Review new requirements', { project_id: 'p1' }),
          automation('au-p2', 'Another project', { project_id: 'p2' }),
          automation('au-ws', 'Welcome the newcomer', { project_id: null, kind: 'triggered', event_type: 'org.member_added' }),
          automation('au-crew', 'Crew review', { project_id: 'p1', agent_id: null, team_id: 'crew-p1' }),
        ]),
      create: (payload) => ok({ ...payload, id: 'au-new' }),
      update: (id, payload) => ok({ ...payload, id }),
    },
    agentsAPI: { list: () => ok([{ id: 'agent-1', name: 'Greeter' }]) },
    crewsAPI: {
      list: () =>
        ok([
          { id: 'crew-p1', name: 'Project crew', description: '', project_id: 'p1', is_default: false },
          { id: 'crew-ws', name: 'Default crew', description: '', project_id: null, is_default: true },
        ]),
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

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

const find = <T extends Element>(selector: string, text: string): T | undefined =>
  Array.from(document.body.querySelectorAll<T>(selector)).find((node) => (node.textContent ?? '').trim() === text);

const byText = <T extends Element>(selector: string, text: string): T => {
  const found = find<T>(selector, text);
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

const rowOf = (name: string): HTMLTableRowElement => {
  const row = Array.from(document.body.querySelectorAll('tbody tr')).find((tr) =>
    (tr.querySelector('td')?.textContent ?? '').startsWith(name)
  );
  expect(row, `the row of ${name}`).toBeTruthy();
  return row as HTMLTableRowElement;
};

const choose = async (selectLabel: string, value: string) => {
  const select = byText<HTMLLabelElement>('label', selectLabel).parentElement!.querySelector('select')!;
  await act(async () => {
    const setValue = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!;
    setValue.call(select, value);
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
};

const setName = async (name: string) => {
  const input = byText<HTMLLabelElement>('label', 'Name').parentElement!.querySelector('input')!;
  await act(async () => {
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    setValue.call(input, name);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await flush();
};

const scopeControl = () => document.body.querySelector('[aria-label="Automation scope"]');

const crewOptions = (): string[] =>
  Array.from(byText<HTMLLabelElement>('label', 'Target').parentElement!.querySelectorAll('select option'))
    .map((o) => (o as HTMLOptionElement).value)
    .filter(Boolean);

const org = (role: 'admin' | 'member'): Org =>
  ({ id: 'o1', name: 'Shop', slug: 'shop', type: 'company', plan: 'business', role, created_at: '' }) as Org;

const mount = async (role: 'admin' | 'member', feature: boolean) => {
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      activeOrgId: 'o1',
      orgs: [org(role)],
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-automations': feature } },
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/automations']}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/automations" element={<AutomationsPage />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

beforeEach(() => {
  vi.mocked(automationsAPI.create).mockClear();
  vi.mocked(automationsAPI.update).mockClear();
  vi.mocked(automationsAPI.list).mockClear();
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

describe('AutomationsPage scope', () => {
  it("lists the project's and the whole workspace's automations, marking the latter", async () => {
    await mount('member', true);
    expect(vi.mocked(automationsAPI.list).mock.calls[0]).toEqual([]);
    const names = Array.from(document.body.querySelectorAll('tbody tr')).map(
      (tr) => tr.querySelector('td')?.textContent ?? ''
    );
    expect(names).toEqual([
      'Review new requirements',
      'Welcome the newcomerWhole workspace',
      'Crew review',
    ]);
  });

  it("lets a member see the whole workspace's automations but not change them", async () => {
    await mount('member', true);
    const wide = rowOf('Welcome the newcomer');
    expect(wide.querySelector('button')).toBeNull();
    expect((wide.querySelector('input[type="checkbox"]') as HTMLInputElement).disabled).toBe(true);
    await click(wide.querySelector('td span')!);
    expect(document.body.textContent).not.toContain('Edit automation');

    const own = rowOf('Review new requirements');
    expect(Array.from(own.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['Run now', 'Delete']);
    await click(byText('button', 'New automation'));
    expect(scopeControl()).toBeNull();
  });

  it('lets an admin make an automation for the whole workspace, with a crew of the whole workspace', async () => {
    await mount('admin', true);
    await click(byText('button', 'New automation'));
    expect(scopeControl()).not.toBeNull();
    await setName('Greet');
    await click(byText('[aria-label="Automation target"] *', 'Crew'));
    expect(crewOptions()).toEqual(['crew-p1', 'crew-ws']);
    await choose('Target', 'crew-p1');
    await click(byText('[aria-label="Automation scope"] *', 'Whole workspace'));
    // The project's crew cannot run it, and is let go.
    expect(crewOptions()).toEqual(['crew-ws']);
    await choose('Target', 'crew-ws');
    await click(byText('button', 'Create automation'));

    const payload = vi.mocked(automationsAPI.create).mock.calls[0][0];
    expect(payload).toMatchObject({ name: 'Greet', project_id: null, agent_id: null, team_id: 'crew-ws' });
  });

  it("makes a new automation the project's by default", async () => {
    await mount('admin', true);
    await click(byText('button', 'New automation'));
    await setName('Review');
    await choose('Target', 'agent-1');
    await click(byText('button', 'Create automation'));
    expect(vi.mocked(automationsAPI.create).mock.calls[0][0]).toMatchObject({ project_id: 'p1', agent_id: 'agent-1' });
  });

  it('moves an automation only when its scope is changed', async () => {
    await mount('admin', true);
    await click(byText('span', 'Review new requirements'));
    await click(byText('[aria-label="Automation scope"] *', 'Whole workspace'));
    await click(byText('button', 'Save changes'));
    expect(vi.mocked(automationsAPI.update).mock.calls[0]).toEqual(['au-p1', expect.objectContaining({ project_id: '' })]);

    await click(byText('span', 'Welcome the newcomer'));
    await click(byText('[aria-label="Automation scope"] *', 'This project'));
    await click(byText('button', 'Save changes'));
    expect(vi.mocked(automationsAPI.update).mock.calls[1]).toEqual(['au-ws', expect.objectContaining({ project_id: 'p1' })]);

    await click(byText('span', 'Review new requirements'));
    await click(byText('button', 'Save changes'));
    expect(vi.mocked(automationsAPI.update).mock.calls[2][1]).not.toHaveProperty('project_id');
  });

  it('offers no scope choice before the workspace has the feature, and says what an automation covers', async () => {
    await mount('admin', false);
    await click(byText('button', 'New automation'));
    expect(scopeControl()).toBeNull();
    await click(byText('button', 'Cancel'));

    await click(byText('span', 'Welcome the newcomer'));
    expect(scopeControl()).toBeNull();
    expect(document.body.textContent).toContain('The whole workspace');
    await click(byText('button', 'Save changes'));
    expect(vi.mocked(automationsAPI.update).mock.calls[0][1]).not.toHaveProperty('project_id');
  });

  it('switches an automation from a crew to an agent, clearing the crew', async () => {
    await mount('member', false);
    await click(byText('span', 'Crew review'));
    await click(byText('[aria-label="Automation target"] *', 'Agent'));
    await choose('Target', 'agent-1');
    await click(byText('button', 'Save changes'));
    expect(vi.mocked(automationsAPI.update).mock.calls[0][1]).toMatchObject({ agent_id: 'agent-1', team_id: '' });
  });
});
