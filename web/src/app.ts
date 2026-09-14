import htmx from "htmx.org";

const themeStorageKey = "lansurfer-theme";
const darkTheme = "lansurfer-dark";
const lightTheme = "lansurfer-light";

type Theme = typeof darkTheme | typeof lightTheme;

function storedTheme(): Theme | null {
  try {
    const value = window.localStorage.getItem(themeStorageKey);
    return value === darkTheme || value === lightTheme ? value : null;
  } catch {
    return null;
  }
}

function preferredTheme(): Theme {
  return storedTheme() ?? (window.matchMedia("(prefers-color-scheme: light)").matches ? lightTheme : darkTheme);
}

function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;

  document.querySelectorAll<HTMLButtonElement>("[data-theme-toggle]").forEach((button) => {
    const isDark = theme === darkTheme;
    button.setAttribute("aria-label", isDark ? "Use light theme" : "Use dark theme");
    button.setAttribute("title", isDark ? "Use light theme" : "Use dark theme");
    const icon = button.querySelector<HTMLElement>("[data-theme-icon]");
    if (icon) icon.textContent = isDark ? "☾" : "☀";
  });
}

applyTheme(preferredTheme());

document.querySelectorAll<HTMLInputElement>("[data-timezone-offset]").forEach((input) => {
  input.value = String(new Date().getTimezoneOffset());
});

document.querySelectorAll<HTMLButtonElement>("[data-theme-toggle]").forEach((button) => {
  button.addEventListener("click", () => {
    const nextTheme = document.documentElement.dataset.theme === darkTheme ? lightTheme : darkTheme;
    try {
      window.localStorage.setItem(themeStorageKey, nextTheme);
    } catch {
      // A blocked storage API should not prevent an in-page theme change.
    }
    applyTheme(nextTheme);
  });
});

const description = document.querySelector<HTMLTextAreaElement>("[data-description]");
const descriptionCount = document.querySelector<HTMLElement>("[data-description-count]");
description?.addEventListener("input", () => {
  if (descriptionCount) descriptionCount.textContent = `${Array.from(description.value).length} / 1000`;
});

type GameRequestEvent = {
  type: "GameRequestCreated" | "GameRequestJoined" | "GameRequestLeft" | "GameRequestClosed" | "GameRequestCancelled";
  requestId: string;
  gameId?: string;
};

function refreshGameRequests(event: GameRequestEvent): void {
  if (document.querySelector("[data-game-request-list]")) {
    const highlight = event.type === "GameRequestCreated" ? `?highlight=${encodeURIComponent(event.requestId)}` : "";
    void htmx.ajax("get", `/game-requests/refresh${highlight}`, {
      target: "#game-request-list",
      swap: "outerHTML",
    });
  }

  const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
  if (detail?.dataset.gameRequestId === event.requestId) {
    void htmx.ajax("get", `/game-requests/${encodeURIComponent(event.requestId)}/refresh`, {
      target: "#game-request-detail",
      swap: "outerHTML",
    });
  }
}

const realtime = document.querySelector<HTMLElement>("[data-realtime-url]");
if (realtime?.dataset.realtimeUrl) {
  const events = new EventSource(realtime.dataset.realtimeUrl);
  events.addEventListener("open", () => document.querySelector("[data-live-connection-alert]")?.remove());
  events.addEventListener("game-request", (message) => {
    try {
      refreshGameRequests(JSON.parse(message.data) as GameRequestEvent);
    } catch {
      // A malformed or stale signal can be ignored; the next event refreshes current state.
    }
  });
  events.addEventListener("error", () => {
    if (document.querySelector("[data-live-connection-alert]")) return;
    const notice = document.createElement("div");
    notice.className = "alert alert-warning";
    notice.dataset.liveConnectionAlert = "";
    notice.setAttribute("role", "status");
    notice.textContent = "Live connection lost. Retrying…";
    document.querySelector("#live-notices")?.append(notice);
  });
}

document.addEventListener("click", (event) => {
  const target = event.target;
  if (target instanceof Element && target.closest("[data-dismiss-live-toast]")) {
    target.closest("[data-live-toast]")?.remove();
  }
  if (target instanceof Element && target.closest("[data-close-navigation]")) {
    target.closest<HTMLDetailsElement>("[data-mobile-navigation]")?.removeAttribute("open");
  }
});

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (form instanceof HTMLFormElement && form.dataset.confirm && !window.confirm(form.dataset.confirm)) {
    event.preventDefault();
  }
});

document.addEventListener("htmx:oobAfterSwap", () => {
  document.querySelectorAll<HTMLElement>("[data-live-toast]").forEach((toast) => {
    const targetID = toast.dataset.visibilityTarget;
    const target = targetID ? document.querySelector(`#game-request-${CSS.escape(targetID)}`) : null;
    if (target) {
      const bounds = target.getBoundingClientRect();
      if (bounds.top >= 0 && bounds.bottom <= window.innerHeight) {
        toast.remove();
        return;
      }
    }
    window.setTimeout(() => toast.remove(), 8000);
  });
});
