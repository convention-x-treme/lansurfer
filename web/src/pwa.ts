export interface ServiceWorkerRegistrationResult {
  registration: ServiceWorkerRegistration | null;
  error: string | null;
}

// The one service-worker registration path LANsurfer has. Normal startup
// (app.ts) and the admin diagnostics page both import this function instead
// of calling navigator.serviceWorker themselves — Track E's later
// push-permission UI does the same — so there is never a second registration
// racing this one. Calling it again when already registered is safe: the
// browser resolves it to the existing registration rather than reinstalling.
//
// Registration failure must never surface as a player-facing error, so it is
// reported here rather than thrown; ordinary startup discards it, and only
// the admin diagnostics page reads it.
export async function registerServiceWorker(): Promise<ServiceWorkerRegistrationResult> {
  if (!("serviceWorker" in navigator)) return { registration: null, error: null };
  try {
    const registration = await navigator.serviceWorker.register("/service-worker.js", {
      scope: "/",
      updateViaCache: "none",
    });
    return { registration, error: null };
  } catch (error) {
    return { registration: null, error: error instanceof Error ? error.message : String(error) };
  }
}
