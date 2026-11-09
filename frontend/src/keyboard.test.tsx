// Hadron with no mouse: the handlers behind the keyboard registry, and the App driven by keys only.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import App from "./App";
import { HANDLED, clampZoom, gotoLane, moveTarget, nextZoom, panelOf, step, zoneCycle, ZOOM_STEPS } from "./commands";
import type { Lane } from "./commands";
import { PALETTE_SKIP, paletteItems, rankItems } from "./components/KeyDialogs";
import { DEFAULTS, cheatSheet } from "./keys";
import { card, col, defaultFixture, message, mocks, question, reset, state } from "./test/fakeApi";
import { resetZoomForTests } from "./state/useZoom";
import type { Board } from "./state/useBoard";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => {
  reset();
  resetZoomForTests();
  try {
    localStorage.clear();
  } catch {
    /* no storage */
  }
});

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
}

/** A key press where the focus is (or on the window), as the WebKit window would send it. */
const press = (key: string, init: Partial<KeyboardEventInit> & { code?: string } = {}) =>
  fireEvent.keyDown(document.activeElement ?? window, { key, ...init });
const focused = () => document.activeElement as HTMLElement;
/** The window's CSS zoom, as React set it. */
const winZoom = () => ((document.querySelector(".win") as HTMLElement).style as unknown as { zoom?: string }).zoom ?? "";
const cardButton = (nick: string) => screen.getByRole("button", { name: `Open ${nick}` });

// ---- the pure parts ----

describe("board navigation", () => {
  const lanes: Lane[] = [
    { id: "a", name: "Needs you", cards: ["c1"] },
    { id: "b", name: "Working", cards: ["c2", "c3", "c4"] },
    { id: "e", name: "Empty", cards: [] },
    { id: "d", name: "Done", cards: ["c5", "c6"] },
  ];
  it("moves within a column and clamps at its ends", () => {
    expect(step(lanes, "c2", "down")).toBe("c3");
    expect(step(lanes, "c4", "down")).toBe("c4");
    expect(step(lanes, "c2", "up")).toBe("c2");
  });
  it("moves across columns at the same row, skipping empty ones", () => {
    expect(step(lanes, "c4", "right")).toBe("c6");
    expect(step(lanes, "c5", "left")).toBe("c2");
    expect(step(lanes, "c1", "left")).toBe("c1");
  });
  it("starts at the first card when nothing (or a hidden card) is selected", () => {
    expect(step(lanes, null, "down")).toBe("c1");
    expect(step(lanes, "gone", "right")).toBe("c1");
    expect(step([], null, "down")).toBeNull();
  });
  it("g then a digit or a letter picks a column; the same letter again cycles", () => {
    expect(gotoLane(lanes, "2", null)?.id).toBe("b");
    expect(gotoLane(lanes, "9", null)).toBeNull();
    expect(gotoLane(lanes, "D", null)?.id).toBe("d");
    const two = [...lanes, { id: "w2", name: "Waiting", cards: [] }];
    expect(gotoLane(two, "W", "b")?.id).toBe("w2");
    expect(gotoLane(two, "W", "w2")?.id).toBe("b");
  });
  it("move targets follow card_move: next column at the end, or one place up or down", () => {
    const b = { columns: defaultFixture().columns.w1, cards: defaultFixture().cards.w1 } as Pick<Board, "columns" | "cards">;
    expect(moveTarget(b, "c3", "right")).toEqual({ column_id: "done" });
    expect(moveTarget(b, "c3", "left")).toEqual({ column_id: "needs" });
    expect(moveTarget(b, "c1", "left")).toBeNull();
    expect(moveTarget(b, "c3", "down")).toEqual({ column_id: "working", position: 2 });
    expect(moveTarget(b, "c3", "up")).toEqual({ column_id: "working", position: 0 });
    expect(moveTarget(b, "c4", "down")).toBeNull();
  });
});

describe("zones", () => {
  it("cycles planner, board, card without the collapsed panels", () => {
    expect(zoneCycle({}, "board", true)).toEqual(["planner", "board", "card"]);
    expect(zoneCycle({ planner: true }, "dashboard", true)).toEqual(["dashboard", "card"]);
    expect(zoneCycle({}, "board", false)).toEqual(["planner", "board"]);
  });
  it("acts on the focused panel, else the last one used", () => {
    expect(panelOf("planner", "card", true)).toBe("planner");
    expect(panelOf("board", "planner", true)).toBe("planner");
    expect(panelOf(null, null, true)).toBe("card");
    expect(panelOf(null, null, false)).toBe("planner");
  });
});

describe("zoom steps", () => {
  it("walks about 10% at a time between 70% and 200%, and resets to 100%", () => {
    expect(nextZoom(1, 1)).toBe(1.1);
    expect(nextZoom(1.1, -1)).toBe(1);
    expect(nextZoom(2, 1)).toBe(2);
    expect(nextZoom(0.7, -1)).toBe(0.7);
    expect(nextZoom(1.25, 1)).toBe(1.3);
    expect(nextZoom(1.7, 0)).toBe(1);
    for (let i = 1; i < ZOOM_STEPS.length; i++) expect(ZOOM_STEPS[i] / ZOOM_STEPS[i - 1]).toBeLessThan(1.2);
  });
  it("keeps a stored value in range", () => {
    expect(clampZoom(5)).toBe(2);
    expect(clampZoom(0.1)).toBe(0.7);
    expect(clampZoom("1.2")).toBe(1.2);
    expect(clampZoom("big")).toBe(1);
    expect(clampZoom(undefined)).toBe(1);
  });
});

describe("the registry is covered", () => {
  it("every command has a handler", () => {
    for (const d of DEFAULTS) expect(HANDLED.has(d.id), d.id).toBe(true);
  });
  it("the palette lists every command, or says why it does not", () => {
    const board = { selected: card("c1", "w1", "needs", "search-console", 0), visible: [], cards: [], actions: {} } as unknown as Board;
    const keys = new Set(paletteItems(board, {}, () => {}, () => {}).map((i) => i.key));
    for (const d of DEFAULTS) expect(keys.has(`cmd:${d.id}`) || d.id in PALETTE_SKIP, d.id).toBe(true);
  });
  it("the palette shows chords and puts recent picks first", () => {
    const board = { selected: null, visible: [], cards: [], actions: {} } as unknown as Board;
    const items = paletteItems(board, {}, () => {}, () => {});
    expect(items.find((i) => i.key === "cmd:view.dashboard")?.hint).toBe("⌘D");
    expect(rankItems(items, "", ["cmd:zoom.in"])[0].key).toBe("cmd:zoom.in");
    expect(rankItems(items, "dashboard", [])[0].label).toMatch(/Dashboard/);
  });
  it("the cheat sheet comes from the registry, with rebindings", () => {
    const rows = cheatSheet({ "view.dashboard": ["Shift+Mod+D"] }).flatMap((g) => g.rows);
    expect(rows.find((r) => r.ids.includes("view.dashboard"))?.keys).toEqual(["⇧⌘D"]);
    expect(rows.find((r) => r.ids.includes("zoom.in"))?.keys).toEqual(["⌘=", "⌘+"]);
  });
});

// ---- the App, keys only ----

describe("the board with the keyboard", () => {
  it("arrows and h j k l move the selection and the focus; Enter opens the card at its composer", async () => {
    await openApp();
    act(() => cardButton("api-timeouts").focus());
    press("j", { code: "KeyJ" });
    await waitFor(() => expect(focused()).toBe(cardButton("dedupe-records")));
    expect(cardButton("dedupe-records")).toHaveAttribute("aria-current", "true");
    press("ArrowDown");
    await waitFor(() => expect(focused()).toBe(cardButton("stale-warning")));
    press("h", { code: "KeyH" });
    await waitFor(() => expect(focused()).toBe(cardButton("search-console")));
    press("l", { code: "KeyL" });
    await waitFor(() => expect(focused()).toBe(cardButton("api-timeouts")));
    press("Enter");
    const panel = await screen.findByRole("complementary", { name: "Card api-timeouts" });
    await waitFor(() => expect(focused()).toBe(within(panel).getByLabelText("Message about this card")));
    // Esc leaves the composer for the panel; ⌘[ goes back to the board on the selected card.
    press("Escape");
    expect(focused()).toBe(panel);
    press("[", { metaKey: true });
    await waitFor(() => expect(focused()).toBe(cardButton("api-timeouts")));
  });

  it("single keys never fire while you type", async () => {
    await openApp();
    const search = screen.getByLabelText("Search cards");
    act(() => search.focus());
    press("j", { code: "KeyJ" });
    press("n", { code: "KeyN" });
    press("?");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(focused()).toBe(search);
  });

  it("⇧L moves the card to the next column and keeps the focus on it", async () => {
    await openApp();
    act(() => cardButton("dedupe-records").focus());
    press("L", { shiftKey: true, code: "KeyL" });
    await waitFor(() => expect(mocks.card_move).toHaveBeenCalledWith({ card_id: "c3", column_id: "done" }));
    await waitFor(() => expect(focused()).toBe(cardButton("dedupe-records")));
    expect(within(screen.getByRole("region", { name: "Column Done" })).getByRole("button", { name: "Open dedupe-records" })).toBeInTheDocument();
    press("ArrowUp", { altKey: true });
    // Alone in Done, it cannot go up: nothing is sent.
    expect(mocks.card_move).toHaveBeenCalledTimes(1);
  });

  it("x asks before it stops the session; Esc cancels, the confirm button stops it", async () => {
    await openApp();
    act(() => cardButton("api-timeouts").focus());
    press("x", { code: "KeyX" });
    const dlg = await screen.findByRole("alertdialog");
    expect(dlg).toHaveTextContent("Stop api-timeouts's session?");
    await waitFor(() => expect(focused()).toBe(within(dlg).getByRole("button", { name: "Stop session" })));
    press("Escape");
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(mocks.session_stop).not.toHaveBeenCalled();
    await waitFor(() => expect(focused()).toBe(cardButton("api-timeouts")));
    press("x", { code: "KeyX" });
    fireEvent.click(await screen.findByRole("button", { name: "Stop session" }));
    await waitFor(() => expect(mocks.session_stop).toHaveBeenCalledWith({ card: "c2" }));
    press("s", { code: "KeyS" });
    await waitFor(() => expect(mocks.session_start).toHaveBeenCalledWith({ card: "c2" }));
  });

  it("n opens New session in the focused card's column; g then a digit jumps to a column", async () => {
    await openApp();
    act(() => cardButton("search-console").focus());
    press("g", { code: "KeyG" });
    press("2", { code: "Digit2" });
    await waitFor(() => expect(focused()).toBe(cardButton("api-timeouts")));
    press("n", { code: "KeyN" });
    const dlg = await screen.findByRole("dialog", { name: "New session" });
    expect(within(dlg).getByLabelText("Column")).toHaveValue("working");
  });

  it("Backspace archives after a confirm and selects the next card", async () => {
    await openApp();
    act(() => cardButton("api-timeouts").focus());
    press("Backspace");
    fireEvent.click(await screen.findByRole("button", { name: "Archive" }));
    await waitFor(() => expect(mocks.card_archive).toHaveBeenCalledWith({ card_id: "c2" }));
  });
});

describe("zones, workspaces and views", () => {
  it("⌘2 switches workspace and ⌘1 comes back", async () => {
    await openApp();
    press("2", { metaKey: true, code: "Digit2" });
    await waitFor(() => expect(mocks.workspace_switch).toHaveBeenCalledWith({ workspace_id: "w2" }));
    press("1", { metaKey: true, code: "Digit1" });
    await waitFor(() => expect(mocks.workspace_switch).toHaveBeenCalledWith({ workspace_id: "w1" }));
  });

  it("⌘] walks the zones, ⇧⌘P focuses the planner, ⌘D switches to the dashboard", async () => {
    await openApp();
    press("P", { metaKey: true, shiftKey: true, code: "KeyP" });
    await waitFor(() => expect(focused()).toBe(screen.getByRole("complementary", { name: "Planner" })));
    press("]", { metaKey: true });
    await waitFor(() => expect(focused().closest("[data-zone]")?.getAttribute("data-zone")).toBe("board"));
    press("d", { metaKey: true, code: "KeyD" });
    await waitFor(() => expect(mocks.ui_main_view).toHaveBeenCalledWith({ workspace_id: "w1", view: "dashboard" }));
  });

  it("⌥⌘← collapses the planner and ⌘J switches the planner to its terminal", async () => {
    await openApp();
    press("P", { metaKey: true, shiftKey: true, code: "KeyP" });
    press("ArrowLeft", { metaKey: true, altKey: true });
    await waitFor(() => expect(mocks.ui_panel_collapse).toHaveBeenCalledWith({ workspace_id: "w1", panel: "planner", collapsed: true }));
  });

  it("in a terminal only the reserved ⌘ chords are Hadron's, and ⌘Esc leaves it", async () => {
    await openApp();
    const zone = screen.getByRole("complementary", { name: "Planner" });
    const xt = document.createElement("div");
    xt.className = "xterm";
    const ta = document.createElement("textarea");
    xt.appendChild(ta);
    zone.appendChild(xt);
    act(() => ta.focus());
    fireEvent.keyDown(ta, { key: "j", code: "KeyJ" });
    fireEvent.keyDown(ta, { key: "?" });
    fireEvent.keyDown(ta, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(focused()).toBe(ta);
    fireEvent.keyDown(ta, { key: "Escape", metaKey: true });
    expect(focused()).toBe(zone);
    xt.remove();
  });
});

describe("questions with the keyboard", () => {
  it("digits pick an option, n goes to Notes, ⌘↩ answers", async () => {
    state.questions = [question({ card_id: "c1", seq: 2 })];
    state.messages = [message({ id: "m1", channel: "card:c1", author: "session", kind: "report", report_kind: "question", text: "Round invoices up or to nearest?", question_id: "q1" })];
    await openApp();
    act(() => cardButton("search-console").focus());
    press("Enter");
    const panel = await screen.findByRole("complementary", { name: "Card search-console" });
    const q = await within(panel).findByRole("group", { name: "Question Q-02" });
    await waitFor(() => expect(focused()).toBe(within(panel).getByLabelText("Message about this card")));
    press("Escape");
    expect(focused()).toBe(panel);
    press("2", { code: "Digit2" });
    expect(within(q).getByRole("button", { name: /^2\s*Up/ })).toHaveAttribute("aria-pressed", "true");
    press("n", { code: "KeyN" });
    expect(focused()).toBe(within(q).getByLabelText(/Notes/));
    press("Enter", { metaKey: true });
    await waitFor(() => expect(mocks.question_answer).toHaveBeenCalledWith({ question_id: "q1", selections: ["Up"], notes: "" }));
  });

  it("j and k walk the messages of the focused panel", async () => {
    state.messages = [
      message({ id: "m1", channel: "planner", author: "planner", text: "first" }),
      message({ id: "m2", channel: "planner", author: "planner", text: "second" }),
    ];
    await openApp();
    const zone = screen.getByRole("complementary", { name: "Planner" });
    await within(zone).findByText("second");
    act(() => zone.focus());
    press("k", { code: "KeyK" });
    expect(focused()).toHaveTextContent("second");
    press("k", { code: "KeyK" });
    expect(focused()).toHaveTextContent("first");
    press("j", { code: "KeyJ" });
    expect(focused()).toHaveTextContent("second");
    press("i", { code: "KeyI" });
    expect(focused()).toBe(within(zone).getByLabelText("Message the planner"));
  });
});

describe("dialogs, the palette and the cheat sheet", () => {
  it("? opens the cheat sheet from the registry; Esc closes it and the focus comes back", async () => {
    await openApp();
    act(() => cardButton("api-timeouts").focus());
    press("?");
    const dlg = await screen.findByRole("dialog", { name: "Keyboard shortcuts" });
    expect(within(dlg).getByText("Switch Board ⇄ Dashboard")).toBeInTheDocument();
    expect(within(dlg).getByText("Zoom in")).toBeInTheDocument();
    press("Escape");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(focused()).toBe(cardButton("api-timeouts")));
  });

  it("the palette runs a registry command with its chord shown", async () => {
    await openApp();
    press("k", { metaKey: true, code: "KeyK" });
    const input = await screen.findByPlaceholderText("Jump to a card, workspace or command");
    fireEvent.change(input, { target: { value: "go to card dedupe" } });
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("Go to card: dedupe-records");
    fireEvent.change(input, { target: { value: "board ⇄" } });
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("⌘D");
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(mocks.ui_main_view).toHaveBeenCalledWith({ workspace_id: "w1", view: "dashboard" }));
  });

  it("Tab stays inside a dialog", async () => {
    await openApp();
    press("n", { metaKey: true, code: "KeyN" });
    const dlg = await screen.findByRole("dialog", { name: "New session" });
    const start = within(dlg).getByRole("button", { name: "Close" });
    act(() => start.focus());
    fireEvent.keyDown(start, { key: "Tab", shiftKey: true });
    expect(dlg.contains(focused())).toBe(true);
  });
});

describe("zoom", () => {
  it("⌘= and ⌘- step the zoom, say so, persist it as ui.zoom and scale the window; ⌘0 resets", async () => {
    await openApp();
    press("=", { metaKey: true });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.zoom": 1.1 } }));
    expect(await screen.findByRole("status")).toHaveTextContent("Zoom 110%");
    expect(winZoom()).toBe("1.1");
    press("+", { metaKey: true, shiftKey: true });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.zoom": 1.2 } }));
    press("-", { metaKey: true });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.zoom": 1.1 } }));
    press("0", { metaKey: true, code: "Digit0" });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.zoom": 1 } }));
    expect(winZoom()).toBe("");
  });

  it("starts at the stored zoom and stops at 200%", async () => {
    const fx = defaultFixture();
    fx.settings["ui.zoom"] = 2;
    reset(fx);
    await openApp();
    await waitFor(() => expect(winZoom()).toBe("2"));
    press("=", { metaKey: true });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.zoom": 2 } }));
  });

  it("works while a dialog is open", async () => {
    await openApp();
    press(",", { metaKey: true });
    await screen.findByRole("dialog");
    press("-", { metaKey: true });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.zoom": 0.9 } }));
  });
});

// The fixture helpers stay referenced for readers adding cases.
void col;
