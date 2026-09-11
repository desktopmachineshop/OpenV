import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { UserSettingsPanel } from './UserSettingsPanel';
import { notificationPrefsAPI, providerSettingsAPI, pushAPI } from '../api/client';

// Same recipe as Login.test.tsx: CRA's Jest cannot resolve react-router v7's
// package exports, so the router is mocked with the one piece this panel uses.
jest.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

// The panel's own dependencies, mocked down to what the push toggle needs:
// the client module builds an axios instance at import time, and the runner /
// provider cards fetch on mount and are irrelevant here.
jest.mock('../api/client', () => ({
  notificationPrefsAPI: { get: jest.fn(), update: jest.fn() },
  providerSettingsAPI: { list: jest.fn() },
  pushAPI: { config: jest.fn(), list: jest.fn(), subscribe: jest.fn(), unsubscribe: jest.fn() },
}));
jest.mock('./org/MyRunnerCard', () => ({ MyRunnerCard: () => null }));
jest.mock('./org/CloudRunnerCard', () => ({ CloudRunnerCard: () => null }));
jest.mock('./agents/ProviderConnectCard', () => ({ ProviderConnectCard: () => null }));
jest.mock('./ThemeSwitcher', () => ({ ThemeSwitcher: () => null }));
jest.mock('../hooks/useViewport', () => ({ useViewport: () => ({ isPhone: false, isCompact: false }) }));
jest.mock('../state/store', () => ({
  useAppStore: () => ({
    currentUser: { id: 'u-1', email: 'sam@example.com', name: 'Sam' },
    activeOrgId: null,
    orgs: [],
    emailVerificationRequired: false,
  }),
}));

const prefs = notificationPrefsAPI as jest.Mocked<typeof notificationPrefsAPI>;
const providers = providerSettingsAPI as jest.Mocked<typeof providerSettingsAPI>;
const push = pushAPI as jest.Mocked<typeof pushAPI>;

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const PUBLIC_KEY = 'BF-c_Ab0';

let container: HTMLDivElement;
let root: Root;

const subscription = {
  endpoint: 'https://push.example.com/abc',
  options: { applicationServerKey: null },
  toJSON: () => ({ endpoint: 'https://push.example.com/abc', keys: { p256dh: 'p', auth: 'a' } }),
  unsubscribe: jest.fn().mockResolvedValue(true),
};

// installBrowser fakes the push APIs the toggle feature-detects. Passing
// `supported: false` removes them, which is the iPhone-outside-the-installed-
// app case.
const installBrowser = (opts: {
  supported?: boolean;
  permission?: NotificationPermission;
  requestPermission?: NotificationPermission;
  existing?: typeof subscription | null;
}) => {
  if (opts.supported === false) {
    delete (window as any).Notification;
    delete (window as any).PushManager;
    Object.defineProperty(window.navigator, 'serviceWorker', { configurable: true, value: undefined });
    return { subscribe: jest.fn(), getSubscription: jest.fn() };
  }
  const subscribe = jest.fn().mockResolvedValue(subscription);
  const getSubscription = jest.fn().mockResolvedValue(opts.existing ?? null);
  (window as any).Notification = {
    permission: opts.permission || 'default',
    requestPermission: jest.fn().mockResolvedValue(opts.requestPermission || 'granted'),
  };
  (window as any).PushManager = function PushManager() {};
  Object.defineProperty(window.navigator, 'serviceWorker', {
    configurable: true,
    value: { ready: Promise.resolve({ pushManager: { subscribe, getSubscription } }) },
  });
  return { subscribe, getSubscription };
};

const render = async () => {
  await act(async () => {
    root.render(<UserSettingsPanel onClose={() => {}} />);
  });
  // Settle the config fetch and the getSubscription round trip.
  await act(async () => {
    await Promise.resolve();
  });
};

const pushToggle = (): HTMLInputElement => {
  const el = container.querySelector<HTMLInputElement>(
    'input[aria-label="Push notifications on this device"]'
  );
  if (!el) throw new Error('push toggle not rendered');
  return el;
};

const panelText = () => container.textContent || '';

beforeEach(() => {
  jest.clearAllMocks();
  subscription.unsubscribe.mockClear();
  prefs.get.mockResolvedValue({ data: { email_notifications: true, push_notifications: false } } as any);
  prefs.update.mockResolvedValue({ data: { email_notifications: true, push_notifications: true } } as any);
  providers.list.mockResolvedValue({ data: [] } as any);
  push.subscribe.mockResolvedValue({ data: {} } as any);
  push.unsubscribe.mockResolvedValue({ data: undefined } as any);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  delete (window as any).Notification;
  delete (window as any).PushManager;
  Object.defineProperty(window.navigator, 'serviceWorker', { configurable: true, value: undefined });
});

it('offers the toggle off, then subscribes this device and records the opt-in', async () => {
  const { subscribe } = installBrowser({ permission: 'default', requestPermission: 'granted' });
  push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);

  await render();

  const toggle = pushToggle();
  expect(toggle.disabled).toBe(false);
  expect(toggle.checked).toBe(false);

  await act(async () => {
    toggle.click();
  });
  await act(async () => {
    await Promise.resolve();
  });

  expect(subscribe).toHaveBeenCalled();
  expect(push.subscribe).toHaveBeenCalledWith(
    expect.objectContaining({ endpoint: 'https://push.example.com/abc' })
  );
  // The per-user opt-in goes on alongside the device registration, and the
  // email preference is left alone.
  expect(prefs.update).toHaveBeenCalledWith({ push_notifications: true });
  expect(pushToggle().checked).toBe(true);
});

it('shows the device as on when it is already subscribed, and withdraws it', async () => {
  installBrowser({ permission: 'granted', existing: subscription });
  push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);

  await render();
  expect(pushToggle().checked).toBe(true);

  await act(async () => {
    pushToggle().click();
  });
  await act(async () => {
    await Promise.resolve();
  });

  expect(push.unsubscribe).toHaveBeenCalledWith('https://push.example.com/abc');
  expect(subscription.unsubscribe).toHaveBeenCalled();
  expect(pushToggle().checked).toBe(false);
});

it('explains that the server has no keys configured', async () => {
  installBrowser({ permission: 'default' });
  push.config.mockResolvedValue({ data: { enabled: false, public_key: '' } } as any);

  await render();

  expect(pushToggle().disabled).toBe(true);
  expect(panelText()).toMatch(/not configured on this server/i);
});

it('explains that the browser cannot receive push', async () => {
  installBrowser({ supported: false });
  push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);

  await render();

  expect(pushToggle().disabled).toBe(true);
  expect(panelText()).toMatch(/cannot receive push notifications/i);
  expect(push.config).not.toHaveBeenCalled();
});

it('explains that notifications are blocked for the site', async () => {
  installBrowser({ permission: 'denied' });
  push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);

  await render();

  expect(pushToggle().disabled).toBe(true);
  expect(panelText()).toMatch(/blocked for this site/i);
});

it('reports a subscribe failure instead of showing the device as on', async () => {
  installBrowser({ permission: 'default', requestPermission: 'denied' });
  push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);

  await render();
  await act(async () => {
    pushToggle().click();
  });
  await act(async () => {
    await Promise.resolve();
  });

  expect(push.subscribe).not.toHaveBeenCalled();
  expect(prefs.update).not.toHaveBeenCalled();
  expect(pushToggle().checked).toBe(false);
  expect(panelText()).toMatch(/blocked for this site/i);
});
