import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { freeRow, mocks, reset, state } from "./test/fakeApi";
import { FakeXTerm, resetTerm } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

beforeEach(() => {
  reset();
  resetTerm();
});

async function openSessions() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await userEvent.click(screen.getByRole("tab", { name: /Sessions/ }));
  return screen.findByRole("heading", { name: "Sessions" });
}

const tabNames = () => within(screen.getByRole("tablist", { name: "Session tabs" })).queryAllByRole("tab").map((t) => t.textContent);
const press = (key: string, over: Partial<KeyboardEventInit> = {}) => fireEvent.keyDown(document.activeElement ?? document.body, { key, metaKey: true, ...over });

describe("the Sessions space", () => {
  it("is a main view of its own that hides the planner and card panels", async () => {
    await openSessions();
    expect(mocks.ui_main_view).toHaveBeenCalledWith({ workspace_id: "w1", view: "sessions" });
    expect(screen.queryByRole("region", { name: /planner/i })).toBeNull();
    expect(screen.getByText(/No sessions yet/)).toBeInTheDocument();
  });

  it("starts a login shell with ⌘T, opening it in a tab with a terminal", async () => {
    await openSessions();
    press("t");
    await waitFor(() => expect(mocks.free_session_start).toHaveBeenCalledWith({ kind: "shell" }));
    await waitFor(() => expect(tabNames()).toEqual(["zsh · code"]));
    expect(FakeXTerm.instances.filter((t) => !t.disposed)).toHaveLength(1);
  });

  it("offers claude in the same New menu", async () => {
    const user = userEvent.setup();
    await openSessions();
    await user.click(screen.getByRole("button", { name: "More ways to start a session" }));
    await user.click(screen.getByRole("menuitem", { name: /New claude session/ }));
    await waitFor(() => expect(mocks.free_session_start).toHaveBeenCalledWith({ kind: "claude" }));
  });

  it("splits the focused pane, mounts only the shown tab's terminals, and closes panes", async () => {
    state.free = [freeRow("a", { name: "api" }), freeRow("b", { name: "web" })];
    await openSessions();
    await waitFor(() => expect(tabNames()).toEqual(["api", "web"]));
    // Only the shown tab (the newest) has a terminal: the other sends no output.
    expect(FakeXTerm.instances.filter((t) => !t.disposed)).toHaveLength(1);
    await userEvent.click(screen.getAllByRole("button", { name: "Split right" })[0]);
    await waitFor(() => expect(tabNames()).toEqual(["api", "zsh · code +1"]));
    await waitFor(() => expect(FakeXTerm.instances.filter((t) => !t.disposed)).toHaveLength(2));
    expect(mocks.free_session_start).toHaveBeenCalledWith({ kind: "shell", cwd: "/Users/you/code" });

    await userEvent.click(within(screen.getByRole("region", { name: "Session web" })).getByRole("button", { name: "Close the pane" }));
    await waitFor(() => expect(mocks.free_session_stop).toHaveBeenCalledWith({ session: "b", close: true }));
    await waitFor(() => expect(tabNames()).toEqual(["api", "zsh · code"]));
  });

  it("asks before closing a pane whose program is at work", async () => {
    state.free = [freeRow("a", { program: "vim" })];
    await openSessions();
    await userEvent.click(await screen.findByRole("button", { name: "Close the pane" }));
    expect(await screen.findByRole("alertdialog")).toHaveTextContent("It is running vim");
    expect(mocks.free_session_stop).not.toHaveBeenCalled();
  });

  it("renames a session in place and keeps tabs and splits in a setting", async () => {
    const user = userEvent.setup();
    state.free = [freeRow("a")];
    await openSessions();
    await user.dblClick(await screen.findByText("zsh · code", { selector: "strong" }));
    const input = screen.getByLabelText("Session name");
    await user.clear(input);
    await user.type(input, "builds{Enter}");
    expect(mocks.free_session_update).toHaveBeenCalledWith({ session: "a", name: "builds" });
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "sessions.layout": expect.objectContaining({ tabs: [expect.objectContaining({ root: { pane: "a" } })] }) } }), { timeout: 2000 });
  });

  it("makes a card of a session without stopping it", async () => {
    const user = userEvent.setup();
    state.free = [freeRow("a", { kind: "claude", program: "claude", name: "Billing fix" })];
    await openSessions();
    await user.click(await screen.findByRole("button", { name: /Make a card/ }));
    const nick = screen.getByLabelText("Card nickname");
    expect(nick).toHaveValue("billing-fix");
    await user.click(screen.getByRole("button", { name: "Make the card" }));
    await waitFor(() => expect(mocks.free_session_to_card).toHaveBeenCalledWith({ session: "a", workspace_id: "w1", nick: "billing-fix" }));
    expect(mocks.free_session_stop).not.toHaveBeenCalled();
  });

  it("shows an ended session with a way to start it again", async () => {
    state.free = [freeRow("a", { kind: "claude", running: false, status: "exited", liveness: "ended", exit_code: 1 })];
    await openSessions();
    await userEvent.click(await screen.findByRole("button", { name: "Resume" }));
    expect(mocks.free_session_restart).toHaveBeenCalledWith({ session: "a" });
  });

  it("clears the focused terminal with ⌘K, even when the menu sends it as the palette", async () => {
    state.free = [freeRow("a")];
    await openSessions();
    await waitFor(() => expect(FakeXTerm.instances.filter((t) => !t.disposed)).toHaveLength(1));
    const term = FakeXTerm.instances.find((t) => !t.disposed)!;
    const host = screen.getByRole("group", { name: /terminal$/ });
    act(() => host.focus());
    fireEvent.keyDown(host, { key: "k", metaKey: true });
    expect(term.cleared).toBe(1);
    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
  });
});
