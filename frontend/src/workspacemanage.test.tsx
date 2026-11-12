// Renaming, reordering, archiving and deleting workspaces from the workspace bar.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { deleteSummary } from "./components/WorkspaceManage";
import { card, defaultFixture, mocks, reset, state, ws } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openApp() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  return user;
}
const bar = () => screen.getByRole("tablist", { name: "Workspaces" });
const tab = (name: RegExp | string) => within(bar()).getByRole("tab", { name });
const order = () => within(bar()).getAllByRole("tab").map((t) => t.getAttribute("data-ws-id"));
const press = (key: string, init: Partial<KeyboardEventInit> = {}) => fireEvent.keyDown(document.activeElement ?? window, { key, ...init });

describe("renaming a workspace", () => {
  it("edits the name in place on a double-click, and Enter saves it", async () => {
    const user = await openApp();
    await user.dblClick(tab(/Work/));
    const field = within(bar()).getByRole("textbox", { name: "Rename Work" });
    await user.clear(field);
    await user.type(field, "Beta web{Enter}");
    await waitFor(() => expect(mocks.workspace_update).toHaveBeenCalledWith({ workspace_id: "w2", name: "Beta web" }));
    expect(await within(bar()).findByRole("tab", { name: /Beta web/ })).toHaveFocus();
  });

  it("Esc cancels, and an empty name changes nothing", async () => {
    const user = await openApp();
    await user.dblClick(tab(/Work/));
    await user.type(within(bar()).getByRole("textbox", { name: "Rename Work" }), " two{Escape}");
    expect(tab(/Work/)).toBeInTheDocument();
    await user.dblClick(tab(/Work/));
    await user.clear(within(bar()).getByRole("textbox", { name: "Rename Work" }));
    await user.keyboard("{Enter}");
    expect(mocks.workspace_update).not.toHaveBeenCalled();
  });

  it("starts from the keyboard: E on a focused tab", async () => {
    await openApp();
    tab(/Work/).focus();
    press("e");
    expect(await within(bar()).findByRole("textbox", { name: "Rename Work" })).toHaveFocus();
  });
});

describe("reordering workspaces", () => {
  it("moves the focused tab with ⌥← and ⌥→, keeps the focus on it, and stores the order", async () => {
    await openApp();
    expect(order()).toEqual(["w1", "w2"]);
    tab(/Work/).focus();
    press("ArrowLeft", { altKey: true, code: "ArrowLeft" });
    await waitFor(() => expect(order()).toEqual(["w2", "w1"]));
    expect(mocks.workspace_update).toHaveBeenCalledWith({ workspace_id: "w2", position: 0 });
    await waitFor(() => expect(tab(/Work/)).toHaveFocus());
    expect(state.fx.workspaces.map((w) => w.id)).toEqual(["w2", "w1"]);
    // At the left end there is nowhere to go.
    press("ArrowLeft", { altKey: true, code: "ArrowLeft" });
    expect(mocks.workspace_update).toHaveBeenCalledTimes(1);
    press("L", { shiftKey: true, code: "KeyL" });
    await waitFor(() => expect(order()).toEqual(["w1", "w2"]));
  });

  it("moves a tab dropped onto another into its place", async () => {
    state.fx.workspaces.push(ws("w3", "Gamma", { position: 2 }));
    state.fx.columns.w3 = [];
    state.fx.cards.w3 = [];
    await openApp();
    expect(order()).toEqual(["w1", "w2", "w3"]);
    fireEvent.dragStart(tab(/Gamma/));
    fireEvent.dragOver(tab(/Acme/));
    fireEvent.drop(tab(/Acme/));
    await waitFor(() => expect(order()).toEqual(["w3", "w1", "w2"]));
    expect(mocks.workspace_update).toHaveBeenCalledWith({ workspace_id: "w3", position: 0 });
  });
});

describe("archiving and deleting a workspace", () => {
  it("says what goes with it, prefers archive, and keeps it when archived", async () => {
    state.fx.cards.w2 = [card("k1", "w2", "w2-todo", "billing-fix", 0), card("k2", "w2", "w2-todo", "export", 1)];
    const user = await openApp();
    fireEvent.contextMenu(tab(/Work/));
    await user.click(screen.getByRole("menuitem", { name: "Archive or delete…" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Delete Work?" });
    expect(mocks.workspace_delete).toHaveBeenCalledWith({ workspace_id: "w2", dry_run: true });
    expect(await within(dialog).findByText(/2 cards, 0 sessions, 0 chat messages and 0 questions/)).toBeInTheDocument();
    expect(within(dialog).getByText(/are not touched/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Archive instead" })).toHaveFocus();
    await user.click(within(dialog).getByRole("button", { name: "Archive instead" }));
    await waitFor(() => expect(mocks.workspace_archive).toHaveBeenCalledWith({ workspace_id: "w2" }));
    await waitFor(() => expect(order()).toEqual(["w1"]));
    expect(mocks.workspace_delete).toHaveBeenCalledTimes(1); // only the dry run

    // The archived workspace can be restored from a tab's menu.
    fireEvent.contextMenu(tab(/Acme/));
    await user.click(await screen.findByRole("menuitem", { name: "Restore Work" }));
    await waitFor(() => expect(mocks.workspace_archive).toHaveBeenCalledWith({ workspace_id: "w2", archived: false }));
  });

  it("deletes the active workspace after switching to another, from the Delete key", async () => {
    const user = await openApp();
    await user.click(tab(/Work/));
    await waitFor(() => expect(tab(/Work/)).toHaveAttribute("aria-selected", "true"));
    tab(/Work/).focus();
    press("Delete");
    const dialog = await screen.findByRole("alertdialog", { name: "Delete Work?" });
    await user.click(await within(dialog).findByRole("button", { name: "Delete workspace" }));
    await waitFor(() => expect(mocks.workspace_delete).toHaveBeenCalledWith({ workspace_id: "w2" }));
    await waitFor(() => expect(order()).toEqual(["w1"]));
    expect(tab(/Acme/)).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByText("Workspace Work deleted.")).toBeInTheDocument();
  });

  it("refuses the last workspace, with the reason", async () => {
    const fx = defaultFixture();
    fx.workspaces = fx.workspaces.filter((w) => w.id === "w1");
    reset(fx);
    const user = await openApp();
    fireEvent.contextMenu(tab(/Acme/));
    expect(screen.getByRole("menuitem", { name: "Archive or delete…" })).toBeDisabled();
    await user.keyboard("{Escape}");
    tab(/Acme/).focus();
    press("Delete");
    const dialog = await screen.findByRole("alertdialog", { name: "Delete Acme?" });
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("only one in the tab bar");
    expect(within(dialog).queryByRole("button", { name: "Delete workspace" })).toBeNull();
  });

  it("words the counts", () => {
    expect(deleteSummary({ cards: 1, sessions: 3, running_sessions: 1, messages: 1, questions: 2 })).toBe("1 card, 3 sessions, 1 chat message and 2 questions");
  });
});
