import { act, render, screen, waitFor } from "@testing-library/react";
import App from "./App";
import { mocks, reset } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

let listeners: ((e: unknown) => void)[] = [];
beforeEach(() => {
  reset();
  listeners = [];
  mocks.onChanged.mockImplementation(((cb: (e: unknown) => void) => {
    listeners.push(cb);
    return () => void (listeners = listeners.filter((l) => l !== cb));
  }) as never);
});

const fire = (e: unknown) => act(() => listeners.forEach((l) => l(e)));
const settle = () => act(() => new Promise((r) => setTimeout(r, 80)));

async function openBoard() {
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await settle();
  mocks.workspace_list.mockClear();
  mocks.card_list.mockClear();
  mocks.column_list.mockClear();
}

describe("refetching the board on change events", () => {
  it("reloads only the workspace a burst of events names, once", async () => {
    await openBoard();
    for (let i = 0; i < 20; i++) fire({ workspace_id: "w1", entities: ["cards", "sessions"] });
    await settle();
    expect(mocks.card_list.mock.calls).toEqual([[{ workspace_id: "w1" }]]);
    expect(mocks.workspace_list).not.toHaveBeenCalled();
  });

  it("does not reload the board for the MCP call log or chat messages", async () => {
    await openBoard();
    fire({ workspace_id: "w1", entities: ["mcp_calls"] });
    fire({ workspace_id: "w2", entities: ["messages"] });
    await settle();
    expect(mocks.card_list).not.toHaveBeenCalled();
    expect(mocks.column_list).not.toHaveBeenCalled();
  });

  it("reloads everything for workspace or settings changes, events with no workspace, and events with no entities", async () => {
    await openBoard();
    for (const e of [
      { workspace_id: "w1", entities: ["workspaces"] },
      { workspace_id: "w1", entities: ["settings"] },
      { workspace_id: "", entities: ["cards"] },
      { workspace_id: "w2", entities: [] },
    ]) {
      mocks.workspace_list.mockClear();
      mocks.card_list.mockClear();
      fire(e);
      await waitFor(() => expect(mocks.workspace_list).toHaveBeenCalledTimes(1));
      await waitFor(() => expect(mocks.card_list).toHaveBeenCalledTimes(2));
    }
  });

  it("shows a card changed in the workspace an event names", async () => {
    await openBoard();
    const cards = await mocks.card_list({ workspace_id: "w1" });
    mocks.card_list.mockImplementationOnce((async () => cards.map((c) => (c.id === "c3" ? { ...c, nick: "renamed-by-session" } : c))) as never);
    fire({ workspace_id: "w1", entities: ["cards"] });
    expect(await screen.findByText("renamed-by-session")).toBeInTheDocument();
    expect(mocks.card_list.mock.calls.slice(1)).toEqual([[{ workspace_id: "w1" }]]);
  });

  it("an open card's chat reloads for messages in its workspace only", async () => {
    await openBoard();
    const { default: userEvent } = await import("@testing-library/user-event");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Open search-console" }));
    await screen.findByRole("complementary", { name: /Card search-console/ });
    await settle();
    mocks.chat_read.mockClear();
    mocks.session_status.mockClear();
    fire({ workspace_id: "w2", entities: ["messages"] });
    fire({ workspace_id: "w1", entities: ["mcp_calls"] });
    await settle();
    expect(mocks.chat_read).not.toHaveBeenCalled();
    expect(mocks.session_status).not.toHaveBeenCalled();
    fire({ workspace_id: "w1", entities: ["messages"] });
    await waitFor(() => expect(mocks.chat_read).toHaveBeenCalled());
  });
});
