import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ChatLine } from "./components/Chat";
import { act } from "react";
import { WIDGET_DEFAULT_HEIGHT, WIDGET_GUESS_MAX, Widget, estimateHeight, isDiagram, widgetBaseCss, widgetSrcDoc, widgetTheme } from "./components/Widget";
import { message } from "./test/fakeApi";
import { applyTokens, resetThemesForTest } from "./theme";
import appCss from "./styles/app.css?raw";
import kitCss from "./styles/widgetkit.css?raw";

describe("a widget in a chat", () => {
  it("shows no pin button until its menu is opened", () => {
    const onPin = vi.fn();
    render(<Widget title="Plan" html="<p>x</p>" scripts={false} onPin={onPin} />);
    const group = screen.getByRole("group", { name: "Widget Plan" });
    expect(within(group).queryByRole("button", { name: /pin|dashboard/i })).toBeNull();
    expect(within(group).queryByRole("menuitem")).toBeNull();
    expect(within(group).getByRole("button", { name: "More for Plan" })).toHaveAttribute("aria-haspopup", "menu");
  });

  it("the menu is only visible on hover or keyboard focus", () => {
    expect(appCss).toMatch(/\.wmenu-btn\{[^}]*opacity:0/);
    expect(appCss).toMatch(/\.widget:hover \.wmenu-btn,\.widget:focus-within \.wmenu-btn/);
  });

  it("is reached with Tab, offers Add to dashboard, and calls onPin", async () => {
    const user = userEvent.setup();
    const onPin = vi.fn();
    render(<Widget title="Plan" html="<p>x</p>" scripts={false} onPin={onPin} />);
    await user.tab();
    const more = screen.getByRole("button", { name: "More for Plan" });
    expect(more).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(more).toHaveAttribute("aria-expanded", "true");
    const menu = screen.getByRole("menu", { name: "Actions for Plan" });
    expect(within(menu).getAllByRole("menuitem").map((b) => b.textContent)).toEqual(["Open full screen", "Copy HTML", "Add to dashboard"]);
    expect(within(menu).getByRole("menuitem", { name: "Open full screen" })).toHaveFocus();
    await user.click(within(menu).getByRole("menuitem", { name: "Add to dashboard" }));
    expect(onPin).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("Esc closes the menu and gives focus back to its button", async () => {
    const user = userEvent.setup();
    render(<Widget title="Plan" html="<p>x</p>" scripts={false} />);
    await user.click(screen.getByRole("button", { name: "More for Plan" }));
    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Add to dashboard" })).toBeNull();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.getByRole("button", { name: "More for Plan" })).toHaveFocus();
  });

  it("a pinned widget's item reads On the dashboard and is disabled", async () => {
    const user = userEvent.setup();
    const onPin = vi.fn();
    render(<Widget title="Plan" html="<p>x</p>" scripts={false} onPin={onPin} pinned />);
    await user.click(screen.getByRole("button", { name: "More for Plan" }));
    const item = screen.getByRole("menuitem", { name: "On the dashboard ✓" });
    expect(item).toBeDisabled();
    await user.click(item);
    expect(onPin).not.toHaveBeenCalled();
  });

  it("copies its HTML", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<Widget title="Plan" html="<p>x</p>" scripts={false} />);
    await user.click(screen.getByRole("button", { name: "More for Plan" }));
    await user.click(screen.getByRole("menuitem", { name: "Copy HTML" }));
    expect(writeText).toHaveBeenCalledWith("<p>x</p>");
    expect(await screen.findByRole("menuitem", { name: "Copied" })).toBeInTheDocument();
  });

  it("opens full screen in a dialog with the same sandbox, and Esc closes it", async () => {
    const user = userEvent.setup();
    render(<Widget title="Flow" html="<svg viewBox='0 0 10 10'></svg>" scripts={false} />);
    await user.click(screen.getByRole("button", { name: "More for Flow" }));
    await user.click(screen.getByRole("menuitem", { name: "Open full screen" }));
    const dialog = screen.getByRole("dialog", { name: "Flow" });
    expect(dialog).toHaveClass("wfull");
    const f = dialog.querySelector("iframe")!;
    expect(f.getAttribute("sandbox")).toBe("allow-scripts");
    expect(f.getAttribute("sandbox")).not.toContain("allow-same-origin");
    expect(f.getAttribute("srcdoc")).toMatch(/script-src 'nonce-[0-9a-f]{24}'/);
    expect(f.getAttribute("srcdoc")).not.toContain("'unsafe-inline'; img-src data:; script-src 'unsafe-inline'");
    expect(f.hasAttribute("src")).toBe(false);
    expect(f.getAttribute("srcdoc")).toContain("<svg viewBox='0 0 10 10'></svg>");
    expect(within(dialog).getByRole("button", { name: "Close" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("the full screen Close button closes it", async () => {
    const user = userEvent.setup();
    render(<Widget title="Flow" html="<p>x</p>" scripts />);
    await user.click(screen.getByRole("button", { name: "More for Flow" }));
    await user.click(screen.getByRole("menuitem", { name: "Open full screen" }));
    expect(screen.getByRole("dialog").querySelector("iframe")!.getAttribute("sandbox")).toBe("allow-scripts");
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("has a quiet caption when titled, and no bar at all without a title", () => {
    const { container, rerender } = render(<Widget title="Plan" html="<p>x</p>" scripts={false} />);
    expect(container.querySelector(".wcap")).toHaveTextContent("Plan");
    expect(container.querySelector(".wh")).toBeNull();
    rerender(<Widget title="" html="<p>x</p>" scripts={false} />);
    expect(container.querySelector(".wcap")).toBeNull();
    expect(container.querySelector(".wh")).toBeNull();
  });

  it("a diagram takes the full chat width, anything else the bubble width; a dashboard block has no menu", () => {
    const { container, rerender } = render(<Widget title="T" html="<table></table>" scripts={false} />);
    expect(container.querySelector(".widget")).toHaveClass("w-chat");
    expect(container.querySelector(".widget")).not.toHaveClass("w-wide");
    rerender(<Widget title="T" html="  <svg viewBox='0 0 1 1'/>" scripts={false} />);
    expect(container.querySelector(".widget")).toHaveClass("w-wide");
    rerender(<Widget variant="block" title="" html="<p>x</p>" scripts={false} />);
    expect(container.querySelector(".widget")).toHaveClass("w-block");
    expect(screen.queryByRole("button", { name: /More for/ })).toBeNull();
    expect(appCss).toMatch(/\.widget\.w-chat\{[^}]*width:92%/);
    expect(isDiagram("<svg></svg>")).toBe(true);
    expect(isDiagram("<div><svg></svg></div>")).toBe(false);
  });

  it("a chat line passes its pin through", async () => {
    const user = userEvent.setup();
    const onPin = vi.fn();
    render(<ChatLine m={message({ id: "w1", kind: "widget", widget_title: "Steps", widget_html: "<ol><li>a</li></ol>" })} onPin={onPin} pinnedIds={new Set()} />);
    await user.click(screen.getByRole("button", { name: "More for Steps" }));
    await user.click(screen.getByRole("menuitem", { name: "Add to dashboard" }));
    expect(onPin).toHaveBeenCalledWith("w1");
  });
});

describe("a widget's height without scripts", () => {
  it("sizes an SVG exactly from its height or its viewBox and the frame width", () => {
    expect(estimateHeight('<svg viewBox="0 0 320 90" width="100%">', 640)).toEqual({ height: 188, exact: true });
    expect(estimateHeight('<svg viewBox="0 0 320 90" width="320">', 640)).toEqual({ height: 98, exact: true });
    expect(estimateHeight('<svg height="300" viewBox="0 0 1 1">', 640)).toEqual({ height: 308, exact: true });
  });

  it("guesses other HTML from its rows, capped, and an empty widget keeps the default", () => {
    const table = "<table>" + "<tr><td>a</td></tr>".repeat(5) + "</table>";
    expect(estimateHeight(table, 480)).toEqual({ height: 130, exact: false });
    expect(estimateHeight("<ul>" + "<li>x</li>".repeat(100) + "</ul>", 480).height).toBe(WIDGET_GUESS_MAX);
    expect(estimateHeight("", 480).height).toBe(WIDGET_DEFAULT_HEIGHT);
  });

  it("a guessed height can grow with Show more; an exact one or a script-reported one needs none", async () => {
    const user = userEvent.setup();
    const { container, rerender } = render(<Widget title="T" html={"<p>a</p>".repeat(3)} scripts={false} />);
    const frame = () => container.querySelector("iframe")!;
    expect(frame().style.height).toBe("86px");
    await user.click(screen.getByRole("button", { name: "Show more" }));
    expect(frame().style.height).toBe("600px");
    expect(screen.getByRole("button", { name: "Show less" })).toHaveAttribute("aria-expanded", "true");
    rerender(<Widget title="T" html='<svg height="140"></svg>' scripts={false} />);
    expect(frame().style.height).toBe("148px");
    expect(screen.queryByRole("button", { name: /Show (more|less)/ })).toBeNull();
    rerender(<Widget title="T" html="<p>x</p>" height={300} scripts={false} />);
    expect(screen.queryByRole("button", { name: /Show (more|less)/ })).toBeNull();
  });
});

describe("the frame's base styles", () => {
  afterEach(() => resetThemesForTest());
  const dark = { mode: "dark" as const, tokens: { "--fg": "#ece8df", "--card": "#232220", "--sans": '"Inter Tight", sans-serif' } };

  it("set the app's colour scheme and the theme tokens as real values", () => {
    const css = widgetBaseCss(dark);
    expect(css).toContain(":root{color-scheme:dark;--fg:#ece8df;--card:#232220;--sans:\"Inter Tight\", sans-serif;}");
    expect(widgetBaseCss({ ...dark, mode: "light" })).toContain("color-scheme:light;");
  });

  it("give the body an explicit background, colour and font from the tokens, with system fallbacks", () => {
    const css = widgetBaseCss(dark);
    expect(css).toMatch(/body\{[^}]*background:var\(--card,Canvas\)/);
    expect(css).toMatch(/body\{[^}]*color:var\(--fg,CanvasText\)/);
    expect(css).toMatch(/body\{[^}]*font:13px\/1.5 var\(--sans,/);
  });

  it("use border-box, keep images and SVG inside the frame, and wrap long words", () => {
    const css = widgetBaseCss(dark);
    expect(css).toContain("*,*::before,*::after{box-sizing:border-box}");
    expect(css).toMatch(/img,svg,video,canvas\{max-width:100%\}/);
    expect(css).toMatch(/body\{[^}]*overflow-wrap:break-word/);
  });

  it("make tables full width with wrapping cells, and scroll a too-wide one instead of clipping it", () => {
    const css = widgetBaseCss(dark);
    expect(css).toMatch(/table\{width:100%;[^}]*border-collapse:collapse\}/);
    expect(css).toMatch(/th,td\{[^}]*overflow-wrap:anywhere/);
    expect(css).toContain("html{overflow-x:auto}");
  });

  it("come before the kit, and both before the widget's own styles, so the widget wins", () => {
    const doc = widgetSrcDoc("<style>p{color:red}</style><p>x</p>", { scripts: false, nonce: "abc", theme: dark });
    const base = doc.indexOf('<style id="hadron-base">');
    const kit = doc.indexOf('<style id="hadron-kit">');
    const own = doc.indexOf("<style>p{color:red}");
    expect(base).toBeGreaterThan(doc.indexOf("Content-Security-Policy"));
    expect(base).toBeLessThan(kit);
    expect(kit).toBeLessThan(own);
    expect(doc).toContain('<html data-theme="dark">');
    expect(doc).toContain('<script type="application/json" id="hadron-theme">{"mode":"dark"');
  });

  it("let only Hadron's height script run when widget scripts are off", () => {
    const off = widgetSrcDoc("<p>x</p>", { scripts: false, nonce: "abc", theme: dark });
    expect(off).toContain("script-src 'nonce-abc'");
    expect(off).not.toContain("'unsafe-inline'; img-src data:; script-src 'unsafe-inline'");
    expect(off).toMatch(/<script nonce="abc">[^<]*hadronHeight/);
    const on = widgetSrcDoc("<p>x</p>", { scripts: true, nonce: "abc", theme: dark });
    expect(on).toContain("script-src 'unsafe-inline'");
    expect(on).not.toContain("'nonce-");
  });

  it("read the live theme: its mode and every token, fonts included", () => {
    applyTokens("dark", { fg: "#101010", card: "#202020" });
    const t = widgetTheme();
    expect(t.mode).toBe("dark");
    expect(t.tokens["--fg"]).toBe("#101010");
    expect(t.tokens["--card"]).toBe("#202020");
    for (const n of ["--bg", "--fg2", "--fg3", "--acc", "--ok", "--warn", "--bad", "--border", "--sans", "--head", "--mono"]) expect(t.tokens[n]).toBeTruthy();
    expect(t.tokens["--shadow"]).toBeUndefined();
  });

  it("follow a live theme switch, keeping the frame's nonce", () => {
    const { container } = render(<Widget title="T" html="<table><tr><td>x</td></tr></table>" scripts={false} />);
    const doc = () => container.querySelector("iframe")!.getAttribute("srcdoc")!;
    act(() => applyTokens("light", { fg: "#111111" }));
    expect(doc()).toContain("color-scheme:light;");
    const nonce = doc().match(/'nonce-([0-9a-f]+)'/)![1];
    act(() => applyTokens("dark", { fg: "#eeeeee" }));
    expect(doc()).toContain("color-scheme:dark;");
    expect(doc()).toContain("--fg:#eeeeee;");
    expect(doc()).toContain(`'nonce-${nonce}'`);
  });

  it("size the frame to the height it reports, even with widget scripts off", () => {
    const { container } = render(<Widget title="T" html={"<p>a</p>".repeat(3)} scripts={false} />);
    const frame = container.querySelector("iframe")!;
    act(() => {
      window.dispatchEvent(new MessageEvent("message", { data: { hadronHeight: 57 }, source: frame.contentWindow }));
    });
    expect(frame.style.height).toBe("57px");
    expect(screen.queryByRole("button", { name: /Show (more|less)/ })).toBeNull();
    act(() => {
      window.dispatchEvent(new MessageEvent("message", { data: { hadronHeight: 9999 } }));
    });
    expect(frame.style.height).toBe("57px");
  });
});

describe("the widget kit", () => {
  it("has a class for each kind of widget", () => {
    for (const c of ["table.tbl", ".stats", ".stat", ".badge", ".badge.ok", ".badge.warn", ".badge.bad", ".meter", ".bar", "ul.check", "dl.kv", ".callout", ".compare"]) {
      expect(kitCss).toContain(c);
    }
  });

  it("uses the theme tokens only, never a hard-coded colour", () => {
    const body = kitCss.replace(/\/\*[\s\S]*?\*\//g, "");
    expect(body).not.toMatch(/#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(|(?<![-\w])(white|black)(?![-\w])/);
    expect(body).toContain("var(--fg)");
  });

  it("ships in every frame", () => {
    expect(widgetSrcDoc("<p>x</p>", { scripts: false, nonce: "n" })).toContain(kitCss);
  });
});
