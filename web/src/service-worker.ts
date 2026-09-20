// Track D's complete service-worker contract: install/activate only. There is
// deliberately no fetch handler, no CacheStorage, no IndexedDB, no message
// handler, no sync/periodicsync, and no offline shell — LANsurfer does not
// promise offline application behavior. Track E later adds push and
// notificationclick handlers to this same file; it must not need a second
// worker file or a second registration path.
//
// An empty export makes this file a module rather than a global script, so
// the narrower ServiceWorkerGlobalScope declaration below can shadow the
// ambient (generic worker) `self` the "WebWorker" lib provides instead of
// colliding with it.
export {};

// A dedicated Service Worker global scope, not a DOM window: `self` here is
// ServiceWorkerGlobalScope.
declare const self: ServiceWorkerGlobalScope;

self.addEventListener("install", () => {
  // Replace any previously installed worker immediately: this worker caches
  // nothing, so there is no stale content an earlier version could still be
  // serving that a delayed activation would protect.
  void self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
