import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { ConnectedAppsCard } from './ConnectedAppsCard';
import { connectedAppsAPI } from '../api/client';

// Connected apps in the member's settings (REQ-44): the endpoint address to
// give an app, each connected app with its workspace and what it may do,
// and Disconnect, which revokes it after a confirmation.

let mockApps: any[] = [];
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    connectedAppsAPI: {
      list: () => Promise.resolve({ data: mockApps }),
      revoke: vi.fn(() => {
        mockApps = [];
        return Promise.resolve({ data: {} });
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
});

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
