// Progressive enhancement for the read-only /admin/pwa diagnostics page. All
// server-rendered facts already work without this script; it only fills in
// what only the browser can report. Loaded on that one admin page, never
// bundled into app.ts's always-loaded script.
import { registerServiceWorker } from "./pwa";

function setFact(root: HTMLElement, name: string, value: string): void {
  const cell = root.querySelector<HTMLElement>(`[data-fact="${name}"]`);
  if (cell) cell.textContent = value;
}

function registrationState(registration: ServiceWorkerRegistration | null): string {
  if (!registration) return "none";
  const worker = registration.active ?? registration.waiting ?? registration.installing;
  return worker ? `${worker.state} (scope ${registration.scope})` : `registered (scope ${registration.scope})`;
}

async function runDiagnostics(): Promise<void> {
  const root = document.querySelector<HTMLElement>("[data-pwa-diagnostics]");
  if (!root) return;
  root.dataset.diagnosticsRan = "true";
  setFact(root, "diagnostics-ran", "Yes");

  setFact(root, "secure-context", String(window.isSecureContext));
  const supported = "serviceWorker" in navigator;
  setFact(root, "supported", String(supported));
  if (!supported) {
    setFact(root, "registration", "unsupported");
    setFact(root, "controller", "unsupported");
    setFact(root, "error", "none");
    return;
  }

  const { registration, error } = await registerServiceWorker();
  setFact(root, "registration", registrationState(registration));
  setFact(root, "controller", navigator.serviceWorker.controller ? "controlled" : "uncontrolled");
  setFact(root, "error", error ?? "none");
}

void runDiagnostics();
