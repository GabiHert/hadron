import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import type { McpCall, McpInfo } from "./api";
import { McpPanel, argsText, maskToken } from "./components/McpPanel";

const info: McpInfo = {
  running: true, url: "http://127.0.0.1:47820/mcp", port: 47820, token: "0123456789abcdef", tool_count: 61,
  connect_command: 'claude mcp add --transport http hadron http://127.0.0.1:47820/mcp --header "Authorization: Bearer 0123456789abcdef"',
};
const call = (over: Partial<McpCall>): McpCall => ({
  id: "x", created_at: "2026-11-08T14:03:09Z", workspace_id: "w1", actor: "you", tool: "card_list", args: {}, ok: true, error: "", duration_ms: 3, ...over,
});
const fixtures: McpCall[] = [
  call({ id: "1", actor: "session:billing-fix", tool: "card_report", args: { kind: "progress", text: "tests green" }, duration_ms: 12 }),
  call({ id: "2", actor: "planner", tool: "chat_post", args: { channel: "planner", text: "hello" } }),
  call({ id: "3", actor: "external", tool: "card_get", args: { card_id: "nope" }, ok: false, error: "card does not exist" }),
];

function setup(calls: McpCall[] = fixtures) {
  const client = { info: vi.fn(async () => info), calls: vi.fn(async () => calls) };
  const handlers: Array<() => void> = [];
  const on = vi.fn((cb: () => void) => {
    handlers.push(cb);
    return () => {};
  });
  const copy = vi.fn(async () => {});
  render(<McpPanel client={client} on={on} copy={copy} />);
  return { client, copy, handlers };
}

test("shows the endpoint, the tool count and a masked token", async () => {
  setup();
  expect(await screen.findByTestId("mcp-url")).toHaveTextContent("http://127.0.0.1:47820/mcp");
  expect(screen.getByTestId("mcp-tools")).toHaveTextContent("61");
  expect(screen.getByTestId("mcp-token")).toHaveTextContent("••••••••cdef");
  expect(screen.getByTestId("mcp-token")).not.toHaveTextContent("0123456789");
});

test("Copy puts the real token on the clipboard", async () => {
  const { copy } = setup();
  await screen.findByTestId("mcp-url");
  await userEvent.click(screen.getByRole("button", { name: "Copy" }));
  expect(copy).toHaveBeenCalledWith("0123456789abcdef");
});

test("the call log renders who, tool, arguments, time and outcome from fixtures", async () => {
  setup();
  const rows = await screen.findAllByTestId("call-row");
  expect(rows).toHaveLength(3);
  expect(within(rows[0]).getByText("session:billing-fix")).toBeInTheDocument();
  expect(within(rows[0]).getByText("card_report")).toBeInTheDocument();
  expect(within(rows[0]).getByText(/"text":"tests green"/)).toBeInTheDocument();
  expect(within(rows[0]).getByText(/12 ms/)).toBeInTheDocument();
  expect(within(rows[1]).getByText("planner")).toBeInTheDocument();
  expect(within(rows[2]).getByText("external")).toBeInTheDocument();
  expect(within(rows[2]).getByText("failed")).toHaveAttribute("title", "card does not exist");
});

test("the log refetches when the backend says something changed", async () => {
  const { client, handlers } = setup([]);
  expect(await screen.findByText(/No calls yet/)).toBeInTheDocument();
  client.calls.mockResolvedValueOnce(fixtures);
  act(() => handlers[0]());
  await waitFor(() => expect(screen.getAllByTestId("call-row")).toHaveLength(3));
});

test("the connect snippet uses a placeholder, and the copy button carries the real token", async () => {
  const { copy } = setup();
  const code = await screen.findByLabelText("Connect command");
  expect(code).toHaveTextContent("Bearer <token>");
  await userEvent.click(screen.getByRole("button", { name: "Copy command with token" }));
  expect(copy).toHaveBeenCalledWith(info.connect_command);
});

test("helpers", () => {
  expect(maskToken("")).toBe("—");
  expect(argsText({})).toBe("");
  expect(argsText({ a: "x".repeat(200) }, 20)).toHaveLength(20);
});

describe("user-only actions setting", () => {
  function setupMode(initial: string, fail = false) {
    const client = { info: vi.fn(async () => info), calls: vi.fn(async () => []) };
    const userActions = {
      get: vi.fn(async () => initial),
      set: vi.fn(async (_v: string) => {
        if (fail) throw new Error("not allowed");
      }),
    };
    render(<McpPanel client={client} on={() => () => {}} copy={async () => {}} userActions={userActions} />);
    return userActions;
  }

  test("shows the current value and one line for each choice", async () => {
    setupMode("all");
    const all = await screen.findByRole("radio", { name: "Everyone, including the planner" });
    await waitFor(() => expect(all).toBeChecked());
    expect(screen.getByRole("radio", { name: "Your own clients" })).not.toBeChecked();
    expect(screen.getByText(/Only the buttons in this window may/)).toBeInTheDocument();
    expect(screen.getByText(/The planner and card sessions may call them too/)).toBeInTheDocument();
    expect(screen.getByText(/no session header\) may call/)).toBeInTheDocument();
  });

  test("choosing a value saves it", async () => {
    const ua = setupMode("external");
    await userEvent.click(await screen.findByRole("radio", { name: "Only this app" }));
    expect(ua.set).toHaveBeenCalledWith("off");
    await waitFor(() => expect(screen.getByRole("radio", { name: "Only this app" })).toBeChecked());
  });

  test("a refused save keeps the old value and shows the error", async () => {
    setupMode("external", true);
    await userEvent.click(await screen.findByRole("radio", { name: "Only this app" }));
    expect(await screen.findByText("not allowed")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Your own clients" })).toBeChecked();
  });
});
