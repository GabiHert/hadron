import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ChatLine } from "./components/Chat";
import { WIDGET_DEFAULT_HEIGHT, WIDGET_GUESS_MAX, Widget, estimateHeight, isDiagram } from "./components/Widget";
import { message } from "./test/fakeApi";
import appCss from "./styles/app.css?raw";

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
    expect(f.getAttribute("sandbox")).toBe("");
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
