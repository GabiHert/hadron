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
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Prompts" }));
  await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("You are the planner of {{workspace.name}}."));
  return user;
}

describe("Settings → Prompts", () => {
  it("shows the default planner prompt, its variables and the rendered preview", async () => {
    await openPrompts();
    expect(screen.getByText("Hadron default")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Insert card.nick" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("Rendered prompt")).toHaveTextContent("You are the planner of Acme."));
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Reset to default" })).toBeDisabled();
  });

  it("previews an unsaved edit on the backend, and saves it with prompt_set", async () => {
    const user = await openPrompts();
    const area = screen.getByRole("textbox", { name: "Template" });
    await user.clear(area);
    await user.type(area, "Custom for {{{{workspace.name}}");
    await waitFor(() => expect(screen.getByLabelText("Rendered prompt")).toHaveTextContent("Custom for Acme"));
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
    expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("You are the planner of {{workspace.name}}.{{columns}}");
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
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("You work on {{card.nick}}."));
    expect(screen.getByText("Hadron default")).toBeInTheDocument();
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

  it("has a planner handoff tab", async () => {
    const user = await openPrompts();
    await user.click(screen.getByRole("tab", { name: "Handoff (adopted planner)" }));
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Template" })).toHaveValue("Continue as the planner of {{workspace.name}}."));
    expect(screen.getByText(/Typed once into a conversation you adopt as the planner/)).toBeInTheDocument();
  });
});
