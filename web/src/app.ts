import htmx from "htmx.org";

// htmx injects an inline <style> for its indicator classes on startup, which the
// Content-Security-Policy blocks. app.css defines those classes itself.
htmx.config.includeIndicatorStyles = false;

// The theme the document already paints with: the server renders an explicit
// choice into <html>, and without one the stylesheet follows the operating
// system. Setting it from script on load would repaint the page after the
// deferred bundle runs, which is the theme flash this avoids.
const themeCookieName = "lansurfer_theme";
const themeCookieMaxAge = 60 * 60 * 24 * 365;
const darkTheme = "lansurfer-dark";
const lightTheme = "lansurfer-light";

type Theme = typeof darkTheme | typeof lightTheme;

function currentTheme(): Theme {
  const rendered = document.documentElement.dataset.theme;
  if (rendered === darkTheme || rendered === lightTheme) return rendered;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? lightTheme : darkTheme;
}

function rememberTheme(theme: Theme): void {
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  document.cookie = `${themeCookieName}=${theme}; path=/; max-age=${themeCookieMaxAge}; SameSite=Lax${secure}`;
}

function describeToggles(theme: Theme): void {
  document.querySelectorAll<HTMLButtonElement>("[data-theme-toggle]").forEach((button) => {
    const isDark = theme === darkTheme;
    button.setAttribute("aria-label", isDark ? "Use light theme" : "Use dark theme");
    button.setAttribute("title", isDark ? "Use light theme" : "Use dark theme");
    const icon = button.querySelector<HTMLElement>("[data-theme-icon]");
    if (icon) icon.textContent = isDark ? "☾" : "☀";
  });
}

describeToggles(currentTheme());

document.querySelectorAll<HTMLInputElement>("[data-timezone-offset]").forEach((input) => {
  input.value = String(new Date().getTimezoneOffset());
});

document.querySelectorAll<HTMLButtonElement>("[data-theme-toggle]").forEach((button) => {
  button.addEventListener("click", () => {
    const nextTheme = currentTheme() === darkTheme ? lightTheme : darkTheme;
    rememberTheme(nextTheme);
    document.documentElement.dataset.theme = nextTheme;
    describeToggles(nextTheme);
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

// A mutation request targets a single row, and refreshing the list replaces
// that row. Refreshes therefore wait for pending mutations to settle instead of
// removing the element a response is about to be swapped into.
let pendingMutations = 0;
let refreshQueued = false;

document.addEventListener("htmx:beforeRequest", () => {
  pendingMutations += 1;
});

document.addEventListener("htmx:afterRequest", () => {
  pendingMutations = Math.max(0, pendingMutations - 1);
  if (pendingMutations > 0 || !refreshQueued) return;
  refreshQueued = false;
  refreshGameRequests();
});

// Without an event the current view is refreshed as a whole, which is what a
// page needs after it was disconnected from the stream for a while. A deferred
// refresh drops the event, and with it only the highlight on a new row; the
// rows themselves come from the database either way.
function refreshGameRequests(event?: GameRequestEvent): void {
  if (pendingMutations > 0) {
    refreshQueued = true;
    return;
  }

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

// A failed request must say so rather than leaving a control that looks live
// but does nothing. Mirrors the server-rendered action error.
function showActionError(message: string): void {
  const notice = document.createElement("div");
  notice.className = "alert alert-error";
  notice.setAttribute("role", "alert");
  notice.dataset.liveToast = "";
  const text = document.createElement("span");
  text.textContent = message;
  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "btn btn-ghost btn-sm";
  dismiss.dataset.dismissLiveToast = "";
  dismiss.setAttribute("aria-label", "Dismiss error");
  dismiss.textContent = "Dismiss";
  notice.append(text, dismiss);
  document.querySelector("#live-notices")?.append(notice);
  window.setTimeout(() => notice.remove(), 8000);
}

// The kit's square hero button copies the Game Request link. Feedback matters:
// a silent copy leaves no way to tell whether the click did anything.
document.addEventListener("click", (event) => {
  const trigger = (event.target as HTMLElement | null)?.closest<HTMLButtonElement>("[data-copy-link]");
  if (!trigger) return;
  void navigator.clipboard?.writeText(window.location.href).then(() => {
    const previous = trigger.title;
    trigger.title = "Link copied";
    trigger.classList.add("btn-primary");
    window.setTimeout(() => {
      trigger.title = previous;
      trigger.classList.remove("btn-primary");
    }, 1500);
  });
});

document.addEventListener("htmx:responseError", () => showActionError("That action could not be completed. Please try again."));
document.addEventListener("htmx:sendError", () => showActionError("The server could not be reached. Please try again."));
document.addEventListener("htmx:timeout", () => showActionError("The server took too long to respond. Please try again."));

document.addEventListener("click", (event) => {
  const target = event.target;
  if (target instanceof Element && target.closest("[data-dismiss-live-toast]")) {
    target.closest("[data-live-toast]")?.remove();
  }
  if (target instanceof Element && target.closest("[data-close-navigation]")) {
    target.closest<HTMLDetailsElement>("[data-mobile-navigation]")?.removeAttribute("open");
  }
});

// A form that navigates has no response to react to, so the guard is released
// when the next page loads. This timeout only matters when the submission never
// completes at all; it must outlast the server's own request timeout.
const submitRecoveryDelay = 20000;

function releaseSubmitGuard(form: HTMLFormElement): void {
  delete form.dataset.submitting;
  form.querySelectorAll<HTMLButtonElement | HTMLInputElement>("button[type=submit], input[type=submit]").forEach((control) => {
    control.disabled = false;
  });
}

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement)) return;
  if (form.dataset.confirm && !window.confirm(form.dataset.confirm)) {
    event.preventDefault();
    return;
  }
  // An htmx submission cancels the native event and carries its own guards.
  if (event.defaultPrevented) return;
  if (form.dataset.submitting === "true") {
    event.preventDefault();
    return;
  }

  form.dataset.submitting = "true";
  // Disabling a control before the browser has built the form data set would
  // drop its value, so the controls are disabled once submission is under way.
  window.setTimeout(() => {
    form.querySelectorAll<HTMLButtonElement | HTMLInputElement>("button[type=submit], input[type=submit]").forEach((control) => {
      control.disabled = true;
    });
  }, 0);
  window.setTimeout(() => releaseSubmitGuard(form), submitRecoveryDelay);
});

// A page restored from the back/forward cache keeps the DOM it was frozen with,
// including a submit guard whose navigation has long since finished.
window.addEventListener("pageshow", () => {
  document.querySelectorAll<HTMLFormElement>("form[data-submitting]").forEach(releaseSubmitGuard);
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
