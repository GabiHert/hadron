import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { profileExplanation, profileLabel } from "./components/Modals";
import { card, mocks, profile, reset, running, state, ws } from "./test/fakeApi";
import { FakeXTerm, resetTerm, subscribed } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

beforeEach(() => {
  reset();
  resetTerm();
});

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
}

describe("New session dialog", () => {
  it("lists the workspace's launch profiles and explains the chosen one in a line", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "New session" }));
    const picker = await screen.findByLabelText(/Launch profile/);
    await waitFor(() => expect(within(picker).getAllByRole("option")).toHaveLength(3));
    expect(within(picker).getAllByRole("option").map((o) => o.textContent)).toEqual([
      "Brief: claude, then /brief {nick}",
      "Orchestrate: claude, then /orchestrate {key}",
      "Plain: claude, no first prompt",
    ]);
    const explain = screen.getByLabelText("What this profile runs");
    expect(explain).toHaveTextContent("claude --mcp-config <hadron mcp.json> --append-system-prompt <hadron session prompt> → /brief {nick}");

    await user.type(screen.getByLabelText("Nickname"), "billing-fix");
    expect(explain).toHaveTextContent("→ /brief billing-fix");
    await user.selectOptions(picker, "orchestrate");
    expect(explain).toHaveTextContent("→ /orchestrate billing-fix"); // no key yet: the nickname
    await user.type(screen.getByLabelText("Key (optional)"), "acme-9");
    expect(explain).toHaveTextContent("→ /orchestrate ACME-9");
    await user.selectOptions(picker, "plain");
    expect(explain).not.toHaveTextContent("→");

    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() =>
      expect(mocks.card_create).toHaveBeenCalledWith({ workspace_id: "w1", nick: "billing-fix", key: "ACME-9", column_id: "working", launch_profile: "plain" }),
    );
  });

  it("passes the extra instructions as prompt to card_create only when typed", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "New session" }));
    await user.type(await screen.findByLabelText("Nickname"), "billing-fix");
    await user.type(screen.getByLabelText(/Extra instructions for this session/), "Stay in the billing module.");
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(mocks.card_create).toHaveBeenCalledWith(expect.objectContaining({ nick: "billing-fix", prompt: "Stay in the billing module." })));
  });

  it("leaves prompt out of card_create when the extra instructions are empty", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "New session" }));
    await user.type(await screen.findByLabelText("Nickname"), "billing-fix");
    expect(screen.getByText("Added to the system prompt after the workspace and launch profile prompts.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Start session" }));
    await waitFor(() => expect(mocks.card_create).toHaveBeenCalled());
    expect(mocks.card_create.mock.calls[0][0]).not.toHaveProperty("prompt");
  });

  it("describes a profile that is not claude without Hadron's flags", () => {
    const p = { ...profile("shell", ""), command: "/bin/zsh" };
    expect(profileLabel(p)).toBe("Shell: /bin/zsh, no first prompt");
    expect(profileExplanation(p, "x", "")).toBe("/bin/zsh");
  });
});

describe("planner panel", () => {
  it("shows the chat, and the real terminal behind the switch", async () => {
    const user = userEvent.setup();
    await openApp();
    const panel = screen.getByRole("complementary", { name: "Planner" });
    expect(await within(panel).findByText(/runs every session in Acme/)).toBeInTheDocument();
    expect(within(panel).getByLabelText("Message the planner")).toBeInTheDocument();
    expect(FakeXTerm.instances).toHaveLength(0);

    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByRole("group", { name: "planner terminal" })).toBeInTheDocument();
    expect(subscribed("sess-planner")).toBe(true);
    expect(within(panel).getByText("claude · planner · typing here skips the chat")).toBeInTheDocument();
    expect(within(panel).queryByText(/Direct to the session/)).not.toBeInTheDocument();

    await user.click(within(panel).getByRole("button", { name: "Chat" }));
    expect(within(panel).queryByRole("group", { name: "planner terminal" })).not.toBeInTheDocument();
    expect(FakeXTerm.instances[0].disposed).toBe(true);
  });

  it("offers to start a planner that has no session", async () => {
    const user = userEvent.setup();
    delete state.sessions.planner;
    await openApp();
    const panel = screen.getByRole("complementary", { name: "Planner" });
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByText(/The planner has no session yet/)).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Start session" }));
    expect(mocks.planner_start).toHaveBeenCalledWith({ workspace_id: "w1" });
  });

  it("shows the planner's liveness on its dot", async () => {
    await openApp();
    const dot = await screen.findByTitle("Planner working");
    expect(dot).toHaveClass("dot", "working", "pulse");
  });

  it("goes full screen and Shift+Esc in the terminal leaves it", async () => {
    const user = userEvent.setup();
    await openApp();
    const panel = screen.getByRole("complementary", { name: "Planner" });
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    await user.click(within(panel).getByRole("button", { name: "Full screen" }));
    await waitFor(() => expect(screen.getByRole("complementary", { name: "Planner" })).toHaveClass("full"));
    await screen.findByRole("group", { name: "planner terminal" });
    act(() => {
      FakeXTerm.instances[FakeXTerm.instances.length - 1].keyHandler({ type: "keydown", key: "Escape", shiftKey: true });
    });
    await waitFor(() => expect(mocks.ui_fullscreen).toHaveBeenLastCalledWith({ workspace_id: "w1", panel: "" }));
  });
});

describe("card panel", () => {
  const open = async (user: ReturnType<typeof userEvent.setup>, nick = "api-timeouts") => {
    await openApp();
    await user.click(screen.getByRole("button", { name: `Open ${nick}` }));
    return screen.getByRole("complementary", { name: `Card ${nick}` });
  };

  it("shows the card's terminal with the direct-input banner, and Back to chat returns", async () => {
    const user = userEvent.setup();
    state.sessions["c2"] = running("sess-c2", { card_id: "c2", nick: "api-timeouts", liveness: "working" });
    const panel = await open(user);
    expect(await within(panel).findByLabelText("Message about this card")).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByRole("group", { name: "api-timeouts terminal" })).toBeInTheDocument();
    expect(subscribed("sess-c2")).toBe(true);
    expect(within(panel).getByText("Direct to the session. The planner sees this only through the next report.")).toBeInTheDocument();
    expect(within(panel).getByText("claude · api-timeouts · /tmp/w1")).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Back to chat" }));
    expect(within(panel).queryByRole("group", { name: /terminal/ })).not.toBeInTheDocument();
    expect(within(panel).getByLabelText("Message about this card")).toBeInTheDocument();
  });

  it("starts a card that never had a session", async () => {
    const user = userEvent.setup();
    const panel = await open(user, "dedupe-records");
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByText(/has no session yet/)).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Start session" }));
    expect(mocks.session_start).toHaveBeenCalledWith({ card: "c3" });
  });

  it("says when the session ended and restarts it", async () => {
    const user = userEvent.setup();
    state.sessions["c3"] = running("sess-c3", { card_id: "c3", running: false, liveness: "ended" });
    state.sessions["c3"].session!.status = "exited";
    state.sessions["c3"].session!.exit_code = 129;
    const panel = await open(user, "dedupe-records");
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByText("The session has ended (exit code 129).")).toBeInTheDocument();
    expect(within(panel).getByRole("group", { name: "dedupe-records terminal" })).toBeInTheDocument(); // its output stays readable
    await user.click(within(panel).getByRole("button", { name: "Restart session" }));
    expect(mocks.session_restart).toHaveBeenCalledWith({ card: "c3" });
  });

  it("reports why a session never started", async () => {
    const user = userEvent.setup();
    state.sessions["c3"] = running("sess-c3", { card_id: "c3", running: false, liveness: "ended", error: "claude was not found on PATH" });
    const panel = await open(user, "dedupe-records");
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByRole("alert")).toHaveTextContent("Could not start the session: claude was not found on PATH");
  });
});

describe("liveness dots", () => {
  it("shows each card's liveness on its dot", async () => {
    const fx = (await import("./test/fakeApi")).defaultFixture();
    fx.cards.w1.push(card("c9", "w1", "working", "gone", 3, { liveness: "ended" }));
    fx.workspaces[0] = ws("w1", "Acme");
    reset(fx);
    await openApp();
    const dotOf = (nick: string) => screen.getByRole("button", { name: `Open ${nick}` }).querySelector(".dot")!;
    expect(dotOf("api-timeouts")).toHaveClass("working", "pulse");
    expect(dotOf("search-console")).toHaveClass("waiting");
    expect(dotOf("dedupe-records")).toHaveClass("dot");
    expect(dotOf("gone")).toHaveClass("ended");
  });
});
