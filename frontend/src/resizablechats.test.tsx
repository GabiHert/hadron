import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { BOARD_MIN, PANEL_LIMITS, RAIL, panelLayout } from "./state/panelLayout";
import css from "./styles/app.css?raw";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

// jsdom has no PointerEvent; a MouseEvent with a pointerId carries clientX like the real one.
if (typeof window.PointerEvent === "undefined") {
  class PointerEventShim extends MouseEvent {
    pointerId: number;
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init);
      this.pointerId = init.pointerId ?? 1;
    }
  }
  (window as unknown as { PointerEvent: typeof PointerEventShim }).PointerEvent = PointerEventShim;
}

const setWidth = (w: number) => {
  Object.defineProperty(window, "innerWidth", { configurable: true, value: w });
  act(() => {
    window.dispatchEvent(new Event("resize"));
  });
};
const shown = (l: ReturnType<typeof panelLayout>) => (l.planner.auto ? RAIL : l.planner.width) + (l.card.auto ? RAIL : l.card.width);

describe("panelLayout: the window decides what fits", () => {
  const at = (width: number, extra: Partial<Parameters<typeof panelLayout>[0]> = {}) => panelLayout({ width, widths: {}, collapsed: {}, ...extra });

  it("1200 px: both chats at their default widths", () => {
    const l = at(1200);
    expect([l.overlay, l.planner.auto, l.card.auto]).toEqual([false, false, false]);
    expect([l.planner.width, l.card.width]).toEqual([PANEL_LIMITS.planner.def, PANEL_LIMITS.card.def]);
  });

  it("960 and 900 px: both chats shrink toward their minimums, and the board keeps its room", () => {
    for (const w of [960, 900]) {
      const l = at(w);
      expect([l.planner.auto, l.card.auto]).toEqual([false, false]);
      expect(l.planner.width).toBeGreaterThanOrEqual(PANEL_LIMITS.planner.min);
      expect(l.card.width).toBeGreaterThanOrEqual(PANEL_LIMITS.card.min);
      expect(l.planner.width).toBeLessThan(PANEL_LIMITS.planner.def);
      expect(shown(l)).toBeLessThanOrEqual(w - BOARD_MIN);
    }
  });

  it("700 px: only the card panel folds, the planner stays, and asking for the card swaps them", () => {
    const l = at(700);
    expect([l.overlay, l.planner.auto, l.card.auto]).toEqual([false, false, true]);
    expect(shown(l)).toBeLessThanOrEqual(700 - BOARD_MIN);
    const c = at(700, { prefer: "card" });
    expect([c.planner.auto, c.card.auto]).toEqual([true, false]);
    expect(c.card.width).toBeGreaterThanOrEqual(PANEL_LIMITS.card.min);
    expect(shown(c)).toBeLessThanOrEqual(700 - BOARD_MIN);
  });

  it("never folds both: with the planner folded by the user, the card panel stays at 700 px", () => {
    const l = at(700, { collapsed: { planner: true } });
    expect([l.planner.auto, l.card.auto]).toEqual([false, false]);
  });

  it("stored widths re-clamp to the window, and the drag limits follow it", () => {
    const wide = at(2000, { widths: { planner: 720, card: 900 } });
    expect([wide.planner.width, wide.card.width]).toEqual([720, 900]);
    const narrow = at(1200, { widths: { planner: 720, card: 900 } });
    expect(shown(narrow)).toBeLessThanOrEqual(1200 - BOARD_MIN);
    expect(Math.abs(narrow.planner.max - (1200 - BOARD_MIN - narrow.card.width))).toBeLessThanOrEqual(1);
    // Out-of-range or missing widths fall back to the limits and the defaults.
    expect(at(1600, { widths: { planner: 5, card: null } }).planner.width).toBe(PANEL_LIMITS.planner.min);
    expect(at(1600, { widths: { card: null } }).card.width).toBe(PANEL_LIMITS.card.def);
  });

  it("as narrow as a phone, the panels are overlays opened from the header", () => {
    expect(at(600).overlay).toBe(true);
    expect(css).toMatch(/@media \(max-width: 600px\)\{[^@]*\.ppanel,\.cpanel\{position:fixed/);
    // Rails are never hidden while the panels sit beside the board.
    expect(css).not.toMatch(/@media \(max-width: 760px\)\{[^}]*\.rail\{display:none\}/);
  });
});

describe("the chats in the app", () => {
  beforeEach(() => reset());
  afterEach(() => setWidth(1024));

  const app = async (w: number) => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: w });
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
  };

  it.each([1200, 900])("%i px: both panels show, each with a resize handle", async (w) => {
    await app(w);
    expect(await screen.findByRole("complementary", { name: "Planner" })).toBeInTheDocument();
    expect(screen.getByRole("complementary", { name: "Card panel" })).toBeInTheDocument();
    expect(screen.getByRole("separator", { name: "Resize the planner" })).toBeInTheDocument();
    expect(screen.getByRole("separator", { name: "Resize the card panel" })).toBeInTheDocument();
  });

  it("700 px: the card panel folds to its rail, the screen says how to get it back, and its rail swaps the two", async () => {
    await app(700);
    expect(await screen.findByRole("complementary", { name: "Planner" })).toBeInTheDocument();
    const rail = screen.getByRole("navigation", { name: "Card panel, folded to fit the window" });
    expect(await screen.findByText(/too narrow for both chats, so the card panel is folded to the rail on the right\. Click the rail to open it/)).toBeInTheDocument();
    await userEvent.click(rail.querySelector("button")!);
    expect(await screen.findByRole("navigation", { name: "Planner, folded to fit the window" })).toBeInTheDocument();
    expect(screen.getByRole("complementary", { name: "Card panel" })).toBeInTheDocument();
    // Nothing was saved as collapsed: it is only the window.
    expect(mocks.ui_panel_collapse).not.toHaveBeenCalled();
  });

  it("shrinking the window folds the card panel; widening it brings it back", async () => {
    await app(1200);
    await screen.findByRole("separator", { name: "Resize the card panel" });
    setWidth(700);
    expect(await screen.findByRole("navigation", { name: "Card panel, folded to fit the window" })).toBeInTheDocument();
    setWidth(1200);
    expect(await screen.findByRole("complementary", { name: "Card panel" })).toBeInTheDocument();
  });

  it("the collapse buttons still fold a panel to its rail", async () => {
    await app(1200);
    await userEvent.click(await screen.findByRole("button", { name: "Collapse the planner" }));
    await waitFor(() => expect(mocks.ui_panel_collapse).toHaveBeenCalledWith(expect.objectContaining({ panel: "planner", collapsed: true })));
  });

  it("resizes from the keyboard, resets on Enter and double-click, and saves the width", async () => {
    await app(1200);
    const h = await screen.findByRole("separator", { name: "Resize the planner" });
    expect(h).toHaveAttribute("aria-valuenow", "340");
    expect(h).toHaveAttribute("aria-valuemax", String(1200 - BOARD_MIN - PANEL_LIMITS.card.def));
    fireEvent.keyDown(h, { key: "ArrowRight" });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.panel_width.planner": 356 } }));
    expect(h).toHaveAttribute("aria-valuenow", "356");
    fireEvent.keyDown(h, { key: "ArrowLeft", shiftKey: true });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.panel_width.planner": 292 } }));
    fireEvent.keyDown(h, { key: "Home" });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.panel_width.planner": PANEL_LIMITS.planner.min } }));
    fireEvent.keyDown(h, { key: "Enter" });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.panel_width.planner": null } }));
    // The card panel's handle sits on its left edge: → narrows it.
    const c = screen.getByRole("separator", { name: "Resize the card panel" });
    fireEvent.keyDown(c, { key: "ArrowRight" });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.panel_width.card": 434 } }));
    fireEvent.doubleClick(c);
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "ui.panel_width.card": null } }));
  });

  it("drags to a new width, within the limits the window leaves, and saves it once", async () => {
    await app(1200);
    const h = await screen.findByRole("separator", { name: "Resize the planner" });
    const panel = screen.getByRole("complementary", { name: "Planner" });
    mocks.settings_update.mockClear();
    fireEvent.pointerDown(h, { clientX: 340, button: 0, pointerId: 1 });
    expect(document.documentElement).toHaveAttribute("data-resizing");
    fireEvent.pointerMove(h, { clientX: 400, pointerId: 1 });
    await waitFor(() => expect(panel.style.width).toBe("400px"));
    expect(mocks.settings_update).not.toHaveBeenCalled();
    fireEvent.pointerMove(h, { clientX: 2000, pointerId: 1 });
    fireEvent.pointerUp(h, { clientX: 2000, pointerId: 1 });
    expect(document.documentElement).not.toHaveAttribute("data-resizing");
    const max = 1200 - BOARD_MIN - PANEL_LIMITS.card.def;
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.panel_width.planner": max } }));
    expect(mocks.settings_update).toHaveBeenCalledTimes(1);
    expect(panel.style.width).toBe(`${max}px`);
  });

  it("starts from the saved widths", async () => {
    state.fx.settings["ui.panel_width.planner"] = 500;
    await app(1600);
    await waitFor(() => expect(screen.getByRole("separator", { name: "Resize the planner" })).toHaveAttribute("aria-valuenow", "500"));
  });

  it("uses the resize cursor and keeps it for the whole drag", () => {
    expect(css).toMatch(/\.presize\{[^}]*cursor:col-resize/);
    expect(css).toMatch(/:root\[data-resizing\],:root\[data-resizing\] \*\{cursor:col-resize!important/);
    expect(css).toContain(":root[data-resizing] :is(iframe,.xterm){pointer-events:none}");
  });
});
