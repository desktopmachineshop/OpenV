// Minimal service worker for PWA installability and web push.
//
// Deliberately no caching: OpenV is a live collaborative app, and stale
// cached shells cause more trouble than offline support is worth here. The
// no-op fetch handler exists only to satisfy older Chrome versions'
// install criteria; requests go straight to the network as usual.
self.addEventListener('install', () => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener('fetch', () => {
  // Intentionally empty: the browser performs the request normally.
});

// --- Web push (REQ-109) -----------------------------------------------------
//
// The server sends {title, body, url, tag} (internal/notify/push.go). `tag`
// coalesces banners — a second notification with the same tag replaces the
// first rather than stacking — and `url` is the deep link a tap follows, the
// same destination the in-app bell and the notification email use.
//
// Everything here is defensive: a push with no payload, a payload that is not
// JSON, or one missing fields must still show *something*, because some
// browsers show their own "This site has been updated in the background"
// notice if the push event resolves without one.

const FALLBACK_TITLE = 'OpenV';
const FALLBACK_BODY = 'You have a new notification.';

function parsePush(event) {
  if (!event.data) return {};
  try {
    return event.data.json() || {};
  } catch (e) {
    // Not JSON — treat the raw text as the body.
    try {
      return { body: event.data.text() };
    } catch (e2) {
      return {};
    }
  }
}

self.addEventListener('push', (event) => {
  const payload = parsePush(event);
  const title = payload.title || FALLBACK_TITLE;
  const url = payload.url || '/';
  event.waitUntil(
    self.registration.showNotification(title, {
      body: payload.body || FALLBACK_BODY,
      // The installed-app icons double as the notification icon and badge.
      icon: '/icon-192.png',
      badge: '/icon-192.png',
      // Coalesce per the server's tag; renotify so a replacement still
      // alerts rather than updating silently.
      tag: payload.tag || 'openv',
      renotify: Boolean(payload.tag),
      data: { url },
    })
  );
});

// A tap focuses an OpenV window that is already open — navigating it to the
// deep link — and only opens a new one when there is none. Two windows of a
// collaborative app are rarely what anyone wanted.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = (event.notification.data && event.notification.data.url) || '/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      for (const client of clientList) {
        if (!('focus' in client)) continue;
        // Same origin (matchAll is origin-scoped) — reuse this window.
        if ('navigate' in client) {
          return client.focus().then((focused) => (focused || client).navigate(target));
        }
        return client.focus();
      }
      if (self.clients.openWindow) return self.clients.openWindow(target);
      return undefined;
    })
  );
});
