import htmx from "htmx.org";

// htmx injects an inline <style> for its indicator classes on startup, which the
// Content-Security-Policy blocks. app.css defines those classes itself.
htmx.config.includeIndicatorStyles = false;

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

// Without an event the current view is refreshed as a whole, which is what a
// page needs after it was disconnected from the stream for a while.
function refreshGameRequests(event?: GameRequestEvent): void {
  if (document.querySelector("[data-game-request-list]")) {
    const highlight = event?.type === "GameRequestCreated" ? `?highlight=${encodeURIComponent(event.requestId)}` : "";
    void htmx.ajax("get", `/game-requests/refresh${highlight}`, {
      target: "#game-request-list",
      swap: "outerHTML",
    });
  }

  const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
  const detailID = detail?.dataset.gameRequestId;
  if (detailID && (event === undefined || detailID === event.requestId)) {
    void htmx.ajax("get", `/game-requests/${encodeURIComponent(detailID)}/refresh`, {
      target: "#game-request-detail",
      swap: "outerHTML",
    });
  }
}

const realtime = document.querySelector<HTMLElement>("[data-realtime-url]");
if (realtime?.dataset.realtimeUrl) {
  const realtimeURL = realtime.dataset.realtimeUrl;
  let events: EventSource | null = null;

  const connectRealtime = (): void => {
    if (events) return;
    events = new EventSource(realtimeURL);
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
  };

  const disconnectRealtime = (): void => {
    events?.close();
    events = null;
  };

  // An open stream occupies one connection for as long as its page lives, and
  // a browser speaking HTTP/1.1 opens at most six per origin. A few pages
  // holding a stream each therefore exhaust the pool and every further request
  // waits forever. Only a visible page needs live updates, so a hidden one
  // releases its stream and refetches the current state when it returns.
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      disconnectRealtime();
      return;
    }
    connectRealtime();
    refreshGameRequests();
  });

  // A page kept in the back/forward cache is hidden without a visibility
  // change, so release the stream on unload as well.
  window.addEventListener("pagehide", disconnectRealtime);
  window.addEventListener("pageshow", () => {
    if (!document.hidden) connectRealtime();
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
