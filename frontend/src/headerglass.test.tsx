// The workspace tab menu under its tab, the tab strip keeping its place, the newest chat line above
// the glass composer, the glass top bar, the Settings list scrolling, and the restore nudge switch.
import css from "./styles/app.css?raw";
import { fireEvent, render, renderHook, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { GeneralTab } from "./components/GeneralTab";
import { revealInStrip, tabMenuPlace } from "./components/WorkspaceManage";
import { atEnd, useGlassOverlay } from "./state/useGlassOverlay";
import { mocks, reset, state, ws } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());
afterEach(() => vi.restoreAllMocks());

const rect = (left: number, top: number, width: number, height: number) => ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top, toJSON: () => ({}) }) as DOMRect;

async function openApp() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  return user;
}
const tab = (name: RegExp) => within(screen.getByRole("tablist", { name: "Workspaces" })).getByRole("tab", { name });

describe("the workspace tab menu", () => {
  it("sits under its tab, left-aligned, in the header's coordinates, inside the header", () => {
    // A tab at x 300 in a header 1200 wide: the menu starts at 300, 4px below the tab.
    expect(tabMenuPlace(rect(300, 6, 90, 30), rect(0, 0, 1200, 44), 1200, 180)).toEqual({ left: 300, top: 40 });
    // Zoomed 125%: client pixels are scaled back to the header's.
    expect(tabMenuPlace(rect(375, 7.5, 112, 37.5), rect(0, 0, 1500, 55), 1200, 180)).toEqual({ left: 300, top: 40 });
    // Near the right edge it stays inside the header.
    expect(tabMenuPlace(rect(1150, 6, 40, 30), rect(0, 0, 1200, 44), 1200, 180).left).toBe(1200 - 180 - 8);
  });

  it("is a small popover anchored in the header, not stretched across the window", async () => {
    await openApp();
    fireEvent.contextMenu(tab(/Work/));
    const menu = await screen.findByRole("menu", { name: "Actions for Work" });
    await waitFor(() => expect(menu.style.position).toBe("absolute"));
    expect(menu.style.right).toBe("auto");
    expect(menu.closest("header")).not.toBeNull();
    expect(menu).toHaveClass("wstabmenu");
    expect(css).toMatch(/\.wstabmenu\{width:max-content/);
  });

  it("lists the archived workspaces in their own labelled section", async () => {
    state.fx.workspaces.push(ws("w3", "Personal", { position: 2, archived: true }));
    const user = await openApp();
    fireEvent.contextMenu(tab(/Work/));
    const group = await screen.findByRole("group", { name: "Archived workspaces" });
    expect(within(group).getByText("Archived workspaces")).toBeInTheDocument();
    await user.click(within(group).getByRole("menuitem", { name: "Restore Personal" }));
    await waitFor(() => expect(mocks.workspace_archive).toHaveBeenCalledWith({ workspace_id: "w3", archived: false }));
  });

  it("has no archived section when nothing is archived", async () => {
    await openApp();
    fireEvent.contextMenu(tab(/Work/));
    await screen.findByRole("menu", { name: "Actions for Work" });
    expect(screen.queryByRole("group", { name: "Archived workspaces" })).toBeNull();
  });
});

describe("renaming in place", () => {
  it("focuses the field without scrolling the window", async () => {
    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    const user = await openApp();
    await user.dblClick(tab(/Work/));
    const field = await screen.findByRole("textbox", { name: "Rename Work" });
    expect(field).toHaveFocus();
    expect(focus.mock.contexts.some((el, i) => el === field && (focus.mock.calls[i][0] as FocusOptions | undefined)?.preventScroll === true)).toBe(true);
  });

  it("scrolls only the tab strip, and only when the field is out of its view", () => {
    const bar = document.createElement("div");
    bar.className = "wsbar";
    const field = document.createElement("input");
    bar.appendChild(field);
    bar.getBoundingClientRect = () => rect(100, 0, 300, 30);
    field.getBoundingClientRect = () => rect(150, 0, 80, 22);
    revealInStrip(field);
    expect(bar.scrollLeft).toBe(0);
    field.getBoundingClientRect = () => rect(380, 0, 80, 22);
    revealInStrip(field);
    expect(bar.scrollLeft).toBe(64);
  });

  it("nothing above the tab strip can be scrolled sideways", () => {
    expect(css).toContain("html,body,#root,.win{overflow:clip}");
    expect(css).toMatch(/\.top\{position:relative;z-index:30;overflow-x:clip/);
  });
});

describe("the newest line above the glass composer", () => {
  function panel(composerHeight: { h: number }) {
    const el = document.createElement("aside");
    const head = document.createElement("div");
    head.className = "ghead";
    Object.defineProperty(head, "offsetHeight", { get: () => 50 });
    const wrap = document.createElement("div");
    wrap.className = "glasswrap";
    const chat = document.createElement("div");
    chat.className = "chat";
    wrap.appendChild(chat);
    const foot = document.createElement("div");
    foot.className = "composer";
    Object.defineProperty(foot, "offsetHeight", { get: () => composerHeight.h });
    el.append(head, wrap, foot);
    let top = 0;
    const size = { scroll: 2000, client: 600 };
    Object.defineProperty(chat, "scrollHeight", { get: () => size.scroll });
    Object.defineProperty(chat, "clientHeight", { get: () => size.client });
    Object.defineProperty(chat, "scrollTop", { get: () => top, set: (v: number) => (top = Math.min(v, size.scroll - size.client)) });
    document.body.appendChild(el);
    return { el, chat, size };
  }

  it("measures the composer and lands on the newest line", () => {
    const h = { h: 90 };
    const { el, chat } = panel(h);
    renderHook(() => useGlassOverlay({ current: el }, true));
    expect(el.style.getPropertyValue("--gf")).toBe("90px");
    expect(el.style.getPropertyValue("--gh")).toBe("50px");
    expect(atEnd(chat)).toBe(true);
  });

  it("re-pins when the composer grows while the newest line shows, and leaves a reader alone", () => {
    const h = { h: 90 };
    const { el, chat, size } = panel(h);
    let fire = () => {};
    vi.stubGlobal("ResizeObserver", class {
      constructor(cb: () => void) { fire = cb; }
      observe() {}
      disconnect() {}
    });
    renderHook(() => useGlassOverlay({ current: el }, true));
    // The composer grows by 40px (a longer message); the spacer grows the scroll range with it.
    h.h = 130;
    size.scroll += 40;
    fire();
    expect(el.style.getPropertyValue("--gf")).toBe("130px");
    expect(atEnd(chat)).toBe(true);
    // A reader who scrolled up keeps their place.
    chat.scrollTop = 200;
    h.h = 170;
    size.scroll += 40;
    fire();
    expect(chat.scrollTop).toBe(200);
    vi.unstubAllGlobals();
  });

  it("ends the conversation with a spacer as tall as the composer", () => {
    expect(css).toMatch(/\.glasswrap>:is\(\.chat,\.cbody\)::after\{content:"";flex:none;height:calc\(var\(--gf,0px\) \+ 2px\)/);
  });
});

describe("the glass top bar", () => {
  it("is frosted and floats over the window with glass on, and solid under Reduce transparency", () => {
    expect(css).toMatch(/:root\[data-glass="on"\] \.top\{position:absolute;top:0;left:0;right:0;[^}]*backdrop-filter:blur\(20px\)/);
    expect(css).toMatch(/prefers-reduced-transparency: reduce\)\{\s*:root\[data-glass="on"\] \.top\{background:var\(--side\);-webkit-backdrop-filter:none;backdrop-filter:none\}/);
    // The chats run under it; everything else starts below it.
    expect(css).toContain(':root[data-glass="on"] .shell>*{border-top:var(--topbar,0px) solid transparent}');
    expect(css).toContain(':root[data-glass="on"] :is(.ppanel,.cpanel)[data-overlay]>.ghead{top:var(--topbar,0px)}');
    // The tab menu stays opaque on it.
    expect(css).toContain(':root[data-glass="on"] .top .wmenu-list{background:var(--card)}');
  });

  it("keeps the delete dialog out of the bar, so the glass cannot trap it", async () => {
    const user = await openApp();
    fireEvent.contextMenu(tab(/Work/));
    await user.click(await screen.findByRole("menuitem", { name: "Archive or delete…" }));
    const dlg = await screen.findByRole("alertdialog", { name: "Delete Work?" });
    expect(dlg.closest("header")).toBeNull();
  });
});

describe("Settings in a short window", () => {
  it("scrolls the section list on its own and keeps the selected section in view", async () => {
    const seen: string[] = [];
    Element.prototype.scrollIntoView = vi.fn(function (this: Element) {
      seen.push(this.textContent ?? "");
    });
    const user = await openApp();
    await user.click(screen.getByRole("button", { name: "Settings" }));
    const nav = await screen.findByRole("navigation", { name: "Settings sections" });
    await user.click(within(nav).getByRole("button", { name: "Feedback" }));
    await waitFor(() => expect(seen).toContain("Feedback"));
    expect(css).toMatch(/\.snav\{overflow-y:auto;min-height:0/);
  });
});

describe("Settings → General: asking working sessions to continue", () => {
  const box = () => screen.findByRole("checkbox", { name: "Ask sessions that were working to continue" });

  it("is on by default and saves sessions.restore_nudge", async () => {
    render(<GeneralTab toast={vi.fn()} />);
    expect(await box()).toBeChecked();
    await userEvent.click(await box());
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "sessions.restore_nudge": false } }));
  });

  it("is off and disabled when sessions are not resumed at all", async () => {
    state.fx.settings["sessions.restore_on_launch"] = false;
    render(<GeneralTab toast={vi.fn()} />);
    await waitFor(async () => expect(await box()).toBeDisabled());
    expect(await box()).not.toBeChecked();
  });
});
