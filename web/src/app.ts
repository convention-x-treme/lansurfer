import htmx from "htmx.org";
import { registerServiceWorker } from "./pwa";
import { initNotificationSettings } from "./notifications";

window.addEventListener("load", () => {
  void registerServiceWorker();
});
// The service worker asks the page to open a notification's Game Request when
// WindowClient.navigate() is unavailable (iOS home-screen apps).
navigator.serviceWorker?.addEventListener?.("message", (event: MessageEvent) => {
  const data: unknown = event.data;
  if (typeof data !== "object" || data === null || !("type" in data) || data.type !== "open-game-request" || !("url" in data) || typeof data.url !== "string") return;
  const url = new URL(data.url, window.location.href);
  if (url.origin === window.location.origin && url.pathname.startsWith("/game-requests/")) window.location.assign(url.href);
});
initNotificationSettings();

// A page restored from the back/forward cache keeps the DOM it was frozen
// with. On an authenticated page that can be private content a logout since
// (in this tab, or by using the back button after logging out) must not stay
// visible, so a restore forces a fresh request instead of trusting the frozen
// DOM. This is single-tab only: no cross-tab broadcast, no session polling.
window.addEventListener("pageshow", (event) => {
  if (event.persisted && document.querySelector("[data-authenticated-shell]")) {
    window.location.reload();
  }
});

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
    icon?.classList.toggle("ph-moon", isDark);
    icon?.classList.toggle("ph-sun", !isDark);
  });
}

describeToggles(currentTheme());

// Viewer-local time. The server renders UTC fallbacks and an RFC3339
// `datetime`; the browser owns the zone, so it rewrites clocks and hover titles
// (and fills the composer's IANA zone, which the server needs to apply DST at
// the chosen date). Relative labels ("in 5 min") stay server-rendered.
const clockParts = { hour: "2-digit", minute: "2-digit", hourCycle: "h23" } as const;
const clockFormat = new Intl.DateTimeFormat("en-US", clockParts);
const clockSecondsFormat = new Intl.DateTimeFormat("en-US", { ...clockParts, second: "2-digit" });
const dayFormat = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" });
const absoluteFormat = new Intl.DateTimeFormat("en-US", {
  year: "numeric", month: "2-digit", day: "2-digit", ...clockParts,
});
const zoneName = new Intl.DateTimeFormat().resolvedOptions().timeZone;

function absolute(date: Date): string {
  const p = Object.fromEntries(absoluteFormat.formatToParts(date).map((part) => [part.type, part.value]));
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`;
}

// Only the trailing clock or date is replaced, so a server prefix such as
// "created " survives, and relative text (never ending in either) is untouched.
const clockSuffix = /\d{2}:\d{2}(:\d{2})?$/;
const daySuffix = /[A-Z][a-z]{2} \d{1,2}$/;

function localizeTime(element: HTMLElement): void {
  const date = new Date(element.getAttribute("datetime") ?? "");
  if (Number.isNaN(date.getTime())) return;
  element.title = absolute(date);
  const kind = element.dataset.localTime;
  const text = element.textContent ?? "";
  if (kind === "clock") {
    const format = clockSuffix.exec(text)?.[1] ? clockSecondsFormat : clockFormat;
    element.textContent = text.replace(clockSuffix, format.format(date));
  } else if (kind === "start") {
    element.textContent = text.replace(daySuffix, dayFormat.format(date));
  } else if (kind === "absolute") {
    element.textContent = absolute(date);
  }
}

function localizeStartEditor(form: HTMLFormElement): void {
  const startsAt = new Date(form.dataset.startInstant ?? "");
  const dateInput = form.elements.namedItem("start_date");
  const timeInput = form.elements.namedItem("start_time");
  if (Number.isNaN(startsAt.getTime()) || !(dateInput instanceof HTMLInputElement) || !(timeInput instanceof HTMLInputElement)) return;
  if (dateInput.value === dateInput.defaultValue) {
    dateInput.value = `${startsAt.getFullYear()}-${String(startsAt.getMonth() + 1).padStart(2, "0")}-${String(startsAt.getDate()).padStart(2, "0")}`;
    dateInput.defaultValue = dateInput.value;
  }
  if (timeInput.value === timeInput.defaultValue) {
    timeInput.value = `${String(startsAt.getHours()).padStart(2, "0")}:${String(startsAt.getMinutes()).padStart(2, "0")}`;
    timeInput.defaultValue = timeInput.value;
  }
}

function localize(root: ParentNode): void {
  if (root instanceof HTMLElement && root.matches("[data-local-time]")) localizeTime(root);
  root.querySelectorAll<HTMLElement>("[data-local-time]").forEach(localizeTime);
  root.querySelectorAll<HTMLInputElement>("[data-timezone]").forEach((input) => {
    input.value = zoneName;
  });
  if (root instanceof HTMLFormElement && root.matches("[data-local-start]")) localizeStartEditor(root);
  root.querySelectorAll<HTMLFormElement>("form[data-local-start]").forEach(localizeStartEditor);
}


localize(document);
// HTMX swaps and SSE updates insert server-rendered nodes; only added elements
// are visited, and text/attribute writes above are not childList element adds.
new MutationObserver((records) => {
  records.forEach((record) =>
    record.addedNodes.forEach((node) => {
      if (node instanceof HTMLElement) localize(node);
    }),
  );
}).observe(document.documentElement, { childList: true, subtree: true });

// Account deletion stays unavailable until the player retypes their Game Nick
// exactly. Disabled here rather than in markup so the form still works without
// JavaScript; the server compares the nick again either way.
document.querySelectorAll<HTMLFormElement>("form[data-delete-account]").forEach((form) => {
  const input = form.elements.namedItem("confirm_game_nick") as HTMLInputElement;
  const button = form.querySelector<HTMLButtonElement>("button[type=submit]")!;
  const sync = () => {
    button.disabled = input.value !== form.dataset.deleteAccount;
  };
  input.addEventListener("input", sync);
  sync();
});

// IGDB is the only approved remote-content host, and its cover images can 404
// or time out independently of LANsurfer's own availability (spec: "Covers").
// `error` does not bubble on media elements, so this listens during the
// capture phase; that also covers covers rendered into HTMX-swapped markup
// without needing to rebind after every swap.
document.addEventListener(
  "error",
  (event) => {
    const cover = event.target;
    if (!(cover instanceof HTMLImageElement) || !cover.hasAttribute("data-cover-fallback")) return;
    cover.hidden = true;
    const placeholder = cover.nextElementSibling;
    if (placeholder instanceof HTMLElement) placeholder.hidden = false;
  },
  true,
);

document.querySelectorAll<HTMLButtonElement>("[data-theme-toggle]").forEach((button) => {
  button.addEventListener("click", () => {
    const nextTheme = currentTheme() === darkTheme ? lightTheme : darkTheme;
    rememberTheme(nextTheme);
    document.documentElement.dataset.theme = nextTheme;
    describeToggles(nextTheme);
  });
});

// A number input enforces min/max only at submit time, so a typed 1000
// would sit there looking accepted until the form is sent. Correcting it
// as it is typed says the bound immediately; the server still validates
// the same range for anything that reaches it without this script.
document.addEventListener("input", (event) => {
  const field = event.target;
  if (!(field instanceof HTMLInputElement) || !field.matches("input[type=number][max]")) return;
  const value = Number(field.value);
  if (field.value === "" || !Number.isFinite(value)) return;
  const maximum = Number(field.max);
  const minimum = field.min === "" ? -Infinity : Number(field.min);
  if (value > maximum) field.value = field.max;
  else if (value < minimum) field.value = field.min;
});

function setGameRequestSearchBusy(event: Event, busy: boolean): void {
  const source = event.target;
  if (!(source instanceof HTMLFormElement) || !source.hasAttribute("data-game-request-search")) return;
  document.querySelector("#game-request-discovery")?.setAttribute("aria-busy", String(busy));
}

document.addEventListener("htmx:beforeRequest", (event) => setGameRequestSearchBusy(event, true));
document.addEventListener("htmx:afterRequest", (event) => setGameRequestSearchBusy(event, false));

function updateDescriptionCount(description: HTMLTextAreaElement): void {
  const count = description.closest("label")?.querySelector<HTMLElement>("[data-description-count]");
  if (count) count.textContent = `${Array.from(description.value).length} / 1000`;
}

document.addEventListener("input", (event) => {
  const field = event.target;
  if (field instanceof HTMLTextAreaElement && field.matches("textarea[data-description]")) {
    updateDescriptionCount(field);
  }
});

// Every live notice is dismissible: an outage can last as long as the event
// floor's network does, and an undismissable banner just covers the page.
function liveNotice(variant: "info" | "warning" | "error", message: string): HTMLElement {
  const notice = document.createElement("div");
  notice.className = `alert alert-${variant}`;
  notice.setAttribute("role", variant === "error" ? "alert" : "status");
  notice.dataset.liveToast = "";
  const text = document.createElement("span");
  text.textContent = message;
  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "btn btn-ghost btn-sm";
  dismiss.dataset.dismissLiveToast = "";
  dismiss.setAttribute("aria-label", "Dismiss notification");
  const icon = document.createElement("i");
  icon.className = "ph ph-x";
  icon.setAttribute("aria-hidden", "true");
  dismiss.append(icon, "Dismiss");
  const glyph = document.createElement("i");
  glyph.className = `ph ${variant === "info" ? "ph-bell-ringing" : "ph-warning-circle"} live-toast-icon`;
  glyph.setAttribute("aria-hidden", "true");
  notice.append(glyph, text, dismiss);
  document.querySelector("#live-notices")?.append(notice);
  return notice;
}

type GameRequestEvent = {
  type: "GameRequestCreated" | "GameRequestJoined" | "GameRequestLeft" | "GameRequestStarted" | "GameRequestClosed" | "GameRequestCancelled" | "GameRequestUpdated";
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

// Detail refreshes keep only locally dirty or rejected field state. The server
// always owns the refreshed form structure.
type RequestDetailFieldDraft = {
  name: string;
  value: string;
  baseline?: string;
  rejected: boolean;
  focused: boolean;
  error: string;
};
const requestDetailsDrafts = new WeakMap<HTMLElement, RequestDetailFieldDraft[]>();

document.addEventListener("htmx:beforeSwap", (event) => {
  const target = (event as CustomEvent<{ target: HTMLElement }>).detail.target;
  if (target.id !== "game-request-detail") return;
  const form = target.querySelector<HTMLFormElement>("form[action$='/details']");
  if (!form) return;
  const fields = form.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>(
    "input[name='max_players'], input[name='start_date'], input[name='start_time'], textarea[name='description']",
  );
  const active = document.activeElement;
  const drafts: RequestDetailFieldDraft[] = [];
  for (const field of fields) {
    const errorID = field.name === "max_players" ? "max-players-error"
      : field.name === "description" ? "description-error"
      : "start-error";
    const error = form.querySelector<HTMLElement>(`#${errorID}`);
    const rejected = field.getAttribute("aria-invalid") === "true" ||
      (field.name.startsWith("start_") && error !== null);
    const dirty = field.value !== field.defaultValue;
    if (dirty || rejected) {
      drafts.push({
        name: field.name,
        value: field.value,
        baseline: field.name === "start_date" || field.name === "start_time" ? field.defaultValue : undefined,
        rejected,
        focused: active === field,
        error: rejected ? error?.textContent?.trim() ?? "" : "",
      });
    }
  }
  const selectedPreset = form.querySelector<HTMLInputElement>("input[name='start_preset']:checked");
  const presetError = form.querySelector<HTMLElement>("#start-error");
  if (selectedPreset && (selectedPreset.checked !== selectedPreset.defaultChecked || presetError)) {
    drafts.push({
      name: "start_preset",
      value: selectedPreset.value,
      rejected: presetError !== null,
      focused: active === selectedPreset,
      error: presetError?.textContent?.trim() ?? "",
    });
  }
  if (drafts.length) requestDetailsDrafts.set(target, drafts);
});
document.addEventListener("htmx:afterSwap", (event) => {
  const target = (event as CustomEvent<{ target: HTMLElement }>).detail.target;
  const drafts = requestDetailsDrafts.get(target);
  if (!drafts) return;
  requestDetailsDrafts.delete(target);
  const editor = document.querySelector<HTMLFormElement>("#game-request-detail form[action$='/details']");
  if (!editor) return;
  let focus: HTMLElement | null = null;
  for (const draft of drafts) {
    if (draft.name === "start_preset") {
      const radio = editor.querySelector<HTMLInputElement>(`input[name="start_preset"][value="${draft.value}"]`);
      if (!radio) continue;
      radio.checked = true;
      if (draft.rejected) radio.setAttribute("aria-invalid", "true");
      if (draft.focused) focus = radio;
      continue;
    }
    const field = editor.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${draft.name}"]`);
    if (!field) continue;
    if (draft.baseline !== undefined && field instanceof HTMLInputElement) field.defaultValue = draft.baseline;
    field.value = draft.value;
    if (field instanceof HTMLTextAreaElement) updateDescriptionCount(field);
    if (draft.rejected) {
      field.setAttribute("aria-invalid", "true");
      const errorID = draft.name === "max_players" ? "max-players-error"
        : draft.name === "description" ? "description-error"
        : "start-error";
      let error = editor.querySelector<HTMLElement>(`#${errorID}`);
      if (!error && draft.error) {
        error = document.createElement("p");
        error.id = errorID;
        error.className = "field-error";
        if (draft.name.startsWith("start_")) {
          const fieldset = editor.querySelector("fieldset");
          fieldset?.insertBefore(error, fieldset.children[1] ?? null);
        } else {
          field.insertAdjacentElement("afterend", error);
        }
      }
      if (error && draft.error) {
        error.textContent = draft.error;
        const describedBy = new Set((field.getAttribute("aria-describedby") ?? "").split(/\s+/).filter(Boolean));
        describedBy.add(errorID);
        field.setAttribute("aria-describedby", [...describedBy].join(" "));
      }
    }
    if (draft.focused) focus = field;
  }
  focus?.focus({ preventScroll: true });
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

  // The refresh reloads whatever selection the player is looking at, so a live
  // update never silently drops their discovery filters.
  const list = document.querySelector<HTMLElement>("[data-game-request-list]");
  if (list) {
    const parameters = new URLSearchParams(list.dataset.filterQuery ?? "");
    if (event?.type === "GameRequestCreated") parameters.set("highlight", event.requestId);
    const query = parameters.toString();
    void htmx.ajax("get", `/game-requests/refresh${query ? `?${query}` : ""}`, {
      target: "#game-request-list",
      swap: "outerHTML",
    });
  }

  const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
  const detailID = detail?.dataset.gameRequestId;
  const detailRefreshURL = detail?.dataset.refreshUrl;
  if (detailID && detailRefreshURL && (event === undefined || detailID === event.requestId)) {
    void htmx.ajax("get", detailRefreshURL, {
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
      liveNotice("warning", "Live connection lost. Retrying…").dataset.liveConnectionAlert = "";
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
  const notice = liveNotice("error", message);
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

// A link into a collapsed disclosure -- the administration top bar's "Add
// game" is the one that exists -- would otherwise jump to a panel that is
// still shut. The panel opens before the browser scrolls to it, and the
// first field takes the caret so the form is ready to type into. Without
// this the link still works; it just lands on a closed panel.
document.addEventListener("click", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const link = target.closest<HTMLAnchorElement>("a[href^='#']");
  if (!link || link.hash.length < 2) return;
  const panel = document.getElementById(link.hash.slice(1));
  if (!(panel instanceof HTMLDetailsElement)) return;
  panel.open = true;
  // The fragment navigation that follows this click resets focus, so the
  // caret is placed after it rather than before.
  window.setTimeout(() => panel.querySelector<HTMLInputElement>("input:not([type=hidden])")?.focus(), 0);
});

// Adding an alias replaces the whole catalog entry it belongs to, which takes
// the caret with it: the field that was being typed in no longer exists. The
// replacement carries the same id, so the caret goes back into it and several
// aliases can be added in a row without reaching for the mouse again.
let aliasFieldToRefocus = "";

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement) || !form.classList.contains("alias-add")) return;
  aliasFieldToRefocus = form.querySelector<HTMLInputElement>(".alias-input")?.id ?? "";
});

document.addEventListener("htmx:afterSettle", () => {
  if (!aliasFieldToRefocus) return;
  document.getElementById(aliasFieldToRefocus)?.focus();
  aliasFieldToRefocus = "";
});

// A page restored from the back/forward cache keeps the DOM it was frozen with,
// including a submit guard whose navigation has long since finished.
window.addEventListener("pageshow", () => {
  document.querySelectorAll<HTMLFormElement>("form[data-submitting]").forEach(releaseSubmitGuard);
});

document.addEventListener("htmx:oobAfterSwap", () => {
  document.querySelectorAll<HTMLElement>("[data-live-toast]:not([data-live-connection-alert])").forEach((toast) => {
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
