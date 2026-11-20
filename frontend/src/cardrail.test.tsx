import { render, screen } from "@testing-library/react";
import App from "./App";
import css from "./styles/app.css?raw";
import { reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

/** Every rule in app.css (inside @media too) whose selector can land on a folded chat panel, nav.rail. */
function panelRailRules(): { sel: string; body: string }[] {
  const out: { sel: string; body: string }[] = [];
  const src = css.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const m of src.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const sels = m[1].trim().replace(/^@media[^{]*$/, "");
    for (const sel of sels.split(",")) {
      // The element the rule styles is the last compound of the selector.
      const last = sel.trim().split(/[\s>+~]+/).pop() ?? "";
      if (!/\.rail(?![\w-])/.test(last)) continue;
      const tag = /^[a-z]+/.exec(last)?.[0];
      if (tag && tag !== "nav") continue;
      out.push({ sel: sel.trim(), body: m[2] });
    }
  }
  return out;
}

describe("a folded chat panel is a plain strip, like the planner's edge", () => {
  beforeEach(() => reset());

  it("no rule gives the panel rails rounded corners, a box border, a dashed edge or a minimum height", () => {
    const rules = panelRailRules();
    expect(rules.map((r) => r.sel)).toEqual(expect.arrayContaining(["nav.rail", "nav.rail.r"]));
    for (const { sel, body } of rules) {
      expect(body, sel).not.toMatch(/border-radius:(?!0[;}]|0$)/);
      expect(body, sel).not.toMatch(/min-height:(?!0)/);
      expect(body, sel).not.toMatch(/dashed/);
      expect(body, sel).not.toMatch(/(^|;)border:(?!0)/);
    }
    // One straight divider toward the board: the planner's on its right, the card panel's on its left.
    expect(css).toMatch(/nav\.rail\{[^}]*border:0;border-right:1px solid var\(--border\);border-radius:0/);
    expect(css).toMatch(/nav\.rail\.r\{border-right:0;border-left:1px solid var\(--border\)/);
  });

  it("the board's folded columns keep their rounded button, scoped to it", () => {
    expect(css).toMatch(/button\.rail\{[^}]*border-radius:8px/);
    expect(css).not.toMatch(/(^|\n)\.rail\{[^}]*border-radius/);
  });

  it("folded, the card panel shows the same kind of rail as the planner, and open it shows none", async () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 700 });
    try {
      render(<App />);
      await screen.findByRole("heading", { name: "Acme" });
      const rail = screen.getByRole("navigation", { name: "Card panel, folded to fit the window" });
      expect(rail.tagName).toBe("NAV");
      expect(rail.className.split(" ")).toEqual(expect.arrayContaining(["rail", "r"]));
      // The open planner has no rail beside it.
      expect(screen.getByRole("complementary", { name: "Planner" })).toBeInTheDocument();
      expect(screen.queryByRole("navigation", { name: /^Planner/ })).toBeNull();
    } finally {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
    }
  });

  it("at 1200 px both chats are open and neither shows a rail", async () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1200 });
    try {
      render(<App />);
      await screen.findByRole("heading", { name: "Acme" });
      expect(await screen.findByRole("complementary", { name: "Card panel" })).toBeInTheDocument();
      expect(document.querySelector("nav.rail")).toBeNull();
    } finally {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
    }
  });
});

/** The look of an element, from the real stylesheet: what would draw a bar or move the content. */
const LOOK = ["width", "padding", "margin", "border-top-width", "border-right-width", "border-bottom-width", "border-left-width", "border-style", "border-radius", "background-color", "background", "box-shadow", "position", "top", "bottom", "z-index"] as const;
const look = (el: Element) => {
  const c = getComputedStyle(el);
  return Object.fromEntries(LOOK.map((k) => [k, c.getPropertyValue(k)]));
};

describe("the card panel's edge is the planner's, mirrored", () => {
  let sheet: HTMLStyleElement;
  beforeEach(() => {
    reset();
    sheet = document.createElement("style");
    sheet.textContent = css;
    document.head.appendChild(sheet);
  });
  afterEach(() => {
    sheet.remove();
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
  });

  it("open: the two resize handles have the same box and no surface of their own, and only the side differs", async () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1200 });
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    const planner = screen.getByRole("separator", { name: "Resize the planner" });
    const card = screen.getByRole("separator", { name: "Resize the card panel" });
    // Same element, same place in its panel: a direct child of the aside.
    expect(card.tagName).toBe(planner.tagName);
    expect(planner.parentElement!.tagName).toBe("ASIDE");
    expect(card.parentElement!.tagName).toBe("ASIDE");
    expect(look(card)).toEqual(look(planner));
    // An invisible 9 px strip: no padding, border, corners or fill that could cover the panel.
    const c = getComputedStyle(card);
    expect(c.width).toBe("9px");
    expect(c.padding).toBe("");
    expect(c.borderRadius).toBe("");
    expect(c.borderLeftWidth).toBe("");
    // Mirrored: it hangs over the panel's left edge as the planner's does over its right.
    expect(c.left).toBe(getComputedStyle(planner).right);
    // No class on a handle is shared with an unrelated rule (".card" styled it as a card tile).
    for (const h of [planner, card]) expect([...h.classList].filter((k) => k !== "presize" && !k.startsWith("presize-"))).toEqual([]);
  });

  it("open: the two panels' asides differ only in the side their divider is on", async () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1200 });
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    const p = getComputedStyle(screen.getByRole("complementary", { name: "Planner" }));
    const k = getComputedStyle(screen.getByRole("complementary", { name: "Card panel" }));
    // One divider each, on the side facing the board (jsdom cannot expand a border with var(), so read the rule).
    expect(css).toMatch(/\.ppanel\{[^}]*border-right:1px solid var\(--border\)/);
    expect(css).toMatch(/\.cpanel\{[^}]*border-left:1px solid var\(--border\)/);
    for (const prop of ["padding", "margin", "border-radius", "box-shadow"]) expect(k.getPropertyValue(prop), prop).toBe(p.getPropertyValue(prop));
  });
});
