// Colour themes: the list comes from theme_list (built-in and custom themes, every token's colour);
// the chosen theme's tokens are set as inline custom properties on <html>, which beat tokens.css.
// System mode follows macOS with one theme for light and one for dark. The last applied theme is
// cached in localStorage so the next start paints it before the backend answers.
import { useSyncExternalStore } from "react";
import * as api from "./api";

export interface ThemeDef {
  id: string;
  name: string;
  mode: "light" | "dark";
  builtin: boolean;
  tokens: Record<string, string>;
}
export interface TokenGroup {
  name: string;
  tokens: string[];
}
export interface ThemeChoice {
  theme: string;
  light: string;
  dark: string;
}
export interface ThemeList {
  themes: ThemeDef[];
  groups: TokenGroup[];
  active: ThemeChoice;
}

const SHADOW = {
  light: { shadow: "0 1px 2px rgba(29, 35, 48, 0.06), 0 14px 30px -14px rgba(29, 35, 48, 0.22)", scrim: "rgba(29, 35, 48, 0.32)" },
  dark: { shadow: "0 1px 2px rgba(0, 0, 0, 0.4), 0 14px 30px -14px rgba(0, 0, 0, 0.7)", scrim: "rgba(0, 0, 0, 0.55)" },
};
const CACHE_KEY = "hadron.theme";
/** Fired on window after a theme is applied, so widgets and terminals repaint. */
export const THEME_EVENT = "hadron:theme";

interface State {
  list: ThemeList | null;
  systemDark: boolean;
  /** a theme shown in place of the chosen one, such as while editing; null shows the choice */
  preview: ThemeDef | null;
}
let state: State = { list: null, systemDark: false, preview: null };
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());
const set = (p: Partial<State>) => {
  state = { ...state, ...p };
  apply();
  emit();
};

/** The theme in use for a choice and the OS appearance. */
export function resolve(list: ThemeList, systemDark: boolean): ThemeDef | undefined {
  const { theme, light, dark } = list.active;
  const id = theme === "system" ? (systemDark ? dark : light) : theme;
  return list.themes.find((t) => t.id === id) ?? list.themes.find((t) => t.id === (systemDark ? "classic-dark" : "classic-light"));
}

let appliedKey = "";
/** Sets a theme's tokens on <html>. */
export function applyTokens(mode: "light" | "dark", tokens: Record<string, string>) {
  const el = document.documentElement;
  const key = mode + JSON.stringify(tokens);
  if (key === appliedKey) return;
  appliedKey = key;
  for (const [k, v] of Object.entries(tokens)) el.style.setProperty(`--${k}`, v);
  el.style.setProperty("--shadow", SHADOW[mode].shadow);
  el.style.setProperty("--scrim", SHADOW[mode].scrim);
  el.setAttribute("data-theme", mode);
  window.dispatchEvent(new Event(THEME_EVENT));
}

function apply() {
  const t = state.preview ?? (state.list ? resolve(state.list, state.systemDark) : undefined);
  if (!t) return;
  applyTokens(t.mode, t.tokens);
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify({ mode: t.mode, tokens: t.tokens }));
  } catch {
    /* storage may be off */
  }
}

/** Paints the cached theme before React mounts. */
export function applyCachedTheme() {
  try {
    const c = JSON.parse(localStorage.getItem(CACHE_KEY) ?? "null") as { mode: "light" | "dark"; tokens: Record<string, string> } | null;
    if (c && (c.mode === "light" || c.mode === "dark") && c.tokens) applyTokens(c.mode, c.tokens);
  } catch {
    /* a broken cache paints Classic */
  }
}

export async function loadThemes() {
  try {
    const list = await api.theme_list();
    set({ list });
  } catch {
    /* keep what is shown; the next change event retries */
  }
}

let started = false;
/** Loads the themes and keeps them in step with the backend and macOS. Call once. */
export function startThemes(): () => void {
  if (started) return () => {};
  started = true;
  const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
  state = { ...state, systemDark: !!mq?.matches };
  const onMq = (e: MediaQueryListEvent) => set({ systemDark: e.matches });
  mq?.addEventListener?.("change", onMq);
  const off = api.onChanged((e) => {
    const ents = e.entities ?? [];
    if (ents.includes("themes") || ents.includes("settings")) void loadThemes();
  });
  void loadThemes();
  return () => {
    started = false;
    mq?.removeEventListener?.("change", onMq);
    off();
  };
}

export const themeActions = {
  /** Picks a theme. In System mode a light theme fills the light slot and a dark one the dark slot. */
  async pick(t: ThemeDef) {
    const cur = state.list?.active;
    const args = cur?.theme === "system" ? (t.mode === "dark" ? { dark: t.id } : { light: t.id }) : { theme: t.id };
    return choose(args);
  },
  /** Switches between following macOS and one fixed theme (the one showing now). */
  async setSystem(on: boolean) {
    if (on) return choose({ theme: "system" });
    const t = state.list ? resolve(state.list, state.systemDark) : undefined;
    return choose({ theme: t?.id ?? "classic-light" });
  },
  /** The header's moon: flip to the other mode's theme. */
  async toggleMode() {
    const list = state.list;
    if (!list) return;
    const now = resolve(list, state.systemDark);
    const target = now?.mode === "dark" ? list.active.light : list.active.dark;
    return choose({ theme: target });
  },
  preview(t: ThemeDef | null) {
    set({ preview: t });
  },
};

async function choose(a: { theme?: string; light?: string; dark?: string }) {
  if (state.list) set({ list: { ...state.list, active: { ...state.list.active, ...a } } });
  await api.theme_set(a);
  await loadThemes();
}

const snapshot = () => state;
export function useThemes() {
  return useSyncExternalStore((f) => {
    subs.add(f);
    return () => subs.delete(f);
  }, snapshot);
}

/** The theme list as loaded, for the palette. */
export function themeListNow(): ThemeList | null {
  return state.list;
}

/** The theme showing now, for tests and the palette. */
export function currentTheme(): ThemeDef | undefined {
  return state.preview ?? (state.list ? resolve(state.list, state.systemDark) : undefined);
}

/** Resets the module, for tests. */
export function resetThemesForTest() {
  state = { list: null, systemDark: false, preview: null };
  appliedKey = "";
  started = false;
  const el = document.documentElement;
  el.removeAttribute("style");
  el.removeAttribute("data-theme");
}
