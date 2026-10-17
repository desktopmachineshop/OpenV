import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { ConnectedAppsCard } from './ConnectedAppsCard';
import { connectedAppsAPI } from '../api/client';
import { useAppStore } from '../state/store';

// Connected apps in the member's settings (REQ-44): the endpoint address to
// give an app, each connected app with its workspaces and what it may do,
// Workspaces, which changes the workspaces it reaches where that is
// available, and Disconnect, which revokes it after a confirmation.

let mockApps: any[] = [];
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    connectedAppsAPI: {
      list: () => Promise.resolve({ data: mockApps }),
      revoke: vi.fn(() => {
        mockApps = [];
        return Promise.resolve({ data: {} });
      }),
      setWorkspaces: vi.fn((_id: string, orgIds: string[]) => {
        const names: Record<string, string> = { acme: 'Acme Robotics', beta: 'Beta Labs' };
        mockApps[0].workspaces = orgIds.map((id) => ({ org_id: id, org_name: names[id] }));
        return Promise.resolve({ data: mockApps[0] });
      }),
    },
    orgsAPI: {
      list: () =>
        Promise.resolve({
          data: {
            orgs: [
              { id: 'acme', name: 'Acme Robotics' },
              { id: 'beta', name: 'Beta Labs' },
            ],
            active_org: 'acme',
          },
        }),
    },
  })
);
vi.mock('./ui', () => ({
  useConfirm: () => () => Promise.resolve(true),
  ErrorBanner: ({ message }: { message: string }) => message,
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  mockApps = [
    {
      id: 'g1',
      user_id: 'u1',
      org_id: 'acme',
      org_name: 'Acme Robotics',
      workspaces: [{ org_id: 'acme', org_name: 'Acme Robotics' }],
      client_id: 'c1',
      client_name: 'Claude',
      scope: 'openv:read',
      created_at: '2026-10-15T10:00:00Z',
    },
  ];
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState({ features: null });
});

const button = (label: string) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;

it('lists the connected apps and disconnects one', async () => {
  await act(async () => {
    root.render(<ConnectedAppsCard apiBase="https://openv.app/" />);
  });
  expect(container.querySelector('code')?.textContent).toBe('https://openv.app/mcp');
  expect(container.textContent).toContain('Claude');
  expect(container.textContent).toContain('Acme Robotics · read only · last used never');
  const disconnect = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Disconnect')!;
  await act(async () => disconnect.click());
  expect(connectedAppsAPI.revoke).toHaveBeenCalledWith('g1');
  expect(container.textContent).toContain('No apps connected.');
});

it('gives an app another workspace without connecting it again, where that is available', async () => {
  await act(async () => {
    root.render(<ConnectedAppsCard apiBase="https://openv.app/" />);
  });
  expect(button('Workspaces')).toBeUndefined();
  act(() => root.unmount());
  root = createRoot(container);
  useAppStore.setState({
    features: {
      channel: 'nightly',
      stable_release: '',
      preview: false,
      features: { 'connected-app-workspaces': true },
    },
  });
  await act(async () => {
    root.render(<ConnectedAppsCard apiBase="https://openv.app/" />);
  });
  await act(async () => button('Workspaces').click());
  const boxes = Array.from(container.querySelectorAll('input[type="checkbox"]')) as HTMLInputElement[];
  expect(boxes.map((b) => b.checked)).toEqual([true, false]);
  await act(async () => boxes[1].click());
  await act(async () => button('Save').click());
  expect(connectedAppsAPI.setWorkspaces).toHaveBeenCalledWith('g1', ['acme', 'beta']);
  expect(container.textContent).toContain('Acme Robotics, Beta Labs · read only');
  expect(container.querySelector('input[type="checkbox"]')).toBeNull();
});
