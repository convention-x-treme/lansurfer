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

export function initNotificationSettings(): void {
  const form = document.querySelector<HTMLFormElement>("[data-subscribe-form]");
  const button = form?.querySelector<HTMLButtonElement>("[data-subscribe-button]");
  const status = form?.querySelector<HTMLElement>("[data-push-status]");
  const publicKey = form?.closest<HTMLElement>("[data-vapid-public-key]")?.dataset.vapidPublicKey;
  if (!form || !button || !status || !publicKey) return;

  const showStatus = (message: string) => {
    status.hidden = false;
    status.textContent = message;
  };
  if (!window.isSecureContext || !("Notification" in window) || !("PushManager" in window) || !("serviceWorker" in navigator)) {
    button.disabled = true;
    showStatus("Push notifications are unavailable in this browser.");
    return;
  }

  void registerServiceWorker().then(({ registration }) => {
    if (!registration) {
      button.disabled = true;
      showStatus("Push notifications are unavailable in this browser.");
    }
  });

  button.addEventListener("click", async () => {
    if (button.disabled) return;
    button.disabled = true;
    try {
      let permission = Notification.permission;
      if (permission === "default") permission = await Notification.requestPermission();
      if (permission !== "granted") {
        showStatus(permission === "denied" ? "Notifications are blocked in this browser." : "Notification permission is required.");
        return;
      }
      const { registration } = await registerServiceWorker();
      if (!registration) {
        showStatus("Push notifications are unavailable in this browser.");
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
    } catch {
      showStatus("Could not enable notifications on this device. Try again.");
    } finally {
      button.disabled = false;
    }
  });
}
