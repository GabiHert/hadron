import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { WhatsNew } from "./components/WhatsNew";
import { idleUpdate, mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());
afterEach(() => mocks.onEvent.mockImplementation(() => () => {}));

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
}

test("renders the sections, bullets and code spans", async () => {
  render(<WhatsNew onClose={() => {}} />);
  const dlg = await screen.findByRole("dialog", { name: "What's new in Hadron" });
  expect(dlg).toHaveAttribute("aria-modal", "true");
  expect(await within(dlg).findByRole("heading", { name: /Hadron 0\.2\.0/ })).toHaveTextContent("2026-11-08");
  expect(within(dlg).getByRole("heading", { name: "Added" })).toBeInTheDocument();
  expect(within(dlg).getByRole("heading", { name: "Changed" })).toBeInTheDocument();
  expect(within(dlg).getAllByRole("listitem").some((li) => li.textContent === "Dark mode")).toBe(true);
  expect(within(dlg).getByText("hadron --help").tagName).toBe("CODE");
});

test("shows a note when the version has no release notes", async () => {
  mocks.changelog_get.mockResolvedValueOnce({ running: "0.9.0", found: false, release: { version: "0.9.0", date: "", sections: [], body: "" }, highlights: [], releases: [], seen: "", show: false });
  render(<WhatsNew onClose={() => {}} />);
  expect(await screen.findByText("There are no release notes for this version.")).toBeInTheDocument();
});

test("picking an earlier release loads it, and Back returns", async () => {
  const user = userEvent.setup();
  render(<WhatsNew onClose={() => {}} />);
  await screen.findByRole("heading", { name: "Added" });
  await user.click(screen.getByText("Earlier releases"));
  await user.click(screen.getByRole("button", { name: "0.1.1" }));
  expect(await screen.findByRole("heading", { name: /Hadron 0\.1\.1/ })).toBeInTheDocument();
  expect(mocks.changelog_get).toHaveBeenCalledWith({ version: "0.1.1" });
  await user.click(screen.getByText("Back to this version"));
  expect(await screen.findByRole("heading", { name: /Hadron 0\.2\.0/ })).toBeInTheDocument();
});

test("opens automatically when show is true and marks the version seen on close", async () => {
  state.changelogShow = true;
  const user = userEvent.setup();
  await openApp();
  const dlg = await screen.findByRole("dialog", { name: "What's new in Hadron" });
  await screen.findByRole("heading", { name: "Added" });
  await user.click(within(dlg).getByRole("button", { name: "Got it" }));
  expect(screen.queryByRole("dialog", { name: "What's new in Hadron" })).toBeNull();
  expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "changelog.seen": "0.2.0" } });
});

test("the Close button also closes and marks seen", async () => {
  state.changelogShow = true;
  const user = userEvent.setup();
  await openApp();
  const dlg = await screen.findByRole("dialog", { name: "What's new in Hadron" });
  await screen.findByRole("heading", { name: "Added" });
  await user.click(within(dlg).getByRole("button", { name: "Close" }));
  expect(screen.queryByRole("dialog", { name: "What's new in Hadron" })).toBeNull();
  expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "changelog.seen": "0.2.0" } });
});

test("does not open when show is false", async () => {
  await openApp();
  await waitFor(() => expect(mocks.changelog_get).toHaveBeenCalled());
  expect(screen.queryByRole("dialog", { name: "What's new in Hadron" })).toBeNull();
});

test("a failing changelog call never blocks the app", async () => {
  mocks.changelog_get.mockRejectedValue(new Error("boom"));
  await openApp();
  expect(screen.queryByRole("dialog", { name: "What's new in Hadron" })).toBeNull();
  mocks.changelog_get.mockReset();
});

test("the menu:whatsnew event opens it", async () => {
  const handlers: Record<string, () => void> = {};
  mocks.onEvent.mockImplementation(((ev: string, cb: () => void) => { handlers[ev] = cb; return () => {}; }) as never);
  await openApp();
  expect(screen.queryByRole("dialog", { name: "What's new in Hadron" })).toBeNull();
  act(() => handlers["menu:whatsnew"]());
  expect(await screen.findByRole("dialog", { name: "What's new in Hadron" })).toBeInTheDocument();
});

test("the version is in the Settings title, not in the header", async () => {
  const user = userEvent.setup();
  await openApp();
  expect(screen.queryByTestId("version")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Settings" }));
  expect(await screen.findByRole("heading", { name: /Settings · .* · Hadron v0\.0\.1-test/ })).toBeInTheDocument();
});

test("the palette has a What's new command", async () => {
  const user = userEvent.setup();
  await openApp();
  await user.click(screen.getByRole("button", { name: "Command palette" }));
  await user.type(screen.getByPlaceholderText(/Jump to a card/), "what");
  await user.click(screen.getByRole("button", { name: /What's new/ }));
  expect(await screen.findByRole("dialog", { name: "What's new in Hadron" })).toBeInTheDocument();
});

test("Settings → Updates has a What's new button that opens the dialog", async () => {
  const user = userEvent.setup();
  await openApp();
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(screen.getByRole("button", { name: "Updates" }));
  await user.click(await screen.findByRole("button", { name: "What's new" }));
  expect(await screen.findByRole("dialog", { name: "What's new in Hadron" })).toBeInTheDocument();
});

test("the update pill's tooltip carries the first highlights of the release notes", async () => {
  state.update = idleUpdate({ latest: { tag: "v0.3.0", name: "Hadron v0.3.0", notes: "### Added\n- Dark mode.\n- A `Models` tab.\n\n### Fixed\n- A crash.\n- Fourth thing.", date: 0, url: "" } });
  await openApp();
  const pill = await screen.findByRole("button", { name: /v0\.3\.0 available/ });
  expect(pill).toHaveAttribute("title", "v0.3.0 is available. What's new: Dark mode.; A Models tab.; A crash.. Open Settings, Updates to install it.");
});
