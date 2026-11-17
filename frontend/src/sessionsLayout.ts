// The Sessions space's layout: tabs, each a tree of split panes, each pane one free session. It is
// pure data (kept in the sessions.layout setting, so it survives a restart); the free sessions
// themselves live in Go. reconcile() fits a layout to the sessions that exist: panes of gone ones
// close, and a session no tab shows gets a tab of its own (or the split it was asked for).

export type Dir = "row" | "col"; // row: side by side (split right); col: one above the other (split down)
export type Node = { pane: string } | { id: string; split: Dir; a: Node; b: Node; ratio: number };
export interface Tab {
  id: string;
  root: Node;
  /** the pane that has the focus in this tab */
  active: string;
}
export interface Layout {
  tabs: Tab[];
  /** the tab shown */
  active: string;
}

export const EMPTY: Layout = { tabs: [], active: "" };
export const SETTING_LAYOUT = "sessions.layout";

let seq = 0;
const newId = (p: string) => `${p}${Date.now().toString(36)}${(seq++).toString(36)}`;

const isPane = (n: Node): n is { pane: string } => "pane" in n;

/** The panes of a tree, left to right and top to bottom. */
export function panes(n: Node): string[] {
  return isPane(n) ? [n.pane] : [...panes(n.a), ...panes(n.b)];
}

/** Every pane of the layout. */
export const allPanes = (l: Layout) => l.tabs.flatMap((t) => panes(t.root));

export const activeTab = (l: Layout): Tab | undefined => l.tabs.find((t) => t.id === l.active) ?? l.tabs[0];

/** The session that has the focus: the active pane of the active tab. */
export const activePane = (l: Layout): string => activeTab(l)?.active ?? "";

/** The tab that holds a pane. */
export const tabOf = (l: Layout, pane: string) => l.tabs.find((t) => panes(t.root).includes(pane));

function without(n: Node, pane: string): Node | null {
  if (isPane(n)) return n.pane === pane ? null : n;
  const a = without(n.a, pane);
  const b = without(n.b, pane);
  if (!a) return b;
  if (!b) return a;
  return a === n.a && b === n.b ? n : { ...n, a, b };
}

function replace(n: Node, pane: string, by: Node): Node {
  if (isPane(n)) return n.pane === pane ? by : n;
  return { ...n, a: replace(n.a, pane, by), b: replace(n.b, pane, by) };
}

function fix(l: Layout): Layout {
  const tabs = l.tabs.map((t) => (panes(t.root).includes(t.active) ? t : { ...t, active: panes(t.root)[0] }));
  const active = tabs.some((t) => t.id === l.active) ? l.active : (tabs[0]?.id ?? "");
  return { tabs, active };
}

/** A new tab with one pane, shown. */
export function openTab(l: Layout, pane: string, at = l.tabs.length): Layout {
  const t: Tab = { id: newId("t"), root: { pane }, active: pane };
  const tabs = [...l.tabs];
  tabs.splice(Math.max(0, Math.min(at, tabs.length)), 0, t);
  return { tabs, active: t.id };
}

/** Splits a pane: the new pane goes right of it (row) or below it (col) and gets the focus. */
export function split(l: Layout, pane: string, add: string, dir: Dir): Layout {
  const t = tabOf(l, pane);
  if (!t) return openTab(l, add);
  const node: Node = { id: newId("s"), split: dir, a: { pane }, b: { pane: add }, ratio: 0.5 };
  return { tabs: l.tabs.map((x) => (x === t ? { ...x, root: replace(x.root, pane, node), active: add } : x)), active: t.id };
}

/** Closes a pane: its sibling takes the room, an empty tab goes, and the focus moves next door. */
export function close(l: Layout, pane: string): Layout {
  const t = tabOf(l, pane);
  if (!t) return l;
  const list = panes(t.root);
  const root = without(t.root, pane);
  if (!root) {
    const i = l.tabs.indexOf(t);
    const tabs = l.tabs.filter((x) => x !== t);
    const next = l.active === t.id ? (tabs[Math.min(i, tabs.length - 1)]?.id ?? "") : l.active;
    return fix({ tabs, active: next });
  }
  const i = list.indexOf(pane);
  const left = panes(root);
  const focus = t.active === pane ? left[Math.max(0, Math.min(i - 1, left.length - 1))] : t.active;
  return fix({ ...l, tabs: l.tabs.map((x) => (x === t ? { ...x, root, active: focus } : x)) });
}

/** Gives a pane the focus, showing its tab. */
export function focus(l: Layout, pane: string): Layout {
  const t = tabOf(l, pane);
  if (!t) return l;
  return { tabs: l.tabs.map((x) => (x === t ? { ...x, active: pane } : x)), active: t.id };
}

/** Shows the tab at index (0-based); the last tab when the index is past the end. */
export function selectTab(l: Layout, index: number): Layout {
  if (!l.tabs.length) return l;
  const t = l.tabs[Math.max(0, Math.min(index, l.tabs.length - 1))];
  return { ...l, active: t.id };
}

/** Shows the next (1) or previous (-1) tab, wrapping around. */
export function stepTab(l: Layout, d: 1 | -1): Layout {
  if (!l.tabs.length) return l;
  const i = l.tabs.findIndex((t) => t.id === activeTab(l)?.id);
  return { ...l, active: l.tabs[(i + d + l.tabs.length) % l.tabs.length].id };
}

/** Moves a tab one place left (-1) or right (1). */
export function moveTab(l: Layout, id: string, d: 1 | -1): Layout {
  const i = l.tabs.findIndex((t) => t.id === id);
  const j = i + d;
  if (i < 0 || j < 0 || j >= l.tabs.length) return l;
  const tabs = [...l.tabs];
  [tabs[i], tabs[j]] = [tabs[j], tabs[i]];
  return { ...l, tabs };
}

/** Moves a tab to index to (drag and drop). */
export function placeTab(l: Layout, id: string, to: number): Layout {
  const i = l.tabs.findIndex((t) => t.id === id);
  if (i < 0) return l;
  const tabs = [...l.tabs];
  const [t] = tabs.splice(i, 1);
  tabs.splice(Math.max(0, Math.min(to, tabs.length)), 0, t);
  return { ...l, tabs };
}

/** Sets the share of a split's first side, between 10% and 90%. */
export function setRatio(l: Layout, splitId: string, ratio: number): Layout {
  const r = Math.max(0.1, Math.min(0.9, ratio));
  const walk = (n: Node): Node => (isPane(n) ? n : n.id === splitId ? { ...n, ratio: r } : { ...n, a: walk(n.a), b: walk(n.b) });
  return { ...l, tabs: l.tabs.map((t) => ({ ...t, root: walk(t.root) })) };
}

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Where each pane of a tree sits in the unit square. */
export function rects(n: Node, r: Rect = { x: 0, y: 0, w: 1, h: 1 }, out = new Map<string, Rect>()): Map<string, Rect> {
  if (isPane(n)) return out.set(n.pane, r);
  if (n.split === "row") {
    rects(n.a, { ...r, w: r.w * n.ratio }, out);
    rects(n.b, { ...r, x: r.x + r.w * n.ratio, w: r.w * (1 - n.ratio) }, out);
  } else {
    rects(n.a, { ...r, h: r.h * n.ratio }, out);
    rects(n.b, { ...r, y: r.y + r.h * n.ratio, h: r.h * (1 - n.ratio) }, out);
  }
  return out;
}

/** The pane next to the active one in a direction, or "" when there is none. */
export function neighbor(l: Layout, dir: "left" | "right" | "up" | "down"): string {
  const t = activeTab(l);
  if (!t) return "";
  const all = rects(t.root);
  const me = all.get(t.active);
  if (!me) return "";
  const eps = 1e-6;
  let best = "";
  let bestGap = Infinity;
  for (const [id, r] of all) {
    if (id === t.active) continue;
    const overlapsV = r.y < me.y + me.h - eps && r.y + r.h > me.y + eps;
    const overlapsH = r.x < me.x + me.w - eps && r.x + r.w > me.x + eps;
    let gap = Infinity;
    if (dir === "right" && overlapsV && r.x >= me.x + me.w - eps) gap = r.x - (me.x + me.w);
    if (dir === "left" && overlapsV && r.x + r.w <= me.x + eps) gap = me.x - (r.x + r.w);
    if (dir === "down" && overlapsH && r.y >= me.y + me.h - eps) gap = r.y - (me.y + me.h);
    if (dir === "up" && overlapsH && r.y + r.h <= me.y + eps) gap = me.y - (r.y + r.h);
    if (gap < bestGap) {
      bestGap = gap;
      best = id;
    }
  }
  return best;
}

/** Where a session that is about to appear should go: a split of a pane, else its own tab. */
export type Placement = { split?: { pane: string; dir: Dir } };

/**
 * Fits a layout to the sessions that exist (ids, oldest first): panes of sessions that are gone
 * close, and each session no pane shows opens where `placed` asks (a split) or in a new tab at the
 * end. The newest placed session gets the focus.
 */
export function reconcile(l: Layout, ids: string[], placed: Map<string, Placement> = new Map()): Layout {
  const live = new Set(ids);
  let out = l;
  for (const p of allPanes(l)) if (!live.has(p)) out = close(out, p);
  const shown = new Set(allPanes(out));
  for (const id of ids) {
    if (shown.has(id)) continue;
    const want = placed.get(id)?.split;
    out = want && shown.has(want.pane) ? split(out, want.pane, id, want.dir) : openTab(out, id);
    shown.add(id);
  }
  return fix(out);
}

/** Reads a stored layout, dropping anything malformed. */
export function parseLayout(v: unknown): Layout {
  const node = (n: unknown): Node | null => {
    if (!n || typeof n !== "object") return null;
    const o = n as Record<string, unknown>;
    if (typeof o.pane === "string") return { pane: o.pane };
    if ((o.split === "row" || o.split === "col") && typeof o.id === "string") {
      const a = node(o.a);
      const b = node(o.b);
      if (!a) return b;
      if (!b) return a;
      const ratio = typeof o.ratio === "number" && o.ratio > 0 && o.ratio < 1 ? o.ratio : 0.5;
      return { id: o.id, split: o.split, a, b, ratio };
    }
    return null;
  };
  if (!v || typeof v !== "object") return EMPTY;
  const o = v as Record<string, unknown>;
  const tabs: Tab[] = [];
  for (const t of Array.isArray(o.tabs) ? o.tabs : []) {
    const r = t && typeof t === "object" ? (t as Record<string, unknown>) : null;
    const root = r ? node(r.root) : null;
    if (!r || !root || typeof r.id !== "string") continue;
    tabs.push({ id: r.id, root, active: typeof r.active === "string" ? r.active : panes(root)[0] });
  }
  return fix({ tabs, active: typeof o.active === "string" ? o.active : "" });
}
