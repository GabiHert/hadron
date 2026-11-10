// What each command in keys.ts does. The dispatcher (keys.ts), the palette, the cheat sheet and the
// native menus all run commands through runCommand, so a key press, a palette pick and a menu click
// do the same thing. Handlers return false when they did nothing, so the key goes on to the page.
//
// Most handlers act on the DOM the panels render: zones are elements with data-zone, cards carry
// data-card-id inside a column with data-col-id, chat lines carry data-msg, dashboard blocks
// data-block. That keeps the board's filter, folds and the open question exactly as the user sees them.

import * as api from "./api";
import { byId } from "./keys";
import type { Zone } from "./keys";
import type { Board } from "./state/useBoard";

// ---- zoom ----

export const ZOOM_MIN = 0.7;
export const ZOOM_MAX = 2;
/** The steps ⌘= and ⌘- walk through, about 10% apart, as browsers do. */
export const ZOOM_STEPS = [0.7, 0.8, 0.9, 1, 1.1, 1.2, 1.3, 1.5, 1.7, 2];
export const SETTING_ZOOM = "ui.zoom";

/** The next zoom level from z: dir 1 is in, -1 is out, 0 resets to 100%. */
export function nextZoom(z: number, dir: 1 | -1 | 0): number {
  if (dir === 0) return 1;
  const cur = clampZoom(z);
  if (dir > 0) return ZOOM_STEPS.find((s) => s > cur + 0.001) ?? ZOOM_MAX;
  return [...ZOOM_STEPS].reverse().find((s) => s < cur - 0.001) ?? ZOOM_MIN;
}

/** A stored zoom made safe: a number between 70% and 200%, else 100%. */
export function clampZoom(v: unknown): number {
  const n = typeof v === "number" ? v : typeof v === "string" ? parseFloat(v) : NaN;
  if (!Number.isFinite(n)) return 1;
  return Math.round(Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, n)) * 100) / 100;
}

export const zoomLabel = (z: number) => `Zoom ${Math.round(z * 100)}%`;

// ---- the board's grid ----

export interface Lane {
  id: string;
  /** a column's first letter, for g + letter */
  name: string;
  cards: string[];
}

export type Dir = "up" | "down" | "left" | "right";

/**
 * The card the selection moves to. Up and down stay in the column; left and right go to the nearest
 * column in that direction that shows a card, at the same row or its last card. With nothing
 * selected (or a card the filter hides) it starts at the first card.
 */
export function step(lanes: Lane[], selected: string | null, dir: Dir): string | null {
  const first = lanes.find((l) => l.cards.length)?.cards[0] ?? null;
  const li = lanes.findIndex((l) => selected !== null && l.cards.includes(selected));
  if (li < 0) return first;
  const lane = lanes[li];
  const row = lane.cards.indexOf(selected as string);
  if (dir === "down") return lane.cards[Math.min(row + 1, lane.cards.length - 1)];
  if (dir === "up") return lane.cards[Math.max(row - 1, 0)];
  const d = dir === "right" ? 1 : -1;
  for (let i = li + d; i >= 0 && i < lanes.length; i += d) {
    const l = lanes[i];
    if (l.cards.length) return l.cards[Math.min(row, l.cards.length - 1)];
  }
  return selected;
}

/** The column g jumps to: a digit is its number (1 is the first), a letter the next column starting with it. */
export function gotoLane(lanes: Lane[], key: string, current: string | null): Lane | null {
  if (/^[1-9]$/.test(key)) return lanes[parseInt(key, 10) - 1] ?? null;
  if (!/^[A-Z]$/.test(key)) return null;
  const hits = lanes.filter((l) => l.name.trim().toUpperCase().startsWith(key));
  if (!hits.length) return null;
  const at = hits.findIndex((l) => l.id === current);
  return hits[(at + 1) % hits.length];
}

/** Reads the board as the user sees it: unfolded columns with the cards the filter shows. */
export function readLanes(root: ParentNode = document): Lane[] {
  return Array.from(root.querySelectorAll<HTMLElement>("[data-col-id]")).map((col) => ({
    id: col.dataset.colId ?? "",
    name: col.dataset.colName ?? "",
    cards: Array.from(col.querySelectorAll<HTMLElement>("[data-card-id]")).map((c) => c.dataset.cardId ?? ""),
  }));
}

/** Where a card goes for card.moveUp/Down/Left/Right, as card_move's column and position. */
export function moveTarget(board: Pick<Board, "columns" | "cards">, cardId: string, dir: Dir): { column_id: string; position?: number } | null {
  const card = board.cards.find((c) => c.id === cardId);
  if (!card) return null;
  const cols = [...board.columns].sort((a, b) => a.position - b.position);
  const ci = cols.findIndex((c) => c.id === card.column_id);
  if (dir === "left" || dir === "right") {
    const to = cols[ci + (dir === "right" ? 1 : -1)];
    return to ? { column_id: to.id } : null;
  }
  const mates = board.cards.filter((c) => c.column_id === card.column_id).sort((a, b) => a.position - b.position);
  const at = mates.findIndex((c) => c.id === cardId);
  const pos = dir === "down" ? at + 1 : at - 1;
  if (pos < 0 || pos >= mates.length) return null;
  return { column_id: card.column_id, position: pos };
}

// ---- focus ----

/** A value made safe inside a quoted attribute selector. */
export const attrEsc = (v: string) => v.replace(/["\\]/g, "\\$&");

export const zoneEl = (z: Zone | "main"): HTMLElement | null =>
  z === "main" ? document.querySelector<HTMLElement>('[data-zone="board"], [data-zone="dashboard"]') : document.querySelector<HTMLElement>(`[data-zone="${z}"]`);

/** The zone that holds the focus, or null. */
export function focusedZone(): Zone | null {
  const el = document.activeElement;
  return ((el instanceof Element && el.closest("[data-zone]")?.getAttribute("data-zone")) as Zone | null) || null;
}

/** Runs act on the element once find returns it (the panel it lives in may still be rendering). */
export function whenReady<T extends HTMLElement>(find: () => T | null, act: (el: T) => void, tries = 30): void {
  const el = find();
  if (el) return act(el);
  if (tries > 0) setTimeout(() => whenReady(find, act, tries - 1), 16);
}

/** A collapsed column's rail. */
export const railEl = (columnId: string) => document.querySelector<HTMLElement>(`[data-col-id="${attrEsc(columnId)}"] .rail`);

export function focusEl(el: HTMLElement | null | undefined): boolean {
  if (!el) return false;
  el.focus({ preventScroll: true });
  el.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  return true;
}

/** The card panel once it shows this card. */
const cardPanel = (id: string) => document.querySelector<HTMLElement>(`[data-zone="card"][data-card="${attrEsc(id)}"]`);

export const cardEl = (id: string) => document.querySelector<HTMLElement>(`[data-card-id="${attrEsc(id)}"]`);

/** Puts the focus in a zone: the board on its selected card, a panel on itself, the workspace bar on the active workspace. */
export function focusZone(z: Zone, selectedId: string | null = null): void {
  whenReady(
    () => zoneEl(z),
    (zone) => {
      if (z === "board") {
        const card = (selectedId && zone.querySelector<HTMLElement>(`[data-card-id="${attrEsc(selectedId)}"]`)) || zone.querySelector<HTMLElement>("[data-card-id]");
        if (focusEl(card)) return;
      }
      if (z === "dashboard" && focusEl(zone.querySelector<HTMLElement>("[data-block]"))) return;
      if (z === "workspaces" && focusEl(zone.querySelector<HTMLElement>('[aria-selected="true"]'))) return;
      focusEl(zone);
    },
  );
}

/** The panel a command such as Chat ⇄ Terminal acts on: the focused one, else the last one used. */
export function panelOf(zone: Zone | null, last: "planner" | "card" | null, hasCard: boolean): "planner" | "card" {
  if (zone === "planner" || zone === "card") return zone;
  if (last) return last;
  return hasCard ? "card" : "planner";
}

/** The zones ⌘] and ⌘[ cycle through, left to right, without the collapsed panels. */
export function zoneCycle(collapsed: { planner?: boolean; card?: boolean } = {}, mainView: "board" | "dashboard" = "board", hasCard = true): Zone[] {
  const z: Zone[] = [];
  if (!collapsed.planner) z.push("planner");
  z.push(mainView === "dashboard" ? "dashboard" : "board");
  if (!collapsed.card && hasCard) z.push("card");
  return z;
}

// ---- UI events: what a handler asks a panel to do with its own state ----

export type UIRequest = { do: "card.edit" } | { do: "card.model" } | { do: "planner.model" } | { do: "planner.adopt" };
const UI_EVENT = "hadron:ui";
export const requestUI = (r: UIRequest) => window.dispatchEvent(new CustomEvent<UIRequest>(UI_EVENT, { detail: r }));
export function onUIRequest(cb: (r: UIRequest) => void): () => void {
  const h = (e: Event) => cb((e as CustomEvent<UIRequest>).detail);
  window.addEventListener(UI_EVENT, h);
  return () => window.removeEventListener(UI_EVENT, h);
}

// ---- the handlers ----

export type ModalName = "newws" | "newsession" | "settings" | "palette" | "feedback" | "whatsnew" | "keys";

/** What the handlers can reach. App builds it on every render. */
export interface Env {
  board: Board;
  modal: ModalName | null;
  setModal: (m: ModalName | null) => void;
  openSettings: (tab?: string) => void;
  openFeedback: () => void;
  newSessionIn: (columnId?: string) => void;
  plannerView: "chat" | "terminal";
  setPlannerView: (v: "chat" | "terminal") => void;
  cardView: "chat" | "terminal";
  setCardView: (v: "chat" | "terminal") => void;
  setTab: (t: "conv" | "links" | "ctx") => void;
  /** asks before a destructive command; ok runs it */
  confirm: (text: string, okLabel: string, ok: () => void) => void;
  zoom: (dir: 1 | -1 | 0) => void;
  /** the panel the user worked in last */
  lastPanel: () => "planner" | "card" | null;
  toggleTheme: () => void;
}

type Handler = (env: Env, arg?: string) => boolean | void;

/** True when the focus is on a board control that is not a card (a rail, a column's name): Enter and Space press it. */
function onBoardButton() {
  const el = document.activeElement;
  return el instanceof HTMLElement && el.tagName === "BUTTON" && !el.closest("[data-card-id]") && !!el.closest('[data-zone="board"]');
}

/** The card a board command acts on: the focused card, else the selected one. */
function targetCard(env: Env) {
  const el = document.activeElement instanceof HTMLElement ? document.activeElement.closest<HTMLElement>("[data-card-id]") : null;
  const id = el?.dataset.cardId ?? env.board.selected?.id ?? null;
  return env.board.cards.find((c) => c.id === id) ?? null;
}

function selectAndFocus(env: Env, id: string | null) {
  if (!id) return false;
  env.board.actions.select(id);
  whenReady(() => cardEl(id), (el) => focusEl(el));
  return true;
}

const nav = (dir: Dir): Handler => (env) => {
  const cur = targetCard(env)?.id ?? null;
  return selectAndFocus(env, step(readLanes(), cur, dir));
};

const moveCard = (dir: Dir): Handler => (env) => {
  const c = targetCard(env);
  if (!c) return false;
  const to = moveTarget(env.board, c.id, dir);
  if (!to) return true;
  void env.board.actions.dropCard(c.id, to.column_id, to.position);
  // Into a collapsed column the card is out of sight: the focus goes to the column's rail.
  if (env.board.columns.find((x) => x.id === to.column_id)?.fold) {
    whenReady(() => railEl(to.column_id), (el) => focusEl(el));
    return true;
  }
  // Across columns the card's element is rebuilt: wait for it in the new column.
  whenReady(() => document.querySelector<HTMLElement>(`[data-col-id="${attrEsc(to.column_id)}"] [data-card-id="${attrEsc(c.id)}"]`), (el) => focusEl(el));
  return true;
};

/** The focused panel's element, for the chat commands. */
function panelZone(env: Env): HTMLElement | null {
  const z = focusedZone();
  if (z === "planner" || z === "card") return zoneEl(z);
  const last = env.lastPanel();
  return last ? zoneEl(last) : null;
}

/** The open question a panel shows (the newest), or null. */
const openQuestion = (zone: HTMLElement | null, approval = false) => {
  const all = zone ? Array.from(zone.querySelectorAll<HTMLElement>(approval ? ".qbox.appr" : ".qbox")) : [];
  return all[all.length - 1] ?? null;
};

function stepMessage(env: Env, d: 1 | -1): boolean {
  const zone = panelZone(env);
  if (!zone) return false;
  const list = Array.from(zone.querySelectorAll<HTMLElement>("[data-msg]"));
  if (!list.length) return false;
  const at = list.findIndex((m) => m === document.activeElement || m.contains(document.activeElement));
  const next = at < 0 ? list.length - 1 : Math.max(0, Math.min(list.length - 1, at + d));
  return focusEl(list[next]);
}

/** Enter on a focused message: its widget's menu, its first link, or its question's first option. */
export function actOnMessage(msg: HTMLElement): boolean {
  const menu = msg.querySelector<HTMLButtonElement>(".wmenu-btn");
  if (menu) {
    menu.click();
    return true;
  }
  const target = msg.querySelector<HTMLElement>(".opt, a[href], button:not([disabled]), textarea");
  return focusEl(target);
}

function focusComposer(zone: HTMLElement | null): boolean {
  if (!zone) return false;
  return focusEl(zone.querySelector<HTMLElement>(".composer textarea, .xterm textarea, .tnone button, .termwrap .tbanner button"));
}

const togglePanel = (panel: "planner" | "card"): Handler => (env) => {
  const ws = env.board.ws;
  if (!ws) return false;
  const collapsed = !!ws.ui.collapsed?.[panel];
  void env.board.actions.setCollapsed(panel, !collapsed);
  if (collapsed) focusZone(panel);
  else if (focusedZone() === panel) focusZone(ws.ui.main_view === "dashboard" ? "dashboard" : "board", env.board.selected?.id);
  return true;
};

function focusPanel(env: Env, panel: "planner" | "card") {
  const ws = env.board.ws;
  if (!ws) return false;
  if (panel === "card" && !env.board.selected) {
    env.board.actions.toast("Pick a card first: arrows on the board, then Enter.");
    return true;
  }
  if (ws.ui.collapsed?.[panel]) void env.board.actions.setCollapsed(panel, false);
  focusZone(panel);
  return true;
}

function cycle(env: Env, d: 1 | -1) {
  const ws = env.board.ws;
  if (!ws) return false;
  const zones = zoneCycle(ws.ui.collapsed ?? {}, ws.ui.main_view === "dashboard" ? "dashboard" : "board", !!env.board.selected);
  if (ws.ui.full) {
    void env.board.actions.setFullscreen("");
  }
  const cur = focusedZone();
  const at = cur ? zones.indexOf(cur) : -1;
  const next = zones[(at + d + zones.length) % zones.length] ?? zones[0];
  focusZone(next, env.board.selected?.id);
  return true;
}

const cardSession = (what: "start" | "stop" | "restart"): Handler => (env) => {
  const c = targetCard(env);
  if (!c) return false;
  const go = () => void env.board.actions.run(() => (what === "start" ? api.session_start({ card: c.id }) : what === "stop" ? api.session_stop({ card: c.id }) : api.session_restart({ card: c.id })));
  if (what === "start") go();
  else env.confirm(what === "stop" ? `Stop ${c.nick}'s session? The Claude conversation is kept and can be resumed.` : `Restart ${c.nick}'s session?`, what === "stop" ? "Stop session" : "Restart session", go);
  return true;
};

function dashBlocks(): HTMLElement[] {
  return Array.from(document.querySelectorAll<HTMLElement>("[data-block]"));
}
function stepBlock(d: 1 | -1) {
  const list = dashBlocks();
  if (!list.length) return false;
  const at = list.findIndex((b) => b === document.activeElement || b.contains(document.activeElement));
  return focusEl(list[at < 0 ? 0 : Math.max(0, Math.min(list.length - 1, at + d))]);
}
const focusedBlock = () => (document.activeElement instanceof HTMLElement ? document.activeElement.closest<HTMLElement>("[data-block]") : null);

function pickOption(env: Env, n: number): boolean {
  const q = openQuestion(panelZone(env));
  const opt = q?.querySelectorAll<HTMLButtonElement>(".opt")[n - 1];
  if (!opt) return false;
  opt.click();
  focusEl(opt);
  return true;
}

function answerApproval(env: Env, approve: boolean): boolean {
  const q = openQuestion(panelZone(env), true);
  if (!q) return false;
  const opts = Array.from(q.querySelectorAll<HTMLButtonElement>(".opt"));
  const want = approve ? /^(approve|yes|go|ok)/i : /^(reject|decline|no|deny)/i;
  const opt = opts.find((o) => want.test(o.textContent?.trim() ?? "")) ?? (approve ? opts[0] : opts[opts.length - 1]);
  if (!opt) return false;
  const label = opt.querySelector("b")?.textContent ?? opt.textContent ?? "";
  const target = q.querySelector("dd")?.textContent ?? "";
  env.confirm(`${label}${target ? ` on ${target}` : ""}? This answers the approval.`, label || (approve ? "Approve" : "Reject"), () => {
    if (opt.getAttribute("aria-pressed") !== "true") opt.click();
    setTimeout(() => q.querySelector<HTMLButtonElement>(".btn.pri:not([disabled])")?.click(), 0);
  });
  return true;
}

const HANDLERS: Record<string, Handler> = {
  "palette.toggle": (env) => env.setModal(env.modal === "palette" ? null : "palette"),
  "help.keys": (env) => env.setModal(env.modal === "keys" ? null : "keys"),
  "card.new": (env) => env.newSessionIn(),
  "workspace.new": (env) => env.setModal("newws"),
  "settings.open": (env) => env.openSettings(),
  "theme.toggle": (env) => env.toggleTheme(),
  "feedback.open": (env) => env.openFeedback(),
  "whatsnew.open": (env) => env.setModal("whatsnew"),
  "zoom.in": (env) => env.zoom(1),
  "zoom.out": (env) => env.zoom(-1),
  "zoom.reset": (env) => env.zoom(0),
  escape: (env) => {
    const el = document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null;
    const zone = el?.closest<HTMLElement>("[data-zone]") ?? null;
    const full = !!env.board.ws?.ui.full;
    // Inside a zone, Esc steps back out to it: from a text field, a message, an option or a menu.
    // On the board that is the selected card; on the zone itself it leaves full screen.
    const atRest = !el || el === zone || (zone?.dataset.zone === "board" && el.matches("[data-card-id]")) || (zone?.dataset.zone === "dashboard" && el.matches("[data-block]"));
    if (atRest) {
      if (full) return void env.board.actions.setFullscreen("");
      return false;
    }
    if (zone) {
      const z = zone.dataset.zone as Zone;
      if (z === "board" || z === "dashboard") focusZone(z, env.board.selected?.id);
      else focusEl(zone);
      return true;
    }
    el.blur();
    return true;
  },
  "zone.next": (env) => cycle(env, 1),
  "zone.prev": (env) => cycle(env, -1),
  "focus.planner": (env) => focusPanel(env, "planner"),
  "focus.card": (env) => focusPanel(env, "card"),
  "focus.board": (env) => {
    const ws = env.board.ws;
    if (!ws) return false;
    if (ws.ui.full) void env.board.actions.setFullscreen("");
    focusZone(ws.ui.main_view === "dashboard" ? "dashboard" : "board", env.board.selected?.id);
    return true;
  },
  "focus.workspaces": () => void focusZone("workspaces"),
  ...Object.fromEntries(
    [1, 2, 3, 4, 5, 6, 7, 8, 9].map((n) => [
      `workspace.${n}`,
      ((env) => {
        const w = env.board.visible[n - 1];
        if (!w) return false;
        if (w.id !== env.board.activeId) void env.board.actions.switchWorkspace(w.id);
        return true;
      }) as Handler,
    ]),
  ),
  "panel.planner.toggle": togglePanel("planner"),
  "panel.card.toggle": togglePanel("card"),
  "view.terminal": (env) => {
    const p = panelOf(focusedZone(), env.lastPanel(), !!env.board.selected);
    if (p === "card" && !env.board.selected) return false;
    const cur = p === "planner" ? env.plannerView : env.cardView;
    const next = cur === "chat" ? "terminal" : "chat";
    (p === "planner" ? env.setPlannerView : env.setCardView)(next);
    if (env.board.ws?.ui.collapsed?.[p]) void env.board.actions.setCollapsed(p, false);
    // The terminal focuses itself when it mounts; the chat gets its composer.
    if (next === "chat") whenReady(() => zoneEl(p)?.querySelector<HTMLElement>(".composer textarea") ?? null, (el) => focusEl(el));
    else whenReady(() => zoneEl(p)?.querySelector<HTMLElement>(".xterm textarea, .tnone button") ?? null, (el) => focusEl(el));
    return true;
  },
  "view.full": (env) => {
    const ws = env.board.ws;
    if (!ws) return false;
    if (ws.ui.full) return void env.board.actions.setFullscreen("");
    const p = panelOf(focusedZone(), env.lastPanel(), !!env.board.selected);
    if (p === "card" && !env.board.selected) return false;
    void env.board.actions.setFullscreen(p);
    focusZone(p);
    return true;
  },
  "view.dashboard": (env) => {
    const ws = env.board.ws;
    if (!ws) return false;
    const next = ws.ui.main_view === "dashboard" ? "board" : "dashboard";
    void env.board.actions.setMainView(next);
    focusZone(next, env.board.selected?.id);
    return true;
  },
  "terminal.leave": (env) => {
    const el = document.activeElement;
    const zone = el instanceof Element ? el.closest<HTMLElement>("[data-zone]") : null;
    if (!el || !(el instanceof Element) || !el.closest(".xterm, .xterm-host")) return false;
    if (zone) return focusEl(zone);
    (el as HTMLElement).blur();
    return env.board.ws !== null;
  },
  "board.down": nav("down"),
  "board.up": nav("up"),
  "board.right": nav("right"),
  "board.left": nav("left"),
  "board.open": (env) => {
    if (onBoardButton()) return false;
    const c = targetCard(env);
    if (!c) return false;
    env.board.actions.open(c.id);
    env.setTab("conv");
    if (env.board.ws?.ui.full) void env.board.actions.setFullscreen("");
    whenReady(() => cardPanel(c.id)?.querySelector<HTMLElement>(env.cardView === "terminal" ? ".xterm textarea, .tnone button" : ".composer textarea") ?? null, (el) => focusEl(el));
    return true;
  },
  "board.peek": (env) => {
    if (onBoardButton()) return false;
    const c = targetCard(env);
    if (!c) return false;
    env.board.actions.open(c.id);
    return true;
  },
  "board.new": (env) => {
    const c = targetCard(env);
    const col = document.activeElement instanceof HTMLElement ? document.activeElement.closest<HTMLElement>("[data-col-id]")?.dataset.colId : undefined;
    env.newSessionIn(col ?? c?.column_id);
  },
  "board.filter": () => focusEl(document.querySelector<HTMLElement>('[data-zone="board"] input[aria-label="Search cards"]')),
  "board.search": (env) => {
    if (env.board.ws?.ui.main_view === "dashboard") void env.board.actions.setMainView("board");
    whenReady(() => document.querySelector<HTMLElement>('[data-zone="board"] input[aria-label="Search cards"]'), (el) => focusEl(el));
    return true;
  },
  "board.goto": (env, key) => {
    if (key === undefined) return env.board.ws?.ui.main_view !== "dashboard";
    const lanes = readLanes();
    const cur = targetCard(env)?.column_id ?? null;
    const lane = gotoLane(lanes, key, cur);
    if (!lane) return true;
    if (lane.cards.length) return selectAndFocus(env, lane.cards[0]);
    focusEl(document.querySelector<HTMLElement>(`[data-col-id="${attrEsc(lane.id)}"] :is(.cname, .rail)`));
    return true;
  },
  "column.collapse": (env) => {
    const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const id = active?.closest<HTMLElement>("[data-col-id]")?.dataset.colId ?? targetCard(env)?.column_id;
    const col = env.board.columns.find((c) => c.id === id);
    if (!col) return false;
    void env.board.actions.collapseColumn(col.id, !col.fold);
    if (col.fold) {
      // Expanded: back to the column's first card, else its name.
      const sel = `[data-col-id="${attrEsc(col.id)}"]:not([data-folded])`;
      whenReady(() => document.querySelector<HTMLElement>(`${sel} [data-card-id]`) ?? document.querySelector<HTMLElement>(`${sel} .cname`), (el) => {
        const cid = el.dataset.cardId;
        if (cid) env.board.actions.select(cid);
        focusEl(el);
      });
    } else {
      whenReady(() => railEl(col.id), (el) => focusEl(el));
    }
    return true;
  },
  "column.expandAll": (env) => {
    const folded = env.board.columns.filter((c) => c.fold);
    for (const c of folded) void env.board.actions.collapseColumn(c.id, false);
    return true;
  },
  "card.moveRight": moveCard("right"),
  "card.moveLeft": moveCard("left"),
  "card.moveDown": moveCard("down"),
  "card.moveUp": moveCard("up"),
  "card.start": cardSession("start"),
  "card.stop": cardSession("stop"),
  "card.restart": cardSession("restart"),
  "card.edit": (env) => {
    const c = targetCard(env);
    if (!c) return false;
    env.board.actions.open(c.id);
    whenReady(() => cardPanel(c.id), () => requestUI({ do: "card.edit" }));
    return true;
  },
  "card.model": (env) => {
    const c = targetCard(env);
    if (!c) return false;
    env.board.actions.open(c.id);
    whenReady(() => cardPanel(c.id), () => requestUI({ do: "card.model" }));
    return true;
  },
  "card.archive": (env) => {
    const c = targetCard(env);
    if (!c) return false;
    const lanes = readLanes();
    const after = step(lanes, c.id, "down") === c.id ? step(lanes, c.id, "up") : step(lanes, c.id, "down");
    env.confirm(`Archive ${c.nick}? It leaves the board; its history is kept.`, "Archive", () => {
      void env.board.actions.archiveCard(c.id).then((out) => {
        if (out && after && after !== c.id) selectAndFocus(env, after);
      });
    });
    return true;
  },
  "card.delete": (env) => {
    const c = targetCard(env);
    if (!c) return false;
    env.confirm(`Delete ${c.nick}? Its sessions, questions and links go with it. This cannot be undone.`, "Delete card", () => void env.board.actions.deleteCard(c.id));
    return true;
  },
  "card.resume": (env) => {
    const c = targetCard(env);
    if (!c) return false;
    void env.board.actions.run(() => api.session_start({ card: c.id }));
    return true;
  },
  "column.new": (env) => void env.board.actions.addColumn(),
  "planner.start": (env) => (env.board.ws ? void env.board.actions.run(() => api.planner_start({ workspace_id: env.board.ws!.id })) : false),
  "planner.restart": (env) => {
    const ws = env.board.ws;
    if (!ws) return false;
    env.confirm("Restart the planner? It keeps its conversation.", "Restart planner", () => void env.board.actions.run(() => api.planner_restart({ workspace_id: ws.id })));
    return true;
  },
  "planner.adopt": (env) => {
    if (env.board.ws?.ui.collapsed?.planner) void env.board.actions.setCollapsed("planner", false);
    whenReady(() => zoneEl("planner"), () => requestUI({ do: "planner.adopt" }));
    return true;
  },
  "planner.model": (env) => {
    if (env.board.ws?.ui.collapsed?.planner) void env.board.actions.setCollapsed("planner", false);
    whenReady(() => zoneEl("planner"), () => requestUI({ do: "planner.model" }));
    return true;
  },
  // Enter on a focused message acts on it; otherwise (and always for i) it goes to the composer.
  "panel.act": (env) => {
    const el = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const msg = el?.closest<HTMLElement>("[data-msg]");
    if (msg && el === msg && actOnMessage(msg)) return true;
    // Enter on a button, link or summary (in a message or the panel's header) is that control's own.
    if (el && el !== msg && el.matches("button, a[href], summary")) return false;
    return focusComposer(panelZone(env));
  },
  "panel.compose": (env) => {
    const el = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    // Enter on a button or a link inside a message is that control's own.
    if (el?.closest("[data-msg]") && el.matches("button, a[href], summary")) return false;
    return focusComposer(panelZone(env));
  },
  "panel.nextMessage": (env) => stepMessage(env, 1),
  "panel.prevMessage": (env) => stepMessage(env, -1),
  ...Object.fromEntries(
    (["conv", "links", "ctx"] as const).map((t, i) => [
      `panel.tab.${i + 1}`,
      ((env) => {
        if (!env.board.selected) return false;
        env.setCardView("chat");
        env.setTab(t);
        if (env.board.ws?.ui.collapsed?.card) void env.board.actions.setCollapsed("card", false);
        focusZone("card");
        return true;
      }) as Handler,
    ]),
  ),
  ...Object.fromEntries([1, 2, 3, 4, 5, 6, 7, 8, 9].map((n) => [`question.pick.${n}`, ((env) => pickOption(env, n)) as Handler])),
  "question.notes": (env) => focusEl(openQuestion(panelZone(env))?.querySelector<HTMLElement>("textarea")),
  "question.answer": (env) => {
    const el = document.activeElement;
    const q = (el instanceof Element && el.closest<HTMLElement>(".qbox")) || openQuestion(panelZone(env));
    // In a text field (the composer), ⌘↩ answers only the question the field belongs to.
    if (!q || (el instanceof HTMLElement && (el.tagName === "TEXTAREA" || el.tagName === "INPUT") && !q.contains(el))) return false;
    const btn = q.querySelector<HTMLButtonElement>(".btn.pri");
    if (!btn || btn.disabled) return true;
    btn.click();
    return true;
  },
  "approval.approve": (env) => answerApproval(env, true),
  "approval.reject": (env) => answerApproval(env, false),
  "dash.next": () => stepBlock(1),
  "dash.prev": () => stepBlock(-1),
  "dash.actions": () => {
    const b = focusedBlock();
    if (!b || b !== document.activeElement) return false;
    return focusEl(b.querySelector<HTMLElement>("button:not([disabled]), a[href]"));
  },
  "dash.moveUp": () => {
    const b = focusedBlock();
    b?.querySelector<HTMLButtonElement>('[aria-label="Move earlier"]:not([disabled])')?.click();
    if (b) whenReady(() => document.querySelector<HTMLElement>(`[data-block="${attrEsc(b.dataset.block ?? "")}"]`), (el) => focusEl(el));
    return !!b;
  },
  "dash.moveDown": () => {
    const b = focusedBlock();
    b?.querySelector<HTMLButtonElement>('[aria-label="Move later"]:not([disabled])')?.click();
    if (b) whenReady(() => document.querySelector<HTMLElement>(`[data-block="${attrEsc(b.dataset.block ?? "")}"]`), (el) => focusEl(el));
    return !!b;
  },
  "dash.add": () => {
    const btn = document.querySelector<HTMLButtonElement>('[data-zone="dashboard"] [data-dash-add]');
    if (!btn) return false;
    if (btn.getAttribute("aria-expanded") !== "true") btn.click();
    whenReady(() => document.querySelector<HTMLElement>('[data-zone="dashboard"] [data-dash-form] input, [data-zone="dashboard"] [data-dash-form] select'), (el) => focusEl(el));
    return true;
  },
  "dash.remove": (env) => {
    const b = focusedBlock();
    const btn = b?.querySelector<HTMLButtonElement>('[aria-label^="Remove "]');
    if (!b || !btn) return false;
    env.confirm(`Remove ${b.getAttribute("aria-label") ?? "this block"} from the dashboard?`, "Remove block", () => btn.click());
    return true;
  },
};

/** The ids that have a handler; keys.test checks every registry command has one. */
export const HANDLED = new Set(Object.keys(HANDLERS));

/**
 * Runs a command. Returns false when it did nothing (no card selected, no open question), so a key
 * press falls through to the page.
 */
export function runCommand(env: Env, id: string, arg?: string): boolean | void {
  const h = HANDLERS[id];
  if (!h || !byId.has(id)) return false;
  return h(env, arg);
}
