// Regression test for the Terminal tab: with the real xterm.js (not the fake), keys typed into a
// focused terminal must reach PtyWrite as the exact bytes Claude Code expects, in the order typed.
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import { Terminal, routeKey } from "./components/Terminal";

const sent: string[] = [];
let inFlight = 0;
let maxInFlight = 0;
const decode = (b64: string) => new TextDecoder().decode(Uint8Array.from(atob(b64), (c) => c.charCodeAt(0)));

vi.mock("../wailsjs/go/app/App", () => ({
  PtyAttach: vi.fn(async () => "0:"),
  PtyResize: vi.fn(async () => {}),
  // Each write takes a few milliseconds, as a Wails call does; the terminal must not start the next
  // one before this one returns, or the PTY could get them out of order.
  PtyWrite: vi.fn(async (_id: string, b64: string) => {
    inFlight++;
    maxInFlight = Math.max(maxInFlight, inFlight);
    await new Promise((r) => setTimeout(r, 2));
    sent.push(decode(b64));
    inFlight--;
  }),
  Invoke: vi.fn(async (name: string) => (name === "settings_get" ? "{}" : "{}")),
}));
vi.mock("../wailsjs/runtime/runtime", () => ({ EventsOn: () => () => {}, BrowserOpenURL: () => {} }));

beforeAll(() => {
  window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })) as unknown as typeof window.matchMedia;
});

beforeEach(() => {
  sent.length = 0;
  inFlight = 0;
  maxInFlight = 0;
});

async function openTerminal() {
  const view = render(<Terminal sessionId="s1" label="card" />);
  const ta = view.container.querySelector("textarea.xterm-helper-textarea") as HTMLTextAreaElement;
  expect(ta).toBeTruthy();
  ta.focus();
  await act(async () => {});
  return { ta, ...view };
}

// What WebKit fires for one key: keydown, then keypress for a printable key, then keyup.
function press(ta: HTMLTextAreaElement, key: string, keyCode: number, mods: Partial<KeyboardEventInit> = {}) {
  const init = { key, keyCode, which: keyCode, bubbles: true, cancelable: true, ...mods } as KeyboardEventInit;
  const down = fireEvent.keyDown(ta, init);
  if (down && key.length === 1 && !mods.ctrlKey && !mods.metaKey) fireEvent.keyPress(ta, { ...init, charCode: key.charCodeAt(0) } as KeyboardEventInit);
  fireEvent.keyUp(ta, init);
}

const settle = () => waitFor(() => expect(inFlight).toBe(0));

describe("typing into a focused terminal", () => {
  it("sends @, / and /model as typed, and Enter as CR", async () => {
    const { ta } = await openTerminal();
    press(ta, "@", 50, { shiftKey: true, code: "Digit2" });
    press(ta, "/", 191, { code: "Slash" });
    for (const [k, c] of [["m", 77], ["o", 79], ["d", 68], ["e", 69], ["l", 76]] as const) press(ta, k, c, { code: "Key" + k.toUpperCase() });
    press(ta, "Enter", 13, { code: "Enter" });
    await waitFor(() => expect(sent.join("")).toBe("@/model\r"));
    await settle();
    expect(sent).toEqual(["@", "/", "m", "o", "d", "e", "l", "\r"]);
    // One write at a time: Wails runs each call on its own goroutine, so overlapping calls could land out of order.
    expect(maxInFlight).toBe(1);
  });

  it("sends Claude Code's shortcuts: Shift+Tab, Esc, Esc Esc, Ctrl+C/D/R/L/V, arrows, Tab", async () => {
    const { ta } = await openTerminal();
    press(ta, "Tab", 9, { shiftKey: true, code: "Tab" });
    press(ta, "Escape", 27, { code: "Escape" });
    press(ta, "Escape", 27, { code: "Escape" });
    press(ta, "Escape", 27, { code: "Escape" });
    press(ta, "c", 67, { ctrlKey: true, code: "KeyC" });
    press(ta, "d", 68, { ctrlKey: true, code: "KeyD" });
    press(ta, "r", 82, { ctrlKey: true, code: "KeyR" });
    press(ta, "l", 76, { ctrlKey: true, code: "KeyL" });
    press(ta, "v", 86, { ctrlKey: true, code: "KeyV" });
    press(ta, "k", 75, { ctrlKey: true, code: "KeyK" });
    press(ta, "ArrowUp", 38, { code: "ArrowUp" });
    press(ta, "ArrowDown", 40, { code: "ArrowDown" });
    press(ta, "Tab", 9, { code: "Tab" });
    await waitFor(() => expect(sent).toHaveLength(13));
    expect(sent).toEqual(["\x1b[Z", "\x1b", "\x1b", "\x1b", "\x03", "\x04", "\x12", "\x0c", "\x16", "\x0b", "\x1b[A", "\x1b[B", "\t"]);
  });

  it("sends Shift+Enter and Option+Enter as ESC CR, a new line in Claude Code", async () => {
    const { ta } = await openTerminal();
    press(ta, "Enter", 13, { shiftKey: true, code: "Enter" });
    press(ta, "Enter", 13, { altKey: true, code: "Enter" });
    await waitFor(() => expect(sent).toEqual(["\x1b\r", "\x1b\r"]));
  });

  it("sends what a dead key composes (~ then a is ã) once, through the composition events", async () => {
    const { ta } = await openTerminal();
    // US-International and ABNT2: ~ is a dead key; WebKit reports it as "Dead" and composes.
    fireEvent.keyDown(ta, { key: "Dead", keyCode: 229, code: "Quote", shiftKey: true });
    fireEvent.compositionStart(ta, { data: "" });
    ta.value = "~";
    fireEvent.compositionUpdate(ta, { data: "~" });
    fireEvent.keyUp(ta, { key: "Dead", keyCode: 229 });
    fireEvent.keyDown(ta, { key: "ã", keyCode: 229, code: "KeyA" });
    ta.value = "ã";
    fireEvent.compositionUpdate(ta, { data: "ã" });
    fireEvent.compositionEnd(ta, { data: "ã" });
    fireEvent.keyUp(ta, { key: "a", keyCode: 65 });
    await waitFor(() => expect(sent).toEqual(["ã"]));
    // And a plain key after it still goes through.
    press(ta, "/", 191, { code: "Slash" });
    await waitFor(() => expect(sent).toEqual(["ã", "/"]));
  });

  it("leaves Cmd+letter to the app and the webview", async () => {
    const { ta } = await openTerminal();
    press(ta, "k", 75, { metaKey: true, code: "KeyK" });
    press(ta, "c", 67, { metaKey: true, code: "KeyC" });
    press(ta, "v", 86, { metaKey: true, code: "KeyV" });
    await new Promise((r) => setTimeout(r, 10));
    expect(sent).toEqual([]);
  });

  it("types pasted text and drops nothing on the way", async () => {
    const { ta } = await openTerminal();
    const data = { getData: (t: string) => (t === "text/plain" ? "/model sonnet" : ""), items: [], files: [], types: ["text/plain"] };
    fireEvent.paste(ta, { clipboardData: data });
    await waitFor(() => expect(sent.join("")).toBe("/model sonnet"));
  });

  it("shows the Attach button and the Option as Meta switch", async () => {
    await openTerminal();
    expect(screen.getByRole("button", { name: /Attach/ })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Option as Meta" })).not.toBeChecked();
  });
});

describe("routeKey", () => {
  it("keeps everything for the program except Cmd+letter, and maps Shift+Enter", () => {
    expect(routeKey({ type: "keydown", key: "@", shiftKey: true })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "/" })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "Tab", shiftKey: true })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "Escape" })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "k", ctrlKey: true })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "Dead" })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "k", metaKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "v", metaKey: true })).toBe("app");
    expect(routeKey({ type: "keydown", key: "ArrowLeft", metaKey: true })).toBe("terminal");
    expect(routeKey({ type: "keydown", key: "Enter", shiftKey: true })).toEqual({ send: "\x1b\r" });
    expect(routeKey({ type: "keydown", key: "Enter", shiftKey: true, isComposing: true })).toBe("terminal");
    expect(routeKey({ type: "keyup", key: "k", metaKey: true })).toBe("terminal");
  });
});
