// Collapsing a board column to a rail: the header button, the rail's count and badges, and the keys.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { DEFAULTS } from "./keys";
import { mocks, question, reset, state } from "./test/fakeApi";
import { clearSeen, markSeen, unseen } from "./state/foldSeen";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => {
  reset();
  try {
    localStorage.clear();
  } catch {
    /* no storage */
  }
});

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
}

const press = (key: string, init: Partial<KeyboardEventInit> = {}) => fireEvent.keyDown(document.activeElement ?? window, { key, ...init });
const focused = () => document.activeElement as HTMLElement;
const setFold = (id: string, fold: boolean) => {
  const c = state.fx.columns.w1.find((x) => x.id === id)!;
  c.fold = fold;
};

describe("collapsing a column", () => {
  it("collapses from the header button to a rail with the name and card count, and the rail expands it", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Collapse Working" }));
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "working", fold: true }));
    const rail = await screen.findByRole("button", { name: "Expand Working" });
    expect(rail).toHaveTextContent("Working");
    expect(screen.getByTestId("count-working")).toHaveTextContent("3");
    expect(screen.queryByRole("button", { name: "Open api-timeouts" })).not.toBeInTheDocument();
    await user.click(rail);
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "working", fold: false }));
    expect(await screen.findByRole("button", { name: "Open api-timeouts" })).toBeInTheDocument();
  });

  it("shows the saved state on load: a column collapsed before stays a rail", async () => {
    setFold("done", true);
    await openApp();
    expect(screen.getByRole("button", { name: "Expand Done" })).toBeInTheDocument();
    expect(screen.queryByTestId("new-done")).not.toBeInTheDocument();
  });

  it("badges a card that moves into a collapsed column, and the badge clears on expand", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Collapse Done" }));
    await screen.findByRole("button", { name: "Expand Done" });
    // Move stale-warning (the last card of Working) right, into Done.
    screen.getByRole("button", { name: "Open stale-warning" }).focus();
    press("ArrowRight", { altKey: true });
    await waitFor(() => expect(mocks.card_move).toHaveBeenCalledWith(expect.objectContaining({ card_id: "c4", column_id: "done" })));
    expect(await screen.findByTestId("new-done")).toHaveTextContent("+1");
    expect(screen.getByTestId("count-done")).toHaveTextContent("1");
    // The card is out of sight: the focus lands on the rail.
    await waitFor(() => expect(focused()).toBe(screen.getByRole("button", { name: "Expand Done" })));
    await user.click(screen.getByRole("button", { name: "Expand Done" }));
    await user.click(await screen.findByRole("button", { name: "Collapse Done" }));
    await screen.findByRole("button", { name: "Expand Done" });
    expect(screen.queryByTestId("new-done")).not.toBeInTheDocument();
  });

  it("marks a collapsed column that holds an open question, without expanding it", async () => {
    state.questions = [question({ card_id: "c1" })];
    setFold("needs", true);
    await openApp();
    const rail = screen.getByRole("button", { name: "Expand Needs you" });
    expect(within(rail).getByTestId("asking-needs")).toBeInTheDocument();
    expect(rail.className).toContain("needs");
    expect(rail.title).toContain("1 waiting on you");
  });

  it("collapses the focused card's column with z and lands on the rail; z there expands it again", async () => {
    await openApp();
    screen.getByRole("button", { name: "Open dedupe-records" }).focus();
    press("z");
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "working", fold: true }));
    await waitFor(() => expect(focused()).toBe(screen.getByRole("button", { name: "Expand Working" })));
    press("z");
    await waitFor(() => expect(mocks.column_update).toHaveBeenCalledWith({ column_id: "working", fold: false }));
    await waitFor(() => expect(focused().dataset.cardId).toBe("c2"));
  });

  it("Enter on a focused rail expands it instead of opening the selected card", async () => {
    setFold("done", true);
    await openApp();
    screen.getByRole("button", { name: "Expand Done" }).focus();
    press("Enter");
    // Enter falls through to the button: the dispatcher does not take it.
    expect(mocks.column_update).not.toHaveBeenCalled();
  });

  it("h and l skip a collapsed column; g then its letter lands on its rail", async () => {
    setFold("working", true);
    await openApp();
    screen.getByRole("button", { name: "Open search-console" }).focus();
    press("l");
    await waitFor(() => expect(focused().dataset.cardId).toBe("c1"));
    press("g");
    press("W");
    await waitFor(() => expect(focused()).toBe(screen.getByRole("button", { name: "Expand Working" })));
  });

  it("the commands are in the registry, so Settings → Keyboard and the palette list them", () => {
    expect(DEFAULTS.find((d) => d.id === "column.collapse")?.keys).toEqual(["Z"]);
    expect(DEFAULTS.find((d) => d.id === "column.expandAll")).toBeTruthy();
  });
});

describe("the rail's seen cards", () => {
  it("badges only cards that came after the collapse, and starts clean for a column with no record", () => {
    expect(unseen("x", ["a", "b"])).toEqual([]);
    expect(unseen("x", ["a", "b", "c"])).toEqual(["c"]);
    markSeen("x", ["a"]);
    expect(unseen("x", ["a", "b"])).toEqual(["b"]);
    clearSeen("x");
    expect(unseen("x", ["a", "b"])).toEqual([]);
  });
});
