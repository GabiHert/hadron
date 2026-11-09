import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { cardFolder, tildePath } from "./lib";
import { mocks, reset, state } from "./test/fakeApi";
import { FakeXTerm, resetTerm } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

const now = new Date().toISOString();
const elsewhere = [
  { claude_session_id: "c-web", first_user_message: "Ship the landing page", last_activity: now, size: 4096, cwd: "/Users/ana/code/acme-web", already_in_hadron: false },
  { claude_session_id: "c-api", first_user_message: "Fix the api timeouts", last_activity: now, size: 4096, cwd: "/Users/ana/code/acme-api", already_in_hadron: false },
];

beforeEach(() => {
  reset();
  resetTerm();
  void FakeXTerm;
});

async function openAdopt() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "New session" }));
  await user.click(await screen.findByRole("button", { name: "Bring in a session" }));
  return user;
}

describe("folder helpers", () => {
  it("shortens the home folder to ~ and hides the root", () => {
    expect(tildePath("/Users/ana/code/acme-web")).toBe("~/code/acme-web");
    expect(tildePath("/Users/ana")).toBe("~");
    expect(tildePath("/opt/acme")).toBe("/opt/acme");
    expect(cardFolder("", "/tmp/w1")).toBe("");
    expect(cardFolder("/tmp/w1/", "/tmp/w1")).toBe("");
    expect(cardFolder("/tmp/acme-web", "/tmp/w1")).toBe("/tmp/acme-web");
  });
});

describe("the adopt dialog's folder", () => {
  it("looks in the workspace root first, then in the folder typed", async () => {
    const user = await openAdopt();
    const folder = screen.getByRole("textbox", { name: "Conversation folder" });
    expect(folder).toHaveValue("/tmp/w1");
    await waitFor(() => expect(mocks.session_adoptable_list).toHaveBeenCalledWith({ workspace_id: "w1" }));
    mocks.session_adoptable_list.mockResolvedValueOnce([elsewhere[0]]);
    await user.clear(folder);
    await user.type(folder, "~/code/acme-web");
    await user.click(screen.getByRole("button", { name: "Show" }));
    await waitFor(() => expect(mocks.session_adoptable_list).toHaveBeenLastCalledWith({ workspace_id: "w1", cwd: "~/code/acme-web" }));
    await user.click(await screen.findByRole("option", { name: /Ship the landing page/ }));
    expect(screen.getByLabelText("Adopted folder")).toHaveTextContent("Runs in ~/code/acme-web");
    await user.click(screen.getByRole("button", { name: "Adopt" }));
    await waitFor(() =>
      expect(mocks.session_adopt).toHaveBeenCalledWith(expect.objectContaining({ claude_session_id: "c-web", nick: "ship-the-landing", cwd: "/Users/ana/code/acme-web" })),
    );
  });

  it("lists every folder with All folders and shows where each ran", async () => {
    const user = await openAdopt();
    await waitFor(() => expect(mocks.session_adoptable_list).toHaveBeenCalledTimes(1));
    mocks.session_adoptable_list.mockResolvedValueOnce(elsewhere);
    await user.click(screen.getByRole("checkbox", { name: "All folders" }));
    await waitFor(() => expect(mocks.session_adoptable_list).toHaveBeenLastCalledWith({ workspace_id: "w1", all: true }));
    const api = await screen.findByRole("option", { name: /Fix the api timeouts/ });
    expect(api).toHaveTextContent("~/code/acme-api");
    expect(screen.getByRole("textbox", { name: "Conversation folder" })).toBeDisabled();
    await user.click(api);
    await user.click(screen.getByRole("button", { name: "Adopt" }));
    await waitFor(() => expect(mocks.session_adopt).toHaveBeenCalledWith(expect.objectContaining({ claude_session_id: "c-api", cwd: "/Users/ana/code/acme-api" })));
  });

  it("passes the conversation's folder to planner_adopt", async () => {
    mocks.session_adoptable_list.mockResolvedValueOnce(elsewhere);
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("button", { name: "Planner actions" }));
    await user.click(screen.getByRole("button", { name: "Adopt planner conversation" }));
    await user.click(await screen.findByRole("option", { name: /Ship the landing page/ }));
    await user.click(screen.getByRole("button", { name: "Adopt as planner" }));
    await waitFor(() => expect(mocks.planner_adopt).toHaveBeenCalledWith({ workspace_id: "w1", claude_session_id: "c-web", cwd: "/Users/ana/code/acme-web" }));
  });
});

describe("a card's folder", () => {
  it("shows in the card panel, shortened with ~, only when it is not the root", async () => {
    state.fx.cards.w1[2].cwd = "/Users/ana/code/acme-web";
    const user = userEvent.setup();
    render(<App />);
    await user.click(await screen.findByRole("button", { name: "Open dedupe-records" }));
    const panel = await screen.findByRole("complementary", { name: "Card dedupe-records" });
    const path = within(panel).getByLabelText("Card folder path");
    expect(path).toHaveTextContent("~/code/acme-web");
    expect(path).toHaveClass("mono");
    expect(path).toHaveAttribute("title", "/Users/ana/code/acme-web");

    await user.click(await screen.findByRole("button", { name: "Open stale-warning" }));
    const other = await screen.findByRole("complementary", { name: "Card stale-warning" });
    expect(within(other).queryByLabelText("Card folder path")).toBeNull();
  });

  it("is changed from the card menu", async () => {
    const user = userEvent.setup();
    render(<App />);
    await user.click(await screen.findByRole("button", { name: "Open dedupe-records" }));
    const panel = await screen.findByRole("complementary", { name: "Card dedupe-records" });
    await user.click(within(panel).getByRole("button", { name: "Card actions" }));
    await user.type(within(panel).getByRole("textbox", { name: "Card folder" }), "~/code/acme-web");
    await user.click(within(panel).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.card_update).toHaveBeenCalledWith({ card_id: "c3", cwd: "~/code/acme-web" }));
  });

  it("is an optional field of the new-card form", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("button", { name: "New session" }));
    const folder = await screen.findByRole("textbox", { name: "Session folder" });
    expect(folder).toHaveAttribute("placeholder", "/tmp/w1");
    await user.type(screen.getByLabelText("Nickname"), "landing");
    await user.type(folder, "~/code/acme-web");
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(mocks.card_create).toHaveBeenCalledWith(expect.objectContaining({ nick: "landing", cwd: "~/code/acme-web" })));
  });
});
