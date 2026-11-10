import { fireEvent, render, screen } from "@testing-library/react";
import { useLayoutEffect } from "react";
import { vi } from "vitest";
import type { Message } from "./api";
import { ChatThread } from "./components/Chat";
import { message, reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

// jsdom has no layout. The log gets one: each of its elements is 100px tall, the view 300px, and scrollTop is
// clamped to the content the way a browser clamps it when lines go away.
const LINE = 100;
const VIEW = 300;
const tops = new WeakMap<Element, number>();
const saved = {
  scrollTop: Object.getOwnPropertyDescriptor(Element.prototype, "scrollTop"),
  scrollHeight: Object.getOwnPropertyDescriptor(Element.prototype, "scrollHeight"),
  clientHeight: Object.getOwnPropertyDescriptor(Element.prototype, "clientHeight"),
};
const height = (el: Element) => (el.getAttribute("role") === "log" ? el.childElementCount * LINE : 0);
const view = (el: Element) => (el.getAttribute("role") === "log" ? VIEW : 0);

beforeEach(() => {
  reset();
  Object.defineProperty(Element.prototype, "scrollHeight", { configurable: true, get() { return height(this); } });
  Object.defineProperty(Element.prototype, "clientHeight", { configurable: true, get() { return view(this); } });
  Object.defineProperty(Element.prototype, "scrollTop", {
    configurable: true,
    get() { return Math.max(0, Math.min(tops.get(this) ?? 0, height(this) - view(this))); },
    set(v: number) { tops.set(this, Math.max(0, Math.min(v, height(this) - view(this)))); },
  });
});
afterEach(() => {
  for (const [k, d] of Object.entries(saved)) if (d) Object.defineProperty(Element.prototype, k, d);
});

const lines = (card: string, n: number): Message[] =>
  Array.from({ length: n }, (_, i) => message({ id: `${card}-${i}`, channel: `card:${card}`, author: "session", text: `${card} line ${i + 1}` }));

/**
 * WebKit dispatches the scroll event of the old card's lines going away late: after the new card's
 * lines are on screen and before a passive effect runs. This sends it at that moment.
 */
function LateScroll({ n }: { n: number }) {
  useLayoutEffect(() => {
    const log = document.querySelector('[role="log"]');
    if (log) fireEvent.scroll(log);
  }, [n]);
  return null;
}

function Card({ card, messages }: { card: string; messages: Message[] }) {
  return (
    <>
      <LateScroll n={messages.length} />
      <ChatThread className="cbody" scope={`card:${card}`} messages={messages} questions={[]} onAnswer={() => undefined} empty={null} />
    </>
  );
}

const log = () => screen.getByRole("log");
const atBottom = () => log().scrollHeight - log().scrollTop - log().clientHeight;

describe("opening a card's conversation", () => {
  it("shows the newest line, even when a late scroll event lands before the view is pinned", () => {
    const { rerender } = render(<Card card="billing-fix" messages={lines("billing-fix", 12)} />);
    expect(atBottom()).toBe(0);
    // Another card: its lines load after the panel switched (useChannel empties, then reads).
    rerender(<Card card="export" messages={[]} />);
    rerender(<Card card="export" messages={lines("export", 30)} />);
    expect(log().scrollTop).toBeGreaterThan(0);
    expect(atBottom()).toBe(0);
    // And back again.
    rerender(<Card card="billing-fix" messages={[]} />);
    rerender(<Card card="billing-fix" messages={lines("billing-fix", 12)} />);
    expect(atBottom()).toBe(0);
  });

  it("follows new lines at the bottom, and leaves a reader who scrolled up where they are", () => {
    const { rerender } = render(<Card card="billing-fix" messages={lines("billing-fix", 20)} />);
    expect(atBottom()).toBe(0);
    rerender(<Card card="billing-fix" messages={lines("billing-fix", 21)} />);
    expect(atBottom()).toBe(0);

    // The reader scrolls up to read.
    log().scrollTop = 500;
    fireEvent.scroll(log());
    rerender(<Card card="billing-fix" messages={lines("billing-fix", 23)} />);
    expect(log().scrollTop).toBe(500);

    // Back at the bottom, the view follows again.
    log().scrollTop = log().scrollHeight;
    fireEvent.scroll(log());
    rerender(<Card card="billing-fix" messages={lines("billing-fix", 25)} />);
    expect(atBottom()).toBe(0);

    // Opening another card starts at its newest line, whatever the last card's position was.
    log().scrollTop = 0;
    fireEvent.scroll(log());
    rerender(<Card card="export" messages={[]} />);
    rerender(<Card card="export" messages={lines("export", 9)} />);
    expect(atBottom()).toBe(0);
  });
});
