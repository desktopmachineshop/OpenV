import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { PlatformAdmin } from './PlatformAdmin';
import { useAppStore } from '../state/store';

// The admin-minted password reset link (REQ-158): offered for password
// accounts only, shown once with its expiry after a confirmation.

jest.mock('react-router-dom', () => ({
  Navigate: ({ to }: any) => require('react').createElement('div', { 'data-navigate': String(to) }),
}));
jest.mock('../components/Navbar', () => ({ Navbar: () => null }));
jest.mock('../hooks/useViewport', () => ({ useViewport: () => ({ isCompact: false }) }));
jest.mock('../components/ui', () => ({
  ErrorBanner: ({ message }: any) => (message ? <div role="alert">{message}</div> : null),
  useConfirm: () => () => Promise.resolve(true),
}));

const issued: string[] = [];
jest.mock('../api/client', () => ({
  PLANS: [{ value: 'business', label: 'Business' }],
  adminAPI: {
    workspaces: () => Promise.resolve({ data: [] }),
    users: () =>
      Promise.resolve({
        data: [
          { id: 'root', name: 'Root', email: 'root@example.com', auth_provider: 'password', is_admin: true, created_at: '2026-09-01T00:00:00Z' },
          { id: 'dave', name: 'Dave', email: 'dave@example.com', auth_provider: 'password', is_admin: false, created_at: '2026-09-01T00:00:00Z' },
          { id: 'sso', name: 'Sso', email: 'sso@example.com', auth_provider: 'oidc', is_admin: false, created_at: '2026-09-01T00:00:00Z' },
        ],
      }),
    issuePasswordReset: (id: string) => {
      issued.push(id);
      return Promise.resolve({ data: { link: 'https://app.example/reset-password?token=raw', expires_at: '2026-09-15T14:00:00Z' } });
    },
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  issued.length = 0;
  useAppStore.setState({ currentUser: { id: 'root', email: 'root@example.com', is_admin: true } as any });
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState({ currentUser: null });
});

// A macrotask inside act drains every microtask the click chained
// (confirm → busy → API → state), not just the first.
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

describe('PlatformAdmin reset links', () => {
  it('offers a reset link for password accounts only and shows the minted link once', async () => {
    await act(async () => {
      root.render(<PlatformAdmin />);
    });
    await flush();
    const buttons = Array.from(container.querySelectorAll('button[aria-label^="Make a password reset link"]'));
    expect(buttons.map((b) => b.getAttribute('aria-label'))).toEqual([
      'Make a password reset link for Root',
      'Make a password reset link for Dave',
    ]);
    expect(container.textContent).toContain('via oidc');

    await act(async () => {
      (buttons[1] as HTMLButtonElement).click();
    });
    await flush();
    expect(issued).toEqual(['dave']);
    // eslint-disable-next-line no-console
    const status = container.querySelector('[role="status"]');
    expect(status?.textContent).toContain('Reset link for Dave');
    expect((container.querySelector('input[aria-label="Password reset link"]') as HTMLInputElement).value).toBe(
      'https://app.example/reset-password?token=raw'
    );
  });
});
