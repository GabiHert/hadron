import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { permissionText } from "./components/NotificationsTab";
import { fireNotificationOpen, mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

async function openTab() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Notifications" }));
  await screen.findByRole("heading", { name: "Notifications" });
  return user;
}

describe("Settings → Notifications", () => {
  it("says what macOS allows", () => {
    expect(permissionText({ permission: "allowed", alert_style: "banner" })).toEqual({ text: "macOS shows Hadron's notifications.", fix: false });
    expect(permissionText({ permission: "allowed", alert_style: "none" }).fix).toBe(true);
    expect(permissionText({ permission: "denied" })).toEqual({ text: "macOS blocks Hadron's notifications.", fix: true });
    expect(permissionText({ permission: "unavailable" }).fix).toBe(false);
  });

  it("switches events, the sound and a workspace, and sends a test", async () => {
    const user = await openTab();
    expect(await screen.findByText("macOS shows Hadron's notifications.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open Notification Settings" })).not.toBeInTheDocument();
    const done = screen.getByRole("checkbox", { name: "A card is done" });
    expect(done).toBeChecked();
    await user.click(done);
    await waitFor(() => expect(mocks.notifications_settings_set).toHaveBeenCalledWith({ events: { done: false } }));
    expect(screen.getByRole("checkbox", { name: "A card is done" })).not.toBeChecked();

    await user.selectOptions(screen.getByRole("combobox", { name: "Notification sound" }), "hadron");
    await waitFor(() => expect(mocks.notifications_settings_set).toHaveBeenCalledWith({ sound: "hadron" }));

    await user.click(screen.getByRole("checkbox", { name: "Notify for Work" }));
    await waitFor(() => expect(mocks.notifications_settings_set).toHaveBeenCalledWith({ muted_workspaces: ["w2"] }));
    expect(screen.getByRole("checkbox", { name: "Notify for Work" })).not.toBeChecked();

    await user.click(screen.getByRole("button", { name: "Send a test" }));
    await waitFor(() => expect(mocks.notifications_test).toHaveBeenCalled());
    expect(await screen.findByText("Test notification sent.")).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: "Show notifications" }));
    await waitFor(() => expect(mocks.notifications_settings_set).toHaveBeenCalledWith({ enabled: false }));
    expect(screen.getByRole("checkbox", { name: "A card needs you" })).toBeDisabled();
  });

  it("offers System Settings when macOS blocks them", async () => {
    state.notifications.status = { permission: "denied" };
    const user = await openTab();
    await user.click(await screen.findByRole("button", { name: "Open Notification Settings" }));
    expect(mocks.notifications_open_system_settings).toHaveBeenCalled();
  });
});

describe("notifications and the window", () => {
  it("a click on a planner reply opens the planner chat", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    act(() => fireNotificationOpen({ workspace_id: "w1", planner: true }));
    await waitFor(() => expect(document.activeElement?.closest("[data-zone]")?.getAttribute("data-zone")).toBe("planner"));
  });

  it("reports focus and the card on screen", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await waitFor(() => expect(mocks.ui_presence).toHaveBeenCalledWith(expect.objectContaining({ workspace_id: "w1" })));
    act(() => void window.dispatchEvent(new Event("blur")));
    await waitFor(() => expect(mocks.ui_presence).toHaveBeenLastCalledWith(expect.objectContaining({ workspace_id: "w1" })));
  });

  it("a click opens the card, switching workspace first", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    act(() => fireNotificationOpen({ workspace_id: "w2", card_id: "c5" }));
    await waitFor(() => expect(mocks.workspace_switch).toHaveBeenCalledWith({ workspace_id: "w2" }));
    expect(await screen.findByRole("complementary", { name: "Card invoicing" })).toBeInTheDocument();
  });
});
