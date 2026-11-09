import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { celOfFilter, fromSpec, isActive, localMatch, needsServer, toSpec } from "./filters";
import type { Filter } from "./filters";
import { card, col, mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openApp(pinned: string[] = ["card.assignee"]) {
  state.fx.workspaces[0].filter_bar = pinned;
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await screen.findByRole("button", { name: "Customize filters" });
}

const open = (nick: string) => screen.queryByRole("button", { name: `Open ${nick}` });

describe("filter bar", () => {
  it("shows the pinned fields as chips with the values found on the board", async () => {
    await openApp();
    const chip = await screen.findByRole("combobox", { name: "Filter by Assignee" });
    expect(within(chip).getAllByRole("option").map((o) => o.textContent)).toEqual(["any", "Ana", "You"]);
    expect(screen.getByLabelText("Filter as CEL").textContent).toBe("true");
  });

  it("filters the board by a chip value in the browser, and previews the CEL", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.selectOptions(await screen.findByRole("combobox", { name: "Filter by Assignee" }), "Ana");
    expect(open("stale-warning")).toBeInTheDocument();
    expect(open("dedupe-records")).not.toBeInTheDocument();
    expect(open("search-console")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Filter as CEL").textContent).toBe('card.assignee == "Ana"');
    expect(mocks.filter_eval).not.toHaveBeenCalled(); // simple equality never asks the backend

    await user.click(screen.getByRole("button", { name: "Clear" }));
    expect(open("dedupe-records")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter as CEL").textContent).toBe("true");
  });

  it("pins and unpins a field from Customize filters through filter_bar_set", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Customize filters" }));
    expect(screen.getByRole("checkbox", { name: "Pin Assignee" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Pin Stage" })).not.toBeChecked();

    await user.click(screen.getByRole("checkbox", { name: "Pin Stage" }));
    await waitFor(() => expect(mocks.filter_bar_set).toHaveBeenCalledWith({ workspace_id: "w1", fields: ["card.assignee", "card.stage"] }));
    expect(await screen.findByRole("combobox", { name: "Filter by Stage" })).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: "Pin Assignee" }));
    await waitFor(() => expect(mocks.filter_bar_set).toHaveBeenLastCalledWith({ workspace_id: "w1", fields: ["card.stage"] }));
    await waitFor(() => expect(screen.queryByRole("combobox", { name: "Filter by Assignee" })).not.toBeInTheDocument());
  });

  it("shows the backend's compile error verbatim and keeps Apply off until the expression compiles", async () => {
    const user = userEvent.setup();
    await openApp();
    state.filterError = "ERROR: <input>:1:13: Syntax error: mismatched input '<EOF>'\n | card.nick ==\n | ............^";
    await user.click(screen.getByRole("button", { name: "Customize filters" }));
    await user.type(screen.getByRole("textbox", { name: "CEL expression" }), "card.nick ==");
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(state.filterError);
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    expect(screen.getByRole("textbox", { name: "CEL expression" })).toHaveAttribute("aria-invalid", "true");
    expect(mocks.filter_eval).toHaveBeenCalledWith({ workspace_id: "w1", expr: "card.nick ==" });

    state.filterError = "";
    await user.clear(screen.getByRole("textbox", { name: "CEL expression" }));
    await user.type(screen.getByRole("textbox", { name: "CEL expression" }), "card.stage == 'x'");
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled();
  });

  it("narrows the board with an expression through filter_eval", async () => {
    const user = userEvent.setup();
    await openApp();
    mocks.filter_eval.mockImplementation(async (a: { expr?: string }) => ({ card_ids: a.expr ? ["c3"] : ["c1", "c2", "c3", "c4"] }));
    await user.click(screen.getByRole("button", { name: "Customize filters" }));
    await user.type(screen.getByRole("textbox", { name: "CEL expression" }), "card.nick == 'dedupe-records'");
    await waitFor(() => expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled());
    await user.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(open("stale-warning")).not.toBeInTheDocument());
    expect(open("dedupe-records")).toBeInTheDocument();
    expect(mocks.filter_eval).toHaveBeenCalledWith({ workspace_id: "w1", vals: {}, q: "", expr: "card.nick == 'dedupe-records'" });
    expect(screen.getByLabelText("Filter as CEL").textContent).toBe("(card.nick == 'dedupe-records')");
  });

  it("saves the filter as a view with the right payload, and a view chip restores it", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.selectOptions(await screen.findByRole("combobox", { name: "Filter by Assignee" }), "Ana");
    await user.type(screen.getByLabelText("Search cards"), "stale");
    await user.click(screen.getByRole("button", { name: "Save as view" }));
    await user.type(screen.getByRole("textbox", { name: "View name" }), "Ana's stale{Enter}");
    await waitFor(() =>
      expect(mocks.view_save).toHaveBeenCalledWith({ workspace_id: "w1", name: "Ana's stale", filters: { vals: { "card.assignee": "Ana" }, q: "stale" } }),
    );
    const chip = await screen.findByRole("button", { name: "Ana's stale" });
    expect(chip).toHaveAttribute("aria-pressed", "true");

    await user.click(screen.getByRole("button", { name: "Clear" }));
    expect(chip).toHaveAttribute("aria-pressed", "false");
    expect(open("dedupe-records")).toBeInTheDocument();
    await user.click(chip);
    expect(chip).toHaveAttribute("aria-pressed", "true");
    expect(open("dedupe-records")).not.toBeInTheDocument();
    expect(open("stale-warning")).toBeInTheDocument();
    expect(screen.getByLabelText("Search cards")).toHaveValue("stale");
  });

  it("offers Save as view only while a filter is set, and deletes a view from the menu", async () => {
    const user = userEvent.setup();
    state.views = [{ id: "v1", created_at: "", updated_at: "", workspace_id: "w1", name: "Mine", filters: { vals: { "card.assignee": "You" } }, position: 0 }];
    await openApp();
    expect(screen.queryByRole("button", { name: "Save as view" })).not.toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Mine" }));
    expect(open("stale-warning")).not.toBeInTheDocument(); // Ana's card
    expect(screen.getByRole("button", { name: "Save as view" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Customize filters" }));
    await user.click(screen.getByRole("button", { name: "Delete view Mine" }));
    await waitFor(() => expect(mocks.view_delete).toHaveBeenCalledWith({ view_id: "v1" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Mine" })).not.toBeInTheDocument());
    expect(open("stale-warning")).not.toBeInTheDocument(); // deleting the view keeps the filter on; it is just not saved any more
    await user.click(screen.getByRole("button", { name: "Clear" }));
    expect(open("stale-warning")).toBeInTheDocument();
  });

  it("sends a value on an integration's field to the backend, which knows those fields", async () => {
    const user = userEvent.setup();
    state.fx.workspaces[0].integrations = ["jira"];
    mocks.filter_eval.mockImplementation(async (a: { vals?: Record<string, string> }) => ({ card_ids: a.vals?.["jira.status"] === "Finished" ? ["c2"] : ["c1", "c2", "c3", "c4"] }));
    await openApp(["jira.status"]);
    await user.selectOptions(await screen.findByRole("combobox", { name: "Filter by Status" }), "Finished");
    await waitFor(() => expect(open("dedupe-records")).not.toBeInTheDocument());
    expect(open("api-timeouts")).toBeInTheDocument();
    expect(mocks.filter_eval).toHaveBeenCalledWith({ workspace_id: "w1", vals: { "jira.status": "Finished" }, q: "", expr: "" });
  });
});

describe("filter helpers", () => {
  const cols = [col("working", "w1", "Working", "active", 0)];
  const c = card("c1", "w1", "working", "billing-fix", 0, { key: "ACME-1", title: "Round invoices", assignee: "Ana", fields: { priority: "high", size: 5 } });
  const f = (p: Partial<Filter>): Filter => ({ vals: {}, q: "", expr: "", view: null, ...p });

  it("matches pinned values on native and custom fields, and the search on nick, key and title", () => {
    expect(localMatch(c, cols, f({}))).toBe(true);
    expect(localMatch(c, cols, f({ vals: { "card.assignee": "Ana" } }))).toBe(true);
    expect(localMatch(c, cols, f({ vals: { "card.assignee": "You" } }))).toBe(false);
    expect(localMatch(c, cols, f({ vals: { "card.column": "Working", "custom.priority": "high" } }))).toBe(true);
    expect(localMatch(c, cols, f({ vals: { "custom.size": "5" } }))).toBe(true);
    expect(localMatch(c, cols, f({ vals: { "card.assignee": "" } }))).toBe(true); // any
    expect(localMatch(c, cols, f({ q: "invoice" }))).toBe(true);
    expect(localMatch(c, cols, f({ q: "ACME-1" }))).toBe(true);
    expect(localMatch(c, cols, f({ q: "nope" }))).toBe(false);
  });

  it("knows when the backend is needed", () => {
    expect(needsServer(f({ vals: { "card.stage": "x" }, q: "a" }))).toBe(false);
    expect(needsServer(f({ expr: "true" }))).toBe(true);
    expect(needsServer(f({ vals: { "jira.status": "Finished" } }))).toBe(true);
    expect(needsServer(f({ vals: { "jira.status": "" } }))).toBe(false);
  });

  it("saves only what is set, and round-trips a spec", () => {
    expect(toSpec(f({ vals: { "card.assignee": "Ana", "card.stage": "" }, expr: "a == b" }))).toEqual({ vals: { "card.assignee": "Ana" }, expr: "a == b" });
    expect(toSpec(f({}))).toEqual({});
    expect(fromSpec({ vals: { x: "1" }, q: "z" }, "v1")).toEqual({ vals: { x: "1" }, q: "z", expr: "", view: "v1" });
    expect(isActive(f({ vals: { a: "" } }))).toBe(false);
    expect(isActive(f({ q: "a" }))).toBe(true);
  });

  it("writes the CEL preview", () => {
    expect(celOfFilter(f({}))).toBe("true");
    expect(celOfFilter(f({ vals: { "card.assignee": "Ana" }, q: "x", expr: "card.stage != ''" }))).toBe(`card.assignee == "Ana" && card.nick.contains("x") && (card.stage != '')`);
  });
});
