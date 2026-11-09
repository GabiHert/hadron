import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset, state } from "./test/fakeApi";
import { FakeXTerm, resetTerm } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

const WARNING = "Exit that conversation in its terminal first. Two processes resuming the same Claude Code conversation corrupt it.";
const now = new Date().toISOString();
const list = [
  { claude_session_id: "c-free", first_user_message: "Fix the billing export", last_activity: now, size: 4096, already_in_hadron: false },
  { claude_session_id: "c-held", first_user_message: "Old beta work", last_activity: now, size: 2048, already_in_hadron: true, held_by: "card beta" },
];

beforeEach(() => {
  reset();
  resetTerm();
  void state;
  void FakeXTerm;
});

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
}

describe("bring in a session", () => {
  it("lists conversations, disables the held one and adopts the picked one", async () => {
    mocks.session_adoptable_list.mockResolvedValueOnce(list);
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "New session" }));
    await user.click(await screen.findByRole("button", { name: "Bring in a session" }));
    expect(screen.getByRole("note")).toHaveTextContent(WARNING);
    const held = await screen.findByRole("option", { name: /Old beta work/ });
    expect(held).toBeDisabled();
    expect(held).toHaveAttribute("title", "card beta");
    expect(held).toHaveTextContent("in Hadron");
    await user.click(screen.getByRole("option", { name: /Fix the billing export/ }));
    const nick = screen.getByLabelText("Nickname");
    expect(nick).toHaveValue("fix-the-billing");
    await user.clear(nick);
    await user.type(nick, "billing-fix");
    await user.click(screen.getByRole("button", { name: "Adopt" }));
    await waitFor(() =>
      expect(mocks.session_adopt).toHaveBeenCalledWith(expect.objectContaining({ workspace_id: "w1", claude_session_id: "c-free", nick: "billing-fix" })),
    );
    expect(await screen.findByText(/Exit that conversation in its terminal first\./)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("says so when no conversation was found", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "New session" }));
    await user.click(await screen.findByRole("button", { name: "Bring in a session" }));
    expect(await screen.findByText("No Claude Code conversations were found for /tmp/w1.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByRole("button", { name: "Start session" })).toBeInTheDocument();
  });
});

describe("adopt the planner conversation", () => {
  it("opens from the planner menu and calls planner_adopt", async () => {
    mocks.session_adoptable_list.mockResolvedValueOnce(list);
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Planner actions" }));
    await user.click(screen.getByRole("button", { name: "Adopt planner conversation" }));
    expect(screen.getByRole("note")).toHaveTextContent(WARNING);
    expect(await screen.findByRole("option", { name: /Old beta work/ })).toBeDisabled();
    const adopt = screen.getByRole("button", { name: "Adopt as planner" });
    expect(adopt).toBeDisabled();
    await user.click(screen.getByRole("option", { name: /Fix the billing export/ }));
    await user.click(adopt);
    await waitFor(() => expect(mocks.planner_adopt).toHaveBeenCalledWith({ workspace_id: "w1", claude_session_id: "c-free" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
