import { act, render, waitFor } from "@testing-library/react";
import { Terminal } from "./components/Terminal";
import { FakeFit, FakeXTerm, emitPty, ptyMock, resetTerm, resolveAttach, subscribed } from "./test/fakeTerm";
import { publishZoomForTests, resetZoomForTests } from "./state/useZoom";
import { routeKey } from "./components/Terminal";

vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);

beforeEach(() => {
  resetTerm();
  resetZoomForTests();
});

const term = () => FakeXTerm.instances[FakeXTerm.instances.length - 1];

describe("Terminal", () => {
  it("follows the zoom: a bigger font in an unzoomed host, then a refit that reaches the PTY", async () => {
    const view = render(<Terminal sessionId="s1" label="planner" />);
    await act(async () => resolveAttach(""));
    expect(term().options.fontSize).toBe(12);
    const fit = FakeFit.instances[FakeFit.instances.length - 1];
    const before = fit.fit.mock.calls.length;
    // jsdom drops the zoom property, so watch the call that sets it.
    const set = vi.spyOn(CSSStyleDeclaration.prototype, "setProperty");
    act(() => publishZoomForTests(1.5));
    expect(term().options.fontSize).toBe(18);
    const host = view.container.querySelector(".xterm-host") as HTMLElement;
    const zoomCall = set.mock.calls.find((c) => c[0] === "zoom");
    expect(parseFloat(String(zoomCall?.[1]))).toBeCloseTo(1 / 1.5);
    expect(host).toBeInTheDocument();
    expect(fit.fit.mock.calls.length).toBeGreaterThan(before);
    // The refit's new size goes out through onResize, as any resize does.
    act(() => term().resizeHandlers.forEach((h) => h({ cols: 66, rows: 20 })));
    expect(ptyMock.resize).toHaveBeenLastCalledWith("s1", 66, 20);
    act(() => publishZoomForTests(1));
    expect(term().options.fontSize).toBe(12);
    expect(set.mock.calls[set.mock.calls.length - 1]).toEqual(["zoom", ""]);
    set.mockRestore();
  });

  it("a terminal opened while zoomed starts at the zoomed font", () => {
    publishZoomForTests(0.8);
    render(<Terminal sessionId="s1" label="planner" />);
    expect(term().options.fontSize).toBe(10);
  });

  it("lets the reserved ⌘ chords out to Hadron and keeps the rest for the program", () => {
    expect(routeKey({ type: "keydown", key: "Escape", metaKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "=", metaKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "-", metaKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "]", metaKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "1", code: "Digit1", metaKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "ArrowLeft", code: "ArrowLeft", metaKey: true, altKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "ArrowLeft", metaKey: true })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "Escape" })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "]", ctrlKey: true })).toBe("terminal");
  });

  it("replays the scrollback, then applies live output without repeating what the replay holds", async () => {
    render(<Terminal sessionId="s1" label="planner" />);
    expect(ptyMock.attach).toHaveBeenCalledWith("s1");
    expect(subscribed("s1")).toBe(true);
    // Output that arrives while the replay is still in flight is held back...
    emitPty("s1", 0, "abcde");
    emitPty("s1", 5, "fg");
    expect(term().text()).toBe("");
    // ...then written after it: the replay ends at byte 3, so the first batch (bytes 0 to 5) adds only "de".
    await act(async () => resolveAttach("abc", 3));
    expect(term().text()).toBe("abcdefg");
    // A batch the replay already covers is dropped; the next one is applied.
    emitPty("s1", 5, "fg");
    emitPty("s1", 7, "h");
    expect(term().text()).toBe("abcdefgh");
  });

  it("sends typing to PtyWrite and size changes to PtyResize, and reports its size after attaching", async () => {
    render(<Terminal sessionId="s1" label="planner" />);
    await act(async () => resolveAttach(""));
    expect(ptyMock.resize).toHaveBeenCalledWith("s1", 100, 30);
    act(() => term().dataHandlers.forEach((h) => h("echo hi\r")));
    expect(ptyMock.write).toHaveBeenCalledWith("s1", "echo hi\r");
    act(() => term().resizeHandlers.forEach((h) => h({ cols: 90, rows: 20 })));
    expect(ptyMock.resize).toHaveBeenLastCalledWith("s1", 90, 20);
  });

  it("themes itself from the design tokens and focuses", () => {
    document.documentElement.style.setProperty("--term", "#101010");
    document.documentElement.style.setProperty("--tfg", "#fefefe");
    render(<Terminal sessionId="s1" label="planner" />);
    expect(term().options.theme).toMatchObject({ background: "#101010", foreground: "#fefefe" });
    expect(term().focused).toBe(true);
    document.documentElement.style.removeProperty("--term");
    document.documentElement.style.removeProperty("--tfg");
  });

  it("leaves Esc to the program, but Shift+Esc exits full screen and Cmd+letter goes to the app", () => {
    const exit = vi.fn();
    render(<Terminal sessionId="s1" label="card" onShiftEscape={exit} />);
    const h = term().keyHandler;
    expect(h({ type: "keydown", key: "Escape" })).toBe(true);
    expect(exit).not.toHaveBeenCalled();
    expect(h({ type: "keydown", key: "Escape", shiftKey: true })).toBe(false);
    expect(exit).toHaveBeenCalledTimes(1);
    expect(h({ type: "keydown", key: "k", metaKey: true })).toBe(false);
    expect(h({ type: "keydown", key: "ArrowLeft", metaKey: true })).toBe(true);
    expect(h({ type: "keyup", key: "k", metaKey: true })).toBe(true);
  });

  it("mounts a fresh terminal for a new session and cleans up the old one", async () => {
    const { rerender, unmount } = render(<Terminal sessionId="s1" label="card" />);
    const first = term();
    rerender(<Terminal sessionId="s2" label="card" />);
    expect(first.disposed).toBe(true);
    expect(subscribed("s1")).toBe(false);
    expect(subscribed("s2")).toBe(true);
    expect(FakeXTerm.instances).toHaveLength(2);
    // A replay for the old session that lands late must not write into the new terminal.
    await act(async () => resolveAttach("late", 4, 0));
    expect(term().text()).toBe("");
    unmount();
    expect(term().disposed).toBe(true);
    await waitFor(() => expect(subscribed("s2")).toBe(false));
  });
});
