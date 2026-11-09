import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openPrompts() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Harness" }));
  await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue(""));
  return user;
}

describe("Settings → Harness", () => {
  it("shows the core harness read-only in a closed details block, and the empty workspace prompt", async () => {
    await openPrompts();
    expect(screen.getByRole("heading", { name: "Harness" })).toBeInTheDocument();
    expect(screen.getByText("No workspace prompt")).toBeInTheDocument();
    expect(screen.getByText("Core harness (built in, v3)")).toBeInTheDocument();
    const core = screen.getByLabelText("Core harness");
    expect(core).toHaveTextContent("Core harness for {{workspace.name}}");
    expect(core.closest("details")).not.toHaveAttribute("open");
    expect(screen.queryByRole("textbox", { name: "Core harness" })).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Template" })).toHaveAttribute("placeholder", "Optional. Added after the core harness for every session of this workspace.");
    expect(screen.getByRole("button", { name: "Insert card.nick" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Reset to default" })).toBeDisabled();
  });

  it("previews an unsaved edit on the backend, and saves it with prompt_set", async () => {
    const user = await openPrompts();
    const area = screen.getByRole("textbox", { name: "Template" });
    await user.clear(area);
    await user.type(area, "Custom for {{{{workspace.name}}");
    await waitFor(() => expect(screen.getByLabelText("Rendered prompt")).toHaveTextContent("Custom for Acme"));
    expect(screen.getByLabelText("Rendered prompt")).toHaveTextContent("Core harness for Acme");
    expect(screen.getByText(/What the planner receives \(composed\)/)).toBeInTheDocument();
    expect(mocks.prompt_render).toHaveBeenCalledWith(expect.objectContaining({ workspace_id: "w1", kind: "planner", template: "Custom for {{workspace.name}}" }));
    expect(mocks.prompt_set).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.prompt_set).toHaveBeenCalledWith({ workspace_id: "w1", kind: "planner", template: "Custom for {{workspace.name}}" }));
    expect(await screen.findByText("custom for Acme")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reset to default" })).toBeEnabled();
  });

  it("inserts a variable from its chip and warns about an unknown one when saving", async () => {
    const user = await openPrompts();
    await user.click(screen.getByRole("button", { name: "Insert columns" }));
    expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("{{columns}}");
    mocks.prompt_render.mockResolvedValueOnce({ text: "x", unknown: ["who"] });
    await user.type(screen.getByRole("textbox", { name: "Template" }), " {{{{who}}");
    expect(await screen.findByRole("status")).toHaveTextContent("{{who}}");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/unknown variable\(s\) who/)).toBeInTheDocument();
  });

  it("switches to the session prompt, previews it for the selected card, and resets to the default", async () => {
    state.prompts.session = { ...state.prompts.session, template: "Custom session {{card.nick}}", custom: true };
    const user = await openPrompts();
    await user.click(screen.getByRole("tab", { name: "Session" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("Custom session {{card.nick}}"));
    expect(mocks.prompt_get).toHaveBeenLastCalledWith({ workspace_id: "w1", kind: "session" });
    expect(screen.getByText("custom for Acme")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Reset to default" }));
    await waitFor(() => expect(mocks.prompt_reset).toHaveBeenCalledWith({ workspace_id: "w1", kind: "session" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue(""));
    expect(screen.getByText("No workspace prompt")).toBeInTheDocument();
    expect(screen.getByText(/A launch profile prompt and a card prompt are added after this one/)).toBeInTheDocument();
  });

  it("switches to the handoff prompt and saves it with kind handoff", async () => {
    const user = await openPrompts();
    await user.click(screen.getByRole("tab", { name: "Handoff (adopted session)" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("Continue {{card.nick}} in {{workspace.name}}."));
    expect(mocks.prompt_get).toHaveBeenLastCalledWith({ workspace_id: "w1", kind: "handoff" });
    expect(screen.getByText(/Typed once into a conversation you bring in/)).toBeInTheDocument();
    await user.type(screen.getByRole("textbox", { name: "Template" }), " Go.");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.prompt_set).toHaveBeenCalledWith({ workspace_id: "w1", kind: "handoff", template: "Continue {{card.nick}} in {{workspace.name}}. Go." }));
  });

  it("has no core block for a handoff kind", async () => {
    const user = await openPrompts();
    await user.click(screen.getByRole("tab", { name: "Handoff (adopted session)" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("Continue {{card.nick}} in {{workspace.name}}."));
    expect(screen.queryByLabelText("Core harness")).not.toBeInTheDocument();
    expect(screen.getByText("Hadron default")).toBeInTheDocument();
  });

  it("has a planner handoff tab", async () => {
    const user = await openPrompts();
    await user.click(screen.getByRole("tab", { name: "Handoff (adopted planner)" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("Continue as the planner of {{workspace.name}}."));
    expect(screen.getByText(/Typed once into a conversation you adopt as the planner/)).toBeInTheDocument();
  });
});

describe("Card Context tab → Card prompt", () => {
  it("saves the card prompt with card_update and shows the composed prompt", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("button", { name: "Open api-timeouts" }));
    const panel = await screen.findByRole("complementary", { name: "Card api-timeouts" });
    await user.click(within(panel).getByRole("tab", { name: "Context" }));
    expect(within(panel).getByText("Applies when this card's session next starts.")).toBeInTheDocument();
    const area = within(panel).getByRole("textbox", { name: "Card prompt" });
    expect(within(panel).getByRole("button", { name: "Save" })).toBeDisabled();
    await user.type(area, "Only touch the billing module.");
    await user.click(within(panel).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.card_update).toHaveBeenCalledWith({ card_id: "c2", prompt: "Only touch the billing module." }));

    await user.click(within(panel).getByText("Show composed prompt"));
    await waitFor(() => expect(mocks.prompt_layers_get).toHaveBeenCalledWith({ workspace_id: "w1", kind: "session", card: "c2" }));
    const composed = await within(panel).findByLabelText("Composed prompt");
    await waitFor(() => expect(composed).toHaveTextContent("Core harness for Acme"));
    expect(composed).toHaveTextContent("Only touch the billing module.");
  });
});
