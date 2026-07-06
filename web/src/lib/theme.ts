// Theme handling: dark is the product default; light is opt-in.
// Persisted per browser.

const KEY = "wispbox.theme";

export type Theme = "dark" | "light";

export function currentTheme(): Theme {
  const stored = localStorage.getItem(KEY);
  if (stored === "light" || stored === "dark") return stored;
  return "dark";
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem(KEY, theme);
}

export function initTheme() {
  document.documentElement.dataset.theme = currentTheme();
}

export function toggleTheme(): Theme {
  const next: Theme = currentTheme() === "dark" ? "light" : "dark";
  applyTheme(next);
  return next;
}
