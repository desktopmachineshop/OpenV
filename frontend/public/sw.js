// Minimal service worker for PWA installability.
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
