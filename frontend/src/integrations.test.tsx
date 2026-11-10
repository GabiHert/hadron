import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import type { Link, ScriptView } from "./api";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const view = (over: Partial<ScriptView>): ScriptView => ({
  id: "i1", name: "jira", kind: "integration", status: "enabled", workspace_id: "w1", global: false, version: "1.0.0", description: "Jira issues",
  used_here: true, fields: [], ops: ["search", "actions.comment"], permissions: {}, new_domains: [], config: [], secrets: [], tests: 0, test_result: null,
  poll_seconds: 0, problems: [], approved_by: "", approved_at: null, sessions: false, links: 0, discover: null, ...over,
});
const link = (over: Partial<Link> = {}): Link => ({
  id: "l1", workspace_id: "w1", card_id: "c2", integration: "jira", item_id: "ACME-541", title: "Dashboard timeouts", url: "https://jira.example/ACME-541",
  fields: { status: "To do", reporter: "Ana" }, synced_at: null, ...over,
});

async function openApp() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  return user;
}

async function openLinks() {
  const user = await openApp();
  await user.click(screen.getByRole("button", { name: "Open api-timeouts" }));
  await user.click(await screen.findByRole("tab", { name: /^Links/ }));
  return user;
}

async function openIntegrations() {
  const user = await openApp();
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Integrations" }));
  return user;
}

describe("card Links tab", () => {
  beforeEach(() => {
    state.integrations = [view({})];
    state.schema.push({ path: "jira.reporter", label: "Reporter", type: "text", filterable: true, editable: false, card_chip: false, source: "jira" });
  });

  it("shows the real link count and edits an enum field through card_field_set", async () => {
    state.links = [link()];
    const user = await openLinks();
    expect(screen.getByRole("tab", { name: "Links · 1" })).toBeInTheDocument();
    const box = await screen.findByRole("group", { name: "jira ACME-541" });
    // read-only field: text, no input
    expect(within(box).getByText("Ana")).toBeInTheDocument();
    expect(within(box).queryByRole("textbox", { name: "Reporter" })).not.toBeInTheDocument();
    expect(within(box).queryByRole("combobox", { name: "Reporter" })).not.toBeInTheDocument();
    await user.selectOptions(within(box).getByRole("combobox", { name: "Status" }), "Finished");
    await waitFor(() => expect(mocks.card_field_set).toHaveBeenCalledWith({ card: "c2", path: "jira.status", value: "Finished" }));
  });

  it("links an item found by search, and unlinks one", async () => {
    state.links = [link()];
    mocks.link_search.mockResolvedValueOnce({ items: [{ item_id: "ACME-600", title: "Another issue", url: "", fields: {} }] });
    const user = await openLinks();
    await screen.findByRole("group", { name: "jira ACME-541" });
    await user.type(screen.getByLabelText("Search"), "another{Enter}");
    await waitFor(() => expect(mocks.link_search).toHaveBeenCalledWith({ integration: "jira", q: "another" }));
    await user.click(await screen.findByRole("button", { name: /ACME-600/ }));
    expect(screen.getByLabelText("Item id")).toHaveValue("ACME-600");
    await user.click(screen.getByRole("button", { name: "Link" }));
    await waitFor(() => expect(mocks.link_add).toHaveBeenCalledWith({ card: "c2", integration: "jira", item_id: "ACME-600" }));

    await user.click(within(screen.getByRole("group", { name: "jira ACME-541" })).getByRole("button", { name: "Unlink" }));
    await waitFor(() => expect(mocks.link_remove).toHaveBeenCalledWith({ link_id: "l1" }));
  });

  it("explains the empty state when no integration is enabled", async () => {
    state.integrations = [];
    await openLinks();
    expect(await screen.findByText(/no integrations yet/)).toBeInTheDocument();
  });
});

describe("Settings → Integrations", () => {
  it("shows what a pending integration asks for and approves it by name", async () => {
    state.integrations = [view({
      name: "linear", status: "pending", used_here: false, permissions: { network: ["api.linear.app"], secrets: ["token"], api: ["card.read"] },
      new_domains: ["api.linear.app"], tests: 2, test_result: { ok: false, passed: 1, failed: 1, ran_at: "", cases: [{ name: "search works", op: "search", ok: false, error: "boom: bad token" }] },
    })];
    const user = await openIntegrations();
    const box = await screen.findByRole("group", { name: "Integration linear" });
    expect(within(box).getByText("needs approval")).toBeInTheDocument();
    expect(within(box).getAllByText("api.linear.app").length).toBeGreaterThan(0);
    expect(within(box).getByText("1/2 passed")).toBeInTheDocument();
    expect(within(box).getByText("Asks for new access")).toBeInTheDocument();
    expect(within(box).getByText(/boom: bad token/)).toBeInTheDocument();

    mocks.integration_approve.mockRejectedValueOnce(new Error("planner policy: the planner cannot approve new network access"));
    await user.click(within(box).getByRole("button", { name: "Approve and enable" }));
    await waitFor(() => expect(mocks.integration_approve).toHaveBeenCalledWith({ name: "linear" }));
    expect(await screen.findByText(/planner policy/)).toBeInTheDocument();
  });

  it("keeps secrets in a password input and never renders the value", async () => {
    state.integrations = [view({ secrets: [{ name: "token", set: false }] })];
    const user = await openIntegrations();
    const input = await screen.findByLabelText("Secret token");
    expect(input).toHaveAttribute("type", "password");
    await user.type(input, "s3cr3t-value");
    await user.click(screen.getByRole("button", { name: "Save secret token" }));
    await waitFor(() => expect(mocks.integration_secret_set).toHaveBeenCalledWith({ name: "jira", secret: "token", value: "s3cr3t-value" }));
    await waitFor(() => expect(screen.getByLabelText("Secret token")).toHaveValue(""));
    expect(document.body.innerHTML).not.toContain("s3cr3t-value");
  });
});

describe("linked-field chips", () => {
  it("shows a chip on the card face with the label in its tooltip", async () => {
    state.chips = { c2: [{ path: "jira.status", label: "Status", value: "In progress" }] };
    await openApp();
    const face = screen.getByRole("button", { name: "Open api-timeouts" });
    const chip = await within(face).findByText("In progress");
    expect(chip).toHaveAttribute("title", "Status: In progress");
  });
});

describe("card discovery", () => {
  const disc = (over: Partial<NonNullable<ScriptView["discover"]>> = {}) => ({ enabled: false, column: "", max_per_run: 0, last_run_at: null, last_created: 0, last_error: "", discovered: 0, ...over });

  it("shows only for an integration with ops.discover that the workspace uses", async () => {
    state.integrations = [view({ name: "jira", discover: disc() }), view({ id: "i2", name: "plain" }), view({ id: "i3", name: "off", used_here: false, discover: disc() })];
    await openIntegrations();
    expect(await screen.findByRole("group", { name: "Card discovery for jira" })).toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "Card discovery for plain" })).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "Card discovery for off" })).not.toBeInTheDocument();
  });

  it("switches discovery on, picks the column and the cap, and runs it now", async () => {
    state.integrations = [view({ name: "jira", poll_seconds: 600, discover: disc({ discovered: 3, last_run_at: "2026-11-10T12:00:00Z", last_created: 2 }) })];
    const user = await openIntegrations();
    const box = await screen.findByRole("group", { name: "Card discovery for jira" });
    await user.click(within(box).getByRole("switch", { name: "Create cards from new jira items" }));
    await waitFor(() => expect(mocks.integration_discover_set).toHaveBeenCalledWith({ name: "jira", enabled: true }));
    await user.selectOptions(within(box).getByRole("combobox", { name: "Column for new cards" }), "working");
    await waitFor(() => expect(mocks.integration_discover_set).toHaveBeenCalledWith({ name: "jira", column: "working" }));
    const cap = within(box).getByRole("spinbutton", { name: "Cards per run" });
    await user.clear(cap);
    await user.type(cap, "5");
    await user.tab();
    await waitFor(() => expect(mocks.integration_discover_set).toHaveBeenCalledWith({ name: "jira", max_per_run: 5 }));
    expect(screen.getByTestId("discover-status-jira")).toHaveTextContent("3 items on the board so far");
    expect(screen.getByTestId("discover-status-jira")).toHaveTextContent("2 new");
    await user.click(within(screen.getByRole("group", { name: "Card discovery for jira" })).getByRole("button", { name: "Run now" }));
    await waitFor(() => expect(mocks.integration_discover_run).toHaveBeenCalledWith({ name: "jira" }));
    expect(await screen.findByText("jira added 1 card; 1 more next run.")).toBeInTheDocument();
  });
});
