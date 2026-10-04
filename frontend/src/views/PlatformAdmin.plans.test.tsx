import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { PlatformAdmin } from './PlatformAdmin';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// The plan picker on the platform admin page (REQ-155). A workspace still on
// a legacy plan name, free or team, shows it by a readable label and selected;
// no other workspace is offered a legacy plan (#379, bug 85).

vi.mock('react-router-dom', () => ({
  Navigate: ({ to }: any) => require('react').createElement('div', { 'data-navigate': String(to) }),
}));
vi.mock('../components/Navbar', () => ({ Navbar: () => null }));
vi.mock('../hooks/useViewport', () => ({ useViewport: () => ({ isCompact: false }) }));

const ws = (id: string, plan: string) => ({
  id,
  name: `Workspace ${id}`,
  slug: id,
  type: 'company',
  plan,
  members: 1,
  created_at: '2026-09-01T00:00:00Z',
});

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    adminAPI: {
      workspaces: () => Promise.resolve({ data: [ws('old-single', 'free'), ws('old-business', 'team'), ws('acme', 'business')] }),
      users: () => Promise.resolve({ data: [] }),
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

const picker = (name: string) => container.querySelector<HTMLSelectElement>(`select[aria-label="Plan for ${name}"]`)!;
const shown = (select: HTMLSelectElement) => select.options[select.selectedIndex];
const choices = (select: HTMLSelectElement) => Array.from(select.options).map((o) => `${o.value}=${o.text}`);

const OFFERED = [
  'single=Single User',
  'business_lite=Business Lite',
  'business=Business',
  'enterprise=Enterprise',
  'open_source=Open source',
  'self_host=Self-hosted',
];

describe('PlatformAdmin plan picker', () => {
  it('shows a legacy plan by name for its workspace and offers it to no other', async () => {
    await act(async () => {
      root.render(
        <DialogProvider>
          <PlatformAdmin />
        </DialogProvider>
      );
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    const free = picker('Workspace old-single');
    expect(free.value).toBe('free');
    expect(shown(free).text).toBe('Free (legacy Single User)');
    expect(choices(free)).toEqual([...OFFERED, 'free=Free (legacy Single User)']);

    const team = picker('Workspace old-business');
    expect(team.value).toBe('team');
    expect(shown(team).text).toBe('Team (legacy Business)');
    expect(choices(team)).toEqual([...OFFERED, 'team=Team (legacy Business)']);

    const current = picker('Workspace acme');
    expect(current.value).toBe('business');
    expect(choices(current)).toEqual(OFFERED);
  });
});
