import React, { act } from 'react';
import { readFileSync } from 'fs';
import { join } from 'path';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { AutomationsPage } from './AutomationsPage';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// The events a triggered automation can listen to (#379, bug 84). The form
// offered ten of the backend's event types, so an automation could not be
// set to fire on a status change, a restore, a link update, a chatter
// entry, a work item update or any of the workspace member events, and one
// saved on such an event through the API opened with another event shown.
// It now offers every event type Go declares (contracts/vocab.json, which
// TestVocabulary writes from internal/domain/events), in Go's order.

const GO_EVENT_TYPES: string[] = JSON.parse(
  readFileSync(join(__dirname, '../../../contracts/vocab.json'), 'utf8')
).event_types;

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    automationsAPI: {
      list: () =>
        ok([
          {
            id: 'au-1',
            org_id: 'o1',
            name: 'Welcome the newcomer',
            agent_id: 'agent-1',
            project_id: 'p1',
            kind: 'triggered',
            enabled: true,
            prompt_template: '',
            cron_expr: '',
            catch_up: false,
            event_type: 'org.member_added',
            event_filter: {},
            cooldown_seconds: 60,
            max_runs_per_hour: 10,
            created_at: '2026-10-01T09:00:00Z',
            updated_at: '2026-10-01T09:00:00Z',
          },
        ]),
    },
    agentsAPI: { list: () => ok([{ id: 'agent-1', name: 'Greeter' }]) },
    crewsAPI: { list: () => ok([]) },
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

const byText = <T extends Element>(selector: string, text: string): T => {
  const found = Array.from(document.body.querySelectorAll<T>(selector)).find(
    (node) => (node.textContent ?? '').trim() === text
  );
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

// The event type <select> of the open form, found by its label.
const eventTypeSelect = (): HTMLSelectElement => {
  const select = byText<HTMLLabelElement>('label', 'Event type').parentElement?.querySelector('select');
  expect(select, 'the event type select').toBeTruthy();
  return select!;
};

beforeEach(() => {
  useAppStore.setState({ ...initialStore, projectId: 'p1', activeOrgId: 'o1' }, true);
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

const mount = async () => {
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

describe('AutomationsPage event types', () => {
  it('offers every event type the backend declares, in its order', async () => {
    expect(GO_EVENT_TYPES.length).toBeGreaterThan(20);
    await mount();
    await click(byText('button', 'New automation'));
    await click(byText('[aria-label="Automation kind"] *', 'Triggered'));

    const select = eventTypeSelect();
    expect(Array.from(select.options).map((o) => o.value)).toEqual(GO_EVENT_TYPES);
    expect(select.value).toBe('artifact.created');
  });

  it('opens an automation saved on a workspace member event with that event selected', async () => {
    await mount();
    await click(byText('span', 'Welcome the newcomer'));

    expect(eventTypeSelect().value).toBe('org.member_added');
  });
});
