import { registerServiceWorker } from "./pwa";

function decodeVapidKey(key: string): Uint8Array<ArrayBuffer> {
  const encoded = key.replace(/-/g, "+").replace(/_/g, "/");
  const decoded = atob(encoded.padEnd(Math.ceil(encoded.length / 4) * 4, "="));
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0));
}

function encodeKey(key: ArrayBuffer | null): string {
  if (!key) throw new Error("Browser subscription is incomplete.");
  return btoa(String.fromCharCode(...new Uint8Array(key)));
}

async function unsubscribeCurrentBrowser(): Promise<void> {
  try {
    const { registration } = await registerServiceWorker();
    const subscription = await registration?.pushManager?.getSubscription();
    await subscription?.unsubscribe();
  } catch {
    // Browser cleanup must not block the server-side action.
  }
}

export function initNotificationSettings(): void {
  const logoutForm = document.querySelector<HTMLFormElement>("[data-logout-form]");
  logoutForm?.addEventListener("submit", (event) => {
    event.preventDefault();
    void unsubscribeCurrentBrowser().then(() => logoutForm.submit());
  });
  const form = document.querySelector<HTMLFormElement>("[data-subscribe-form]");
  const button = form?.querySelector<HTMLButtonElement>("[data-subscribe-button]");
  const status = form?.querySelector<HTMLElement>("[data-push-status]");
  const panel = form?.closest<HTMLElement>("[data-vapid-public-key]");
  const publicKey = panel?.dataset.vapidPublicKey;
  if (!form || !button || !status || !publicKey) return;
  document.querySelectorAll<HTMLFormElement>('[data-current-device-remove="true"]').forEach((removeForm) => {
    removeForm.addEventListener("submit", (event) => {
      event.preventDefault();
      void unsubscribeCurrentBrowser().then(() => removeForm.submit());
    });
  });


  const blockedMessage = "Notifications are blocked in this browser. Allow notifications in your browser settings, then reload this page.";
  const showStatus = (state: string, message: string) => {
    const error = ["blocked", "permission-required", "unavailable", "error"].includes(state);
    status.className = error ? "alert alert-error text-sm" : "text-sm";
    status.setAttribute("role", error ? "alert" : "status");
    status.dataset.pushState = state;
    status.hidden = false;
    if (state === "blocked") button.disabled = true;
    status.textContent = message;
  };
  if (!window.isSecureContext || !("Notification" in window) || !("PushManager" in window) || !("serviceWorker" in navigator)) {
    button.disabled = true;
    showStatus("unavailable", "Push notifications are unavailable in this browser.");
    return;
  }

  void registerServiceWorker().then(async ({ registration }) => {
    const permission = Notification.permission;
    if (!registration && permission !== "denied") {
      button.disabled = true;
      showStatus("unavailable", "Push notifications are unavailable in this browser.");
      return;
    }
    try {
      const subscription = registration ? await registration.pushManager.getSubscription() : null;
      if (panel?.dataset.currentDeviceId !== "0" && (permission === "denied" || !subscription)) {
        const csrf = form.elements.namedItem("csrf_token");
        if (!(csrf instanceof HTMLInputElement)) throw new Error("Missing CSRF token");
        const response = await fetch("/account/notifications/reconcile", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/x-www-form-urlencoded" },
          body: new URLSearchParams({ csrf_token: csrf.value }),
        });
        if (!response.ok) throw new Error("Device reconciliation failed");
        window.location.reload();
        return;
      }
      if (permission === "denied") showStatus("blocked", blockedMessage);
      else if (panel?.dataset.pushEnabled === "false") showStatus("disabled", "Push delivery is disabled for your account.");
      else if (panel?.dataset.deviceCount === "0") showStatus("no-devices", "Push delivery is enabled, but no devices are registered.");
      else if (panel?.dataset.currentDeviceId !== "0") showStatus("current-device", "This browser is registered for push notifications.");
      else showStatus("other-devices", "Other devices are registered for push notifications.");
    } catch {
      showStatus("unavailable", "Could not check browser push state. Try reloading.");
    }
  });

  button.addEventListener("click", async () => {
    if (button.disabled) return;
    button.disabled = true;
    try {
      let permission = Notification.permission;
      if (permission === "default") permission = await Notification.requestPermission();
      if (permission !== "granted") {
        showStatus(permission === "denied" ? "blocked" : "permission-required", permission === "denied" ? blockedMessage : "Notification permission is required.");
        return;
      }
      const { registration } = await registerServiceWorker();
      if (!registration) {
        showStatus("unavailable", "Push notifications are unavailable in this browser.");
        return;
      }
      const subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: decodeVapidKey(publicKey),
      });
      form.querySelector<HTMLInputElement>("[data-subscribe-endpoint]")!.value = subscription.endpoint;
      form.querySelector<HTMLInputElement>("[data-subscribe-p256dh]")!.value = encodeKey(subscription.getKey("p256dh"));
      form.querySelector<HTMLInputElement>("[data-subscribe-auth]")!.value = encodeKey(subscription.getKey("auth"));
      form.requestSubmit();
    } catch (error) {
      // Brave ships with its push service switched off: permission is granted,
      // but subscribe() rejects with AbortError until the user opts in.
      if ("brave" in navigator && error instanceof DOMException && error.name === "AbortError") {
        showStatus("error", "Brave blocks push messaging by default. Turn on “Use Google services for push messaging” in brave://settings/privacy, restart Brave, then try again.");
      } else {
        showStatus("error", "Could not enable notifications on this device. Try again.");
      }
    } finally {
      if (status.dataset.pushState !== "blocked") button.disabled = false;
    }
  });
}
