import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../api";

/**
 * How wide the planner and card panels are, and which one folds when the window is too narrow for
 * both. The user drags a panel's inner edge to resize it; the width is a global setting per panel.
 * The window decides what is shown: the stored width is clamped to what leaves the board room, the
 * panels shrink toward their minimums first, and only then one folds to its rail (the card panel,
 * unless the user just asked for it). Both never fold on their own.
 */
export type PanelName = "planner" | "card";

export const PANEL_LIMITS: Record<PanelName, { min: number; def: number; max: number }> = {
  planner: { min: 260, def: 340, max: 720 },
  card: { min: 320, def: 450, max: 960 },
};
/** The setting each panel's width is kept in (px; unset or null is the default). */
export const SETTING_PANEL_WIDTH: Record<PanelName, string> = { planner: "ui.panel_width.planner", card: "ui.panel_width.card" };
/** The narrowest the board gets while both panels show. */
export const BOARD_MIN = 300;
/** A folded panel's rail. */
export const RAIL = 40;
/** At or under this window width the panels are overlays opened from the header (app.css). */
export const OVERLAY_MAX = 600;
/** Arrow keys on a resize handle move it this far; with Shift, KEY_STEP_BIG. */
export const KEY_STEP = 16;
export const KEY_STEP_BIG = 64;

export interface PanelSlot {
  /** the width to show, px */
  width: number;
  /** folded because the window is too narrow, not by the user */
  auto: boolean;
  /** the drag limits at this window width */
  min: number;
  max: number;
}
export interface Layout {
  overlay: boolean;
  planner: PanelSlot;
  card: PanelSlot;
}

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));
const stored = (p: PanelName, v: number | null | undefined) => clamp(typeof v === "number" && Number.isFinite(v) ? v : PANEL_LIMITS[p].def, PANEL_LIMITS[p].min, PANEL_LIMITS[p].max);

/**
 * The layout for a window `width` px wide (in the page's own px, after zoom). `collapsed` is what
 * the user folded; `prefer` is the panel the user last asked to see, which keeps its place when only
 * one fits.
 */
export function panelLayout(o: {
  width: number;
  widths: Partial<Record<PanelName, number | null>>;
  collapsed: Partial<Record<PanelName, boolean>>;
  prefer?: PanelName;
}): Layout {
  const want = { planner: stored("planner", o.widths.planner), card: stored("card", o.widths.card) };
  const folded = { planner: !!o.collapsed.planner, card: !!o.collapsed.card };
  const auto = { planner: false, card: false };
  const slot = (p: PanelName, w: number, max: number): PanelSlot => ({ width: Math.round(w), auto: auto[p], min: PANEL_LIMITS[p].min, max: Math.max(PANEL_LIMITS[p].min, Math.floor(Math.min(PANEL_LIMITS[p].max, max))) });
  if (o.width <= OVERLAY_MAX) return { overlay: true, planner: slot("planner", want.planner, want.planner), card: slot("card", want.card, want.card) };

  const room = o.width - BOARD_MIN;
  const open = (p: PanelName) => !folded[p] && !auto[p];
  const need = () => (open("planner") ? PANEL_LIMITS.planner.min : RAIL) + (open("card") ? PANEL_LIMITS.card.min : RAIL);
  // Only one fits at its minimum: the card panel folds, unless the user asked for it last.
  if (open("planner") && open("card") && need() > room) auto[o.prefer === "card" ? "planner" : "card"] = true;

  const w = { ...want };
  const used = (p: PanelName) => (open(p) ? w[p] : RAIL);
  let excess = used("planner") + used("card") - room;
  if (excess > 0) {
    // Shrink the open panels toward their minimums, each by its share of the slack.
    const slack = (p: PanelName) => (open(p) ? w[p] - PANEL_LIMITS[p].min : 0);
    const total = slack("planner") + slack("card");
    for (const p of ["planner", "card"] as const) {
      if (!open(p) || total <= 0) continue;
      w[p] -= Math.min(slack(p), (excess * slack(p)) / total);
    }
    excess = used("planner") + used("card") - room;
    // Still too wide (a panel alone at its minimum): the board gives way, a panel never vanishes.
  }
  const other = (p: PanelName) => used(p === "planner" ? "card" : "planner");
  return {
    overlay: false,
    planner: slot("planner", w.planner, room - other("planner")),
    card: slot("card", w.card, room - other("card")),
  };
}

// What the keyboard command for folding a panel needs to know, and how to show an auto-folded one.
let current: Layout | null = null;
let showPanel: ((p: PanelName) => void) | null = null;
/** The layout on screen now, for commands outside React. */
export const currentLayout = () => current;
/** Shows a panel the window folded (the other folds instead). False if none is mounted. */
export function showAutoFolded(p: PanelName): boolean {
  if (!showPanel) return false;
  showPanel(p);
  return true;
}

/** The page's width in its own px: the window's, divided by the zoom applied to it. */
function pageWidth(zoom: number) {
  return (typeof window === "undefined" ? 1360 : window.innerWidth) / (zoom || 1);
}

const NOTE: Record<PanelName, string> = {
  card: "The window is too narrow for both chats, so the card panel is folded to the rail on the right. Click the rail to open it; the planner folds instead.",
  planner: "The window is too narrow for both chats, so the planner is folded to the rail on the left. Click the rail to open it; the card panel folds instead.",
};

export interface PanelControls {
  slot: PanelSlot;
  /** live while dragging (commit false), saved when the drag or key press ends (commit true) */
  resize: (width: number, commit: boolean) => void;
  reset: () => void;
  /** expand the panel's rail: unfolds it, and when the window folded it, folds the other one */
  expand: () => void;
}

/**
 * The live layout for the App: follows the window's width and the zoom, reads and saves the
 * widths, and says on screen when the window folds a panel.
 */
export function usePanelLayout(o: {
  zoom: number;
  collapsed: Partial<Record<PanelName, boolean>>;
  setCollapsed: (p: PanelName, c: boolean) => unknown;
  toast: (t: string) => void;
  /** a card was just picked: the card panel is the one to keep when only one fits */
  selectedId?: string;
}): { layout: Layout; planner: PanelControls; card: PanelControls } {
  const { zoom, collapsed, setCollapsed, toast } = o;
  const [width, setWidth] = useState(() => pageWidth(zoom));
  useEffect(() => {
    const on = () => setWidth(pageWidth(zoom));
    on();
    window.addEventListener("resize", on);
    return () => window.removeEventListener("resize", on);
  }, [zoom]);

  const [widths, setWidths] = useState<Partial<Record<PanelName, number | null>>>({});
  const live = useRef<Partial<Record<PanelName, number>>>({});
  const [, bump] = useState(0);
  const load = useCallback(async () => {
    try {
      const s = (await api.settings_get()) as Record<string, unknown> | null;
      const num = (v: unknown) => (typeof v === "number" ? v : null);
      setWidths({ planner: num(s?.[SETTING_PANEL_WIDTH.planner]), card: num(s?.[SETTING_PANEL_WIDTH.card]) });
    } catch {
      // no backend: the defaults stay
    }
  }, []);
  useEffect(() => {
    void load();
    return api.onChanged((e) => {
      if (!e?.entities || e.entities.includes("settings")) void load();
    });
  }, [load]);

  const [prefer, setPrefer] = useState<PanelName>("planner");
  const selected = o.selectedId;
  useEffect(() => {
    if (selected) setPrefer("card");
  }, [selected]);

  const shown = { planner: live.current.planner ?? widths.planner, card: live.current.card ?? widths.card };
  const layout = useMemo(
    () => panelLayout({ width, widths: shown, collapsed, prefer }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [width, shown.planner, shown.card, collapsed.planner, collapsed.card, prefer],
  );
  current = layout;

  // Say it once each time the window folds a panel, so it is clear how to get it back.
  const said = useRef<PanelName | null>(null);
  const autoNow: PanelName | null = layout.card.auto ? "card" : layout.planner.auto ? "planner" : null;
  useEffect(() => {
    if (autoNow && said.current !== autoNow) toast(NOTE[autoNow]);
    said.current = autoNow;
  }, [autoNow, toast]);

  const expand = useCallback(
    (p: PanelName) => {
      setPrefer(p);
      if (collapsed[p]) void setCollapsed(p, false);
    },
    [collapsed, setCollapsed],
  );
  useEffect(() => {
    showPanel = expand;
    return () => {
      if (showPanel === expand) showPanel = null;
    };
  }, [expand]);

  const controls = (p: PanelName): PanelControls => ({
    slot: layout[p],
    resize: (w, commit) => {
      const v = Math.round(clamp(w, layout[p].min, layout[p].max));
      if (!commit) {
        live.current[p] = v;
        bump((n) => n + 1);
        return;
      }
      delete live.current[p];
      setWidths((cur) => ({ ...cur, [p]: v }));
      void Promise.resolve(api.settings_update({ values: { [SETTING_PANEL_WIDTH[p]]: v } })).catch(() => undefined);
    },
    reset: () => {
      delete live.current[p];
      setWidths((cur) => ({ ...cur, [p]: null }));
      void Promise.resolve(api.settings_update({ values: { [SETTING_PANEL_WIDTH[p]]: null } })).catch(() => undefined);
    },
    expand: () => expand(p),
  });
  return { layout, planner: controls("planner"), card: controls("card") };
}
