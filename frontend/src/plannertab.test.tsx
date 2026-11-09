import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset, running, state } from "./test/fakeApi";
import { FakeXTerm, resetTerm } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

const now = new Date().toISOString();
const adoptable = [{ claude_session_id: "c-free", first_user_message: "Fix the billing export", last_activity: now, size: 4096, already_in_hadron: false }];

beforeEach(() => {
  reset();
  resetTerm();
  void FakeXTerm;
});

async function openPlanner() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Planner" }));
  await screen.findByRole("heading", { name: "Planner · Acme" });
  return user;
}

describe("Settings → Planner", () => {
  it("sets the launch profile and shows what it grants", async () => {
    state.profiles[0] = { ...state.profiles[0], args: ["--dangerously-skip-permissions", "--model", "opus"], model: "opus", env: { ACME_TOKEN: "s3cret" } };
    const user = await openPlanner();
    const select = screen.getByRole("combobox", { name: "Planner launch profile" });
    await waitFor(() => expect(within(select).getByRole("option", { name: "brief" })).toBeInTheDocument());
    await user.selectOptions(select, "brief");
    await waitFor(() => expect(mocks.planner_set_profile).toHaveBeenCalledWith({ workspace_id: "w1", launch_profile: "brief" }));
    const grant = screen.getByLabelText("What the planner will run");
    expect(grant).toHaveTextContent("claude --dangerously-skip-permissions --model opus");
    expect(grant).toHaveTextContent("ACME_TOKEN");
    expect(grant).not.toHaveTextContent("s3cret");
  });

  it("sets the policy per workspace, inherit included, and explains it", async () => {
    const user = await openPlanner();
    const select = screen.getByRole("combobox", { name: "Planner policy" });
    await screen.findByText("In force: decisions_only (global)");
    await user.selectOptions(select, "user_only");
    await waitFor(() => expect(mocks.planner_policy_set).toHaveBeenCalledWith({ workspace_id: "w1", policy: "user_only" }));
    expect(await screen.findByText("In force: user_only (workspace)")).toBeInTheDocument();
    expect(screen.getByText("The planner answers and clears nothing itself; it asks you, then relays what you said in chat.", { selector: "p" })).toBeInTheDocument();
    await user.selectOptions(select, "inherit");
    await waitFor(() => expect(mocks.planner_policy_set).toHaveBeenLastCalledWith({ workspace_id: "w1", policy: "inherit" }));
    expect(await screen.findByText("In force: decisions_only (global)")).toBeInTheDocument();
  });

  it("sets the planner model", async () => {
    const user = await openPlanner();
    await user.selectOptions(screen.getByRole("combobox", { name: "Planner model" }), "haiku");
    await waitFor(() => expect(mocks.planner_set_model).toHaveBeenCalledWith({ workspace_id: "w1", model: "haiku" }));
  });

  it("writes the global autostart setting", async () => {
    const user = await openPlanner();
    const box = screen.getByRole("checkbox", { name: "Start the planner when Hadron opens" });
    expect(box).toBeChecked();
    await user.click(box);
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "planner.autostart": false } }));
    expect(box).not.toBeChecked();
  });

  it("opens the adopt list and adopts with the planner's launch profile", async () => {
    state.plannerProfile.w1 = "plain";
    mocks.session_adoptable_list.mockResolvedValueOnce(adoptable as never);
    const user = await openPlanner();
    await user.click(screen.getByRole("button", { name: "Adopt planner conversation" }));
    const dialog = await screen.findByRole("dialog", { name: "Adopt planner conversation" });
    await user.click(await within(dialog).findByText(/Fix the billing export/));
    await waitFor(() => expect(within(dialog).getByRole("combobox", { name: "Adopted planner launch profile" })).toHaveValue("plain"));
    await user.click(within(dialog).getByRole("button", { name: "Adopt as planner" }));
    await waitFor(() => expect(mocks.planner_adopt).toHaveBeenCalledWith({ workspace_id: "w1", claude_session_id: "c-free", launch_profile: "plain" }));
  });

  it("starts, restarts and stops the planner", async () => {
    state.sessions.planner = running("", { running: false, liveness: "none" });
    const user = await openPlanner();
    expect(await screen.findByText("The planner is Not running.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Stop" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Restart" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Start" }));
    await waitFor(() => expect(mocks.planner_start).toHaveBeenCalledWith({ workspace_id: "w1" }));
  });

  it("restarts and stops a running planner", async () => {
    const user = await openPlanner();
    expect(await screen.findByText("The planner is Running.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Restart" }));
    await waitFor(() => expect(mocks.planner_restart).toHaveBeenCalledWith({ workspace_id: "w1" }));
    await user.click(screen.getByRole("button", { name: "Stop" }));
    await waitFor(() => expect(mocks.planner_stop).toHaveBeenCalledWith({ workspace_id: "w1" }));
  });
});

describe("launch profile when bringing a session in", () => {
  async function bringIn() {
    mocks.session_adoptable_list.mockResolvedValueOnce(adoptable as never);
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("button", { name: /New session/ }));
    await user.click(await screen.findByRole("button", { name: "Bring in a session" }));
    await user.click(await screen.findByText(/Fix the billing export/));
    return user;
  }

  it("omits launch_profile when none is chosen", async () => {
    const user = await bringIn();
    await user.click(await screen.findByRole("button", { name: "Adopt" }));
    await waitFor(() => expect(mocks.session_adopt).toHaveBeenCalled());
    expect((mocks.session_adopt.mock.calls as unknown[][])[0][0]).not.toHaveProperty("launch_profile");
  });

  it("carries launch_profile when chosen", async () => {
    const user = await bringIn();
    const select = await screen.findByRole("combobox", { name: "Adopt launch profile" });
    await waitFor(() => expect(within(select).getByRole("option", { name: "brief" })).toBeInTheDocument());
    await user.selectOptions(select, "brief");
    await user.click(screen.getByRole("button", { name: "Adopt" }));
    await waitFor(() => expect(mocks.session_adopt).toHaveBeenCalledWith(expect.objectContaining({ launch_profile: "brief" })));
  });
});
