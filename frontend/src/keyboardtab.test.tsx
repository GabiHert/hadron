import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import App from "./App";
import { KeyboardTab, keyboardRows } from "./components/KeyboardTab";
import { DEFAULTS, activeOverrides } from "./keys";
import { mocks, reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const row = (id: string) => document.querySelector(`tr[data-command="${id}"]`) as HTMLElement;

test("Settings has a Keyboard section listing every shortcut, by scope", async () => {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  const nav = screen.getByRole("navigation", { name: "Settings sections" });
  await user.click(within(nav).getByRole("button", { name: "Keyboard" }));
  expect(await screen.findByRole("heading", { name: "Keyboard" })).toBeInTheDocument();
  for (const label of ["Everywhere", "Board", "Planner and card panels", "Dashboard", "Terminal"]) expect(screen.getByRole("region", { name: `${label} shortcuts` })).toBeInTheDocument();
  for (const d of DEFAULTS) expect(row(d.id), d.id).toBeTruthy();
  expect(within(row("view.dashboard")).getByText("⌘D")).toBeInTheDocument();
});

test("search narrows the list by title or by chord", async () => {
  const user = userEvent.setup();
  render(<KeyboardTab toast={() => {}} />);
  await user.type(screen.getByRole("searchbox", { name: "Search shortcuts" }), "dashboard");
  expect(row("view.dashboard")).toBeTruthy();
  expect(row("card.start")).toBeNull();
  expect(keyboardRows({}, "⌘J").flatMap((g) => g.rows.map((r) => r.id))).toEqual(["view.terminal"]);
  await user.clear(screen.getByRole("searchbox", { name: "Search shortcuts" }));
  await user.type(screen.getByRole("searchbox", { name: "Search shortcuts" }), "zzzz");
  expect(screen.getByText(/No shortcut matches/)).toBeInTheDocument();
});

test("rebinding: press a new chord, it is saved as keys.bindings and takes effect", async () => {
  const user = userEvent.setup();
  render(<KeyboardTab toast={() => {}} />);
  const change = within(row("view.dashboard")).getByRole("button", { name: /Change the shortcut/ });
  await user.click(change);
  const listening = within(row("view.dashboard")).getByRole("button", { name: /Press the new keys/ });
  fireEvent.keyDown(listening, { key: "Shift", shiftKey: true, metaKey: true }); // a modifier alone waits
  fireEvent.keyDown(listening, { key: "d", shiftKey: true, metaKey: true, code: "KeyD" });
  await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "keys.bindings": { "view.dashboard": ["Shift+Mod+D"] } } }));
  expect(within(row("view.dashboard")).getByText("⇧⌘D")).toBeInTheDocument();
  expect(within(row("view.dashboard")).getByText("changed")).toBeInTheDocument();
  expect(activeOverrides()).toEqual({ "view.dashboard": ["Shift+Mod+D"] });
  await user.click(within(row("view.dashboard")).getByRole("button", { name: /^Reset/ }));
  await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "keys.bindings": {} } }));
  expect(within(row("view.dashboard")).getByText("⌘D")).toBeInTheDocument();
});

test("a chord that is taken shows the conflict; replacing moves it, cancelling keeps both", async () => {
  const user = userEvent.setup();
  render(<KeyboardTab toast={() => {}} />);
  await user.click(within(row("view.terminal")).getByRole("button", { name: /Change the shortcut/ }));
  fireEvent.keyDown(within(row("view.terminal")).getByRole("button", { name: /Press the new keys/ }), { key: "d", metaKey: true, code: "KeyD" });
  const alert = await screen.findByRole("alert", { name: "Shortcut conflict" });
  expect(alert).toHaveTextContent("⌘D already runs “Switch Board ⇄ Dashboard”");
  expect(mocks.settings_update).not.toHaveBeenCalled();
  await user.click(within(alert).getByRole("button", { name: "Use ⌘D here instead" }));
  await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "keys.bindings": { "view.terminal": ["Mod+D"], "view.dashboard": [] } } }));
  expect(within(row("view.dashboard")).getByText("none")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Reset all/ }));
  await waitFor(() => expect(mocks.settings_update).toHaveBeenLastCalledWith({ values: { "keys.bindings": {} } }));
});

test("macOS's own chords are refused, and Esc cancels listening", async () => {
  const user = userEvent.setup();
  render(<KeyboardTab toast={() => {}} />);
  await user.click(within(row("view.dashboard")).getByRole("button", { name: /Change the shortcut/ }));
  fireEvent.keyDown(within(row("view.dashboard")).getByRole("button", { name: /Press the new keys/ }), { key: "q", metaKey: true, code: "KeyQ" });
  expect(await screen.findByRole("alert", { name: "Shortcut conflict" })).toHaveTextContent("Quit Hadron");
  expect(screen.queryByRole("button", { name: /Use ⌘Q/ })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(within(row("view.dashboard")).getByRole("button", { name: /Change the shortcut/ }));
  fireEvent.keyDown(within(row("view.dashboard")).getByRole("button", { name: /Press the new keys/ }), { key: "Escape" });
  expect(within(row("view.dashboard")).getByRole("button", { name: /Change the shortcut/ })).toBeInTheDocument();
  expect(mocks.settings_update).not.toHaveBeenCalled();
});

test("a binding set over MCP shows after the changed event", async () => {
  let changed: (e: unknown) => void = () => {};
  mocks.onChanged.mockImplementation(((cb: (e: unknown) => void) => { changed = cb; return () => {}; }) as never);
  render(<KeyboardTab toast={() => {}} />);
  await waitFor(() => expect(mocks.settings_get).toHaveBeenCalled());
  await mocks.settings_update({ values: { "keys.bindings": { "card.start": ["Shift+S"] } } });
  changed({ workspace_id: "", entities: ["settings"] });
  await waitFor(() => expect(within(row("card.start")).getByText("⇧S")).toBeInTheDocument());
});
