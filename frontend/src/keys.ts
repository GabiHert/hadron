// The keyboard: one registry of commands with their default chords, the rules that decide which
// command a key press runs (scopes), the user's rebindings, and the text the cheat sheet, the
// palette, tooltips and Settings → Keyboard show. Handlers live in commands.ts; this file has no
// React and no DOM besides reading a key event's target.
//
// A chord is modifiers then a key, joined by "+": "Mod+K", "Alt+ArrowRight", "Shift+H", "?".
// Mod is ⌘. Modifiers are written in the macOS order ⌃⌥⇧⌘ (Ctrl, Alt, Shift, Mod). A letter is
// upper case; a punctuation key is its character, and Shift is implied by it ("?" not "Shift+/").
//
// Each command is one line of DEFAULTS. internal/app/menu_test.go reads this file to check that the
// native menus carry the same default chords, so keep every entry on a single line.

export type Scope = "global" | "board" | "panel" | "dashboard" | "terminal" | "dialog";
export type Zone = "workspaces" | "planner" | "board" | "card" | "dashboard";
export type Group = "General" | "Zones and panels" | "Board" | "Card" | "Conversation" | "Questions" | "Dashboard" | "Terminal";
export const GROUPS: Group[] = ["General", "Zones and panels", "Board", "Card", "Conversation", "Questions", "Dashboard", "Terminal"];

export interface CommandDef {
  id: string;
  title: string;
  group: Group;
  scope: Scope;
  /** default chords; the user's bindings replace them */
  keys: string[];
  /** fires from a focused terminal (only its ⌘ chords do) */
  terminal?: boolean;
  /** fires while a dialog is open */
  dialog?: boolean;
  /** fires while a text field has focus; by default only chords with ⌘ or ⌃ do */
  input?: boolean;
  /** rows the cheat sheet folds into one, with this title */
  family?: string;
  /** the key waits for one more key, which is passed to the handler */
  prefix?: boolean;
  /** the native menu the command also appears in */
  menu?: "View" | "Go" | "Card";
  /** a destructive command asks before it runs */
  confirm?: boolean;
}

const ws = (n: number): CommandDef => ({ id: `workspace.${n}`, title: `Switch to workspace ${n}`, group: "Zones and panels", scope: "global", keys: [`Mod+${n}`], terminal: true, family: "Switch to workspace 1–9", menu: "Go" });
const pick = (n: number): CommandDef => ({ id: `question.pick.${n}`, title: `Pick option ${n} of the open question`, group: "Questions", scope: "panel", keys: [String(n)], family: "Pick an option of the open question" });

export const DEFAULTS: CommandDef[] = [
  { id: "palette.toggle", title: "Command palette", group: "General", scope: "global", keys: ["Mod+K"], terminal: true, dialog: true, menu: "View" },
  { id: "help.keys", title: "Keyboard shortcuts", group: "General", scope: "global", keys: ["?", "Mod+/"], terminal: true, menu: "View" },
  { id: "card.new", title: "New session", group: "General", scope: "global", keys: ["Mod+N"], terminal: true, menu: "Card" },
  { id: "workspace.new", title: "New workspace", group: "General", scope: "global", keys: [] },
  { id: "settings.open", title: "Settings", group: "General", scope: "global", keys: ["Mod+,"], terminal: true, menu: "Go" },
  { id: "theme.toggle", title: "Switch light or dark", group: "General", scope: "global", keys: [], menu: "View" },
  { id: "feedback.open", title: "Send feedback", group: "General", scope: "global", keys: [] },
  { id: "whatsnew.open", title: "What's new", group: "General", scope: "global", keys: [] },
  { id: "zoom.in", title: "Zoom in", group: "General", scope: "global", keys: ["Mod+=", "Mod++"], terminal: true, dialog: true, menu: "View" },
  { id: "zoom.out", title: "Zoom out", group: "General", scope: "global", keys: ["Mod+-"], terminal: true, dialog: true, menu: "View" },
  { id: "zoom.reset", title: "Actual size", group: "General", scope: "global", keys: ["Mod+0"], terminal: true, dialog: true, menu: "View" },
  { id: "escape", title: "Leave the text field, or full screen", group: "Zones and panels", scope: "global", keys: ["Escape"], input: true },
  { id: "zone.next", title: "Next zone", group: "Zones and panels", scope: "global", keys: ["Mod+]", "Ctrl+Tab"], terminal: true, menu: "Go" },
  { id: "zone.prev", title: "Previous zone", group: "Zones and panels", scope: "global", keys: ["Mod+[", "Ctrl+Shift+Tab"], terminal: true, menu: "Go" },
  { id: "focus.planner", title: "Focus the planner chat", group: "Zones and panels", scope: "global", keys: ["Shift+Mod+P"], terminal: true, menu: "Go" },
  { id: "focus.board", title: "Focus the board", group: "Zones and panels", scope: "global", keys: ["Shift+Mod+B"], terminal: true, menu: "Go" },
  { id: "focus.card", title: "Focus the card panel", group: "Zones and panels", scope: "global", keys: ["Shift+Mod+C"], terminal: true, menu: "Go" },
  { id: "focus.workspaces", title: "Focus the workspace bar", group: "Zones and panels", scope: "global", keys: ["Shift+Mod+W"], terminal: true, menu: "Go" },
  ...[1, 2, 3, 4, 5, 6, 7, 8, 9].map(ws),
  { id: "panel.planner.toggle", title: "Collapse or expand the planner", group: "Zones and panels", scope: "global", keys: ["Alt+Mod+ArrowLeft"], terminal: true, menu: "View" },
  { id: "panel.card.toggle", title: "Collapse or expand the card panel", group: "Zones and panels", scope: "global", keys: ["Alt+Mod+ArrowRight"], terminal: true, menu: "View" },
  { id: "view.terminal", title: "Switch Chat ⇄ Terminal in the focused panel", group: "Zones and panels", scope: "global", keys: ["Mod+J"], terminal: true, menu: "View" },
  { id: "view.full", title: "Full screen for the focused panel", group: "Zones and panels", scope: "global", keys: ["Shift+Mod+F"], terminal: true, menu: "View" },
  { id: "view.dashboard", title: "Switch Board ⇄ Dashboard", group: "Zones and panels", scope: "global", keys: ["Mod+D"], terminal: true, menu: "View" },
  { id: "terminal.leave", title: "Leave the terminal (back to its panel)", group: "Terminal", scope: "terminal", keys: ["Mod+Escape", "Shift+Mod+ArrowUp"], terminal: true, menu: "Go" },
  { id: "board.down", title: "Next card", group: "Board", scope: "board", keys: ["ArrowDown", "J"] },
  { id: "board.up", title: "Previous card", group: "Board", scope: "board", keys: ["ArrowUp", "K"] },
  { id: "board.right", title: "Next column", group: "Board", scope: "board", keys: ["ArrowRight", "L"] },
  { id: "board.left", title: "Previous column", group: "Board", scope: "board", keys: ["ArrowLeft", "H"] },
  { id: "board.open", title: "Open the card and write to it", group: "Board", scope: "board", keys: ["Enter"] },
  { id: "board.peek", title: "Quick peek", group: "Board", scope: "board", keys: ["Space"] },
  { id: "board.new", title: "New card here", group: "Board", scope: "board", keys: ["N"] },
  { id: "board.filter", title: "Filter the board", group: "Board", scope: "board", keys: ["/"] },
  { id: "board.search", title: "Search cards", group: "Board", scope: "global", keys: ["Mod+F"], terminal: true },
  { id: "board.goto", title: "Go to column (then its number or first letter)", group: "Board", scope: "board", keys: ["G"], prefix: true },
  { id: "card.moveRight", title: "Move the card to the next column", group: "Card", scope: "board", keys: ["Alt+ArrowRight", "Shift+L"] },
  { id: "card.moveLeft", title: "Move the card to the previous column", group: "Card", scope: "board", keys: ["Alt+ArrowLeft", "Shift+H"] },
  { id: "card.moveDown", title: "Move the card down", group: "Card", scope: "board", keys: ["Alt+ArrowDown", "Shift+J"] },
  { id: "card.moveUp", title: "Move the card up", group: "Card", scope: "board", keys: ["Alt+ArrowUp", "Shift+K"] },
  { id: "card.start", title: "Start the card's session", group: "Card", scope: "board", keys: ["S"], menu: "Card" },
  { id: "card.stop", title: "Stop the card's session", group: "Card", scope: "board", keys: ["X"], menu: "Card", confirm: true },
  { id: "card.restart", title: "Restart the card's session", group: "Card", scope: "board", keys: ["R"], menu: "Card", confirm: true },
  { id: "card.edit", title: "Edit the card", group: "Card", scope: "board", keys: ["E"], menu: "Card" },
  { id: "card.model", title: "Change the card's model", group: "Card", scope: "board", keys: ["M"], menu: "Card" },
  { id: "card.archive", title: "Archive the card", group: "Card", scope: "board", keys: ["Backspace", "Delete"], menu: "Card", confirm: true },
  { id: "card.delete", title: "Delete the card", group: "Card", scope: "board", keys: [], confirm: true },
  { id: "card.resume", title: "Resume the card's Claude conversation", group: "Card", scope: "board", keys: [] },
  { id: "column.new", title: "Add a column", group: "Board", scope: "board", keys: [] },
  { id: "column.collapse", title: "Collapse or expand the column", group: "Board", scope: "board", keys: ["Z"] },
  { id: "column.expandAll", title: "Expand every collapsed column", group: "Board", scope: "board", keys: [] },
  { id: "planner.start", title: "Start the planner", group: "Conversation", scope: "global", keys: [] },
  { id: "planner.restart", title: "Restart the planner", group: "Conversation", scope: "global", keys: [], confirm: true },
  { id: "planner.adopt", title: "Adopt a planner conversation", group: "Conversation", scope: "global", keys: [] },
  { id: "planner.model", title: "Change the planner's model", group: "Conversation", scope: "global", keys: [] },
  { id: "panel.compose", title: "Write a message", group: "Conversation", scope: "panel", keys: ["I"] },
  { id: "panel.act", title: "Act on the focused message (widget menu, link, question), else write one", group: "Conversation", scope: "panel", keys: ["Enter"] },
  { id: "panel.nextMessage", title: "Next message", group: "Conversation", scope: "panel", keys: ["J"] },
  { id: "panel.prevMessage", title: "Previous message", group: "Conversation", scope: "panel", keys: ["K"] },
  { id: "panel.tab.1", title: "Card tab: Conversation", group: "Conversation", scope: "global", keys: ["Alt+Mod+1"], terminal: true, family: "Card tabs: Conversation, Links, Context" },
  { id: "panel.tab.2", title: "Card tab: Links", group: "Conversation", scope: "global", keys: ["Alt+Mod+2"], terminal: true, family: "Card tabs: Conversation, Links, Context" },
  { id: "panel.tab.3", title: "Card tab: Context", group: "Conversation", scope: "global", keys: ["Alt+Mod+3"], terminal: true, family: "Card tabs: Conversation, Links, Context" },
  ...[1, 2, 3, 4, 5, 6, 7, 8, 9].map(pick),
  { id: "question.notes", title: "Write notes for the open question", group: "Questions", scope: "panel", keys: ["N"] },
  { id: "question.answer", title: "Answer the open question", group: "Questions", scope: "panel", keys: ["Mod+Enter"], input: true },
  { id: "approval.approve", title: "Approve the open approval", group: "Questions", scope: "panel", keys: ["Shift+Mod+A"], confirm: true },
  { id: "approval.reject", title: "Reject the open approval", group: "Questions", scope: "panel", keys: ["Shift+Mod+X"], confirm: true },
  { id: "dash.next", title: "Next block", group: "Dashboard", scope: "dashboard", keys: ["ArrowDown", "ArrowRight", "J"] },
  { id: "dash.prev", title: "Previous block", group: "Dashboard", scope: "dashboard", keys: ["ArrowUp", "ArrowLeft", "K"] },
  { id: "dash.actions", title: "The block's actions", group: "Dashboard", scope: "dashboard", keys: ["Enter"] },
  { id: "dash.moveUp", title: "Move the block earlier", group: "Dashboard", scope: "dashboard", keys: ["Alt+ArrowUp", "Shift+K"] },
  { id: "dash.moveDown", title: "Move the block later", group: "Dashboard", scope: "dashboard", keys: ["Alt+ArrowDown", "Shift+J"] },
  { id: "dash.add", title: "Add a block", group: "Dashboard", scope: "dashboard", keys: ["N"] },
  { id: "dash.remove", title: "Remove the block", group: "Dashboard", scope: "dashboard", keys: ["Backspace", "Delete"], confirm: true },
];

export const byId = new Map(DEFAULTS.map((d) => [d.id, d]));

/** The user's rebindings: command id → chords. A missing id keeps its defaults; [] unbinds it. */
export type Overrides = Record<string, string[]>;

/** The setting that holds Overrides. */
export const SETTING_KEYS = "keys.bindings";

// ---- chords ----

const MOD_ORDER = ["Ctrl", "Alt", "Shift", "Mod"] as const;
type ModName = (typeof MOD_ORDER)[number];
const MOD_ALIAS: Record<string, ModName> = { ctrl: "Ctrl", control: "Ctrl", alt: "Alt", option: "Alt", opt: "Alt", shift: "Shift", mod: "Mod", cmd: "Mod", meta: "Mod", command: "Mod", cmdorctrl: "Mod" };
const KEY_ALIAS: Record<string, string> = {
  esc: "Escape", escape: "Escape", enter: "Enter", return: "Enter", space: "Space", " ": "Space", tab: "Tab", backspace: "Backspace", delete: "Delete", del: "Delete",
  left: "ArrowLeft", right: "ArrowRight", up: "ArrowUp", down: "ArrowDown", arrowleft: "ArrowLeft", arrowright: "ArrowRight", arrowup: "ArrowUp", arrowdown: "ArrowDown",
  home: "Home", end: "End", pageup: "PageUp", pagedown: "PageDown",
};
const MODIFIER_KEYS = new Set(["Shift", "Meta", "Control", "Alt", "AltGraph", "CapsLock", "Fn", "OS", "Hyper", "Super"]);

/** True when the key is one character that is not a letter or a digit, so Shift is part of it. */
const isPunct = (key: string) => key.length === 1 && !/[A-Za-z0-9]/.test(key);

function normKey(k: string): string {
  if (k.length === 1) return /[a-z]/.test(k) ? k.toUpperCase() : k === " " ? "Space" : k;
  return KEY_ALIAS[k.toLowerCase()] ?? (k.length > 1 ? k.charAt(0).toUpperCase() + k.slice(1) : k);
}

function join(mods: Set<ModName>, key: string): string {
  if (isPunct(key)) mods.delete("Shift");
  return [...MOD_ORDER.filter((m) => mods.has(m)), key].join("+");
}

/** The canonical spelling of a chord ("cmd+shift+p" is "Shift+Mod+P"), or "" when it is not one. */
export function normalizeChord(s: string): string {
  const t = s.trim();
  if (!t || /\s/.test(t)) return "";
  const parts = t.endsWith("++") ? [...t.slice(0, -2).split("+").filter(Boolean), "+"] : t.split("+");
  const key = parts.pop() ?? "";
  if (!key) return "";
  const mods = new Set<ModName>();
  for (const p of parts) {
    const m = MOD_ALIAS[p.toLowerCase()];
    if (!m || mods.has(m)) return "";
    mods.add(m);
  }
  const k = normKey(key);
  if (MODIFIER_KEYS.has(k)) return "";
  return join(mods, k);
}

export interface KeyLike {
  key: string;
  code?: string;
  metaKey?: boolean;
  ctrlKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
}

/**
 * The chord a key press is, or null for a modifier on its own. Letters and digits come from the
 * physical key when Option (or ⌘ for digits) is held, because macOS turns Option+H into ˙.
 */
export function chordOf(e: KeyLike): string | null {
  const k = e.key ?? "";
  if (!k || MODIFIER_KEYS.has(k) || k === "Dead" || k === "Unidentified" || k === "Process") return null;
  const code = e.code ?? "";
  let key: string;
  if (/^Key[A-Z]$/.test(code) && (e.altKey || !/^[A-Za-z]$/.test(k))) key = code.slice(3);
  else if (/^Digit[0-9]$/.test(code) && (e.altKey || e.metaKey || !/^[0-9]$/.test(k))) key = code.slice(5);
  else key = normKey(k);
  const mods = new Set<ModName>();
  if (e.ctrlKey) mods.add("Ctrl");
  if (e.altKey) mods.add("Alt");
  if (e.shiftKey) mods.add("Shift");
  if (e.metaKey) mods.add("Mod");
  return join(mods, key);
}

const modsOf = (chord: string) => chord.split("+").slice(0, -1).filter((p) => p !== "");
/** True when the chord holds ⌘ (or ⌃): such chords fire in text fields. */
export const hasCommandMod = (chord: string) => modsOf(chord).some((m) => m === "Mod" || m === "Ctrl");

const SYMBOL: Record<string, string> = { Ctrl: "⌃", Alt: "⌥", Shift: "⇧", Mod: "⌘" };
const KEY_LABEL: Record<string, string> = {
  ArrowLeft: "←", ArrowRight: "→", ArrowUp: "↑", ArrowDown: "↓", Escape: "Esc", Enter: "↩", Backspace: "⌫", Delete: "⌦", Tab: "⇥", Space: "Space", PageUp: "PgUp", PageDown: "PgDn",
};

/** How a chord reads on screen: "⇧⌘P", "⌥→", "j", "?". */
export function formatChord(chord: string): string {
  if (!chord) return "";
  const parts = chord.split("+");
  const key = chord.endsWith("++") ? "+" : (parts.pop() ?? "");
  const mods = chord.endsWith("++") ? parts.slice(0, -2) : parts;
  const label = KEY_LABEL[key] ?? (mods.length ? key : key.toLowerCase());
  return mods.map((m) => SYMBOL[m] ?? m).join("") + label;
}

/** The words a screen reader says for a chord: "Shift Command P". */
export function speakChord(chord: string): string {
  const words: Record<string, string> = { Ctrl: "Control", Alt: "Option", Shift: "Shift", Mod: "Command" };
  return chord.split("+").map((p) => words[p] ?? p).join(" ");
}

// ---- bindings ----

/** A command's chords after the user's bindings. */
export function keysOf(def: CommandDef, overrides: Overrides = {}): string[] {
  const o = overrides[def.id];
  return Array.isArray(o) ? o.map(normalizeChord).filter(Boolean) : def.keys;
}

/** Reads the setting's value; anything that is not {id: [chords]} is ignored. */
export function parseOverrides(v: unknown): Overrides {
  const out: Overrides = {};
  if (!v || typeof v !== "object" || Array.isArray(v)) return out;
  for (const [id, val] of Object.entries(v as Record<string, unknown>)) {
    if (!byId.has(id)) continue;
    const list = typeof val === "string" ? [val] : Array.isArray(val) ? val.filter((x): x is string => typeof x === "string") : null;
    if (list) out[id] = list.map(normalizeChord).filter(Boolean);
  }
  return out;
}

/** The first chord of a command, as shown in a tooltip or a menu: "⌘K", or "" when it has none. */
export function chordLabel(id: string, overrides: Overrides = activeOverrides()): string {
  const d = byId.get(id);
  const k = d ? keysOf(d, overrides)[0] : "";
  return k ? formatChord(k) : "";
}

/** A tooltip with its shortcut: "Command palette (⌘K)". */
export function withChord(title: string, id: string, overrides: Overrides = activeOverrides()): string {
  const c = chordLabel(id, overrides);
  return c ? `${title} (${c})` : title;
}

// The bindings in force, for code that cannot reach React state (the terminal's key filter, tooltips).
let active: Overrides = {};
const listeners = new Set<() => void>();
export function setActiveOverrides(o: Overrides) {
  active = o;
  listeners.forEach((l) => l());
}
export const activeOverrides = () => active;
export function onOverridesChange(l: () => void): () => void {
  listeners.add(l);
  return () => void listeners.delete(l);
}

/**
 * True when a key press in a focused terminal belongs to the app: one of the reserved ⌘ chords
 * (zones, panels, workspaces, the palette, leaving the terminal). Everything else is the program's.
 */
export function escapesTerminal(e: KeyLike, overrides: Overrides = active): boolean {
  const c = chordOf(e);
  if (!c || !modsOf(c).includes("Mod")) return false;
  return DEFAULTS.some((d) => d.terminal && keysOf(d, overrides).includes(c));
}

// ---- scopes and the dispatcher ----

export interface KeyCtx {
  /** the zone that holds the focus, or null (nothing focused) */
  zone: Zone | null;
  /** a text field, a select or an editable element has the focus */
  input: boolean;
  /** a terminal has the focus */
  terminal: boolean;
  /** a dialog is open */
  modal: boolean;
  mainView: "board" | "dashboard";
}

/** True when the element takes typed text, so single keys are its own. */
export function isTextInput(el: Element | null): boolean {
  if (!el) return false;
  if ((el as HTMLElement).isContentEditable) return true;
  const tag = el.tagName;
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag !== "INPUT") return false;
  const type = ((el as HTMLInputElement).type || "text").toLowerCase();
  return !["button", "checkbox", "radio", "submit", "reset", "range", "color", "file", "image"].includes(type);
}

/** The context of a key event, from where it happened. */
export function contextOf(target: EventTarget | null, modal: boolean, mainView: "board" | "dashboard"): KeyCtx {
  const el = target instanceof Element ? target : null;
  const zone = (el?.closest("[data-zone]")?.getAttribute("data-zone") as Zone | null) ?? null;
  return { zone, input: isTextInput(el), terminal: !!el?.closest(".xterm, .xterm-host"), modal, mainView };
}

/** Whether a command may fire for this chord in this context. */
export function allowed(def: CommandDef, chord: string, ctx: KeyCtx): boolean {
  if (ctx.modal) return !!def.dialog;
  if (ctx.terminal) return !!def.terminal && modsOf(chord).includes("Mod");
  if (ctx.input && !(def.input ?? hasCommandMod(chord))) return false;
  switch (def.scope) {
    case "global":
    case "terminal":
      return true;
    case "board":
      return ctx.zone === "board" || (ctx.zone === null && ctx.mainView === "board");
    case "dashboard":
      return ctx.zone === "dashboard" || (ctx.zone === null && ctx.mainView === "dashboard");
    case "panel":
      return ctx.zone === "planner" || ctx.zone === "card";
    case "dialog":
      return false;
  }
}

const SPECIFIC: Scope[] = ["board", "dashboard", "panel", "terminal", "dialog"];

/** The commands a chord could run in this context, the most specific scope first. */
export function candidates(chord: string, ctx: KeyCtx, overrides: Overrides = {}): CommandDef[] {
  const hits = DEFAULTS.filter((d) => keysOf(d, overrides).includes(chord) && allowed(d, chord, ctx));
  return hits.sort((a, b) => Number(SPECIFIC.includes(b.scope)) - Number(SPECIFIC.includes(a.scope)));
}

/** A handler returns false when it did nothing, so the key goes on to the next candidate (or the page). */
export type Run = (id: string, arg?: string) => boolean | void;

/**
 * Turns key presses into commands. A prefix command (g) waits for one more key and gets it as its
 * argument. handle() returns true when it used the key; the caller then prevents the default.
 */
export class Dispatcher {
  private pending: { id: string; until: number } | null = null;
  constructor(
    private overrides: () => Overrides,
    private ctxOf: (target: EventTarget | null) => KeyCtx,
    private run: Run,
    private now: () => number = () => Date.now(),
  ) {}

  /** True while a prefix waits for its second key. */
  get waiting(): string | null {
    return this.pending && this.pending.until > this.now() ? this.pending.id : null;
  }

  handle(e: KeyLike & { target?: EventTarget | null; defaultPrevented?: boolean; isComposing?: boolean; keyCode?: number }): boolean {
    if (e.defaultPrevented || e.isComposing || e.keyCode === 229) return false;
    const chord = chordOf(e);
    if (!chord) return false;
    const wait = this.waiting;
    this.pending = null;
    if (wait) {
      if (chord !== "Escape") this.run(wait, chord);
      return true;
    }
    const ctx = this.ctxOf(e.target ?? null);
    for (const d of candidates(chord, ctx, this.overrides())) {
      if (d.prefix) {
        if (this.run(d.id) === false) continue;
        this.pending = { id: d.id, until: this.now() + 2000 };
        return true;
      }
      if (this.run(d.id) !== false) return true;
    }
    return false;
  }
}

// ---- conflicts ----

/** Chords macOS or the Edit menu own; Hadron never takes them. */
export const RESERVED: Record<string, string> = {
  "Mod+Q": "Quit Hadron", "Mod+W": "Close the window", "Mod+H": "Hide Hadron", "Alt+Mod+H": "Hide others", "Mod+M": "Minimize",
  "Mod+C": "Copy", "Mod+V": "Paste", "Mod+X": "Cut", "Mod+A": "Select all", "Mod+Z": "Undo", "Shift+Mod+Z": "Redo", "Ctrl+Mod+F": "Full screen window",
  "Alt+Shift+Mod+V": "Paste and match style", "Mod+Tab": "Switch apps", "Mod+Space": "Spotlight", "Mod+`": "Cycle windows",
};

/** Two scopes can see the same key press. */
export function scopesOverlap(a: Scope, b: Scope): boolean {
  if (a === b) return true;
  if (a === "dialog" || b === "dialog") return false;
  return a === "global" || b === "global" || a === "terminal" || b === "terminal";
}

/** The other commands that already use the chord where this command would. */
export function conflictsOf(id: string, chord: string, overrides: Overrides = {}): CommandDef[] {
  const me = byId.get(id);
  if (!me) return [];
  return DEFAULTS.filter((d) => d.id !== id && keysOf(d, overrides).includes(chord) && (scopesOverlap(d.scope, me.scope) || (!!d.dialog && !!me.dialog)));
}

export type ChordCheck = { ok: true } | { ok: false; reason: string; conflicts: CommandDef[] };

/** Whether the chord can be bound to the command: not reserved, not taken in an overlapping scope. */
export function checkChord(id: string, chord: string, overrides: Overrides = {}): ChordCheck {
  const c = normalizeChord(chord);
  if (!c) return { ok: false, reason: "That is not a key Hadron can bind.", conflicts: [] };
  if (RESERVED[c]) return { ok: false, reason: `${formatChord(c)} is macOS's: ${RESERVED[c]}.`, conflicts: [] };
  const hits = conflictsOf(id, c, overrides);
  if (hits.length) return { ok: false, reason: `${formatChord(c)} already runs ${hits.map((h) => `“${h.title}”`).join(", ")}.`, conflicts: hits };
  return { ok: true };
}

/**
 * Binds a chord to a command (replacing its chords), and, when `steal` is set, takes the chord away
 * from the commands that had it. Returns the new overrides with defaults left out.
 */
export function rebind(overrides: Overrides, id: string, chord: string, steal = false): Overrides {
  const c = normalizeChord(chord);
  const next: Overrides = { ...overrides, [id]: [c] };
  if (steal) for (const d of conflictsOf(id, c, overrides)) next[d.id] = keysOf(d, overrides).filter((k) => k !== c);
  return compact(next);
}

/** Puts a command back to its default chords. */
export function resetBinding(overrides: Overrides, id: string): Overrides {
  const next = { ...overrides };
  delete next[id];
  return next;
}

/** Drops entries that equal the defaults, so the setting holds only what the user changed. */
export function compact(o: Overrides): Overrides {
  const out: Overrides = {};
  for (const [id, keys] of Object.entries(o)) {
    const d = byId.get(id);
    if (!d) continue;
    if (keys.join(" ") !== d.keys.join(" ")) out[id] = keys;
  }
  return out;
}

// ---- what the user reads ----

export interface CheatRow {
  title: string;
  keys: string[];
  ids: string[];
}

/** The cheat sheet: every command with a chord, by group, families folded into one row. */
export function cheatSheet(overrides: Overrides = {}): { group: Group; rows: CheatRow[] }[] {
  return GROUPS.map((group) => {
    const rows: CheatRow[] = [];
    const families = new Map<string, CheatRow>();
    for (const d of DEFAULTS.filter((x) => x.group === group)) {
      const keys = keysOf(d, overrides);
      if (!keys.length) continue;
      if (d.family) {
        const f = families.get(d.family);
        if (f) {
          f.ids.push(d.id);
          f.keys.push(...keys.map(formatChord));
          continue;
        }
        const row = { title: d.family, keys: keys.map(formatChord), ids: [d.id] };
        families.set(d.family, row);
        rows.push(row);
        continue;
      }
      rows.push({ title: d.title, keys: keys.map((k) => (d.prefix ? `${formatChord(k)} then …` : formatChord(k))), ids: [d.id] });
    }
    for (const f of families.values()) if (f.keys.length > 3) f.keys = [`${f.keys[0]}–${f.keys[f.keys.length - 1]}`];
    return { group, rows };
  }).filter((g) => g.rows.length);
}

/**
 * Fuzzy match: every character of the query in order. Returns a score (higher is better) or -1.
 * A whole-word prefix beats a substring, which beats scattered letters.
 */
export function fuzzyScore(text: string, query: string): number {
  const t = text.toLowerCase();
  const q = query.toLowerCase().trim();
  if (!q) return 0;
  if (t.startsWith(q)) return 1000 - t.length;
  const at = t.indexOf(q);
  if (at >= 0) return (t[at - 1] === " " || t[at - 1] === ":" ? 800 : 600) - at;
  let i = 0;
  let gaps = 0;
  let last = -1;
  for (const ch of q) {
    if (ch === " ") continue;
    const j = t.indexOf(ch, last + 1);
    if (j < 0) return -1;
    if (last >= 0) gaps += j - last - 1;
    last = j;
    i++;
  }
  return i ? 300 - gaps : -1;
}
