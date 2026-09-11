import {
  currentPermission,
  getExistingSubscription,
  matchesApplicationServerKey,
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
const installPushAPIs = (opts: {
  permission?: NotificationPermission;
  requestPermission?: NotificationPermission;
  existing?: FakeSubscription | null;
  subscribeResult?: FakeSubscription;
}) => {
  const subscribe = jest.fn().mockResolvedValue(opts.subscribeResult || fakeSubscription());
  const getSubscription = jest.fn().mockResolvedValue(opts.existing ?? null);
  const requestPermission = jest.fn().mockResolvedValue(opts.requestPermission || 'granted');

  (window as any).Notification = {
    permission: opts.permission || 'default',
    requestPermission,
  };
  (window as any).PushManager = function PushManager() {};
  Object.defineProperty(window.navigator, 'serviceWorker', {
    configurable: true,
    value: { ready: Promise.resolve({ pushManager: { subscribe, getSubscription } }) },
  });
  return { subscribe, getSubscription, requestPermission };
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
