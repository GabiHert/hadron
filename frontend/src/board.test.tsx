import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { card, col, defaultFixture, mocks, reset, ws } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
}

const column = (name: string) => screen.getByRole("region", { name: `Column ${name}` });

describe("column rename", () => {
  it("commits on Enter through column_update and keeps the id", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(within(column("Working")).getByRole("button", { name: "Working" }));
    const input = screen.getByRole("textbox", { name: "Column name" });
    await user.clear(input);
    await user.type(input, "Doing{Enter}");
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "working", name: "Doing" }));
    expect(mocks.column_update).toHaveBeenCalledTimes(1);
    expect(await screen.findByRole("region", { name: "Column Doing" })).toBeInTheDocument();
  });

  it("cancels on Escape without calling the backend", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(within(column("Working")).getByRole("button", { name: "Working" }));
    await user.type(screen.getByRole("textbox", { name: "Column name" }), "zzz{Escape}");
    expect(screen.queryByRole("textbox", { name: "Column name" })).not.toBeInTheDocument();
    expect(mocks.column_update).not.toHaveBeenCalled();
    expect(column("Working")).toBeInTheDocument();
  });

  it("ignores a blank name", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(within(column("Working")).getByRole("button", { name: "Working" }));
    const input = screen.getByRole("textbox", { name: "Column name" });
    await user.clear(input);
    await user.type(input, "   {Enter}");
    expect(mocks.column_update).not.toHaveBeenCalled();
    expect(column("Working")).toBeInTheDocument();
  });
});

describe("WIP display", () => {
  it("shows n / wip, warns when over, and omits the limit when it is 0", async () => {
    const fx = defaultFixture();
    fx.columns.w1[1].wip = 4;
    reset(fx);
    await openApp();
    expect(screen.getByTestId("count-working")).toHaveTextContent("3 / 4");
    expect(screen.getByTestId("count-working")).not.toHaveClass("warn");
    expect(screen.getByTestId("count-done")).toHaveTextContent(/^0$/);
  });

  it("uses the warn pill over the limit", async () => {
    const fx = defaultFixture();
    fx.columns.w1[1].wip = 2;
    reset(fx);
    await openApp();
    expect(screen.getByTestId("count-working")).toHaveTextContent("3 / 2");
    expect(screen.getByTestId("count-working")).toHaveClass("warn");
  });

  it("gives a needs-you column with cards the bad pill", async () => {
    await openApp();
    expect(screen.getByTestId("count-needs")).toHaveClass("bad");
    expect(screen.getByTestId("count-done")).not.toHaveClass("bad");
  });
});

describe("column menu", () => {
  it("changes role and WIP through column_update", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Column options for Done" }));
    await user.selectOptions(screen.getByRole("combobox"), "blocked");
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "done", role: "blocked" }));
    const wip = document.getElementById("menu-wip-done") as HTMLInputElement;
    fireEvent.change(wip, { target: { value: "5" } });
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "done", wip: 5 }));
  });

  it("deletes through column_delete and toasts where the cards went", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Column options for Working" }));
    await user.click(screen.getByRole("button", { name: "Delete column" }));
    await waitFor(() => expect(mocks.column_delete).toHaveBeenCalledWith({ column_id: "working" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Column deleted. Its cards moved to Needs you.");
  });

  it("reorders and folds", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Column options for Working" }));
    await user.click(screen.getByRole("button", { name: "Move →" }));
    await waitFor(() => expect(mocks.column_reorder).toHaveBeenCalledWith({ workspace_id: "w1", column_ids: ["needs", "done", "working"] }));
    await user.click(screen.getByRole("button", { name: "Column options for Done" }));
    await user.click(screen.getByRole("button", { name: "Fold" }));
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "done", fold: true }));
    expect(await screen.findByRole("button", { name: "Unfold Done" })).toBeInTheDocument();
  });

  it("adds a column and goes straight to renaming it", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: /Column$/ }));
    await waitFor(() => expect(mocks.column_create).toHaveBeenCalledWith({ workspace_id: "w1", name: "New column", role: "active" }));
    expect(await screen.findByRole("textbox", { name: "Column name" })).toHaveValue("New column");
  });
});

describe("drag and drop", () => {
  it("drops a card on another card in another column with card_move and the index", async () => {
    await openApp();
    const dt = { setData: vi.fn(), effectAllowed: "" };
    const dragged = screen.getByRole("button", { name: "Open search-console" });
    const target = screen.getByRole("button", { name: "Open dedupe-records" }); // index 1 in Working
    fireEvent.dragStart(dragged, { dataTransfer: dt });
    fireEvent.dragOver(target, { dataTransfer: dt });
    fireEvent.drop(target, { dataTransfer: dt, clientY: 0 });
    await waitFor(() => expect(mocks.card_move).toHaveBeenCalledWith({ card_id: "c1", column_id: "working", position: 1 }));
    const working = column("Working");
    await waitFor(() => expect(within(working).getByRole("button", { name: "Open search-console" })).toBeInTheDocument());
  });

  it("drops on empty column space and sends no position (the bottom)", async () => {
    await openApp();
    const dt = { setData: vi.fn(), effectAllowed: "" };
    fireEvent.dragStart(screen.getByRole("button", { name: "Open api-timeouts" }), { dataTransfer: dt });
    fireEvent.drop(column("Done"), { dataTransfer: dt });
    await waitFor(() => expect(mocks.card_move).toHaveBeenCalledWith({ card_id: "c2", column_id: "done" }));
  });

  it("moves the card before the drop reaches the backend (optimistic)", async () => {
    let release: () => void = () => {};
    mocks.card_move.mockImplementationOnce(() => new Promise((r) => { release = () => r({} as never); }));
    await openApp();
    const dt = { setData: vi.fn(), effectAllowed: "" };
    fireEvent.dragStart(screen.getByRole("button", { name: "Open api-timeouts" }), { dataTransfer: dt });
    fireEvent.drop(column("Done"), { dataTransfer: dt });
    expect(within(column("Done")).getByRole("button", { name: "Open api-timeouts" })).toBeInTheDocument();
    await act(async () => release());
  });
});

describe("shell", () => {
  it("renders workspace tabs with needs-you counts, columns and cards", async () => {
    await openApp();
    const tabs = screen.getByRole("tablist", { name: "Workspaces" });
    expect(within(tabs).getByRole("tab", { name: /Acme/ })).toHaveAttribute("aria-selected", "true");
    expect(within(tabs).getByRole("tab", { name: /Work/ })).toHaveTextContent("1");
    expect(screen.getByRole("button", { name: "Open search-console" })).toBeInTheDocument();
    expect(screen.queryByTestId("version")).toBeNull();
  });

  it("opens the card panel when a card is clicked and switches workspaces", async () => {
    const user = userEvent.setup();
    await openApp();
    expect(screen.getByText("Pick a card to open its chat and terminal.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Open api-timeouts" }));
    const panel = await screen.findByRole("complementary", { name: "Card api-timeouts" });
    expect(within(panel).getByText("ACME-541")).toBeInTheDocument();
    await user.click(within(panel).getByRole("tab", { name: "Context" }));
    expect(within(panel).getByText("Working (active)")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: /Work/ }));
    await waitFor(() => expect(mocks.workspace_switch).toHaveBeenCalledWith({ workspace_id: "w2" }));
    expect(await screen.findByRole("heading", { name: "Work" })).toBeInTheDocument();
  });

  it("collapses the planner through ui_panel_collapse", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Collapse the planner" }));
    await waitFor(() => expect(mocks.ui_panel_collapse).toHaveBeenCalledWith({ workspace_id: "w1", panel: "planner", collapsed: true }));
    expect(await screen.findByRole("navigation", { name: "Planner, collapsed" })).toBeInTheDocument();
  });

  it("creates a session card with a kebab-case nick", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "New session" }));
    await user.type(screen.getByLabelText("Nickname"), "Billing Fix");
    await waitFor(() => expect(screen.getByLabelText("What this profile runs")).toHaveTextContent("/brief billing-fix"));
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(mocks.card_create).toHaveBeenCalledWith({ workspace_id: "w1", nick: "billing-fix", column_id: "working", launch_profile: "brief" }));
  });

  it("creates and switches to a new workspace", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "New workspace" }));
    await user.type(screen.getByLabelText("Name"), "Side");
    await user.click(screen.getByRole("button", { name: "Create workspace" }));
    await waitFor(() => expect(mocks.workspace_create).toHaveBeenCalled());
    expect(mocks.workspace_create.mock.calls[0][0]).toMatchObject({ name: "Side", template: "planner" });
    await waitFor(() => expect(mocks.workspace_switch).toHaveBeenCalledWith({ workspace_id: "new-Side" }));
  });

  it("filters cards client-side with the search box", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.type(screen.getByLabelText("Search cards"), "stale");
    expect(screen.queryByRole("button", { name: "Open dedupe-records" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open stale-warning" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Clear" }));
    expect(screen.getByRole("button", { name: "Open dedupe-records" })).toBeInTheDocument();
  });

  it("toggles the theme through settings_update and opens the palette with the keyboard", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Switch light or dark" }));
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { theme: "dark" } }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    fireEvent.keyDown(window, { key: "k", metaKey: true });
    const input = await screen.findByPlaceholderText("Jump to a card, workspace or command");
    await user.type(input, "new session{Enter}");
    expect(await screen.findByRole("dialog", { name: "New session" })).toBeInTheDocument();
  });

  it("shows the Settings columns tab with the extendable nav", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Settings" }));
    const nav = screen.getByRole("navigation", { name: "Settings sections" });
    for (const t of ["Harness", "Columns", "Rules", "Integrations", "MCP server"]) expect(within(nav).getByRole("button", { name: t })).toBeInTheDocument();
    await user.click(within(nav).getByRole("button", { name: "Integrations" }));
    expect(await screen.findByRole("button", { name: "Ask the planner to build one" })).toBeInTheDocument();
  });

  it("hides archived workspaces from the tabs", async () => {
    const fx = defaultFixture();
    fx.workspaces.push(ws("w3", "Old", { archived: true, position: 2 }));
    fx.columns.w3 = [col("x", "w3", "X", "", 0)];
    fx.cards.w3 = [card("c9", "w3", "x", "z", 0)];
    reset(fx);
    await openApp();
    expect(screen.queryByRole("tab", { name: /Old/ })).not.toBeInTheDocument();
  });
});
