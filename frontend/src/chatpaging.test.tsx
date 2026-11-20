import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { CHAT_PAGE } from "./state/useChannel";
import { message, mocks, question, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);
vi.mock("./pty", async () => (await import("./test/fakeTerm")).ptyMock);
vi.mock("@xterm/xterm", async () => (await import("./test/fakeTerm")).xtermModule);
vi.mock("@xterm/addon-fit", async () => (await import("./test/fakeTerm")).fitModule);

const planner = (n: number) =>
  Array.from({ length: n }, (_, i) => message({ id: `p${i}`, channel: "planner", author: i % 2 ? "planner" : "you", text: `line ${i}` }));

async function openApp() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  return screen.getByRole("complementary", { name: "Planner" });
}

describe("a long conversation holds its newest messages", () => {
  beforeEach(() => reset());

  it(`asks for the newest ${CHAT_PAGE}, and "Show earlier messages" adds ${CHAT_PAGE} more without emptying the thread`, async () => {
    state.messages = planner(CHAT_PAGE * 2 + 50);
    const panel = await openApp();
    const log = within(panel).getByRole("log", { name: "Conversation" });
    expect(await within(log).findByText(`line ${CHAT_PAGE * 2 + 49}`)).toBeInTheDocument();
    expect(mocks.chat_read).toHaveBeenCalledWith(expect.objectContaining({ channel: "planner", limit: CHAT_PAGE }));
    expect(within(log).queryByText("line 0")).toBeNull();
    expect(within(log).queryByText(`line ${CHAT_PAGE + 49}`)).toBeNull();

    await userEvent.click(within(log).getByRole("button", { name: "Show earlier messages" }));
    expect(await within(log).findByText(`line ${CHAT_PAGE + 49}`)).toBeInTheDocument();
    expect(mocks.chat_read).toHaveBeenLastCalledWith(expect.objectContaining({ limit: CHAT_PAGE * 2 }));
    // The newest lines never left the screen while the older ones loaded.
    expect(within(log).getByText(`line ${CHAT_PAGE * 2 + 49}`)).toBeInTheDocument();

    await userEvent.click(within(log).getByRole("button", { name: "Show earlier messages" }));
    expect(await within(log).findByText("line 0")).toBeInTheDocument();
    // Everything is held: the button goes away.
    await waitFor(() => expect(within(log).queryByRole("button", { name: "Show earlier messages" })).toBeNull());
  });

  it("a short conversation has no button", async () => {
    state.messages = planner(12);
    const panel = await openApp();
    expect(await within(panel).findByText("line 11")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: "Show earlier messages" })).toBeNull();
  });

  it("an open question older than the messages held still shows, and can be answered", async () => {
    state.questions = [question({ id: "q-old", card_id: "c1", seq: 3, text: "Ship the ledger fix today?" })];
    state.messages = [
      message({ id: "q-msg", channel: "card:c1", author: "session", kind: "report", report_kind: "question", text: "Ship the ledger fix today?", question_id: "q-old" }),
      ...Array.from({ length: CHAT_PAGE + 5 }, (_, i) => message({ id: `c${i}`, channel: "card:c1", author: "session", kind: "report", report_kind: "progress", text: `step ${i}` })),
    ];
    await openApp();
    await userEvent.click(await screen.findByRole("button", { name: "Open search-console" }));
    const card = screen.getByRole("complementary", { name: "Card search-console" });
    expect(await within(card).findByText(`step ${CHAT_PAGE + 4}`)).toBeInTheDocument();
    expect(within(card).queryByText("step 0")).toBeNull();
    expect(within(card).getByRole("group", { name: "Question Q-03" })).toBeInTheDocument();
  });
});
