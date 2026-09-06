import "htmx.org";

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
