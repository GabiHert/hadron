import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import App from "./App";
import { ChatLine, ChatThread, QuestionCard, plannerIsReplying, reasonText, TYPING_MAX_MS } from "./components/Chat";
import { COMPOSER_MAX, Composer } from "./components/Composer";
import { message, mocks, question, reset, state, ws } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

beforeEach(() => reset());

describe("question card", () => {
  it("sends the picked option and the notes", async () => {
    const user = userEvent.setup();
    const onAnswer = vi.fn();
    render(<QuestionCard q={question()} onAnswer={onAnswer} />);
    // The first option is pre-picked: it is the session's recommendation.
    expect(screen.getByRole("button", { name: /Nearest/ })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: /^2\s*Up/ }));
    expect(screen.getByRole("button", { name: /^2\s*Up/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: /Nearest/ })).toHaveAttribute("aria-pressed", "false");
    await user.type(screen.getByLabelText(/Notes/), "only for invoices over 100");
    await user.click(screen.getByRole("button", { name: "Answer" }));
    expect(onAnswer).toHaveBeenCalledTimes(1);
    expect(onAnswer).toHaveBeenCalledWith(["Up"], "only for invoices over 100");
    await waitFor(() => expect(screen.getByLabelText(/Notes/)).toHaveValue(""));
  });

  it("numbers its options and lets a multi question pick several", async () => {
    const user = userEvent.setup();
    const onAnswer = vi.fn();
    const q = question({ multi: true, options: [{ label: "EU", description: "" }, { label: "US", description: "" }, { label: "APAC", description: "slow" }] });
    render(<QuestionCard q={q} onAnswer={onAnswer} />);
    expect(screen.getByRole("button", { name: "Answer" })).toBeDisabled(); // nothing picked, no notes
    await user.click(screen.getByRole("button", { name: /^3\s*APAC/ }));
    await user.click(screen.getByRole("button", { name: /^1\s*EU/ }));
    await user.click(screen.getByRole("button", { name: "Answer" }));
    expect(onAnswer).toHaveBeenCalledWith(["EU", "APAC"], "");
    // Unpicking works too.
    await user.click(screen.getByRole("button", { name: /^1\s*EU/ }));
    expect(screen.getByRole("button", { name: /^1\s*EU/ })).toHaveAttribute("aria-pressed", "false");
  });

  it("an answer in notes alone is allowed when no option fits", async () => {
    const user = userEvent.setup();
    const onAnswer = vi.fn();
    render(<QuestionCard q={question({ options: [] })} onAnswer={onAnswer} />);
    expect(screen.getByRole("button", { name: "Answer" })).toBeDisabled();
    await user.type(screen.getByLabelText(/Notes/), "round half to even");
    await user.click(screen.getByRole("button", { name: "Answer" }));
    expect(onAnswer).toHaveBeenCalledWith([], "round half to even");
  });

  it("shows an approval's target, runner, reversibility and command", () => {
    const q = question({
      kind: "approval", text: "Run the migration?", options: [{ label: "Approve", description: "" }, { label: "Reject", description: "" }],
      approval: { target: "the production database · reports", runs_as: "acme-db MCP, prod profile", reversible: false, command: "go run ./cmd/backfill-reports --env production" },
    });
    render(<QuestionCard q={q} onAnswer={vi.fn()} />);
    const box = screen.getByRole("group", { name: "Question Q-01" });
    expect(within(box).getByText("Target").nextSibling).toHaveTextContent("the production database · reports");
    expect(within(box).getByText("Runs as").nextSibling).toHaveTextContent("acme-db MCP, prod profile");
    expect(within(box).getByText("Reversible").nextSibling).toHaveTextContent("No");
    expect(within(box).getByLabelText("Command")).toHaveTextContent("go run ./cmd/backfill-reports --env production");
    expect(within(box).getByText(/planner may also answer this/)).toBeInTheDocument();
  });

  it("shows reversible Yes/No/Partly, its note, the meanwhile line and the command language", () => {
    const base = { target: "Acme invoices", runs_as: "acme-db", command: "psql -c 'select 1'" };
    const mk = (rev: "yes" | "no" | "partial" | boolean, extra = {}) =>
      question({ kind: "approval", meanwhile: "I keep drafting the billing-fix", options: [{ label: "Approve", description: "" }], approval: { ...base, reversible: rev, ...extra } });
    const { rerender } = render(<QuestionCard q={mk("partial", { reversible_note: "rows can be restored, emails cannot", command_lang: "sql" })} onAnswer={vi.fn()} />);
    expect(screen.getByText("Reversible").nextSibling).toHaveTextContent("Partly");
    expect(screen.getByLabelText("Reversible note")).toHaveTextContent("rows can be restored, emails cannot");
    expect(screen.getByLabelText("Meanwhile")).toHaveTextContent("While you decide: I keep drafting the billing-fix");
    expect(screen.getByLabelText("Command language")).toHaveTextContent("sql");
    expect(screen.getByLabelText("Command")).toHaveTextContent("psql -c 'select 1'");
    rerender(<QuestionCard q={mk("no")} onAnswer={vi.fn()} />);
    expect(screen.getByText("Reversible").nextSibling).toHaveTextContent("No");
    expect(screen.queryByLabelText("Command language")).toBeNull();
    rerender(<QuestionCard q={mk("yes")} onAnswer={vi.fn()} />);
    expect(screen.getByText("Reversible").nextSibling).toHaveTextContent("Yes");
    rerender(<QuestionCard q={mk(true)} onAnswer={vi.fn()} />);
    expect(screen.getByText("Reversible").nextSibling).toHaveTextContent("Yes");
    rerender(<QuestionCard q={question({ kind: "approval", approval: { ...base, reversible: true } })} onAnswer={vi.fn()} />);
    expect(screen.queryByLabelText("Meanwhile")).toBeNull();
  });

  it("an answer relayed by the planner says so", () => {
    render(<QuestionCard q={question({ status: "answered", answer: { selections: ["Nearest"], notes: "", via: "planner-relay" } })} onAnswer={vi.fn()} />);
    expect(screen.getByTestId("question-answered")).toHaveTextContent("relayed by the planner from your chat message");
  });

  it("an answered or withdrawn question shrinks to a line with its outcome", () => {
    const { rerender } = render(
      <QuestionCard q={question({ status: "answered", answer: { selections: ["Nearest"], notes: "keep cents", via: "planner" } })} onAnswer={vi.fn()} />,
    );
    const done = screen.getByTestId("question-answered");
    expect(done).toHaveTextContent("Answered by the planner: Nearest");
    expect(done).toHaveTextContent("notes: keep cents");
    expect(screen.queryByRole("button", { name: "Answer" })).toBeNull();
    rerender(<QuestionCard q={question({ status: "withdrawn" })} onAnswer={vi.fn()} />);
    expect(screen.getByTestId("question-withdrawn")).toHaveTextContent("Withdrawn");
  });
});

describe("composer", () => {
  const setup = (onSend = vi.fn()) => {
    render(<Composer id="c" label="Message" placeholder="Say it" onSend={onSend} />);
    return { onSend, box: screen.getByLabelText("Message") as HTMLTextAreaElement };
  };

  it("Enter sends the trimmed text and clears the field", async () => {
    const user = userEvent.setup();
    const { onSend, box } = setup();
    await user.type(box, "  hello planner  {Enter}");
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith("hello planner");
    await waitFor(() => expect(box).toHaveValue(""));
  });

  it("Shift+Enter adds a line and sends nothing", async () => {
    const user = userEvent.setup();
    const { onSend, box } = setup();
    await user.type(box, "one{Shift>}{Enter}{/Shift}two");
    expect(onSend).not.toHaveBeenCalled();
    expect(box).toHaveValue("one\ntwo");
    await user.keyboard("{Enter}");
    expect(onSend).toHaveBeenCalledWith("one\ntwo");
  });

  it("Enter that confirms an input-method composition does not send", async () => {
    const { onSend, box } = setup();
    fireEvent.change(box, { target: { value: "にほん" } });
    fireEvent.keyDown(box, { key: "Enter", isComposing: true, keyCode: 229 });
    expect(onSend).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSend).toHaveBeenCalledWith("にほん");
    await waitFor(() => expect(box).toHaveValue(""));
  });

  it("sends nothing for blank text, and the Send button works too", async () => {
    const user = userEvent.setup();
    const { onSend, box } = setup();
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    await user.type(box, "   {Enter}");
    expect(onSend).not.toHaveBeenCalled();
    await user.type(box, "go");
    await user.click(screen.getByRole("button", { name: "Send" }));
    expect(onSend).toHaveBeenCalledWith("go");
  });

  it("keeps what you typed when sending fails", async () => {
    const user = userEvent.setup();
    const { onSend, box } = setup(vi.fn().mockRejectedValue(new Error("nope")));
    await user.type(box, "keep me{Enter}");
    await waitFor(() => expect(onSend).toHaveBeenCalled());
    expect(box).toHaveValue("keep me");
  });

  it("grows with its text up to the maximum, then scrolls vertically only", () => {
    const { box } = setup();
    let height = 60;
    Object.defineProperty(box, "scrollHeight", { configurable: true, get: () => height });
    fireEvent.change(box, { target: { value: "a\nb" } });
    expect(box.style.height).toBe("62px");
    expect(box.className).not.toContain("grow-max");
    height = 500;
    fireEvent.change(box, { target: { value: "a\nb\nc\nd" } });
    expect(box.style.height).toBe(`${COMPOSER_MAX}px`);
    expect(box.className).toContain("grow-max");
  });
});

describe("lines and the typing indicator", () => {
  const T = new Date("2026-11-08T14:00:00Z").getTime();
  const mine = (over = {}) => message({ author: "you", created_at: new Date(T).toISOString(), ...over });

  it("the planner replies for two minutes after your delivered message, never after", () => {
    expect(plannerIsReplying([mine()], T + 1000)).toBe(true);
    expect(plannerIsReplying([mine()], T + TYPING_MAX_MS - 1)).toBe(true);
    expect(plannerIsReplying([mine()], T + TYPING_MAX_MS)).toBe(false);
    expect(plannerIsReplying([mine(), message({ author: "planner", created_at: new Date(T + 500).toISOString() })], T + 1000)).toBe(false);
    expect(plannerIsReplying([mine({ delivered: false, reason: "planner_not_running" })], T + 1000)).toBe(false);
    expect(plannerIsReplying([], T)).toBe(false);
  });

  it("the thread shows Planner is replying… under your newest message", () => {
    const { rerender } = render(
      <ChatThread className="chat" scope="p" messages={[message({ author: "you", text: "status?" })]} questions={[]} onAnswer={vi.fn()} empty={null} />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Planner is replying…");
    rerender(
      <ChatThread className="chat" scope="p" messages={[message({ author: "you", text: "status?" }), message({ id: "m2", author: "planner", text: "All **green**." })]} questions={[]} onAnswer={vi.fn()} empty={null} />,
    );
    expect(screen.queryByText("Planner is replying…")).toBeNull();
    expect(screen.getByText("green").tagName).toBe("STRONG");
  });

  it("an undelivered message carries a mark with the reason", () => {
    render(<ChatLine m={message({ author: "you", text: "hello?", delivered: false, reason: "planner_not_running" })} />);
    expect(screen.getByRole("note")).toHaveTextContent("Not delivered: the planner is not running");
    expect(reasonText("type_failed: broken pipe")).toContain("broken pipe");
  });

  it("reports show a kind pill, events a rule line, and widgets a titled placeholder", () => {
    render(
      <>
        <ChatLine m={message({ author: "session", kind: "report", report_kind: "blocked", text: "VPN is down" })} />
        <ChatLine m={message({ author: "rule", kind: "event", text: "Moved to Needs you: it needs you" })} />
        <ChatLine m={message({ author: "planner", kind: "widget", widget_title: "Sessions", widget_html: "<b>x</b>" })} />
      </>,
    );
    expect(screen.getByText("blocked")).toHaveClass("bad");
    expect(screen.getByText("VPN is down")).toBeInTheDocument();
    expect(screen.getByText("Moved to Needs you: it needs you")).toHaveClass("b-ev");
    expect(screen.getByRole("group", { name: "Widget Sessions" })).toBeInTheDocument();
  });
});

describe("the chat in the app", () => {
  async function openApp() {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
  }

  it("the planner chat lists messages and Enter sends one to the planner channel", async () => {
    const user = userEvent.setup();
    state.messages = [
      message({ id: "m1", author: "you", text: "start billing-fix", created_at: "2026-11-08T13:00:00Z" }),
      message({ id: "m2", author: "planner", text: "Started **billing-fix**.", created_at: "2026-11-08T13:00:05Z" }),
    ];
    await openApp();
    const panel = screen.getByRole("complementary", { name: "Planner" });
    expect(await within(panel).findByText("start billing-fix")).toBeInTheDocument();
    expect(within(panel).getByText("billing-fix").tagName).toBe("STRONG");
    await user.type(within(panel).getByLabelText("Message the planner"), "what is blocked?{Enter}");
    await waitFor(() => expect(mocks.chat_send).toHaveBeenCalledWith({ workspace_id: "w1", channel: "planner", text: "what is blocked?" }));
    expect(await within(panel).findByText("what is blocked?")).toBeInTheDocument();
  });

  it("a card with an open question shows it on its face, and in its conversation where Answer sends the payload", async () => {
    const user = userEvent.setup();
    state.questions = [question({ card_id: "c1", seq: 2, text: "Round invoices up or to nearest?" })];
    state.messages = [
      message({ id: "m1", channel: "card:c1", author: "session", kind: "report", report_kind: "question", text: "Round invoices up or to nearest?", question_id: "q1" }),
    ];
    await openApp();

    const face = await screen.findByRole("button", { name: "Open search-console" });
    await waitFor(() => expect(within(face).getByText("Q-02")).toBeInTheDocument());
    expect(within(face).getByText("Round invoices up or to nearest?")).toHaveClass("q");

    await user.click(face);
    const panel = screen.getByRole("complementary", { name: "Card search-console" });
    const q = await within(panel).findByRole("group", { name: "Question Q-02" });
    await user.click(within(q).getByRole("button", { name: /^2\s*Up/ }));
    await user.type(within(q).getByLabelText(/Notes/), "yes please");
    await user.click(within(q).getByRole("button", { name: "Answer" }));
    await waitFor(() => expect(mocks.question_answer).toHaveBeenCalledWith({ question_id: "q1", selections: ["Up"], notes: "yes please" }));
    expect(await within(panel).findByTestId("question-answered")).toHaveTextContent("Answered: Up");
  });

  it("a card message goes to its own channel and tells where it will go", async () => {
    const user = userEvent.setup();
    await openApp();
    await user.click(screen.getByRole("button", { name: "Open search-console" }));
    const panel = screen.getByRole("complementary", { name: "Card search-console" });
    const box = await within(panel).findByLabelText("Message about this card");
    expect(box).toHaveAttribute("placeholder", "Goes to the planner tagged [ACME-508 search-console]; it relays to the session.");
    await user.type(box, "check the retry{Enter}");
    await waitFor(() => expect(mocks.chat_send).toHaveBeenCalledWith({ workspace_id: "w1", channel: "card:c1", text: "check the retry" }));
  });

  it("a full-screen chat is the same panel, centred by the full layout", async () => {
    state.fx.workspaces[0] = ws("w1", "Acme", { ui: { full: "planner" } });
    await openApp();
    const panel = screen.getByRole("complementary", { name: "Planner" });
    expect(panel).toHaveClass("full");
    expect(within(panel).getByRole("log", { name: "Conversation" })).toHaveClass("chat");
    expect(within(panel).getByLabelText("Message the planner").closest(".composer")).not.toBeNull();
  });

  it("refetches the conversation when a changed event reports new messages", async () => {
    type Cb = (e: { workspace_id: string; entities: string[] }) => void;
    const subs: Cb[] = [];
    const fire: Cb = (e) => subs.forEach((cb) => cb(e));
    mocks.onChanged.mockImplementation(((cb: Cb) => {
      subs.push(cb);
      return () => {};
    }) as never);
    await openApp();
    const panel = screen.getByRole("complementary", { name: "Planner" });
    await within(panel).findByLabelText("Message the planner");
    state.messages = [message({ id: "m9", author: "planner", text: "I started the session.", created_at: "2026-11-08T13:00:00Z" })];
    act(() => fire({ workspace_id: "w1", entities: ["messages"] }));
    expect(await within(panel).findByText("I started the session.")).toBeInTheDocument();
    mocks.onChanged.mockImplementation((() => () => {}) as never);
  });
});
