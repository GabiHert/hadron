import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { GeneralTab } from "./components/GeneralTab";
import { QuitGuard, quitSessionsText } from "./components/QuitGuard";
import { mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

let handlers: Record<string, (d?: unknown) => void> = {};
beforeEach(() => {
  reset();
  handlers = {};
  mocks.onEvent.mockImplementation(((ev: string, cb: (d?: unknown) => void) => {
    handlers[ev] = cb;
    return () => {};
  }) as never);
});
afterEach(() => {
  mocks.onEvent.mockImplementation(() => () => {});
  vi.useRealTimers();
});

const fire = (ev: string, d?: unknown) => act(() => handlers[ev](d));

test("holding ⌘Q shows the ring, and letting go early leaves the reminder, which fades", async () => {
  vi.useFakeTimers();
  render(<QuitGuard toast={vi.fn()} />);
  fire("quit:hold", { state: "start", ms: 1000 });
  const st = screen.getByRole("status");
  expect(st).toHaveTextContent("Hold ⌘Q to quit");
  expect(st).toHaveClass("holding");
  expect(st.style.getPropertyValue("--hold")).toBe("1000ms");
  fire("quit:hold", { state: "cancel", ms: 1000 });
  expect(screen.getByRole("status")).toHaveClass("tap");
  expect(screen.getByRole("status")).toHaveTextContent("Hold ⌘Q to quit");
  act(() => vi.advanceTimersByTime(2000));
  expect(screen.queryByRole("status")).toBeNull();
  expect(mocks.app_quit).not.toHaveBeenCalled();
});

test("a finished hold says it is quitting; the window never calls app_quit for it", async () => {
  render(<QuitGuard toast={vi.fn()} />);
  fire("quit:hold", { state: "start", ms: 1000 });
  fire("quit:hold", { state: "done", ms: 1000 });
  expect(screen.getByRole("status")).toHaveTextContent("Quitting Hadron…");
  // Go quits on its own after a hold; app_quit is only the confirmation's button.
  expect(mocks.app_quit).not.toHaveBeenCalled();
});

test("Quit from the menu or the Dock asks, says how many sessions run, and Quit calls app_quit", async () => {
  state.running.sessions = ["planner", "billing-fix", "acme-508"];
  state.running.restore_on_launch = true;
  render(<QuitGuard toast={vi.fn()} />);
  fire("quit:confirm");
  const dlg = await screen.findByRole("alertdialog", { name: "Quit Hadron?" });
  expect(await within(dlg).findByText("3 sessions are running. They resume when Hadron opens again.")).toBeInTheDocument();
  await userEvent.click(within(dlg).getByRole("button", { name: "Quit" }));
  expect(mocks.app_quit).toHaveBeenCalledTimes(1);
});

test("Cancel keeps Hadron running", async () => {
  render(<QuitGuard toast={vi.fn()} />);
  fire("quit:confirm");
  const dlg = await screen.findByRole("alertdialog", { name: "Quit Hadron?" });
  await userEvent.click(within(dlg).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(mocks.app_quit).not.toHaveBeenCalled();
});

test("the sessions line follows the count and the restore setting", () => {
  expect(quitSessionsText({ count: 0, sessions: [], restore_on_launch: true })).toBe("No sessions are running.");
  expect(quitSessionsText({ count: 1, sessions: ["planner"], restore_on_launch: true })).toBe("1 session is running. It resumes when Hadron opens again.");
  expect(quitSessionsText({ count: 2, sessions: ["a", "b"], restore_on_launch: false })).toMatch(/^2 sessions are running and will stop\. Turn on Resume running sessions/);
});

test("the App listens for the guard's events", async () => {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  expect(handlers["quit:hold"]).toBeDefined();
  fire("quit:confirm");
  expect(await screen.findByRole("alertdialog", { name: "Quit Hadron?" })).toBeInTheDocument();
});

describe("Settings → General: the quit guard switch", () => {
  const box = () => screen.findByRole("checkbox", { name: "Guard against quitting by accident" });

  it("is on by default and saves app.quit_guard", async () => {
    render(<GeneralTab toast={vi.fn()} />);
    expect(await box()).toBeChecked();
    await userEvent.click(await box());
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "app.quit_guard": false } }));
    expect(await box()).not.toBeChecked();
  });

  it("is off when the setting is false", async () => {
    state.fx.settings["app.quit_guard"] = false;
    render(<GeneralTab toast={vi.fn()} />);
    await waitFor(async () => expect(await box()).not.toBeChecked());
  });
});
