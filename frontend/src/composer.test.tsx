import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import App from "./App";
import { ChatLine } from "./components/Chat";
import { Composer } from "./components/Composer";
import { applyPick, findTrigger, fuzzy, isCommand, pathMention, unescapeSlash } from "./complete";
import { card, message, mocks, reset, running, state } from "./test/fakeApi";
import type { Card } from "./api";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

beforeEach(() => reset());

describe("the / and @ helpers", () => {
  it("finds a / only at the start of the message, and an @ only at the start of a word", () => {
    expect(findTrigger("/com", 4)).toEqual({ kind: "/", start: 0, end: 4, query: "com" });
    expect(findTrigger("/compact now", 3)).toEqual({ kind: "/", start: 0, end: 8, query: "co" });
    expect(findTrigger("/compact now", 12)).toBeNull();
    expect(findTrigger("say /compact", 12)).toBeNull();
    expect(findTrigger("//etc", 5)).toBeNull();
    expect(findTrigger("look at @src/bi", 15)).toEqual({ kind: "@", start: 8, end: 15, query: "src/bi" });
    expect(findTrigger("@", 1)).toEqual({ kind: "@", start: 0, end: 1, query: "" });
    expect(findTrigger("me@acme.dev", 11)).toBeNull();
    expect(findTrigger("a\n@x", 4)).toEqual({ kind: "@", start: 2, end: 4, query: "x" });
  });

  it("matches fuzzily and marks the matched characters", () => {
    expect(fuzzy("cl", "clear")).toEqual({ score: expect.any(Number), idx: [0, 1] });
    expect(fuzzy("sbi", "src/billing/invoice.go")?.idx).toEqual([0, 4, 5]);
    expect(fuzzy("zz", "clear")).toBeNull();
    expect(fuzzy("inv", "src/billing/invoice.go")!.score).toBeGreaterThan(fuzzy("inv", "invoices/old/readme.md")!.score);
  });

  it("inserts a pick with a space, a folder without one, and quotes a path with spaces", () => {
    expect(applyPick("/co", { kind: "/", start: 0, end: 3, query: "co" }, "/compact")).toEqual({ value: "/compact ", caret: 9 });
    expect(applyPick("see @sr and", { kind: "@", start: 4, end: 7, query: "sr" }, "@src/")).toEqual({ value: "see @src/ and", caret: 9 });
    expect(applyPick("see @bil and", { kind: "@", start: 4, end: 8, query: "bil" }, "@billing-fix")).toEqual({ value: "see @billing-fix and", caret: 17 });
    expect(pathMention("docs/release plan.md", false)).toBe('@"docs/release plan.md"');
    expect(pathMention("src/billing", true)).toBe("@src/billing/");
    expect(isCommand("/compact")).toBe(true);
    expect(isCommand("//compact")).toBe(false);
    expect(isCommand("/")).toBe(false);
    expect(unescapeSlash("//etc/hosts is here")).toBe("/etc/hosts is here");
  });
});

const cards: Card[] = [
  card("c1", "w1", "todo", "billing-fix", 0, { key: "ACME-508", title: "Fix the invoice rounding" }),
  card("c2", "w1", "todo", "docs-pass", 1, { key: "ACME-509", title: "Tidy the docs" }),
];

function setup(over: Partial<Parameters<typeof Composer>[0]> = {}) {
  const onSend = vi.fn();
  const onCommand = vi.fn().mockResolvedValue(undefined);
  render(
    <Composer
      id="cm"
      label="Message"
      placeholder="Say it"
      onSend={onSend}
      onCommand={onCommand}
      target={{ workspace_id: "w1", card: "c9" }}
      cards={cards}
      columns={[{ id: "todo", workspace_id: "w1", name: "To do", role: "", position: 0, wip: 0, fold: false, color: "" }]}
      mentionPlanner
      running
      {...over}
    />,
  );
  return { onSend, onCommand, box: screen.getByLabelText("Message") as HTMLTextAreaElement };
}

/** A menu row's name, whole: highlighting splits it into marked and plain runs. */
const named = (text: string) => (_: string, el: Element | null) => !!el?.classList.contains("sg-name") && el.textContent === text;
const options = () => screen.getAllByRole("option");
const selected = () => screen.getAllByRole("option").find((o) => o.getAttribute("aria-selected") === "true");

describe("the / menu", () => {
  it("opens on / at the start with the session's commands, their descriptions and sources", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "/");
    const list = await screen.findByRole("listbox", { name: "Slash commands" });
    expect(mocks.session_commands_list).toHaveBeenCalledWith({ workspace_id: "w1", card: "c9" });
    const row = within(list).getByText("/compact").closest("[role=option]")!;
    expect(row).toHaveTextContent("Compact the conversation");
    expect(row).toHaveTextContent("built-in");
    expect(within(list).getByText("/billing:close-month").closest("[role=option]")).toHaveTextContent("project");
    expect(within(list).getByText("/standup").closest("[role=option]")).toHaveTextContent("user skill");
    expect(box).toHaveAttribute("aria-controls", list.id);
    expect(box).toHaveFocus();
  });

  it("does not open for a / later in the message, or for //", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "say /");
    expect(screen.queryByRole("listbox")).toBeNull();
    await user.clear(box);
    await user.type(box, "//");
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("filters as you type and highlights the match", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "/cl");
    await screen.findByRole("listbox");
    const names = options().map((o) => o.querySelector(".sg-name")!.textContent);
    expect(names[0]).toBe("/clear");
    expect(names).toContain("/billing:close-month");
    expect(names).not.toContain("/cost");
    expect(Array.from(options()[0].querySelectorAll("mark")).map((m) => m.textContent).join("")).toBe("cl");
  });

  it("arrows move the selection, Enter and Tab pick, and nothing is sent", async () => {
    const user = userEvent.setup();
    const { box, onCommand, onSend } = setup();
    await user.type(box, "/c");
    await screen.findByRole("listbox");
    expect(selected()).toHaveTextContent("/cost");
    await user.keyboard("{ArrowDown}");
    expect(selected()).toHaveTextContent("/clear");
    expect(box).toHaveAttribute("aria-activedescendant", selected()!.id);
    await user.keyboard("{ArrowUp}{ArrowUp}");
    expect(selected()).toHaveTextContent("/billing:close-month"); // wraps around
    await user.keyboard("{Enter}");
    expect(box).toHaveValue("/billing:close-month ");
    expect(screen.queryByRole("listbox")).toBeNull();
    await user.clear(box);
    await user.type(box, "/comp");
    await screen.findByRole("listbox");
    await user.keyboard("{Tab}");
    expect(box).toHaveValue("/compact ");
    expect(box).toHaveFocus();
    expect(onCommand).not.toHaveBeenCalled();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("a click picks without taking the focus from the field", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "/st");
    await user.click(await screen.findByText(named("/standup")));
    expect(box).toHaveValue("/standup ");
    expect(box).toHaveFocus();
  });

  it("Esc closes it for this word, and blur closes it", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "/co");
    await screen.findByRole("listbox");
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).toBeNull();
    await user.type(box, "s");
    expect(screen.queryByRole("listbox")).toBeNull();
    await user.clear(box);
    await user.type(box, "/c");
    await screen.findByRole("listbox");
    act(() => box.blur());
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("does not open while an input method is composing, and opens once it ends", async () => {
    const { box } = setup();
    act(() => box.focus());
    fireEvent.compositionStart(box);
    fireEvent.change(box, { target: { value: "/", selectionStart: 1 } });
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole("listbox")).toBeNull();
    fireEvent.keyDown(box, { key: "Enter", isComposing: true, keyCode: 229 });
    expect(box).toHaveValue("/");
    fireEvent.compositionEnd(box);
    expect(await screen.findByRole("listbox")).toBeInTheDocument();
  });
});

describe("sending a /command", () => {
  it("goes to the terminal path, not the planner, and Enter on a full command sends it", async () => {
    const user = userEvent.setup();
    const { box, onCommand, onSend } = setup();
    await user.type(box, "/compact");
    await screen.findByRole("listbox");
    expect(screen.getByText("→ terminal")).toBeInTheDocument();
    await user.keyboard("{Enter}");
    expect(onCommand).toHaveBeenCalledWith("/compact");
    expect(onSend).not.toHaveBeenCalled();
    await waitFor(() => expect(box).toHaveValue(""));
    await user.type(box, "/model sonnet{Enter}");
    expect(onCommand).toHaveBeenLastCalledWith("/model sonnet");
  });

  it("// sends a literal slash message to the planner", async () => {
    const user = userEvent.setup();
    const { box, onCommand, onSend } = setup();
    await user.type(box, "//etc/hosts looks wrong{Enter}");
    expect(onSend).toHaveBeenCalledWith("/etc/hosts looks wrong");
    expect(onCommand).not.toHaveBeenCalled();
  });

  it("without a terminal path, a slash is plain text for the planner", async () => {
    const user = userEvent.setup();
    const { box, onSend } = setup({ onCommand: undefined });
    await user.type(box, "/compact{Enter}");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(onSend).toHaveBeenCalledWith("/compact");
  });

  it("when the session is not running it says so inline, offers Start, and sends nothing", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn().mockResolvedValue(undefined);
    const { box, onCommand } = setup({ running: false, onStart, sessionName: "billing-fix's session" });
    await user.type(box, "/cost {Enter}");
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("billing-fix's session is not running, so /cost was not sent. Start it, then send again.");
    expect(onCommand).not.toHaveBeenCalled();
    expect(box).toHaveValue("/cost ");
    await user.click(within(alert).getByRole("button", { name: "Start" }));
    expect(onStart).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });

  it("a session that stopped meanwhile gets the same inline error", async () => {
    const user = userEvent.setup();
    const onCommand = vi.fn().mockRejectedValue(new Error('card "billing-fix" has no session yet. Start it with session_start'));
    const { box } = setup({ onCommand, onStart: vi.fn() });
    await user.type(box, "/cost {Enter}");
    expect(await screen.findByRole("alert")).toHaveTextContent("is not running, so /cost was not sent");
    expect(screen.getByRole("button", { name: "Start" })).toBeInTheDocument();
    expect(box).toHaveValue("/cost ");
  });
});

describe("the @ menu", () => {
  it("opens after whitespace with cards first, then files, and inserts the pick", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "look at @bil");
    const list = await screen.findByRole("listbox", { name: "Mentions" });
    await waitFor(() => expect(within(list).getByText("Files")).toBeInTheDocument());
    const rows = options().map((o) => o.querySelector(".sg-name")!.textContent);
    expect(rows[0]).toBe("@billing-fix");
    expect(rows.indexOf("@billing-fix")).toBeLessThan(rows.indexOf("src/billing/"));
    expect(options()[0]).toHaveTextContent("ACME-508 · Fix the invoice rounding");
    expect(options()[0]).toHaveTextContent("To do");
    expect(mocks.files_search).toHaveBeenLastCalledWith({ workspace_id: "w1", card: "c9", q: "bil", limit: 40 });
    await user.keyboard("{Enter}");
    expect(box).toHaveValue("look at @billing-fix ");
  });

  it("lists @planner, and a folder pick keeps the menu open inside it", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "@plan");
    await screen.findByText(named("@planner"));
    await user.clear(box);
    await user.type(box, "@src");
    await screen.findByText(named("src/"));
    await user.click(screen.getByText(named("src/")));
    expect(box).toHaveValue("@src/");
    await waitFor(() => expect(mocks.files_search).toHaveBeenLastCalledWith(expect.objectContaining({ q: "src/" })));
    expect(await screen.findByText(named("src/billing/invoice.go"))).toBeInTheDocument();
  });

  it("quotes a path with spaces", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "read @release");
    await user.click(await screen.findByText(named("docs/release plan.md")));
    expect(box).toHaveValue('read @"docs/release plan.md" ');
  });

  it("does not open inside a word such as an email address", async () => {
    const user = userEvent.setup();
    const { box } = setup();
    await user.type(box, "mail me@acme");
    await new Promise((r) => setTimeout(r, 150));
    expect(screen.queryByRole("listbox")).toBeNull();
  });
});

describe("in the app", () => {
  async function openCard() {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Open search-console" }));
    const panel = screen.getByRole("complementary", { name: "Card search-console" });
    return { user, panel, box: await within(panel).findByLabelText("Message about this card") };
  }

  it("a card's /command calls chat_command, not chat_send, and shows as you → terminal", async () => {
    state.sessions.c1 = running("sess-c1", { card_id: "c1" });
    const { user, panel, box } = await openCard();
    await user.type(box, "/cost{Enter}");
    await waitFor(() => expect(mocks.chat_command).toHaveBeenCalledWith({ workspace_id: "w1", channel: "card:c1", text: "/cost" }));
    expect(mocks.chat_send).not.toHaveBeenCalled();
    expect(await within(panel).findByText(/you → terminal/)).toBeInTheDocument();
    expect(within(panel).getByText("/cost")).toHaveClass("mono");
    expect(mocks.session_commands_list).toHaveBeenCalledWith({ workspace_id: "w1", card: "c1" });
  });

  it("a card whose session is not running offers Start, which starts it", async () => {
    const { user, panel, box } = await openCard();
    await waitFor(() => expect(mocks.session_status).toHaveBeenCalled());
    await user.type(box, "/cost {Enter}");
    const alert = await within(panel).findByRole("alert");
    expect(alert).toHaveTextContent("search-console's session is not running");
    expect(mocks.chat_command).not.toHaveBeenCalled();
    await user.click(within(alert).getByRole("button", { name: "Start" }));
    await waitFor(() => expect(mocks.session_start).toHaveBeenCalledWith({ card: "c1" }));
  });

  it("the planner chat sends a /command to the planner's terminal and // to the planner chat", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    const user = userEvent.setup();
    const panel = screen.getByRole("complementary", { name: "Planner" });
    const box = within(panel).getByLabelText("Message the planner");
    await user.type(box, "/context{Enter}");
    await waitFor(() => expect(mocks.chat_command).toHaveBeenCalledWith({ workspace_id: "w1", channel: "planner", text: "/context" }));
    await user.type(box, "//context means the window{Enter}");
    await waitFor(() => expect(mocks.chat_send).toHaveBeenCalledWith({ workspace_id: "w1", channel: "planner", text: "/context means the window" }));
    await user.type(box, "@sea");
    expect(await within(panel).findByText(named("@search-console"))).toBeInTheDocument();
    expect(within(panel).queryByText(named("@planner"))).toBeNull();
    await waitFor(() => expect(mocks.files_search).toHaveBeenCalledWith(expect.objectContaining({ workspace_id: "w1", planner: true, q: "sea" })));
  });

  it("draws a stored command as a mono pill", () => {
    render(<ChatLine m={message({ author: "you", kind: "command", text: "/compact keep notes" })} />);
    expect(screen.getByText(/you → terminal/)).toBeInTheDocument();
    expect(screen.getByText("/compact keep notes").closest(".b-cmd")).not.toBeNull();
  });
});
