import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { UserSettingsPanel } from './UserSettingsPanel';
import { notificationPrefsAPI, providerSettingsAPI, pushAPI } from '../api/client';
import { SERVICE_WORKER_READY_TIMEOUT_MS } from '../push/webPush';

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
// app case; `readyHangs` is a service worker that never activates, which used
// to leave the toggle waiting forever.
const installBrowser = (opts: {
  supported?: boolean;
  permission?: NotificationPermission;
  requestPermission?: NotificationPermission;
  existing?: typeof subscription | null;
  readyHangs?: boolean;
}) => {
  if (opts.supported === false) {
    delete (window as any).Notification;
    delete (window as any).PushManager;
    Object.defineProperty(window.navigator, 'serviceWorker', { configurable: true, value: undefined });
    return { subscribe: jest.fn(), getSubscription: jest.fn() };
  }
  const subscribe = jest.fn().mockResolvedValue(subscription);
  const getSubscription = jest.fn().mockResolvedValue(opts.existing ?? null);
  const registration = { pushManager: { subscribe, getSubscription } };
  (window as any).Notification = {
    permission: opts.permission || 'default',
    requestPermission: jest.fn().mockResolvedValue(opts.requestPermission || 'granted'),
  };
  (window as any).PushManager = function PushManager() {};
  Object.defineProperty(window.navigator, 'serviceWorker', {
    configurable: true,
    value: {
      ready: opts.readyHangs ? new Promise(() => {}) : Promise.resolve(registration),
      getRegistration: jest.fn().mockResolvedValue(opts.readyHangs ? undefined : registration),
    },
  });
  return { subscribe, getSubscription };
};

// settle runs the queued promises the mount effect chains through: the config
// fetch, the registration, getSubscription, and the device list.
const settle = async () => {
  for (let i = 0; i < 8; i += 1) {
    // eslint-disable-next-line no-await-in-loop
    await act(async () => {
      await Promise.resolve();
    });
  }
};

const render = async () => {
  await act(async () => {
    root.render(<UserSettingsPanel onClose={() => {}} />);
  });
  await settle();
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
  // By default the server knows of no devices, so a browser subscription on
  // its own never shows as on.
  push.list.mockResolvedValue({ data: { subscriptions: [] } } as any);
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
  await settle();

  expect(subscribe).toHaveBeenCalled();
  expect(push.subscribe).toHaveBeenCalledWith(
    expect.objectContaining({ endpoint: 'https://push.example.com/abc' })
  );
  // The per-user opt-in goes on alongside the device registration, and the
  // email preference is left alone.
  expect(prefs.update).toHaveBeenCalledWith({ push_notifications: true });
  expect(pushToggle().checked).toBe(true);
});

it('shows the device as on when it is subscribed AND on file, and withdraws it', async () => {
  installBrowser({ permission: 'granted', existing: subscription });
  push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);
  push.list.mockResolvedValue({
    data: { subscriptions: [{ id: 's-1', endpoint: subscription.endpoint }] },
  } as any);

  await render();
  expect(pushToggle().checked).toBe(true);

  await act(async () => {
    pushToggle().click();
  });
  await settle();

  expect(push.unsubscribe).toHaveBeenCalledWith('https://push.example.com/abc');
  expect(subscription.unsubscribe).toHaveBeenCalled();
  expect(pushToggle().checked).toBe(false);
});

// The defect this guards: the toggle read the browser alone, so a device
// whose server-side row was gone (workspace restored from a backup, row
// deleted after a 410, a POST that never landed) showed as on while nothing
// would ever be delivered to it.
it('shows off, and offers to re-register, when the server does not know this device', async () => {
  installBrowser({ permission: 'granted', existing: subscription });
  push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);
  push.list.mockResolvedValue({
    data: { subscriptions: [{ id: 's-9', endpoint: 'https://push.example.com/another-device' }] },
  } as any);

  await render();

  expect(pushToggle().checked).toBe(false);
  expect(pushToggle().disabled).toBe(false);
  expect(panelText()).toMatch(/server has no record of it/i);

  // Turning it on re-posts the SAME endpoint (the POST is idempotent).
  await act(async () => {
    pushToggle().click();
  });
  await settle();

  expect(push.subscribe).toHaveBeenCalledWith(
    expect.objectContaining({ endpoint: 'https://push.example.com/abc' })
  );
  expect(pushToggle().checked).toBe(true);
  expect(panelText()).not.toMatch(/server has no record of it/i);
});

it('explains that the service worker is unavailable instead of waiting forever', async () => {
  jest.useFakeTimers();
  try {
    installBrowser({ permission: 'granted', existing: subscription, readyHangs: true });
    push.config.mockResolvedValue({ data: { enabled: true, public_key: PUBLIC_KEY } } as any);

    await act(async () => {
      root.render(<UserSettingsPanel onClose={() => {}} />);
    });
    await settle();
    await act(async () => {
      jest.advanceTimersByTime(SERVICE_WORKER_READY_TIMEOUT_MS);
    });
    await settle();

    expect(pushToggle().disabled).toBe(true);
    expect(pushToggle().checked).toBe(false);
    expect(panelText()).toMatch(/service worker for this site is unavailable/i);
  } finally {
    jest.useRealTimers();
  }
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
  await settle();

  expect(push.subscribe).not.toHaveBeenCalled();
  expect(prefs.update).not.toHaveBeenCalled();
  expect(pushToggle().checked).toBe(false);
  expect(panelText()).toMatch(/blocked for this site/i);
});
