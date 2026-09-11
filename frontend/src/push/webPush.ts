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
  | 'denied';

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
  if (!supportsPush()) return null;
  const registration = await navigator.serviceWorker.ready;
  return registration.pushManager.getSubscription();
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

  const registration = await navigator.serviceWorker.ready;
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
  const registration = await navigator.serviceWorker.ready;
  const subscription = await registration.pushManager.getSubscription();
  if (subscription) {
    await pushAPI.unsubscribe(subscription.endpoint);
    await subscription.unsubscribe();
  }
};
