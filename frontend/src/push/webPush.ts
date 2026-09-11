// Browser-side web push plumbing (REQ-109).
//
// Kept out of the settings component so the awkward parts — feature
// detection, the base64url → Uint8Array conversion the Push API insists on,
// and the order of operations when subscribing — are testable on their own
// and have one place to be wrong.
//
// The flow, in both directions:
//   subscribe:   permission → service worker ready → pushManager.subscribe
//                → POST the subscription to OpenV
//   unsubscribe: POST-less first — tell OpenV to forget the endpoint, then
//                drop the browser-side subscription

import { pushAPI } from '../api/client';

/** Why push cannot be offered right now; '' means it can. */
export type PushUnavailableReason =
  | ''
  /** The browser has no Notification/PushManager/service worker. */
  | 'unsupported'
  /** The deployment has no VAPID keys configured. */
  | 'not-configured'
  /** The member (or their OS) has blocked notifications for this origin. */
  | 'denied'
  /** The service worker never became available (blocked, failed, private window). */
  | 'no-service-worker';

/**
 * How long to wait for `navigator.serviceWorker.ready`. That promise has no
 * timeout of its own and never rejects: if registration failed, was blocked
 * by the browser, or the worker script 404s, it simply never settles — and
 * every await of it hangs forever, which is what put the settings toggle at
 * "Working…" with nothing to explain it.
 */
export const SERVICE_WORKER_READY_TIMEOUT_MS = 5000;

/** The message shown (and thrown) when the service worker never turns up. */
export const NO_SERVICE_WORKER_MESSAGE =
  'The service worker for this site is unavailable, so push cannot be set up here. Reload the page, or try again outside a private window.';

/** withTimeout resolves null if `p` has not settled within `ms`. */
const withTimeout = <T>(p: Promise<T>, ms: number): Promise<T | null> => {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return Promise.race<T | null>([
    p.then((value) => {
      clearTimeout(timer);
      return value;
    }),
    new Promise<null>((resolve) => {
      timer = setTimeout(() => resolve(null), ms);
    }),
  ]);
};

/**
 * getServiceWorkerRegistration answers this page's service worker
 * registration, or null when there is none to be had.
 *
 * `getRegistration()` is asked first because it *settles* — with the
 * registration, or with undefined — where `ready` only settles on success.
 * Only when nothing is registered yet is `ready` awaited, and then against a
 * timeout, so a browser that will never activate a worker produces a reason
 * rather than a wait with no end.
 */
export const getServiceWorkerRegistration = async (): Promise<ServiceWorkerRegistration | null> => {
  if (!supportsPush()) return null;
  const container = navigator.serviceWorker;
  try {
    if (typeof container.getRegistration === 'function') {
      const registered = await container.getRegistration();
      if (registered) return registered;
    }
  } catch {
    // Fall through: `ready` may still produce one.
  }
  try {
    return await withTimeout(container.ready, SERVICE_WORKER_READY_TIMEOUT_MS);
  } catch {
    return null;
  }
};

/**
 * supportsPush reports whether this browser has the three APIs web push
 * needs. iOS only gained them in 16.4, and only for an installed app, so a
 * false here is routine rather than exceptional.
 */
export const supportsPush = (): boolean =>
  typeof window !== 'undefined' &&
  'Notification' in window &&
  'serviceWorker' in navigator &&
  'PushManager' in window;

/**
 * urlBase64ToUint8Array converts the server's base64url VAPID public key into
 * the raw bytes `applicationServerKey` requires. Browsers accept a string in
 * theory and a BufferSource in practice, so always pass the bytes.
 */
export const urlBase64ToUint8Array = (base64String: string): Uint8Array<ArrayBuffer> => {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = window.atob(base64);
  // Backed by a plain ArrayBuffer (not SharedArrayBuffer), which is what
  // applicationServerKey's BufferSource type requires.
  const output = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i += 1) output[i] = raw.charCodeAt(i);
  return output;
};

/** currentPermission answers the Notification permission, or 'default' when unsupported. */
export const currentPermission = (): NotificationPermission => {
  if (!supportsPush()) return 'default';
  return Notification.permission;
};

/**
 * getExistingSubscription answers this device's current push subscription, or
 * null. Used on mount so the toggle shows the real state of the device rather
 * than the last thing this tab did.
 */
export const getExistingSubscription = async (): Promise<PushSubscription | null> => {
  const registration = await getServiceWorkerRegistration();
  if (!registration) return null;
  return registration.pushManager.getSubscription();
};

/**
 * DevicePushState is what the settings toggle needs to know about THIS
 * device, having reconciled the browser with the server:
 *
 *  - `on`           — subscribed here, with this VAPID key, and the server
 *                     has the endpoint on file;
 *  - `unregistered` — the browser holds a subscription the server does not
 *                     know (its row was lost, or the POST never landed).
 *                     Shown as off, with the toggle offered: turning it on
 *                     re-registers the same endpoint;
 *  - `off`          — no usable subscription here;
 *  - `unavailable`  — push cannot be offered at all, and why.
 */
export type DevicePushState =
  | { status: 'on'; subscription: PushSubscription }
  | { status: 'unregistered'; subscription: PushSubscription }
  | { status: 'off' }
  | { status: 'unavailable'; reason: PushUnavailableReason };

/**
 * reconcileThisDevice answers the real state of push on this device.
 *
 * A browser subscription on its own proves nothing: it may have been taken
 * with a previous VAPID pair (the push service would answer 403), or its row
 * may be gone server-side, and in both cases showing the toggle ON tells the
 * member they will be notified when they will not be. Every condition —
 * subscription present, key current, endpoint known to the server — has to
 * hold, or the answer is off.
 */
export const reconcileThisDevice = async (publicKey: string): Promise<DevicePushState> => {
  if (!supportsPush()) return { status: 'unavailable', reason: 'unsupported' };
  const registration = await getServiceWorkerRegistration();
  if (!registration) return { status: 'unavailable', reason: 'no-service-worker' };

  const subscription = await registration.pushManager.getSubscription();
  if (!subscription) return { status: 'off' };
  // Taken with a different application server key: unusable, and dropped and
  // retaken by subscribeThisDevice when the member turns the toggle on.
  if (publicKey && !matchesApplicationServerKey(subscription, publicKey)) return { status: 'off' };

  try {
    const { data } = await pushAPI.list();
    const known = (data.subscriptions || []).some((s) => s.endpoint === subscription.endpoint);
    return known ? { status: 'on', subscription } : { status: 'unregistered', subscription };
  } catch {
    // The device list could not be read, so the endpoint cannot be confirmed.
    // Off is the honest answer; turning it on re-registers harmlessly.
    return { status: 'off' };
  }
};

/**
 * matchesApplicationServerKey reports whether an existing subscription was
 * taken with this VAPID public key. A browser that does not expose
 * `options.applicationServerKey` gets the benefit of the doubt — there is
 * nothing to compare, and needlessly re-subscribing would lose the endpoint.
 */
export const matchesApplicationServerKey = (
  subscription: PushSubscription,
  publicKey: string
): boolean => {
  const current = subscription.options && subscription.options.applicationServerKey;
  if (!current) return true;
  const want = urlBase64ToUint8Array(publicKey);
  const have = new Uint8Array(current as ArrayBuffer);
  if (have.length !== want.length) return false;
  for (let i = 0; i < want.length; i += 1) if (have[i] !== want[i]) return false;
  return true;
};

/** serializeSubscription turns a PushSubscription into the POST body. */
const serializeSubscription = (sub: PushSubscription) => {
  const json = sub.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } };
  return {
    endpoint: json.endpoint || sub.endpoint,
    keys: { p256dh: json.keys?.p256dh || '', auth: json.keys?.auth || '' },
    user_agent: typeof navigator !== 'undefined' ? navigator.userAgent : '',
  };
};

/**
 * subscribeThisDevice asks for permission if needed, subscribes with the
 * server's VAPID public key, and registers the result with OpenV.
 *
 * Re-subscribing an already-subscribed device is fine and is in fact the
 * repair path: the POST is idempotent on the endpoint, so a device whose row
 * was lost server-side gets it back.
 *
 * Throws with a message fit to show when permission is refused or the browser
 * rejects the subscription.
 */
export const subscribeThisDevice = async (publicKey: string): Promise<PushSubscription> => {
  if (!supportsPush()) throw new Error('This browser does not support push notifications.');
  if (!publicKey) throw new Error('Push notifications are not configured on this server.');

  const permission = await Notification.requestPermission();
  if (permission !== 'granted') {
    throw new Error(
      permission === 'denied'
        ? 'Notifications are blocked for this site. Allow them in your browser settings and try again.'
        : 'Notification permission was not granted.'
    );
  }

  const registration = await getServiceWorkerRegistration();
  if (!registration) throw new Error(NO_SERVICE_WORKER_MESSAGE);
  // An existing subscription may have been taken with a previous VAPID pair.
  // Its encryption keys would still work, but the push service checks the
  // signature against the key the subscription was made with and answers 403,
  // so a stale one has to be dropped and taken again.
  let subscription: PushSubscription | null = await registration.pushManager.getSubscription();
  if (subscription && !matchesApplicationServerKey(subscription, publicKey)) {
    await subscription.unsubscribe();
    subscription = null;
  }
  if (!subscription) {
    subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    });
  }

  await pushAPI.subscribe(serializeSubscription(subscription));
  return subscription;
};

/**
 * unsubscribeThisDevice withdraws the subscription: OpenV forgets the
 * endpoint first, so a send can never race a browser-side unsubscribe and
 * leave a row nothing will ever clean up. A browser that has no subscription
 * to drop is not an error — the member asked to be unsubscribed and is.
 */
export const unsubscribeThisDevice = async (): Promise<void> => {
  if (!supportsPush()) return;
  const registration = await getServiceWorkerRegistration();
  if (!registration) return;
  const subscription = await registration.pushManager.getSubscription();
  if (subscription) {
    await pushAPI.unsubscribe(subscription.endpoint);
    await subscription.unsubscribe();
  }
};
