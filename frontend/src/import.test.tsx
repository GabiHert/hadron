import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { mocks, reset, running, state } from "./test/fakeApi";
import type { ImportPlan } from "./api";
import { resetTerm } from "./test/fakeTerm";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

const plan = (over: Partial<ImportPlan> = {}): ImportPlan => ({
  dir: "/tmp/w1/.ai/runs/planner", dry_run: true, created: 1, skipped: 1, messages: 7, questions: 3, gates: 2, unmatched_log_lines: 4, warnings: ["log.md line 9 names no session"],
  cards: [
    { nick: "billing-fix", key: "ACME-508", action: "create", column: "Working", stage: "review", liveness: "idle", claude_session_id: "abc12345-x", messages: 5, questions: 2, open_questions: 1, gates: 1 },
    { nick: "invoice-sync", key: "ACME-509", action: "skip", reason: "already exists", column: "Done", stage: "", liveness: "ended", claude_session_id: "", messages: 2, questions: 1, open_questions: 0, gates: 1 },
  ],
  ...over,
});

beforeEach(() => {
  reset();
  resetTerm();
});

// The settings nav also has an "Import" button; the action button is the last one.
const importBtn = () => screen.getAllByRole("button", { name: "Import" }).at(-1)!;

async function openImport() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Import" }));
  return user;
}

describe("Settings → Import", () => {
  it("previews, then imports the same folder", async () => {
    mocks.workspace_import_planner_dir.mockResolvedValueOnce(plan()).mockResolvedValueOnce(plan({ dry_run: false }));
    const user = await openImport();
    expect(screen.getByText("Choose a folder and press Preview.")).toBeInTheDocument();
    const dir = screen.getByLabelText("Folder");
    expect(dir).toHaveValue("/tmp/w1/.ai/runs/planner");
    expect(importBtn()).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Preview" }));
    expect(mocks.workspace_import_planner_dir).toHaveBeenCalledWith({ workspace_id: "w1", dir: "/tmp/w1/.ai/runs/planner", dry_run: true });
    expect(await screen.findByText(/1 cards to create, 1 skipped · 7 messages, 3 questions, 2 gates/)).toBeInTheDocument();
    expect(screen.getByText(/4 log lines matched no card/)).toBeInTheDocument();
    expect(screen.getByText("log.md line 9 names no session")).toBeInTheDocument();
    const rows = within(screen.getByRole("table", { name: "Import preview" })).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("billing-fix");
    expect(rows[1]).toHaveTextContent("ACME-508");
    expect(rows[1]).toHaveTextContent("Working");
    expect(rows[1]).toHaveTextContent("review");
    expect(rows[1]).toHaveTextContent("1 open");
    expect(rows[1]).toHaveTextContent("create");
    expect(rows[2]).toHaveTextContent("skip: already exists");
    expect(within(rows[2]).getByTitle("already exists")).toBeInTheDocument();

    await user.type(dir, "x");
    expect(importBtn()).toBeDisabled();
    await user.type(dir, "{Backspace}");
    expect(importBtn()).toBeEnabled();

    const calls = mocks.card_list ? mocks.card_list.mock.calls.length : 0;
    await user.click(importBtn());
    await waitFor(() =>
      expect(mocks.workspace_import_planner_dir).toHaveBeenLastCalledWith({ workspace_id: "w1", dir: "/tmp/w1/.ai/runs/planner", dry_run: false }),
    );
    expect(await screen.findByText("Imported 1 cards. Open one and press Resume to continue its conversation.")).toBeInTheDocument();
    expect(screen.queryByRole("table", { name: "Import preview" })).toBeNull();
    expect(importBtn()).toBeDisabled();
    void calls;
  });

  it("keeps Import disabled when nothing is new", async () => {
    mocks.workspace_import_planner_dir.mockResolvedValueOnce(plan({ created: 0, skipped: 2, cards: [] }));
    const user = await openImport();
    await user.click(screen.getByRole("button", { name: "Preview" }));
    expect(await screen.findByText("Nothing new to import.")).toBeInTheDocument();
    expect(importBtn()).toBeDisabled();
  });

  it("shows a backend error inline and as a toast", async () => {
    mocks.workspace_import_planner_dir.mockRejectedValueOnce(new Error("folder does not exist"));
    const user = await openImport();
    await user.click(screen.getByRole("button", { name: "Preview" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("folder does not exist");
    expect(screen.getAllByText("folder does not exist").length).toBeGreaterThan(1);
    expect(importBtn()).toBeDisabled();
  });
});

describe("Resume", () => {
  const ended = () =>
    running("sess-c3", {
      card_id: "c3", nick: "dedupe-records", running: false, liveness: "ended",
    });

  it("reads Resume in the terminal view and starts the session", async () => {
    const s = ended();
    s.session!.status = "exited";
    s.session!.claude_session_id = "abcdef1234567890";
    state.sessions.c3 = s;
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("button", { name: "Open dedupe-records" }));
    const panel = await screen.findByRole("complementary", { name: "Card dedupe-records" });
    const header = await within(panel).findByRole("button", { name: "Resume the Claude conversation" });
    expect(header).toHaveAttribute("title", "claude --resume abcdef12");
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    const btn = await within(panel).findByRole("button", { name: "Resume" });
    expect(btn).toHaveAttribute("title", "claude --resume abcdef12");
    await user.click(btn);
    await waitFor(() => expect(mocks.session_start).toHaveBeenCalledWith({ card: "c3" }));
  });

  it("keeps Start session when no Claude conversation is stored", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("button", { name: "Open dedupe-records" }));
    const panel = await screen.findByRole("complementary", { name: "Card dedupe-records" });
    expect(within(panel).queryByRole("button", { name: "Resume the Claude conversation" })).toBeNull();
    await user.click(within(panel).getByRole("button", { name: "Terminal" }));
    expect(await within(panel).findByRole("button", { name: "Start session" })).toBeInTheDocument();
  });
});
