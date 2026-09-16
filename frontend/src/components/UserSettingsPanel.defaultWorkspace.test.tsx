import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { UserSettingsPanel } from './UserSettingsPanel';
import { defaultWorkspaceAPI } from '../api/client';

// The Default workspace section (REQ-156): a member picks the workspace a
// sign-in lands in from the workspaces they belong to, the personal one
// being the empty choice, and the choice is saved through the API and
// reflected on the signed-in user.

vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

// Plain functions, not vi.fn(): mocks are cleared between tests, which
// would strip a factory-set implementation and leave the panel's mount
// effects awaiting undefined (the push test explains the same trap).
vi.mock('../api/client', () => ({
  notificationPrefsAPI: {
    get: () => Promise.resolve({ data: { email_notifications: true, push_notifications: false } }),
    update: () => Promise.resolve({ data: {} }),
  },
  providerSettingsAPI: { list: () => Promise.resolve({ data: [] }) },
  pushAPI: {
    config: () => Promise.resolve({ data: { enabled: false } }),
    list: () => Promise.resolve({ data: { subscriptions: [] } }),
    subscribe: () => Promise.resolve({ data: {} }),
    unsubscribe: () => Promise.resolve({ data: {} }),
  },
  DEFAULT_MIN_PASSWORD_LENGTH: 8,
  passwordAPI: { change: vi.fn() },
  authAPI: { policy: () => Promise.resolve({ data: { min_password_length: 8 } }) },
  defaultWorkspaceAPI: { get: vi.fn(), set: vi.fn() },
}));
vi.mock('./org/MyRunnerCard', () => ({ MyRunnerCard: () => null }));
vi.mock('./org/CloudRunnerCard', () => ({ CloudRunnerCard: () => null }));
vi.mock('./agents/ProviderConnectCard', () => ({ ProviderConnectCard: () => null }));
vi.mock('./ThemeSwitcher', () => ({ ThemeSwitcher: () => null }));
vi.mock('../hooks/useViewport', () => ({ useViewport: () => ({ isPhone: false, isCompact: false }) }));

let mockGateOn = true;
vi.mock('../hooks/useFeature', () => ({ useFeature: () => mockGateOn }));

const mockSetCurrentUser = vi.fn();
let mockCurrentUser: any;
vi.mock('../state/store', () => ({
  useAppStore: () => ({
    currentUser: mockCurrentUser,
    activeOrgId: 'personal',
    orgs: [
      { id: 'personal', name: "Sam's Space", type: 'personal' },
      { id: 'acme', name: 'Acme Robotics', type: 'company' },
    ],
    emailVerificationRequired: false,
    setCurrentUser: mockSetCurrentUser,
  }),
}));

const api = vi.mocked(defaultWorkspaceAPI);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  mockGateOn = true;
  mockCurrentUser = { id: 'u-1', email: 'sam@example.com', name: 'Sam', default_org_id: '' };
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const mount = async () => {
  await act(async () => {
    root.render(<UserSettingsPanel onClose={() => {}} />);
  });
};

const select = () => container.querySelector('select[aria-label="Default workspace"]') as HTMLSelectElement | null;

it('offers the personal workspace and every company workspace the member is in', async () => {
  await mount();
  const options = Array.from(select()!.options).map((o) => [o.value, o.textContent]);
  expect(options).toEqual([
    ['', 'My personal workspace'],
    ['acme', 'Acme Robotics'],
  ]);
  expect(select()!.value).toBe('');
});

it('saves the choice and reflects it on the signed-in user', async () => {
  api.set.mockResolvedValue({ data: { org_id: 'acme' } } as any);
  await mount();
  await act(async () => {
    select()!.value = 'acme';
    select()!.dispatchEvent(new Event('change', { bubbles: true }));
  });
  expect(api.set).toHaveBeenCalledWith('acme');
  expect(mockSetCurrentUser).toHaveBeenCalledWith(expect.objectContaining({ default_org_id: 'acme' }));
});

it('shows the stored choice, and says why when the server refuses', async () => {
  mockCurrentUser = { ...mockCurrentUser, default_org_id: 'acme' };
  api.set.mockRejectedValue({ response: { data: { error: 'this feature reaches stable-channel workspaces at their next stable release' } } });
  await mount();
  expect(select()!.value).toBe('acme');
  await act(async () => {
    select()!.value = '';
    select()!.dispatchEvent(new Event('change', { bubbles: true }));
  });
  expect(container.textContent).toContain('next stable release');
  expect(mockSetCurrentUser).not.toHaveBeenCalled();
});

// A stable-channel workspace whose release predates the feature is told
// when it arrives rather than shown a control that would be refused.
it('explains instead of offering the control while the gate is closed', async () => {
  mockGateOn = false;
  await mount();
  expect(select()).toBeNull();
  expect(container.textContent).toContain('next stable release');
});
