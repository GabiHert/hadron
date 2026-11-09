import { vi } from "vitest";
import {
  DEFAULTS, Dispatcher, allowed, byId, candidates, checkChord, cheatSheet, chordOf, compact, conflictsOf, escapesTerminal, formatChord, fuzzyScore,
  keysOf, normalizeChord, parseOverrides, rebind, resetBinding, scopesOverlap,
} from "./keys";
import type { KeyCtx } from "./keys";

const ctx = (over: Partial<KeyCtx> = {}): KeyCtx => ({ zone: "board", input: false, terminal: false, modal: false, mainView: "board", ...over });

describe("chords", () => {
  it("normalizes spellings to one canonical order", () => {
    expect(normalizeChord("cmd+shift+p")).toBe("Shift+Mod+P");
    expect(normalizeChord("Mod+Alt+left")).toBe("Alt+Mod+ArrowLeft");
    expect(normalizeChord("shift+/")).toBe("/");
    expect(normalizeChord("?")).toBe("?");
    expect(normalizeChord("esc")).toBe("Escape");
    expect(normalizeChord("Mod+Mod+K")).toBe("");
    expect(normalizeChord("Shift")).toBe("");
    expect(normalizeChord("")).toBe("");
  });

  it("reads a key event, using the physical key when Option changes the character", () => {
    expect(chordOf({ key: "k", metaKey: true })).toBe("Mod+K");
    expect(chordOf({ key: "P", metaKey: true, shiftKey: true })).toBe("Shift+Mod+P");
    expect(chordOf({ key: "?", shiftKey: true, code: "Slash" })).toBe("?");
    expect(chordOf({ key: "˙", altKey: true, code: "KeyH" })).toBe("Alt+H");
    expect(chordOf({ key: "¡", altKey: true, metaKey: true, code: "Digit1" })).toBe("Alt+Mod+1");
    expect(chordOf({ key: " " })).toBe("Space");
    expect(chordOf({ key: "Tab", ctrlKey: true, shiftKey: true })).toBe("Ctrl+Shift+Tab");
    expect(chordOf({ key: "Shift", shiftKey: true })).toBeNull();
    expect(chordOf({ key: "Dead" })).toBeNull();
  });

  it("formats chords the macOS way", () => {
    expect(formatChord("Shift+Mod+P")).toBe("⇧⌘P");
    expect(formatChord("Alt+Mod+ArrowLeft")).toBe("⌥⌘←");
    expect(formatChord("J")).toBe("j");
    expect(formatChord("Shift+H")).toBe("⇧H");
    expect(formatChord("Mod+Escape")).toBe("⌘Esc");
  });

  it("every default chord is already canonical, and every id is unique", () => {
    const ids = new Set<string>();
    for (const d of DEFAULTS) {
      expect(ids.has(d.id)).toBe(false);
      ids.add(d.id);
      for (const k of d.keys) expect(normalizeChord(k)).toBe(k);
    }
  });

  it("no two default commands share a chord where both could fire", () => {
    for (const d of DEFAULTS) for (const k of d.keys) expect(conflictsOf(d.id, k).map((c) => c.id), `${d.id} ${k}`).toEqual([]);
  });
});

describe("scope rules", () => {
  const palette = byId.get("palette.toggle")!;
  const down = byId.get("board.down")!;
  const compose = byId.get("panel.compose")!;
  const leave = byId.get("terminal.leave")!;
  const dash = byId.get("view.dashboard")!;

  it("a text field keeps single keys but lets ⌘ chords through", () => {
    expect(allowed(down, "J", ctx({ input: true }))).toBe(false);
    expect(allowed(palette, "Mod+K", ctx({ input: true }))).toBe(true);
    expect(allowed(byId.get("help.keys")!, "?", ctx({ input: true }))).toBe(false);
    expect(allowed(byId.get("help.keys")!, "Mod+/", ctx({ input: true }))).toBe(true);
  });

  it("a terminal keeps everything but the reserved ⌘ chords", () => {
    const t = ctx({ terminal: true, zone: "card" });
    expect(allowed(down, "J", t)).toBe(false);
    expect(allowed(compose, "Enter", t)).toBe(false);
    expect(allowed(byId.get("zone.next")!, "Ctrl+Tab", t)).toBe(false);
    expect(allowed(byId.get("zone.next")!, "Mod+]", t)).toBe(true);
    expect(allowed(leave, "Mod+Escape", t)).toBe(true);
    expect(allowed(dash, "Mod+D", t)).toBe(true);
    expect(allowed(byId.get("question.answer")!, "Mod+Enter", t)).toBe(false);
  });

  it("the escape chord leaves a terminal; Esc, Ctrl+C and ⌘← stay the program's", () => {
    expect(escapesTerminal({ key: "Escape", metaKey: true })).toBe(true);
    expect(escapesTerminal({ key: "ArrowUp", metaKey: true, shiftKey: true })).toBe(true);
    expect(escapesTerminal({ key: "Escape" })).toBe(false);
    expect(escapesTerminal({ key: "c", ctrlKey: true })).toBe(false);
    expect(escapesTerminal({ key: "ArrowLeft", metaKey: true })).toBe(false);
    expect(escapesTerminal({ key: "1", metaKey: true, code: "Digit1" })).toBe(true);
    // A rebinding moves the escape chord too.
    expect(escapesTerminal({ key: "Escape", metaKey: true }, { "terminal.leave": ["Mod+;"] })).toBe(false);
    expect(escapesTerminal({ key: ";", metaKey: true }, { "terminal.leave": ["Mod+;"] })).toBe(true);
  });

  it("board keys need the board, panel keys a panel, and a dialog only lets its own through", () => {
    expect(allowed(down, "J", ctx({ zone: "card" }))).toBe(false);
    expect(allowed(down, "J", ctx({ zone: null }))).toBe(true);
    expect(allowed(down, "J", ctx({ zone: null, mainView: "dashboard" }))).toBe(false);
    expect(allowed(compose, "I", ctx({ zone: "planner" }))).toBe(true);
    expect(allowed(compose, "I", ctx({ zone: "board" }))).toBe(false);
    expect(allowed(dash, "Mod+D", ctx({ modal: true }))).toBe(false);
    expect(allowed(palette, "Mod+K", ctx({ modal: true }))).toBe(true);
  });

  it("the same key means different things by zone", () => {
    expect(candidates("J", ctx({ zone: "board" })).map((d) => d.id)).toEqual(["board.down"]);
    expect(candidates("J", ctx({ zone: "card" })).map((d) => d.id)).toEqual(["panel.nextMessage"]);
    expect(candidates("J", ctx({ zone: "dashboard" })).map((d) => d.id)).toEqual(["dash.next"]);
    expect(candidates("N", ctx({ zone: "board" })).map((d) => d.id)).toEqual(["board.new"]);
    expect(candidates("N", ctx({ zone: "planner" })).map((d) => d.id)).toEqual(["question.notes"]);
  });
});

describe("the dispatcher", () => {
  const make = (c: Partial<KeyCtx> = {}, result: boolean | void = undefined) => {
    const run = vi.fn((_id: string, _arg?: string) => result);
    let t = 0;
    const d = new Dispatcher(() => ({}), () => ctx(c), run, () => t);
    return { d, run, tick: (ms: number) => void (t += ms) };
  };

  it("runs the command for a chord and reports that it used the key", () => {
    const { d, run } = make();
    expect(d.handle({ key: "j" })).toBe(true);
    expect(run).toHaveBeenCalledWith("board.down");
  });

  it("ignores a key the page already handled, and an input method composing", () => {
    const { d, run } = make();
    expect(d.handle({ key: "j", defaultPrevented: true })).toBe(false);
    expect(d.handle({ key: "j", isComposing: true })).toBe(false);
    expect(run).not.toHaveBeenCalled();
  });

  it("lets a key through when its handler did nothing", () => {
    const { d } = make({}, false);
    expect(d.handle({ key: "j" })).toBe(false);
  });

  it("g waits for a column key and passes it on; Esc cancels; the wait times out", () => {
    const { d, run, tick } = make();
    expect(d.handle({ key: "g" })).toBe(true);
    expect(d.handle({ key: "2" })).toBe(true);
    expect(run).toHaveBeenLastCalledWith("board.goto", "2");
    d.handle({ key: "g" });
    d.handle({ key: "Escape" });
    expect(run).toHaveBeenCalledTimes(3); // g, 2, g
    d.handle({ key: "g" });
    tick(5000);
    d.handle({ key: "j" });
    expect(run).toHaveBeenLastCalledWith("board.down");
  });
});

describe("rebinding", () => {
  it("binds a free chord, keeping only what differs from the defaults", () => {
    const o = rebind({}, "view.dashboard", "Shift+Mod+D");
    expect(o).toEqual({ "view.dashboard": ["Shift+Mod+D"] });
    expect(keysOf(byId.get("view.dashboard")!, o)).toEqual(["Shift+Mod+D"]);
    expect(resetBinding(o, "view.dashboard")).toEqual({});
    expect(compact({ "view.dashboard": ["Mod+D"] })).toEqual({});
  });

  it("detects a conflict in an overlapping scope, and can take the chord over", () => {
    const check = checkChord("view.terminal", "Mod+D");
    expect(check.ok).toBe(false);
    if (!check.ok) {
      expect(check.reason).toMatch(/⌘D already runs “Switch Board ⇄ Dashboard”/);
      expect(check.conflicts.map((c) => c.id)).toEqual(["view.dashboard"]);
    }
    const o = rebind({}, "view.terminal", "Mod+D", true);
    expect(o["view.terminal"]).toEqual(["Mod+D"]);
    expect(o["view.dashboard"]).toEqual([]);
    expect(checkChord("view.dashboard", "Mod+D", o).ok).toBe(false);
  });

  it("allows the same key in scopes that never meet, and refuses macOS's own chords", () => {
    expect(scopesOverlap("board", "panel")).toBe(false);
    expect(checkChord("board.peek", "N").ok).toBe(false); // board.new has N on the board
    expect(checkChord("dash.add", "S").ok).toBe(true); // S is a board key; the dashboard never sees it
    const q = checkChord("view.dashboard", "Mod+Q");
    expect(q.ok).toBe(false);
    if (!q.ok) expect(q.reason).toMatch(/macOS's: Quit Hadron/);
  });

  it("reads the setting defensively", () => {
    expect(parseOverrides({ "view.dashboard": "cmd+shift+d", "nope.x": ["Mod+1"], "card.start": [], bad: 3 })).toEqual({ "view.dashboard": ["Shift+Mod+D"], "card.start": [] });
    expect(parseOverrides("x")).toEqual({});
    expect(parseOverrides(null)).toEqual({});
  });
});

describe("the cheat sheet", () => {
  it("is generated from the registry: every bound command appears once, families folded", () => {
    const sheet = cheatSheet();
    const ids = sheet.flatMap((g) => g.rows.flatMap((r) => r.ids));
    for (const d of DEFAULTS.filter((x) => x.keys.length)) expect(ids).toContain(d.id);
    expect(new Set(ids).size).toBe(ids.length);
    const zones = sheet.find((g) => g.group === "Zones and panels")!;
    expect(zones.rows.find((r) => r.title === "Switch to workspace 1–9")?.keys).toEqual(["⌘1–⌘9"]);
  });

  it("follows the user's bindings", () => {
    const sheet = cheatSheet({ "view.dashboard": ["Shift+Mod+D"], "help.keys": [] });
    const rows = sheet.flatMap((g) => g.rows);
    expect(rows.find((r) => r.ids.includes("view.dashboard"))?.keys).toEqual(["⇧⌘D"]);
    expect(rows.find((r) => r.ids.includes("help.keys"))).toBeUndefined();
  });
});

describe("fuzzy search", () => {
  it("prefers a prefix, then a word, then scattered letters", () => {
    expect(fuzzyScore("New session", "new s")).toBeGreaterThan(fuzzyScore("Open new-session", "new s"));
    expect(fuzzyScore("Switch Board ⇄ Dashboard", "dash")).toBeGreaterThan(fuzzyScore("Switch Board ⇄ Dashboard", "sbd"));
    expect(fuzzyScore("Switch Board ⇄ Dashboard", "sbd")).toBeGreaterThan(0);
    expect(fuzzyScore("Settings", "xyz")).toBe(-1);
  });
});
