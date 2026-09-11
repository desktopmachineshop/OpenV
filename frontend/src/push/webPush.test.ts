import {
  NO_SERVICE_WORKER_MESSAGE,
  SERVICE_WORKER_READY_TIMEOUT_MS,
  currentPermission,
  getExistingSubscription,
  getServiceWorkerRegistration,
  matchesApplicationServerKey,
  reconcileThisDevice,
  subscribeThisDevice,
  supportsPush,
  unsubscribeThisDevice,
  urlBase64ToUint8Array,
} from './webPush';
import { pushAPI } from '../api/client';

// The client module builds an axios instance at import time, so it is mocked
// wholesale; only the push endpoints matter here.
jest.mock('../api/client', () => ({
  pushAPI: {
    config: jest.fn(),
    list: jest.fn(),
    subscribe: jest.fn(),
    unsubscribe: jest.fn(),
  },
}));

const api = pushAPI as jest.Mocked<typeof pushAPI>;

// A public key with every base64url character class in it, so the padding and
// the -/_ → +// substitutions are actually exercised.
const PUBLIC_KEY = 'BF-c_Ab0';

type FakeSubscription = {
  endpoint: string;
  options?: { applicationServerKey?: ArrayBuffer | null };
  toJSON: () => { endpoint: string; keys: { p256dh: string; auth: string } };
  unsubscribe: jest.Mock;
};

const fakeSubscription = (endpoint = 'https://push.example.com/abc'): FakeSubscription => ({
  endpoint,
  options: { applicationServerKey: urlBase64ToUint8Array(PUBLIC_KEY).buffer },
  toJSON: () => ({ endpoint, keys: { p256dh: 'the-p256dh', auth: 'the-auth' } }),
  unsubscribe: jest.fn().mockResolvedValue(true),
});

// installPushAPIs fakes the three browser APIs webPush feature-detects.
//
// `registered: false` is a container that has no registration yet (so `ready`
// is consulted); `readyHangs` is the case that used to wedge the toggle at
// "Working…" — `ready` never settles, as it does not when registration failed
// or the worker script 404s; `hasGetRegistration: false` is an older browser
// without getRegistration at all.
const installPushAPIs = (opts: {
  permission?: NotificationPermission;
  requestPermission?: NotificationPermission;
  existing?: FakeSubscription | null;
  subscribeResult?: FakeSubscription;
  registered?: boolean;
  readyHangs?: boolean;
  hasGetRegistration?: boolean;
}) => {
  const subscribe = jest.fn().mockResolvedValue(opts.subscribeResult || fakeSubscription());
  const getSubscription = jest.fn().mockResolvedValue(opts.existing ?? null);
  const requestPermission = jest.fn().mockResolvedValue(opts.requestPermission || 'granted');
  const registration = { pushManager: { subscribe, getSubscription } };

  (window as any).Notification = {
    permission: opts.permission || 'default',
    requestPermission,
  };
  (window as any).PushManager = function PushManager() {};
  const container: any = {
    ready: opts.readyHangs ? new Promise(() => {}) : Promise.resolve(registration),
  };
  const getRegistration = jest
    .fn()
    .mockResolvedValue(opts.registered === false ? undefined : registration);
  if (opts.hasGetRegistration !== false) container.getRegistration = getRegistration;
  Object.defineProperty(window.navigator, 'serviceWorker', {
    configurable: true,
    value: container,
  });
  return { subscribe, getSubscription, requestPermission, getRegistration, registration };
};

/** Lets every already-resolved promise in the chain settle. */
const flushMicrotasks = async () => {
  for (let i = 0; i < 8; i += 1) await Promise.resolve();
};

const removePushAPIs = () => {
  delete (window as any).Notification;
  delete (window as any).PushManager;
  Object.defineProperty(window.navigator, 'serviceWorker', { configurable: true, value: undefined });
};

beforeEach(() => {
  jest.clearAllMocks();
  api.subscribe.mockResolvedValue({ data: {} } as any);
  api.unsubscribe.mockResolvedValue({ data: undefined } as any);
  api.list.mockResolvedValue({ data: { subscriptions: [] } } as any);
});

afterEach(removePushAPIs);

describe('urlBase64ToUint8Array', () => {
  it('decodes base64url, restoring padding and the - _ characters', () => {
    // "BF-c_Ab0" is "BF+c/Ab0" in standard base64, which decodes to these bytes.
    expect(Array.from(urlBase64ToUint8Array(PUBLIC_KEY))).toEqual([4, 95, 156, 252, 6, 244]);
  });

  it('returns bytes backed by a plain ArrayBuffer (what the Push API needs)', () => {
    expect(urlBase64ToUint8Array(PUBLIC_KEY).buffer).toBeInstanceOf(ArrayBuffer);
  });
});

describe('supportsPush', () => {
  it('is false when any of the three APIs is missing', () => {
    removePushAPIs();
    expect(supportsPush()).toBe(false);

    installPushAPIs({});
    delete (window as any).PushManager;
    expect(supportsPush()).toBe(false);
  });

  it('is true with Notification, PushManager and a service worker', () => {
    installPushAPIs({});
    expect(supportsPush()).toBe(true);
  });
});

describe('currentPermission', () => {
  it("reports the browser's permission, or 'default' when unsupported", () => {
    removePushAPIs();
    expect(currentPermission()).toBe('default');

    installPushAPIs({ permission: 'denied' });
    expect(currentPermission()).toBe('denied');
  });
});

describe('getServiceWorkerRegistration', () => {
  it('asks getRegistration first, without touching ready', async () => {
    const { getRegistration, registration } = installPushAPIs({});
    await expect(getServiceWorkerRegistration()).resolves.toBe(registration);
    expect(getRegistration).toHaveBeenCalled();
  });

  it('falls back to ready when nothing is registered yet', async () => {
    const { registration } = installPushAPIs({ registered: false });
    await expect(getServiceWorkerRegistration()).resolves.toBe(registration);
  });

  it('still works on a browser with no getRegistration', async () => {
    const { registration } = installPushAPIs({ hasGetRegistration: false });
    await expect(getServiceWorkerRegistration()).resolves.toBe(registration);
  });

  // The defect this guards: `ready` never settles when registration failed or
  // was blocked, so awaiting it hung the caller forever with nothing to show.
  it('gives up on a ready that never settles instead of hanging', async () => {
    jest.useFakeTimers();
    try {
      installPushAPIs({ registered: false, readyHangs: true });
      const pending = getServiceWorkerRegistration();
      await flushMicrotasks();
      jest.advanceTimersByTime(SERVICE_WORKER_READY_TIMEOUT_MS);
      await expect(pending).resolves.toBeNull();
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('getExistingSubscription', () => {
  it('answers null on a browser without push', async () => {
    removePushAPIs();
    await expect(getExistingSubscription()).resolves.toBeNull();
  });

  it("answers this device's subscription when there is one", async () => {
    const existing = fakeSubscription();
    installPushAPIs({ existing });
    await expect(getExistingSubscription()).resolves.toBe(existing);
  });

  it('answers null, rather than hanging, when no service worker turns up', async () => {
    jest.useFakeTimers();
    try {
      installPushAPIs({ existing: fakeSubscription(), registered: false, readyHangs: true });
      const pending = getExistingSubscription();
      await flushMicrotasks();
      jest.advanceTimersByTime(SERVICE_WORKER_READY_TIMEOUT_MS);
      await expect(pending).resolves.toBeNull();
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('reconcileThisDevice', () => {
  it('is on only when the server also knows the endpoint', async () => {
    const existing = fakeSubscription();
    installPushAPIs({ existing, permission: 'granted' });
    api.list.mockResolvedValue({
      data: { subscriptions: [{ id: 's-1', endpoint: existing.endpoint }] },
    } as any);

    await expect(reconcileThisDevice(PUBLIC_KEY)).resolves.toEqual({
      status: 'on',
      subscription: existing,
    });
  });

  it('reports a browser subscription the server has never heard of', async () => {
    const existing = fakeSubscription();
    installPushAPIs({ existing, permission: 'granted' });
    api.list.mockResolvedValue({
      data: { subscriptions: [{ id: 's-9', endpoint: 'https://push.example.com/someone-else' }] },
    } as any);

    await expect(reconcileThisDevice(PUBLIC_KEY)).resolves.toEqual({
      status: 'unregistered',
      subscription: existing,
    });
  });

  it('is off when this device has no subscription', async () => {
    installPushAPIs({ existing: null });
    await expect(reconcileThisDevice(PUBLIC_KEY)).resolves.toEqual({ status: 'off' });
    expect(api.list).not.toHaveBeenCalled();
  });

  it('is off when the subscription was taken with another VAPID key', async () => {
    const stale = fakeSubscription();
    stale.options = { applicationServerKey: urlBase64ToUint8Array('AAAA').buffer };
    installPushAPIs({ existing: stale, permission: 'granted' });

    await expect(reconcileThisDevice(PUBLIC_KEY)).resolves.toEqual({ status: 'off' });
    expect(api.list).not.toHaveBeenCalled();
  });

  it('is off, not on, when the device list cannot be read', async () => {
    installPushAPIs({ existing: fakeSubscription(), permission: 'granted' });
    api.list.mockRejectedValue(new Error('offline'));

    await expect(reconcileThisDevice(PUBLIC_KEY)).resolves.toEqual({ status: 'off' });
  });

  it('reports an unsupported browser and an unavailable service worker', async () => {
    removePushAPIs();
    await expect(reconcileThisDevice(PUBLIC_KEY)).resolves.toEqual({
      status: 'unavailable',
      reason: 'unsupported',
    });

    jest.useFakeTimers();
    try {
      installPushAPIs({ registered: false, readyHangs: true });
      const pending = reconcileThisDevice(PUBLIC_KEY);
      await flushMicrotasks();
      jest.advanceTimersByTime(SERVICE_WORKER_READY_TIMEOUT_MS);
      await expect(pending).resolves.toEqual({
        status: 'unavailable',
        reason: 'no-service-worker',
      });
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('subscribeThisDevice', () => {
  it('requests permission, subscribes with the VAPID key, and registers the device', async () => {
    const { subscribe, requestPermission } = installPushAPIs({ requestPermission: 'granted' });

    await subscribeThisDevice(PUBLIC_KEY);

    expect(requestPermission).toHaveBeenCalled();
    const options = subscribe.mock.calls[0][0];
    expect(options.userVisibleOnly).toBe(true);
    // The key must be bytes, not the base64url string.
    expect(options.applicationServerKey).toBeInstanceOf(Uint8Array);
    expect(Array.from(options.applicationServerKey as Uint8Array)).toEqual(
      Array.from(urlBase64ToUint8Array(PUBLIC_KEY))
    );

    expect(api.subscribe).toHaveBeenCalledWith(
      expect.objectContaining({
        endpoint: 'https://push.example.com/abc',
        keys: { p256dh: 'the-p256dh', auth: 'the-auth' },
      })
    );
  });

  it('refuses, with a message to show, when permission is denied', async () => {
    const { subscribe } = installPushAPIs({ requestPermission: 'denied' });

    await expect(subscribeThisDevice(PUBLIC_KEY)).rejects.toThrow(/blocked for this site/i);
    expect(subscribe).not.toHaveBeenCalled();
    expect(api.subscribe).not.toHaveBeenCalled();
  });

  it('refuses when the server has no key configured', async () => {
    installPushAPIs({});
    await expect(subscribeThisDevice('')).rejects.toThrow(/not configured/i);
    expect(api.subscribe).not.toHaveBeenCalled();
  });

  it('refuses on a browser without push', async () => {
    removePushAPIs();
    await expect(subscribeThisDevice(PUBLIC_KEY)).rejects.toThrow(/does not support/i);
  });

  it('says the service worker is unavailable rather than hanging', async () => {
    jest.useFakeTimers();
    try {
      const { subscribe } = installPushAPIs({ registered: false, readyHangs: true });
      const pending = subscribeThisDevice(PUBLIC_KEY);
      await flushMicrotasks();
      jest.advanceTimersByTime(SERVICE_WORKER_READY_TIMEOUT_MS);
      await expect(pending).rejects.toThrow(NO_SERVICE_WORKER_MESSAGE);
      expect(subscribe).not.toHaveBeenCalled();
      expect(api.subscribe).not.toHaveBeenCalled();
    } finally {
      jest.useRealTimers();
    }
  });

  it('re-registers an existing subscription instead of taking a second one', async () => {
    const existing = fakeSubscription('https://push.example.com/already');
    const { subscribe } = installPushAPIs({ existing });

    await subscribeThisDevice(PUBLIC_KEY);

    expect(subscribe).not.toHaveBeenCalled();
    // The POST is idempotent on the endpoint, so this repairs a lost row.
    expect(api.subscribe).toHaveBeenCalledWith(
      expect.objectContaining({ endpoint: 'https://push.example.com/already' })
    );
  });

  it('drops a subscription taken with a different VAPID key and takes a fresh one', async () => {
    const stale = fakeSubscription('https://push.example.com/stale');
    stale.options = { applicationServerKey: urlBase64ToUint8Array('AAAA').buffer };
    const fresh = fakeSubscription('https://push.example.com/fresh');
    const { subscribe } = installPushAPIs({ existing: stale, subscribeResult: fresh });

    await subscribeThisDevice(PUBLIC_KEY);

    expect(stale.unsubscribe).toHaveBeenCalled();
    expect(subscribe).toHaveBeenCalled();
    expect(api.subscribe).toHaveBeenCalledWith(
      expect.objectContaining({ endpoint: 'https://push.example.com/fresh' })
    );
  });
});

describe('unsubscribeThisDevice', () => {
  it('tells the server first, then drops the browser subscription', async () => {
    const existing = fakeSubscription();
    installPushAPIs({ existing });
    const order: string[] = [];
    api.unsubscribe.mockImplementation(async () => {
      order.push('server');
      return { data: undefined } as any;
    });
    existing.unsubscribe.mockImplementation(async () => {
      order.push('browser');
      return true;
    });

    await unsubscribeThisDevice();

    expect(api.unsubscribe).toHaveBeenCalledWith(existing.endpoint);
    expect(order).toEqual(['server', 'browser']);
  });

  it('is a no-op when this device has no subscription', async () => {
    installPushAPIs({ existing: null });
    await unsubscribeThisDevice();
    expect(api.unsubscribe).not.toHaveBeenCalled();
  });

  it('is a no-op on a browser without push', async () => {
    removePushAPIs();
    await unsubscribeThisDevice();
    expect(api.unsubscribe).not.toHaveBeenCalled();
  });
});

describe('matchesApplicationServerKey', () => {
  it('gives a browser that hides options the benefit of the doubt', () => {
    const sub = fakeSubscription();
    sub.options = {};
    expect(matchesApplicationServerKey(sub as unknown as PushSubscription, PUBLIC_KEY)).toBe(true);
  });

  it('spots a key of a different length or content', () => {
    const sub = fakeSubscription();
    expect(matchesApplicationServerKey(sub as unknown as PushSubscription, PUBLIC_KEY)).toBe(true);
    expect(matchesApplicationServerKey(sub as unknown as PushSubscription, 'AAAA')).toBe(false);
  });
});
