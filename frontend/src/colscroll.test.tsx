// A full column scrolls instead of squeezing its cards: each tile keeps its natural height, the card
// list is the scroll container, a drag near its edges scrolls it, and keyboard focus brings the card
// into view.
import { createEvent, fireEvent, render, screen } from "@testing-library/react";
import App from "./App";
import { edgeScroll } from "./lib";
import css from "./styles/app.css?raw";
import { card, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

let sheet: HTMLStyleElement;
beforeEach(() => {
  reset();
  try {
    localStorage.clear();
  } catch {
    /* no storage */
  }
  // jsdom does no layout, but it does cascade the stylesheet, so the computed styles show the rules.
  sheet = document.createElement("style");
  sheet.textContent = css;
  document.head.appendChild(sheet);
  state.fx.cards.w1.push(...Array.from({ length: 30 }, (_, i) => card(`x${i}`, "w1", "working", `extra-${i}`, 3 + i, { stage: "review", key: `ACME-${600 + i}` })));
});
afterEach(() => sheet.remove());

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await screen.findByRole("button", { name: "Open extra-29" });
}

/** A dragover at clientY: jsdom has no DragEvent, so the plain event gets the coordinate. */
function dragOver(el: HTMLElement, clientY: number) {
  const ev = createEvent.dragOver(el);
  Object.defineProperty(ev, "clientY", { value: clientY });
  fireEvent(el, ev);
}

/** Gives a list a layout: a viewport `h` tall at `top`, holding `content` px of cards. */
function layout(el: HTMLElement, top: number, h: number, content: number) {
  Object.defineProperty(el, "clientHeight", { configurable: true, value: h });
  Object.defineProperty(el, "scrollHeight", { configurable: true, value: content });
  el.getBoundingClientRect = () => ({ top, bottom: top + h, height: h, left: 0, right: 250, width: 250, x: 0, y: top, toJSON: () => ({}) }) as DOMRect;
}

describe("a column with more cards than fit", () => {
  it("keeps every card at its natural height and scrolls the list", async () => {
    await openApp();
    const list = screen.getByTestId("list-working");
    expect(getComputedStyle(list).overflowY).toBe("auto");
    expect(getComputedStyle(list).minHeight).toBe("40px");
    const tiles = list.querySelectorAll<HTMLElement>("[data-card-id]");
    expect(tiles.length).toBe(33);
    // A card's face is clipped (overflow:hidden), which drops a flex item's minimum height to zero;
    // without flex:none a full column shrank every card instead of scrolling.
    for (const t of tiles) {
      expect(getComputedStyle(t).overflow).toBe("hidden");
      expect(getComputedStyle(t).flexShrink).toBe("0");
    }
    // The column itself never scrolls or grows past the board: only its list does.
    const col = list.closest<HTMLElement>("section")!;
    expect(getComputedStyle(col).minHeight).toMatch(/^0(px)?$/);
    expect(getComputedStyle(col).overflowY).not.toBe("auto");
  });

  it("keeps the rows of a dashboard card group at their height too", () => {
    expect(css).toContain(".clist>*,.grpl>*{flex:none}");
  });

  it("scrolls the list while a card is dragged near its top or bottom edge", async () => {
    await openApp();
    const list = screen.getByTestId("list-working");
    layout(list, 100, 400, 2400);
    list.scrollTop = 500;
    dragOver(list, 495);
    const down = list.scrollTop;
    expect(down).toBeGreaterThan(500);
    dragOver(list, 300);
    expect(list.scrollTop).toBe(down);
    dragOver(list, 105);
    expect(list.scrollTop).toBeLessThan(down);
    // A drag over a card bubbles to the list, so it scrolls with the pointer over a card as well.
    const before = list.scrollTop;
    dragOver(screen.getByRole("button", { name: "Open extra-5" }), 498);
    expect(list.scrollTop).toBeGreaterThan(before);
  });

  it("scrolls faster nearer the edge, and not at all when everything fits", () => {
    const el = document.createElement("div");
    layout(el, 0, 300, 1000);
    expect(edgeScroll(el, 299)).toBeGreaterThan(edgeScroll(el, 270));
    expect(edgeScroll(el, 1)).toBeLessThan(0);
    layout(el, 0, 300, 300);
    expect(edgeScroll(el, 299)).toBe(0);
  });

  it("brings the focused card into view when stepping through the column with the keys", async () => {
    await openApp();
    const spy = vi.fn();
    Element.prototype.scrollIntoView = spy;
    const first = screen.getByRole("button", { name: "Open api-timeouts" });
    first.focus();
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement).toHaveAccessibleName("Open dedupe-records");
    expect(spy).toHaveBeenCalledWith(expect.objectContaining({ block: "nearest" }));
    expect(spy.mock.contexts.at(-1)).toBe(document.activeElement);
  });
});
