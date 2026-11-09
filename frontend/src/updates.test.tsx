import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import type { UpdateStatus } from "./api";
import { UpdatesCard } from "./components/UpdatesCard";

const status = (over: Partial<UpdateStatus> = {}): UpdateStatus => ({
  version: "v0.0.1-test", repo: "GabiHert/hadron", latest: null, upToDate: false, hasToken: false, state: "idle", message: "",
  lines: [], seq: 0, installedApp: "/Applications/Hadron.app", ...over,
});
const release = { tag: "v0.2.0", name: "v0.2.0", notes: "", date: 0, url: "https://example.test" };

function setup(initial: UpdateStatus, checked?: UpdateStatus) {
  const client = {
    getStatus: vi.fn(async () => initial),
    check: vi.fn(async () => checked ?? initial),
    start: vi.fn(async () => ({})),
    setToken: vi.fn(async () => initial),
  };
  const handlers: Record<string, (d: unknown) => void> = {};
  const on = vi.fn((ev: string, cb: (d: unknown) => void) => {
    handlers[ev] = cb;
    return () => {};
  });
  render(<UpdatesCard client={client} on={on} />);
  return { client, handlers };
}

test("shows the running version and the repo", async () => {
  setup(status());
  expect(await screen.findByTestId("running-version")).toHaveTextContent("v0.0.1-test");
  expect(screen.getByText("GabiHert/hadron")).toBeInTheDocument();
});

test("Check for updates shows the newer release, and Update starts it", async () => {
  const { client } = setup(status(), status({ latest: release }));
  await userEvent.click(await screen.findByRole("button", { name: "Check for updates" }));
  const update = await screen.findByRole("button", { name: "Update to v0.2.0" });
  await userEvent.click(update);
  await waitFor(() => expect(client.start).toHaveBeenCalledTimes(1));
});

test("an up-to-date app offers no Update button", async () => {
  setup(status({ latest: { ...release, tag: "v0.0.1-test" }, upToDate: true }));
  expect(await screen.findByText("Hadron is up to date")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /^Update to/ })).toBeNull();
});

test("the token form passes the token and never shows it as text", async () => {
  const { client } = setup(status());
  await userEvent.click(await screen.findByRole("button", { name: "Add token" }));
  const input = screen.getByLabelText("GitHub token");
  expect(input).toHaveAttribute("type", "password");
  await userEvent.type(input, "github_pat_x");
  await userEvent.click(screen.getByRole("button", { name: "Save in keychain" }));
  await waitFor(() => expect(client.setToken).toHaveBeenCalledWith("github_pat_x"));
});

test("a translocated app is told to move to Applications", async () => {
  setup(status({ latest: release, installedApp: "/private/var/AppTranslocation/X/d/Hadron.app" }));
  expect(await screen.findByText(/temporary read-only copy/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Update to v0.2.0" })).toBeDisabled();
});

test("output lines from update:output events appear", async () => {
  const { handlers } = setup(status({ state: "downloading" }));
  await screen.findByRole("status");
  act(() => handlers["update:output"]({ seq: 1, line: "downloading Hadron-v0.2.0-darwin-arm64.zip" }));
  expect(await screen.findByText(/downloading Hadron-v0.2.0/)).toBeInTheDocument();
});
