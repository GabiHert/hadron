import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openModels() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Models" }));
  await waitFor(() => expect(screen.getAllByRole("textbox", { name: "Model id" })).toHaveLength(3));
  return user;
}

describe("Settings → Models", () => {
  it("lists the models and saves an edited list with models_set", async () => {
    const user = await openModels();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Add model" }));
    const ids = screen.getAllByRole("textbox", { name: "Model id" });
    await user.type(ids[3], "claude-example-1");
    await user.type(screen.getAllByRole("textbox", { name: "Model label" })[3], "Example");
    await user.click(screen.getAllByRole("button", { name: "Remove" })[2]);
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(mocks.models_set).toHaveBeenCalledWith({ models: [{ id: "opus", label: "Opus" }, { id: "sonnet", label: "Sonnet" }, { id: "claude-example-1", label: "Example" }] }),
    );
  });

  it("toasts the backend error when saving fails", async () => {
    const user = await openModels();
    mocks.models_set.mockRejectedValueOnce(new Error("duplicate model id"));
    await user.type(screen.getAllByRole("textbox", { name: "Model label" })[0], "!");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("duplicate model id")).toBeInTheDocument();
  });

  it("saves the workspace default and the planner model", async () => {
    const user = await openModels();
    await user.selectOptions(screen.getByRole("combobox", { name: "Default model for cards" }), "sonnet");
    await waitFor(() => expect(mocks.workspace_update).toHaveBeenCalledWith({ workspace_id: "w1", default_model: "sonnet" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Planner model" }), "haiku");
    await waitFor(() => expect(mocks.planner_set_model).toHaveBeenCalledWith({ workspace_id: "w1", model: "haiku" }));
    expect(await screen.findByText("It applies the next time the planner starts.")).toBeInTheDocument();
  });
});

describe("model on the board", () => {
  it("shows a chip on the card face only when a model is set", async () => {
    state.fx.cards.w1[2].model = "opus";
    render(<App />);
    const withModel = await screen.findByRole("button", { name: "Open dedupe-records" });
    expect(within(withModel).getByTitle("Model: opus (card)")).toHaveTextContent("opus");
    expect(within(screen.getByRole("button", { name: "Open stale-warning" })).queryByTitle(/^Model:/)).not.toBeInTheDocument();
  });

  it("inherits the workspace default on the chip", async () => {
    state.fx.workspaces[0].default_model = "haiku";
    render(<App />);
    const face = await screen.findByRole("button", { name: "Open stale-warning" });
    expect(within(face).getByTitle("Model: haiku (workspace default)")).toBeInTheDocument();
  });

  it("changes a card's model from the card panel and toasts the note", async () => {
    const user = userEvent.setup();
    render(<App />);
    await user.click(await screen.findByRole("button", { name: "Open dedupe-records" }));
    const panel = await screen.findByRole("complementary", { name: "Card dedupe-records" });
    const select = within(panel).getByRole("combobox", { name: "Model" });
    expect(select).toHaveValue("");
    expect(within(panel).getByText("From Claude's own default")).toBeInTheDocument();
    await waitFor(() => expect(within(select).getByRole("option", { name: "Sonnet" })).toBeInTheDocument());
    await user.selectOptions(select, "sonnet");
    await waitFor(() => expect(mocks.card_set_model).toHaveBeenCalledWith({ card: "c3", model: "sonnet" }));
    expect(await screen.findByText("It applies the next time the session starts.")).toBeInTheDocument();
    await waitFor(() => expect(within(panel).getByText("Set on this card")).toBeInTheDocument());
  });

  it("sends model from the New session dialog only when chosen", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("button", { name: "New session" }));
    await user.type(await screen.findByLabelText("Nickname"), "billing-fix");
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(mocks.card_create).toHaveBeenCalledTimes(1));
    expect(mocks.card_create.mock.calls[0][0]).not.toHaveProperty("model");

    await user.click(screen.getByRole("button", { name: "New session" }));
    await user.type(await screen.findByLabelText("Nickname"), "invoice-fix");
    const sel = within(screen.getByRole("dialog")).getByRole("combobox", { name: "Model" });
    await waitFor(() => expect(within(sel).getByRole("option", { name: "Opus" })).toBeInTheDocument());
    await user.selectOptions(sel, "opus");
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(mocks.card_create).toHaveBeenCalledTimes(2));
    expect(mocks.card_create.mock.calls[1][0]).toMatchObject({ nick: "invoice-fix", model: "opus" });
  });
});
