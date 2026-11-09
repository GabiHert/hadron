import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import type { HistoryEntry } from "./api";
import { mocks, reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const entry = (over: Partial<HistoryEntry>): HistoryEntry => ({
  id: "h1", at: "2026-11-08T10:00:00Z", actor: "planner", actor_kind: "planner", action: "column_update", kind: "column",
  summary: "column Doing: name Working → Doing", reverted: false, revertible: true, ...over,
});

async function openHistory() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "History" }));
  return user;
}

describe("Settings → History", () => {
  it("lists who changed what, shows the diff, and reverts after confirming", async () => {
    mocks.config_history.mockResolvedValue([entry({}), entry({ id: "h2", actor: "you", actor_kind: "you", action: "rule_update", reverted: true, revertible: false, reason: "it was already reverted", summary: "rule R: name R → R2" })]);
    mocks.config_diff.mockResolvedValue({ change: entry({}), diff: [{ field: "name", before: "Working", after: "Doing" }] });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = await openHistory();
    expect(await screen.findByText("column Doing: name Working → Doing")).toBeInTheDocument();
    expect(screen.getByText("reverted")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revert rule_update" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Show what column_update changed" }));
    expect(await screen.findByRole("group", { name: "Diff" })).toHaveTextContent("name: Working → Doing");
    expect(mocks.config_diff).toHaveBeenCalledWith({ change_id: "h1" });

    await user.click(screen.getByRole("button", { name: "Revert column_update" }));
    expect(confirm).toHaveBeenCalled();
    await waitFor(() => expect(mocks.config_revert).toHaveBeenCalledWith({ change_id: "h1" }));
    confirm.mockRestore();
  });

  it("filters by who made the change", async () => {
    mocks.config_history.mockResolvedValue([]);
    const user = await openHistory();
    expect(await screen.findByText(/No changes yet/)).toBeInTheDocument();
    await user.selectOptions(screen.getByRole("combobox", { name: "Made by" }), "planner");
    await waitFor(() => expect(mocks.config_history).toHaveBeenLastCalledWith(expect.objectContaining({ actor: "planner" })));
  });

  it("shows why a load failed", async () => {
    mocks.config_history.mockRejectedValue(new Error("database is locked"));
    await openHistory();
    expect(await screen.findByRole("alert")).toHaveTextContent("database is locked");
  });
});
