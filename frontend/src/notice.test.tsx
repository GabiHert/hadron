// Notice lines (system, rule and routine events): a pill while they fit one line, a rounded card
// clamped to three lines with "Show more" when they wrap.
import css from "./styles/app.css?raw";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ChatLine, Notice } from "./components/Chat";
import { message } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

/** jsdom has no layout: each span reports one line per 40 characters of its text. */
function fakeLines() {
  return vi.spyOn(HTMLElement.prototype, "getClientRects").mockImplementation(function (this: HTMLElement) {
    const n = Math.max(1, Math.ceil((this.textContent ?? "").length / 40));
    return Array.from({ length: n }, () => new DOMRect()) as unknown as DOMRectList;
  });
}
afterEach(() => vi.restoreAllMocks());

const LONG =
  "Routine \"Nightly disk hygiene\": sent the prompt to the running session. Prompt: run the cleanup in the background, then post one short line in the planner chat: space freed, items skipped, and free space.";

describe("notice lines", () => {
  it("stay a pill when they fit one line", () => {
    fakeLines();
    const { container } = render(<Notice text="Rule moved billing-fix to Review" />);
    const ev = container.querySelector(".b-ev")!;
    expect(ev).not.toHaveClass("multi");
    expect(ev).not.toHaveClass("clamped");
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("become a card when they wrap, clamped to three lines with Show more", async () => {
    fakeLines();
    const { container } = render(<Notice text={LONG} />);
    const ev = container.querySelector(".b-ev")!;
    expect(ev).toHaveClass("multi");
    expect(ev).toHaveClass("clamped");
    const more = screen.getByRole("button", { name: "Show more" });
    expect(more).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(more);
    expect(ev).not.toHaveClass("clamped");
    expect(ev).toHaveClass("multi");
    expect(screen.getByRole("button", { name: "Show less" })).toHaveAttribute("aria-expanded", "true");
  });

  it("a card of two lines needs no Show more", () => {
    fakeLines();
    const { container } = render(<Notice text={"x".repeat(70)} />);
    expect(container.querySelector(".b-ev")).toHaveClass("multi");
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("every notice kind is drawn as a Notice: events, rule and system lines", () => {
    fakeLines();
    for (const m of [
      message({ id: "a", kind: "event", author: "system", text: LONG }),
      message({ id: "b", author: "rule", text: LONG }),
      message({ id: "c", author: "system", text: LONG }),
    ]) {
      const { container, unmount } = render(<ChatLine m={m} />);
      expect(container.querySelector(".b-ev.multi.clamped")).not.toBeNull();
      unmount();
    }
  });

  it("styles: no full-round pill once it wraps, a bubble's radius and width, three lines clamped", () => {
    expect(css).toContain(".b-ev.multi{display:block;align-self:center;max-width:92%;text-align:left;");
    expect(css).toContain(':root:is([data-glass="on"],[data-glass="solid"]) .glasswrap .b-ev.multi{border-radius:14px;padding:8px 13px}');
    expect(css).toContain(".b-ev.clamped .ev-t{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:3;");
    // Without glass the side rules go, so they cannot squeeze the text.
    expect(css).toContain(".b-ev.multi::before,.b-ev.multi::after{display:none}");
  });
});
