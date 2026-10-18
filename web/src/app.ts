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

let pendingSessionDialogOpen = false;
let pendingGameRequestEditOpen = false;

const gameRequestRefreshCoalesceMs = 200;
type GameRequestRefreshTarget = "#game-request-list" | "#game-request-detail";
type GameRequestRefreshOptions = {
  immediate?: boolean;
  highlightId?: string;
  closeEditor?: boolean;
};
type GameRequestRefreshAttempt = {
  target: GameRequestRefreshTarget;
  element: HTMLElement;
  options: GameRequestRefreshOptions;
  xhr: XMLHttpRequest | null;
  invalidated: boolean;
  swapped: boolean;
  failed: boolean;
  isRetry: boolean;
  completed: boolean;
  beforeSwapHandler?: (event: Event) => void;
};
type GameRequestRefreshState = {
  pending: boolean;
  dueAt: number;
  nextAllowedAt: number;
  timer: number | null;
  inFlight: GameRequestRefreshAttempt | null;
  retryUsed: boolean;
  highlightId: string | null;
  closeEditor: boolean;
};
const gameRequestRefreshStates: Record<GameRequestRefreshTarget, GameRequestRefreshState> = {
  "#game-request-list": { pending: false, dueAt: 0, nextAllowedAt: 0, timer: null, inFlight: null, retryUsed: false, highlightId: null, closeEditor: false },
  "#game-request-detail": { pending: false, dueAt: 0, nextAllowedAt: 0, timer: null, inFlight: null, retryUsed: false, highlightId: null, closeEditor: false },
};
const schedulerAttempts = new WeakMap<XMLHttpRequest, GameRequestRefreshAttempt>();
const activeHtmxRequests = new Set<XMLHttpRequest>();
let dispatchingRefreshAttempt: GameRequestRefreshAttempt | null = null;
let pageRefreshSuspended = false;


function sessionDialogFocusSelector(trigger: HTMLElement): string {
  if (trigger.hasAttribute("data-session-add-trigger")) return "[data-session-add-trigger]";
  if (trigger.dataset.sessionEditTrigger) {
    return `[data-session-edit-trigger="${CSS.escape(trigger.dataset.sessionEditTrigger)}"]`;
  }
  const form = trigger.closest<HTMLFormElement>("[data-session-delete-fallback]");
  if (form?.dataset.sessionDeleteEntry) {
    return `[data-session-delete-fallback][data-session-delete-entry="${CSS.escape(form.dataset.sessionDeleteEntry)}"] button`;
  }
  return "";
}

function openSessionDialog(dialog: HTMLDialogElement, trigger: HTMLElement): void {
  dialog.dataset.returnFocusSelector = sessionDialogFocusSelector(trigger);
  if (!dialog.open) dialog.showModal();
}

function closeSessionDialog(dialog: HTMLDialogElement): void {
  if (dialog.open) dialog.close();
}
document.querySelectorAll<HTMLDialogElement>("[data-dialog-shell]").forEach((dialog) => {
  dialog.addEventListener("close", () => {
    const selector = dialog.dataset.returnFocusSelector;
    delete dialog.dataset.returnFocusSelector;
    dialog.querySelector("[data-session-dialog-content], #game-request-edit-dialog-content")?.replaceChildren();
    window.setTimeout(() => {
      const fallback = dialog.id === "game-request-edit-dialog"
        ? document.querySelector<HTMLElement>("[data-game-request-edit-trigger]") ??
          document.querySelector<HTMLElement>("#game-request-detail h1")
        : document.querySelector<HTMLElement>("[data-session-add-trigger]") ??
          document.querySelector<HTMLElement>("#session-information-title");
      const trigger = (selector ? document.querySelector<HTMLElement>(selector) : null) ?? fallback;
      trigger?.focus({ preventScroll: true });
    }, 50);
  });
});

document.addEventListener("htmx:beforeRequest", (event) => {
  const trigger = (event as CustomEvent<{ elt: HTMLElement }>).detail.elt;
  if (trigger.matches("[data-session-dialog-open]")) {
    const dialog = document.querySelector<HTMLDialogElement>("#session-information-dialog");
    if (!dialog) return;
    pendingSessionDialogOpen = true;
    dialog.dataset.returnFocusSelector = sessionDialogFocusSelector(trigger);
  }
});
document.addEventListener("htmx:beforeRequest", (event) => {
  const trigger = (event as CustomEvent<{ elt: HTMLElement }>).detail.elt;
  if (!trigger.matches("[data-game-request-edit-trigger]")) return;
  const dialog = document.querySelector<HTMLDialogElement>("#game-request-edit-dialog");
  if (!dialog) return;
  pendingGameRequestEditOpen = true;
  dialog.dataset.returnFocusSelector = "[data-game-request-edit-trigger]";
});

document.addEventListener("htmx:afterSwap", (event) => {
  const target = (event as CustomEvent<{ target: HTMLElement }>).detail.target;
  if (target.id === "game-request-edit-dialog-content") {
    const form = target.querySelector<HTMLFormElement>("form[action$='/details']");
    const dialog = document.querySelector<HTMLDialogElement>("#game-request-edit-dialog");
    if (!dialog) return;
    const invalid = form?.querySelector<HTMLElement>("[aria-invalid='true']");
    const alert = target.querySelector<HTMLElement>("[role='alert']");
    if (!form) {
      alert?.focus({ preventScroll: true });
      return;
    }
    if (pendingGameRequestEditOpen) {
      pendingGameRequestEditOpen = false;
      dialog.showModal();
    }
    (invalid ?? alert ?? form.querySelector<HTMLElement>("input[name='max_players']"))?.focus({ preventScroll: true });
    return;
  }
});

document.addEventListener("game-request-details-saved", () => {
  queueGameRequestRefresh("#game-request-detail", { immediate: true, closeEditor: true });
});

document.addEventListener("htmx:afterSwap", (event) => {
  const target = (event as CustomEvent<{ target: HTMLElement }>).detail.target;
  if (target.id !== "session-information-dialog-content") return;
  const form = target.querySelector<HTMLFormElement>("[data-session-information-form]");
  if (!form) return;
  const invalidField = form.querySelector<HTMLElement>("[aria-invalid='true']");
  if (!pendingSessionDialogOpen) {
    invalidField?.focus({ preventScroll: true });
    return;
  }
  pendingSessionDialogOpen = false;
  const dialog = document.querySelector<HTMLDialogElement>("#session-information-dialog");
  const title = dialog?.querySelector<HTMLElement>("#session-information-dialog-title");
  if (!dialog || !title) return;
  title.textContent = form.dataset.sessionMode === "edit" ? "Edit session information" : "Add session information";
  if (!dialog.open) dialog.showModal();
  (invalidField ?? form.querySelector<HTMLInputElement>("[name='key']"))?.focus({ preventScroll: true });
});


document.addEventListener("submit", (event) => {
  const source = event.target;
  if (!(source instanceof HTMLFormElement) || !source.matches("[data-session-delete-fallback]")) return;
  event.preventDefault();
  const dialog = document.querySelector<HTMLDialogElement>("#session-information-delete-dialog");
  const confirmForm = dialog?.querySelector<HTMLFormElement>("[data-session-delete-confirm-form]");
  const message = dialog?.querySelector<HTMLElement>("[data-session-delete-message]");
  const title = dialog?.querySelector<HTMLElement>("#session-information-delete-title");
  const key = source.dataset.sessionDeleteKey ?? "this entry";
  if (!dialog || !confirmForm || !message || !title) return;
  const trigger = source.querySelector<HTMLButtonElement>("button[type='submit']");
  if (!trigger) return;
  title.textContent = `Delete ${key}?`;
  message.textContent = "This entry is removed for everyone in this Game Request.";
  confirmForm.action = source.action;
  confirmForm.setAttribute("hx-post", source.action);
  const csrf = source.querySelector<HTMLInputElement>("input[name='csrf_token']");
  const confirmCSRF = confirmForm.querySelector<HTMLInputElement>("input[name='csrf_token']");
  if (csrf && confirmCSRF) confirmCSRF.value = csrf.value;
  htmx.process(confirmForm);
  openSessionDialog(dialog, trigger);
});

document.addEventListener("click", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const close = target.closest<HTMLElement>("[data-dialog-close]");
  if (close) {
    closeSessionDialog(close.closest<HTMLDialogElement>("dialog")!);
    return;
  }
  const dialog = target instanceof HTMLDialogElement
    ? target
    : document.querySelector<HTMLDialogElement>("[data-dialog-shell][open]");
  if (dialog?.open && dialog.matches("[data-dialog-shell]")) {
    const panel = dialog.querySelector<HTMLElement>(".dialog-panel");
    const rect = panel?.getBoundingClientRect();
    if (rect && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom)) {
      closeSessionDialog(dialog);
    }
  }
});


document.addEventListener("session-information-mutated", () => {
  document.querySelectorAll<HTMLDialogElement>("[data-dialog-shell][open]").forEach(closeSessionDialog);
});

// The theme the document already paints with: the server renders an explicit
// choice into <html>, and without one the stylesheet follows the operating
// system. Setting it from script on load would repaint the page after the
// deferred bundle runs, which is the theme flash this avoids.
// The server sets the choice as an HttpOnly cookie (Twitch Developer Services
// Agreement, Schedule 1, A), so script only reports it.
const darkTheme = "lansurfer-dark";
const lightTheme = "lansurfer-light";

type Theme = typeof darkTheme | typeof lightTheme;

function currentTheme(): Theme {
  const rendered = document.documentElement.dataset.theme;
  if (rendered === darkTheme || rendered === lightTheme) return rendered;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? lightTheme : darkTheme;
}

function rememberTheme(theme: Theme, button: HTMLButtonElement): void {
  const csrf = button.form?.elements.namedItem("csrf_token");
  if (!(csrf instanceof HTMLInputElement)) return;
  void fetch("/theme", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ csrf_token: csrf.value, theme }),
  }).catch(() => undefined);
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

const clockSuffix = /\d{2}:\d{2}(:\d{2})?$/;
// Replace only absolute date-and-clock suffixes; relative labels stay server-rendered.
const startSuffix = /[A-Z][a-z]{2} \d{1,2}(?: at \d{2}:\d{2})?$/;
const localStartFormat = new Intl.DateTimeFormat("en-US", {
  month: "short", day: "numeric", ...clockParts,
});

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
    if (startSuffix.test(text)) element.textContent = text.replace(startSuffix, localStartFormat.format(date).replace(",", " at"));
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
    rememberTheme(nextTheme, button);
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

document.addEventListener("animationend", (event) => {
  if (event.animationName === "live-toast-countdown" && event.target instanceof Element) event.target.closest("[data-live-toast]")?.remove();
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
  type: "GameRequestCreated" | "GameRequestJoined" | "GameRequestLeft" | "GameRequestStarted" | "GameRequestClosed" | "GameRequestCancelled" | "GameRequestUpdated" | "GameRequestSessionInformationChanged";
  requestId: string;
  gameId?: string;
};

// A mutation request targets a single row, and refreshing the list replaces
// that row. Refreshes therefore wait for pending mutations to settle instead of
// removing the element a response is about to be swapped into.
let lobbyRefreshQueued = false;
let queuedLobbyRequestID: string | null = null;

document.addEventListener("htmx:beforeRequest", (event) => {
  const detail = (event as CustomEvent<{
    xhr: XMLHttpRequest;
    target?: Element;
    requestConfig?: { verb?: string; path?: string };
  }>).detail;
  const xhr = detail.xhr;
  activeHtmxRequests.add(xhr);
  const attempt = dispatchingRefreshAttempt;
  if (attempt) {
    attempt.xhr = xhr;
    schedulerAttempts.set(xhr, attempt);
    xhr.addEventListener("loadend", () => queueMicrotask(() => completeGameRequestRefresh(attempt)), { once: true });
  } else {
    const method = detail.requestConfig?.verb?.toLowerCase() ?? "get";
    const path = detail.requestConfig?.path ?? "";
    const relevantMutation = method !== "get" && method !== "head" &&
      /^(?:https?:\/\/[^/]+)?\/game-requests(?:\/[^/?#]+\/(?:join|leave|start|close|cancel|details))?(?:\?.*)?$/.test(path);
    const target = detail.target;
    const swapsRefreshTarget = target?.id === "game-request-list" || target?.id === "game-request-detail";
    const changesRefreshTarget = relevantMutation || ((method === "get" || method === "head") && swapsRefreshTarget);
    if (!changesRefreshTarget) return;
    for (const selector of ["#game-request-list", "#game-request-detail"] as const) {
      const state = gameRequestRefreshStates[selector];
      const inFlight = state.inFlight;
      if (!inFlight || inFlight.swapped || inFlight.invalidated || inFlight.failed || inFlight.xhr?.readyState === XMLHttpRequest.DONE) continue;
      inFlight.invalidated = true;
      queueGameRequestRefresh(selector, {
        immediate: true,
        highlightId: inFlight.options.highlightId,
        closeEditor: inFlight.options.closeEditor,
      });
    }
  }
});

document.addEventListener("htmx:afterRequest", (event) => {
  const xhr = (event as CustomEvent<{ xhr: XMLHttpRequest }>).detail.xhr;
  if (xhr) activeHtmxRequests.delete(xhr);
  if (lobbyRefreshQueued && activeHtmxRequests.size === 0) {
    lobbyRefreshQueued = false;
    const requestID = queuedLobbyRequestID;
    queuedLobbyRequestID = null;
    if (requestID) refreshGameRequestLobby(requestID);
  }
  queueMicrotask(flushGameRequestRefreshes);
});

for (const eventName of ["htmx:responseError", "htmx:sendError", "htmx:timeout"]) {
  document.addEventListener(eventName, (event) => {
    const xhr = (event as CustomEvent<{ xhr: XMLHttpRequest }>).detail.xhr;
    const attempt = xhr && schedulerAttempts.get(xhr);
    if (attempt) attempt.failed = true;
  });
}

document.addEventListener("htmx:beforeSwap", (event) => {
  const detail = (event as CustomEvent<{ target: HTMLElement; xhr: XMLHttpRequest }>).detail;
  const attempt = schedulerAttempts.get(detail.xhr);
  if (!attempt) return;
  const state = gameRequestRefreshStates[attempt.target];
  if (document.hidden || pageRefreshSuspended) {
    attempt.invalidated = true;
    queueGameRequestRefresh(attempt.target, {
      immediate: true,
      highlightId: attempt.options.highlightId,
      closeEditor: attempt.options.closeEditor,
    });
    event.preventDefault();
    return;
  }
  const current = document.getElementById(attempt.element.id) === attempt.element;
  if (!current) {
    attempt.invalidated = true;
    if (attempt.target === "#game-request-detail") {
      state.pending = false;
      state.highlightId = null;
      state.closeEditor = false;
    }
    event.preventDefault();
    return;
  }
  if (attempt.invalidated) event.preventDefault();
});

document.addEventListener("htmx:afterSwap", (event) => {
  const detail = (event as CustomEvent<{ target: HTMLElement; xhr: XMLHttpRequest }>).detail;
  const attempt = schedulerAttempts.get(detail.xhr);
  if (!attempt || detail.target !== attempt.element) return;
  attempt.swapped = true;
  if (attempt.options.closeEditor) {
    const dialog = document.querySelector<HTMLDialogElement>("#game-request-edit-dialog");
    if (dialog?.open) dialog.close();
  }
});

// The inline ?edit=1 fallback can run with JavaScript enabled. Preserve its
// draft during detail swaps; enhanced dialog drafts live outside that target.
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
  if (event.defaultPrevented) return;
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
function refreshGameRequests(event?: GameRequestEvent): void {
  const list = document.querySelector<HTMLElement>("[data-game-request-list]");
  if (list && event?.type === "GameRequestCreated") {
    queueGameRequestRefresh("#game-request-list", { highlightId: event.requestId });
  } else if (list) {
    queueGameRequestRefresh("#game-request-list", { immediate: event === undefined });
  }

  const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
  if (detail && (!event || detail.dataset.gameRequestId === event.requestId)) {
    queueGameRequestRefresh("#game-request-detail", { immediate: event === undefined });
  }
}

function queueGameRequestRefresh(target: GameRequestRefreshTarget, options: GameRequestRefreshOptions = {}): void {
  const state = gameRequestRefreshStates[target];
  const element = document.querySelector<HTMLElement>(target);
  if (!element || (target === "#game-request-detail" && !element.dataset.refreshUrl)) {
    if (state.timer !== null) window.clearTimeout(state.timer);
    state.timer = null;
    state.pending = false;
    state.highlightId = null;
    state.closeEditor = false;
    return;
  }
  const now = performance.now();
  if (!state.pending) {
    state.pending = true;
    state.dueAt = options.immediate ? now : Math.max(now + gameRequestRefreshCoalesceMs, state.nextAllowedAt);
  } else if (options.immediate && !state.retryUsed) {
    state.dueAt = Math.min(state.dueAt, now);
  }
  mergeRefreshOptions(state, options);
  scheduleGameRequestRefresh(target);
}

function scheduleGameRequestRefresh(target: GameRequestRefreshTarget): void {
  const state = gameRequestRefreshStates[target];
  if (!state.pending || state.inFlight) return;
  if (document.hidden || pageRefreshSuspended) {
    if (state.timer !== null) window.clearTimeout(state.timer);
    state.timer = null;
    return;
  }
  if (state.timer !== null) window.clearTimeout(state.timer);
  const delay = Math.max(0, state.dueAt - performance.now());
  state.timer = window.setTimeout(() => {
    state.timer = null;
    flushGameRequestRefreshes();
  }, delay);
}

function flushGameRequestRefreshes(): void {
  if (document.hidden || pageRefreshSuspended || activeHtmxRequests.size > 0) return;
  for (const target of ["#game-request-list", "#game-request-detail"] as const) {
    const state = gameRequestRefreshStates[target];
    if (!state.pending || state.inFlight) continue;
    if (performance.now() < state.dueAt) {
      scheduleGameRequestRefresh(target);
      continue;
    }
    dispatchGameRequestRefresh(target, state);
    if (activeHtmxRequests.size > 0) return;
  }
}

function dispatchGameRequestRefresh(target: GameRequestRefreshTarget, state: GameRequestRefreshState): void {
  const element = document.querySelector<HTMLElement>(target);
  if (!element || (target === "#game-request-detail" && !element.dataset.refreshUrl)) {
    state.pending = false;
    state.highlightId = null;
    state.closeEditor = false;
    return;
  }
  const options: GameRequestRefreshOptions = {
    highlightId: state.highlightId ?? undefined,
    closeEditor: state.closeEditor,
  };
  state.pending = false;
  state.nextAllowedAt = performance.now() + 100;
  state.highlightId = null;
  state.closeEditor = false;
  const attempt: GameRequestRefreshAttempt = {
    target, element, options, xhr: null, invalidated: false, swapped: false,
    failed: false, isRetry: state.retryUsed, completed: false,
  };
  attempt.beforeSwapHandler = (event) => {
    const detail = (event as CustomEvent<{ xhr: XMLHttpRequest }>).detail;
    if (detail.xhr !== attempt.xhr) return;
    if (document.hidden || pageRefreshSuspended || document.getElementById(element.id) !== element || attempt.invalidated) {
      attempt.invalidated = true;
      event.preventDefault();
    }
  };
  element.addEventListener("htmx:beforeSwap", attempt.beforeSwapHandler);
  state.inFlight = attempt;
  let url: string;
  if (target === "#game-request-list") {
    const parameters = new URLSearchParams(element.dataset.filterQuery ?? "");
    if (options.highlightId) parameters.set("highlight", options.highlightId);
    const query = parameters.toString();
    url = `/game-requests/refresh${query ? `?${query}` : ""}`;
  } else {
    const refreshURL = new URL(element.dataset.refreshUrl ?? "", window.location.href);
    for (const key of ["edit", "session_add", "session_edit"]) {
      const value = new URL(window.location.href).searchParams.get(key);
      if (value !== null) refreshURL.searchParams.set(key, value);
    }
    url = refreshURL.toString();
  }
  try {
    dispatchingRefreshAttempt = attempt;
    void htmx.ajax("get", url, { source: document.body, target: element, swap: "outerHTML" })
      .catch(() => { attempt.failed = true; })
      .finally(() => queueMicrotask(() => completeGameRequestRefresh(attempt)));
  } catch {
    attempt.failed = true;
    queueMicrotask(() => completeGameRequestRefresh(attempt));
  } finally {
    dispatchingRefreshAttempt = null;
  }
}

function mergeRefreshOptions(state: GameRequestRefreshState, options: GameRequestRefreshOptions): void {
  if (options.highlightId) state.highlightId = options.highlightId;
  state.closeEditor ||= options.closeEditor === true;
}

function completeGameRequestRefresh(attempt: GameRequestRefreshAttempt): void {
  if (attempt.completed) return;
  attempt.completed = true;
  const state = gameRequestRefreshStates[attempt.target];
  const xhr = attempt.xhr;
  if (attempt.beforeSwapHandler) attempt.element.removeEventListener("htmx:beforeSwap", attempt.beforeSwapHandler);
  if (xhr) activeHtmxRequests.delete(xhr);
  if (state.inFlight !== attempt) return;
  state.inFlight = null;
  const succeeded = !attempt.invalidated && !attempt.failed && attempt.swapped &&
    xhr !== null && xhr.status >= 200 && xhr.status < 300;
  if (attempt.invalidated) {
    // Foreign work already queued one revalidation with the consumed metadata.
  } else if (succeeded) {
    state.retryUsed = false;
  } else if (attempt.isRetry) {
    state.retryUsed = false;
    if (state.pending) mergeRefreshOptions(state, attempt.options);
  } else {
    state.pending = true;
    mergeRefreshOptions(state, attempt.options);
    state.retryUsed = true;
    state.dueAt = performance.now() + 1000;
  }
  flushGameRequestRefreshes();
}

function handleOfflineRefreshRejection(refresh: Promise<unknown>): void {
  void refresh.catch((error: unknown) => {
    if (navigator.onLine) throw error;
  });
}

function refreshGameRequestLobby(requestID: string): void {
  if (activeHtmxRequests.size > 0) {
    lobbyRefreshQueued = true;
    queuedLobbyRequestID = requestID;
    return;
  }
  const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
  const log = document.querySelector<HTMLElement>("#game-request-lobby[data-refresh-url]");
  if (detail?.dataset.gameRequestId !== requestID || !log?.dataset.refreshUrl) return;
  handleOfflineRefreshRejection(htmx.ajax("get", log.dataset.refreshUrl, { target: "#game-request-lobby", swap: "outerHTML" }));
}

const realtime = document.querySelector<HTMLElement>("[data-realtime-url]");
if (realtime?.dataset.realtimeUrl) {
  const baseRealtimeURL = realtime.dataset.realtimeUrl;
  let events: EventSource | null = null;
  let reconnectPending = false;
  const connectRealtime = (): void => {
    if (events) return;
    const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
    const requestID = detail?.dataset.gameRequestId;
    const realtimeURL = new URL(baseRealtimeURL, window.location.href);
    if (requestID) realtimeURL.searchParams.set("detailRequestId", requestID);
    events = new EventSource(realtimeURL);
    events.addEventListener("open", () => {
      document.querySelector("[data-live-connection-alert]")?.remove();
      if (reconnectPending) {
        reconnectPending = false;
        refreshGameRequests();
      }
    });
    events.addEventListener("game-request", (message) => {
      try {
        refreshGameRequests(JSON.parse(message.data) as GameRequestEvent);
      } catch {
        // A malformed or stale signal can be ignored; the next event refreshes current state.
      }
    });
    events.addEventListener("game-request-chat", (message) => {
      try {
        const { requestId } = JSON.parse(message.data) as GameRequestEvent;
        const log = document.querySelector<HTMLElement>("#game-request-lobby[data-refresh-url]");
        const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
        if (!log || detail?.dataset.gameRequestId !== requestId) return;
        refreshGameRequestLobby(requestId);
      } catch {
        // A malformed signal is ignored; the next message refreshes the log.
      }
    });
    events.addEventListener("game-request-session-information", (message) => {
      try {
        const { requestId } = JSON.parse(message.data) as GameRequestEvent;
        const section = document.querySelector<HTMLElement>("#session-information");
        const detail = document.querySelector<HTMLElement>("[data-game-request-detail]");
        if (section && detail?.dataset.gameRequestId === requestId && section.dataset.refreshUrl) {
          handleOfflineRefreshRejection(htmx.ajax("get", section.dataset.refreshUrl, { target: "#session-information", swap: "outerHTML" }));
        }
      } catch {
        // Ignore malformed signal.
      }
    });
    events.addEventListener("error", () => {
      reconnectPending = true;
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
  const suspendRefreshScheduler = (): void => {
    pageRefreshSuspended = true;
    for (const state of Object.values(gameRequestRefreshStates)) {
      if (state.timer !== null) window.clearTimeout(state.timer);
      state.timer = null;
    }
  };
  const resumeRealtime = (): void => {
    pageRefreshSuspended = false;
    reconnectPending = false;
    connectRealtime();
    refreshGameRequests();
  };
  window.addEventListener("online", () => refreshGameRequests());

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      suspendRefreshScheduler();
      disconnectRealtime();
      return;
    }
    resumeRealtime();
  });

  // A page kept in the back/forward cache is hidden without a visibility
  // change, so release the stream on unload as well.
  window.addEventListener("pagehide", () => {
    suspendRefreshScheduler();
    disconnectRealtime();
  });
  window.addEventListener("pageshow", () => {
    if (!document.hidden && pageRefreshSuspended) resumeRealtime();
    else if (!document.hidden) connectRealtime();
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


const secretRevealTimeout = 20_000;
const secretRevealTimers = new WeakMap<HTMLElement, number>();

function secretButtonState(tile: HTMLElement, revealed: boolean): void {
  const button = tile.querySelector<HTMLButtonElement>("[data-secret-reveal]");
  const icon = button?.querySelector<HTMLElement>("i");
  if (!button) return;
  const key = tile.querySelector<HTMLElement>(".session-information-key")?.textContent ?? "secret";
  const action = revealed ? "Hide" : "Reveal";
  button.setAttribute("aria-label", `${action} ${key}`);
  button.title = `${action} ${key}`;
  button.setAttribute("aria-pressed", String(revealed));
  icon?.classList.toggle("ph-eye", !revealed);
  icon?.classList.toggle("ph-eye-slash", revealed);
}

function hideSecret(tile: HTMLElement): void {
  const value = tile.querySelector<HTMLElement>("[data-secret-value]");
  if (!value) return;
  const timer = secretRevealTimers.get(tile);
  if (timer !== undefined) window.clearTimeout(timer);
  secretRevealTimers.delete(tile);
  value.textContent = "••••••••";
  value.dataset.revealed = "false";
  secretButtonState(tile, false);
}

function revealSecret(tile: HTMLElement, value: HTMLElement): void {
  value.textContent = value.dataset.secretValue ?? "";
  value.dataset.revealed = "true";
  secretButtonState(tile, true);
  secretRevealTimers.set(tile, window.setTimeout(() => hideSecret(tile), secretRevealTimeout));
}

async function copySecret(secret: string): Promise<void> {
  try {
    const clipboard = navigator.clipboard;
    if (clipboard?.writeText) {
      await clipboard.writeText(secret);
      return;
    }
  } catch {
    // Use the legacy path when the secure-context API is unavailable or denied.
  }

  const textarea = document.createElement("textarea");
  textarea.className = "session-information-copy-fallback";
  textarea.readOnly = true;
  textarea.value = secret;
  document.body.append(textarea);
  textarea.select();
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } catch {
    copied = false;
  } finally {
    textarea.remove();
  }
  if (!copied) throw new Error("secret copy failed");
}

document.addEventListener("click", (event) => {
  if (!(event.target instanceof Element)) return;
  const normalCopy = event.target.closest<HTMLButtonElement>("[data-copy-value]");
  if (normalCopy) {
    void copySecret(normalCopy.dataset.copyValue ?? "").then(() => {
      const notice = liveNotice("info", "Value copied.");
      window.setTimeout(() => notice.remove(), 3000);
    }).catch(() => showActionError("Could not copy value. Please try again."));
    return;
  }
  const fact = event.target.closest<HTMLElement>(".session-information-fact");
  if (!fact) return;
  const value = fact.querySelector<HTMLElement>("[data-secret-value]");
  const button = event.target.closest<HTMLButtonElement>("[data-secret-reveal]");
  if (button && value) {
    if (value.dataset.revealed === "true") hideSecret(fact);
    else revealSecret(fact, value);
    return;
  }
  if (event.target.closest("[data-secret-copy]") && value) {
    void copySecret(value.dataset.secretValue ?? "").then(() => {
      const notice = liveNotice("info", "Secret copied.");
      window.setTimeout(() => notice.remove(), 3000);
    }).catch(() => showActionError("Could not copy secret. Please try again."));
  }
});
document.addEventListener("change", (event) => {
  const toggle = event.target;
  if (!(toggle instanceof HTMLInputElement) || !toggle.matches("[data-session-secret-toggle]")) return;
  const value = toggle.closest("form")?.querySelector<HTMLInputElement>('input[name="value"]');
  if (value) value.type = toggle.checked ? "password" : "text";
});

document.addEventListener("visibilitychange", () => {
  if (document.hidden) {
    document.querySelectorAll<HTMLElement>("[data-secret='true'] [data-secret-value][data-revealed='true']").forEach((value) => {
      const fact = value.closest<HTMLElement>(".session-information-fact");
      if (fact) hideSecret(fact);
    });
  }
});
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

// A sent chat message clears the field; a newly swapped log shows its newest line.
document.addEventListener("htmx:afterRequest", (event) => {
  const form = event.target;
  if (form instanceof HTMLFormElement && form.hasAttribute("data-chat-form") && (event as CustomEvent).detail.successful) form.reset();
});

document.addEventListener("htmx:afterSwap", (event) => {
  const target = event.target;
  if (target instanceof HTMLElement && target.id === "game-request-lobby") target.scrollTop = target.scrollHeight;
});

// A live refresh re-renders each collapsible card in its default state, so the
// viewer's own open/closed choice is remembered and put back after the swap.
const collapsibleState = new Map<string, boolean>();

document.addEventListener(
  "toggle",
  (event) => {
    const card = event.target;
    if (card instanceof HTMLDetailsElement && card.hasAttribute("data-collapsible")) collapsibleState.set(card.id, card.open);
  },
  true,
);

document.addEventListener("htmx:afterSwap", () => {
  for (const card of document.querySelectorAll<HTMLDetailsElement>("details[data-collapsible]")) {
    const open = collapsibleState.get(card.id);
    if (open !== undefined) card.open = open;
  }
});
