import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import type { UpdateStatus } from "./api";
import { GeneralTab } from "./components/GeneralTab";
import { UpdatesCard } from "./components/UpdatesCard";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

describe("Settings → General", () => {
  const box = () => screen.findByRole("checkbox", { name: "Resume running sessions after Hadron restarts" });

  it("is on by default", async () => {
    render(<GeneralTab toast={vi.fn()} />);
    expect(await box()).toBeChecked();
    expect(screen.getByText("Resume running sessions after Hadron restarts or updates")).toBeInTheDocument();
  });

  it("is off when the setting is false", async () => {
    state.fx.settings["sessions.restore_on_launch"] = false;
    render(<GeneralTab toast={vi.fn()} />);
    await waitFor(async () => expect(await box()).not.toBeChecked());
  });

  it("saves the new value when toggled", async () => {
    render(<GeneralTab toast={vi.fn()} />);
    await userEvent.click(await box());
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "sessions.restore_on_launch": false } }));
    expect(await box()).not.toBeChecked();
    await userEvent.click(await box());
    await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "sessions.restore_on_launch": true } }));
  });

  it("toasts when saving fails", async () => {
    const toast = vi.fn();
    mocks.settings_update.mockRejectedValueOnce(new Error("disk full"));
    render(<GeneralTab toast={toast} />);
    await userEvent.click(await box());
    await waitFor(() => expect(toast).toHaveBeenCalledWith("disk full"));
  });
});

describe("Settings → Updates with running sessions", () => {
  const status = (): UpdateStatus => ({
    version: "v0.0.1-test", repo: "GabiHert/hadron", hasToken: false, state: "idle", message: "", lines: [], seq: 0,
    installedApp: "/Applications/Hadron.app", upToDate: false,
    latest: { tag: "v0.2.0", name: "v0.2.0", notes: "", date: 0, url: "https://example.test" },
  });
  function setup() {
    const client = {
      getStatus: vi.fn(async () => status()),
      check: vi.fn(async () => status()),
      start: vi.fn(async () => ({})),
      setToken: vi.fn(async () => status()),
    };
    render(<UpdatesCard client={client} on={() => () => {}} sessions={mocks.sessions_running} />);
    return client;
  }
  const install = () => screen.findByRole("button", { name: "Update to v0.2.0" });

  it("says how many sessions resume", async () => {
    state.running.sessions = ["planner", "alpha"];
    setup();
    expect(await screen.findByLabelText("Running sessions")).toHaveTextContent("2 sessions are running. They resume automatically after the update.");
  });

  it("uses the singular for one session", async () => {
    state.running.sessions = ["alpha"];
    setup();
    expect(await screen.findByLabelText("Running sessions")).toHaveTextContent("1 session is running. It resumes automatically after the update.");
  });

  it("warns that sessions stop when restore is off", async () => {
    state.running.sessions = ["alpha", "beta", "gamma"];
    state.running.restore_on_launch = false;
    setup();
    expect(await screen.findByLabelText("Running sessions")).toHaveTextContent(
      "3 sessions are running and will stop for the update: turn on Resume running sessions in Settings, General to bring them back.",
    );
  });

  it("shows no notice when nothing runs", async () => {
    setup();
    await install();
    expect(screen.queryByLabelText("Running sessions")).toBeNull();
  });

  it("asks before installing, and Cancel does nothing", async () => {
    state.running.sessions = ["alpha"];
    const client = setup();
    await screen.findByLabelText("Running sessions");
    await userEvent.click(await install());
    const row = await screen.findByRole("group", { name: "Confirm update" });
    expect(within(row).getByText(/1 session is running/)).toBeInTheDocument();
    expect(client.start).not.toHaveBeenCalled();
    await userEvent.click(within(row).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("group", { name: "Confirm update" })).toBeNull();
    expect(client.start).not.toHaveBeenCalled();
  });

  it("Install and restart starts the update", async () => {
    state.running.sessions = ["alpha", "beta"];
    const client = setup();
    await screen.findByLabelText("Running sessions");
    await userEvent.click(await install());
    await userEvent.click(await screen.findByRole("button", { name: "Install and restart" }));
    await waitFor(() => expect(client.start).toHaveBeenCalledTimes(1));
  });

  it("installs in one click when none run", async () => {
    const client = setup();
    await userEvent.click(await install());
    await waitFor(() => expect(client.start).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("button", { name: "Install and restart" })).toBeNull();
  });
});
