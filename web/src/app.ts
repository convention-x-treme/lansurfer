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

  const button = document.querySelector<HTMLButtonElement>("[data-theme-toggle]");
  if (button) {
    const isDark = theme === darkTheme;
    button.setAttribute("aria-label", isDark ? "Use light theme" : "Use dark theme");
    button.setAttribute("title", isDark ? "Use light theme" : "Use dark theme");
    button.textContent = isDark ? "Light" : "Dark";
  }
}

applyTheme(preferredTheme());

document.querySelector<HTMLButtonElement>("[data-theme-toggle]")?.addEventListener("click", () => {
  const nextTheme = document.documentElement.dataset.theme === darkTheme ? lightTheme : darkTheme;
  try {
    window.localStorage.setItem(themeStorageKey, nextTheme);
  } catch {
    // A blocked storage API should not prevent an in-page theme change.
  }
  applyTheme(nextTheme);
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
  events.addEventListener("game-request", (message) => {
    try {
      refreshGameRequests(JSON.parse(message.data) as GameRequestEvent);
    } catch {
      // A malformed or stale signal can be ignored; the next event refreshes current state.
    }
  });
}

document.addEventListener("click", (event) => {
  const target = event.target;
  if (target instanceof Element && target.closest("[data-dismiss-live-toast]")) {
    target.closest("[data-live-toast]")?.remove();
  }
});

document.addEventListener("htmx:oobAfterSwap", () => {
  document.querySelectorAll<HTMLElement>("[data-live-toast]").forEach((toast) => {
    window.setTimeout(() => toast.remove(), 8000);
  });
});
