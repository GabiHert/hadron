import { cleanup, createEvent, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { GROUP_DRAG, dropPosition } from "./components/CardGroup";
import { dashBlock, mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

const rows = (ids: string[], last: Record<string, string> = {}) =>
  ids.map((id) => ({ id, nick: id, key: "", title: "", column: "", column_id: "", stage: "", liveness: "idle", needs_you: false, question: "", last: last[id] ?? "", last_kind: "report", last_author: "session", last_at: "" }));

beforeEach(() => {
  reset();
  state.dash = [
    dashBlock("ga", { type: "group", title: "In flight", source: { cards: ["c1", "c2"] }, data: { query: "group.cards", shape: "rows", groups: [], rows: rows(["c1", "c2"], { c2: "Fixed the retry loop" }) } }),
    dashBlock("gb", { type: "group", title: "Later", position: 1, source: { cards: ["c3"] }, data: { query: "group.cards", shape: "rows", groups: [], rows: rows(["c3"]) } }),
  ];
});
afterEach(cleanup);

async function openDashboard() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("tab", { name: /Dashboard/ }));
  await screen.findByRole("region", { name: "In flight" });
  return user;
}

function dataTransfer() {
  const data: Record<string, string> = {};
  return {
    data,
    get types() {
      return Object.keys(data);
    },
    setData: (k: string, v: string) => {
      data[k] = v;
    },
    getData: (k: string) => data[k] ?? "",
    dropEffect: "move",
    effectAllowed: "all",
  };
}

describe("card groups", () => {
  it("shows each card live from the board with its column, liveness, needs-you and last line", async () => {
    await openDashboard();
    const g = screen.getByRole("list", { name: "Cards in In flight" });
    const needs = within(g).getByRole("button", { name: "Open search-console, needs you" });
    expect(within(needs).getByText("ACME-508")).toBeInTheDocument();
    expect(within(needs).getByText("needs you")).toBeInTheDocument();
    const api = within(g).getByRole("button", { name: "Open api-timeouts" });
    expect(within(api).getByText("working")).toBeInTheDocument();
    expect(within(api).getByText("Working")).toBeInTheDocument();
    expect(within(api).getByText("Fixed the retry loop")).toBeInTheDocument();
  });

  it("opens a card's chat in the card panel without leaving the dashboard", async () => {
    const user = await openDashboard();
    await user.click(screen.getByRole("button", { name: "Open api-timeouts" }));
    const panel = await screen.findByRole("complementary", { name: "Card api-timeouts" });
    expect(within(panel).getByRole("tab", { name: /Conversation/ })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => expect(document.activeElement?.id).toBe("card-msg-c2"));
    expect(screen.getByRole("region", { name: "In flight" })).toBeInTheDocument();
  });

  it("makes a group from Add block and adds cards with the picker", async () => {
    const user = await openDashboard();
    await user.click(screen.getByRole("button", { name: "Add block" }));
    expect(screen.getByLabelText("Block type")).toHaveValue("group");
    await user.type(screen.getByLabelText("Title"), "Reviews");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(mocks.card_group_create).toHaveBeenCalledWith({ workspace_id: "w1", title: "Reviews" });
    const picker = await screen.findByRole("dialog", { name: "Add cards to the group" });
    await user.type(within(picker).getByLabelText("Filter cards"), "dedupe{Enter}");
    expect(mocks.card_group_add).toHaveBeenCalledWith({ group: "g3", cards: ["c3"] });
  });

  it("moves, copies and removes a card from its menu, and renames a group", async () => {
    const user = await openDashboard();
    await user.click(screen.getByRole("button", { name: "Actions for api-timeouts" }));
    await user.click(screen.getByRole("menuitem", { name: "Move to Later" }));
    expect(mocks.card_group_move).toHaveBeenLastCalledWith({ card: "c2", from: "ga", to: "gb", position: undefined, copy: false });

    await user.click(await screen.findByRole("button", { name: "Actions for search-console" }));
    await user.click(screen.getByRole("menuitem", { name: "Copy to Later" }));
    expect(mocks.card_group_move).toHaveBeenLastCalledWith({ card: "c1", from: "ga", to: "gb", position: undefined, copy: true });

    const later = screen.getByRole("region", { name: "Later" });
    await user.click(await within(later).findByRole("button", { name: "Actions for dedupe-records" }));
    await user.click(screen.getByRole("menuitem", { name: "Remove from group" }));
    expect(mocks.card_group_remove).toHaveBeenCalledWith({ group: "gb", cards: ["c3"] });

    await user.click(screen.getByRole("button", { name: "Rename In flight" }));
    const input = screen.getByLabelText("New name for In flight");
    await user.clear(input);
    await user.type(input, "Today{Enter}");
    expect(mocks.dash_block_update).toHaveBeenCalledWith({ block_id: "ga", title: "Today" });
  });

  it("reorders with ⌥↓ and removes with ⌫ from the keyboard", async () => {
    const user = await openDashboard();
    screen.getByRole("button", { name: "Open search-console, needs you" }).focus();
    await user.keyboard("{Alt>}{ArrowDown}{/Alt}");
    expect(mocks.card_group_move).toHaveBeenCalledWith({ card: "c1", from: "ga", to: "ga", position: 1, copy: false });
    expect(mocks.dash_block_move).not.toHaveBeenCalled();
    screen.getByRole("button", { name: "Open api-timeouts" }).focus();
    await user.keyboard("{Backspace}");
    expect(mocks.card_group_remove).toHaveBeenCalledWith({ group: "ga", cards: ["c2"] });
    expect(mocks.dash_block_remove).not.toHaveBeenCalled();
  });

  it("drags a card to another group to move it, and with ⌥ to copy it", async () => {
    await openDashboard();
    const later = screen.getByRole("region", { name: "Later" }).querySelector("[data-group]")!;
    let dt = dataTransfer();
    fireEvent.dragStart(screen.getByRole("button", { name: "Open api-timeouts" }), { dataTransfer: dt });
    expect(JSON.parse(dt.data[GROUP_DRAG])).toEqual({ card: "c2", from: "ga" });
    fireEvent.dragOver(later, { dataTransfer: dt });
    fireEvent.drop(later, { dataTransfer: dt });
    await waitFor(() => expect(mocks.card_group_move).toHaveBeenCalledWith({ card: "c2", from: "ga", to: "gb", position: undefined, copy: false }));

    dt = dataTransfer();
    fireEvent.dragStart(await screen.findByRole("button", { name: "Open search-console, needs you" }), { dataTransfer: dt });
    const target = screen.getByRole("button", { name: "Open dedupe-records" }).closest("[role=listitem]")!;
    // jsdom has no DragEvent, so the ⌥ key is set on the event by hand.
    const drop = createEvent.drop(target, { dataTransfer: dt });
    Object.defineProperty(drop, "altKey", { value: true });
    fireEvent(target, drop);
    await waitFor(() => expect(mocks.card_group_move).toHaveBeenLastCalledWith({ card: "c1", from: "ga", to: "gb", position: 0, copy: true }));
    expect(mocks.card_move).not.toHaveBeenCalled();
  });

  it("counts a drop inside the same group without the card's own slot", () => {
    const ids = ["a", "b", "c"];
    expect(dropPosition(ids, { card: "a", from: "g" }, "g", 2)).toBe(1);
    expect(dropPosition(ids, { card: "c", from: "g" }, "g", 0)).toBe(0);
    expect(dropPosition(ids, { card: "a", from: "h" }, "g", 2)).toBe(2);
    expect(dropPosition(ids, { card: "a", from: "g" }, "g", undefined)).toBeUndefined();
  });

  it("watches a card in a group from the card's menu", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(await screen.findByRole("button", { name: "Open dedupe-records" }));
    await user.click(screen.getByRole("button", { name: "Card actions" }));
    const watch = await screen.findByRole("group", { name: "Watch in group" });
    const inFlight = await within(watch).findByRole("checkbox", { name: "In flight" });
    expect(inFlight).not.toBeChecked();
    expect(within(watch).getByRole("checkbox", { name: "Later" })).toBeChecked();
    await user.click(inFlight);
    expect(mocks.card_group_add).toHaveBeenCalledWith({ group: "ga", cards: ["c3"] });
    await user.type(within(watch).getByLabelText("New group name"), "Mine{Enter}");
    expect(mocks.card_group_create).toHaveBeenCalledWith({ workspace_id: "w1", title: "Mine", cards: ["c3"] });
  });
});
