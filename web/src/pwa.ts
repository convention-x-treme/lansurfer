export interface ServiceWorkerRegistrationResult {
  registration: ServiceWorkerRegistration | null;
  error: string | null;
}

// The one service-worker registration path LANsurfer has. Normal startup
// (app.ts) and the admin diagnostics page both import this function instead
// of calling navigator.serviceWorker themselves — Track E's later
// push-permission UI does the same. The in-flight/settled promise is
// memoized so concurrent callers on the same page (app.ts's window "load"
// listener and the admin diagnostics module both run on /admin/pwa) share
// one actual navigator.serviceWorker.register() call rather than each
// issuing their own.
//
// Registration failure must never surface as a player-facing error, so it is
// reported here rather than thrown; ordinary startup discards it, and only
// the admin diagnostics page reads it.
let registrationResult: Promise<ServiceWorkerRegistrationResult> | null = null;

export async function registerServiceWorker(): Promise<ServiceWorkerRegistrationResult> {
  if (!("serviceWorker" in navigator)) return { registration: null, error: null };
  if (!registrationResult) {
    registrationResult = navigator.serviceWorker
      .register("/service-worker.js", { scope: "/", updateViaCache: "none" })
      .then((registration) => ({ registration, error: null }))
      .catch((error: unknown) => ({
        registration: null,
        error: error instanceof Error ? error.message : String(error),
      }));
  }
  return registrationResult;
}
