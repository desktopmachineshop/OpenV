import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { PlatformAdmin } from './PlatformAdmin';
import { useAppStore } from '../state/store';

// The usage view (FeatureAdminUsage): workspaces grouped by plan and kind
// with counts and pace, members under each, and people filtered by
// workspace with the admin controls beside them.

vi.mock('react-router-dom', () => ({
  Navigate: ({ to }: any) => require('react').createElement('div', { 'data-navigate': String(to) }),
}));
vi.mock('../components/Navbar', () => ({ Navbar: () => null }));
vi.mock('../components/admin/UserDashboard', () => ({ UserDashboard: () => null }));
vi.mock('../hooks/useViewport', () => ({ useViewport: () => ({ isCompact: false }) }));
vi.mock('../hooks/useFeature', () => ({ useFeature: (key: string) => key === 'admin-usage' }));
vi.mock('../components/ui', () => ({
  ErrorBanner: ({ message }: any) => (message ? <div role="alert">{message}</div> : null),
  useConfirm: () => () => Promise.resolve(true),
}));

const zero = Array(12).fill(0);
const rising = [...Array(4).fill(0), 1, 1, 1, 1, 3, 3, 3, 3];
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    PLANS: [
      { value: 'enterprise', label: 'Enterprise' },
      { value: 'single', label: 'Single User' },
    ],
    adminAPI: {
      workspaces: () =>
        Promise.resolve({
          data: [
            { id: 'w-solo', name: 'Solo', slug: 'solo', plan: 'single', type: 'personal', members: 1, created_at: '2026-01-01T00:00:00Z' },
            { id: 'w-acme', name: 'Acme', slug: 'acme', plan: 'enterprise', type: 'company', members: 2, created_at: '2026-01-01T00:00:00Z' },
          ],
        }),
      users: () =>
        Promise.resolve({
          data: [
            { id: 'root', name: 'Root', email: 'root@example.com', auth_provider: 'password', is_admin: true, created_at: '' },
            { id: 'dave', name: 'Dave', email: 'dave@example.com', auth_provider: 'password', is_admin: false, created_at: '' },
          ],
        }),
      usage: () =>
        Promise.resolve({
          data: {
            generated_at: '',
            weeks: zero.map(() => '2026-11-10'),
            pace_weeks: 4,
            workspaces: [
              { id: 'w-acme', projects: 2, requirements: 40, test_cases: 12, other_artifacts: 3, links: 30, test_runs: 2, test_results: 12, comments: 5,
                members: 2, active_members_30d: 2, changes: rising, last_activity: '2026-11-10', pace: { recent: 12, previous: 4, change: 2, trend: 'accelerating' } },
            ],
            users: [
              { id: 'dave', workspaces: [{ workspace_id: 'w-acme', role: 'member' }], changes: rising, active_days: zero, pace: { recent: 12, previous: 4, change: 2, trend: 'accelerating' } },
              { id: 'root', workspaces: [{ workspace_id: 'w-solo', role: 'owner' }], changes: zero, active_days: zero, pace: { recent: 0, previous: 0, change: 0, trend: 'idle' } },
            ],
          },
        }),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
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

const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const click = async (el: Element | null) => {
  await act(async () => {
    (el as HTMLElement).click();
  });
  await flush();
};

describe('PlatformAdmin usage view', () => {
  it('groups workspaces by plan with counts and pace, and narrows people to a workspace', async () => {
    await act(async () => {
      root.render(<PlatformAdmin />);
    });
    await flush();
    const tiers = Array.from(container.querySelectorAll('section[aria-label$=" workspaces"]')).map((s) => s.getAttribute('aria-label'));
    expect(tiers).toEqual(['Enterprise workspaces', 'Single User workspaces']);
    const acme = container.querySelector('section[aria-label="Enterprise workspaces"]')!;
    expect(acme.textContent).toContain('40 req · 12 tests · 30 links · 2 runs');
    expect(acme.textContent).toContain('Accelerating+200%');
    expect(acme.textContent).toContain('Company workspaces');
    expect(container.querySelector('section[aria-label="Single User workspaces"]')?.textContent).toContain('Personal workspaces');

    await click(container.querySelector('button[aria-label="Show the members of Acme"]'));
    expect(acme.textContent).toContain('Dave member');

    await click(container.querySelector('button[aria-label="People in Acme"]'));
    const rows = Array.from(container.querySelectorAll('#usage tbody tr'));
    expect(rows).toHaveLength(1);
    expect(rows[0].textContent).toContain('dave@example.com');
    expect(rows[0].querySelector('button[aria-label="Make a password reset link for Dave"]')).not.toBeNull();
    expect((container.querySelector('select[aria-label="Filter people by workspace"]') as HTMLSelectElement).value).toBe('w-acme');
  });
});
