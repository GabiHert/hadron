import { render, screen, waitFor, within, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { alphaOf, withAlpha } from "./components/AppearanceTab";
import { ChatThread } from "./components/Chat";
import { message, mocks, question, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => {
  reset();
  delete document.documentElement.dataset.glass;
});

const glass = () => document.documentElement.dataset.glass;

async function openAppearance() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("button", { name: "Settings" }));
  await user.click(within(screen.getByRole("navigation", { name: "Settings sections" })).getByRole("button", { name: "Appearance" }));
  await screen.findByRole("heading", { name: "Appearance" });
  return user;
}

describe("glass chats", () => {
  it("is on by default and solid while macOS reduces transparency", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await waitFor(() => expect(glass()).toBe("on"));
    state.reduceTransparency = true;
    fireEvent.focus(window);
    await waitFor(() => expect(glass()).toBe("solid"));
  });

  it("the Appearance switch turns it off and back on", async () => {
    const user = await openAppearance();
    const box = await screen.findByRole("checkbox", { name: "Glass chats" });
    await waitFor(() => expect(box).toBeEnabled());
    expect(box).toBeChecked();
    await user.click(box);
    await waitFor(() => expect(mocks.settings_update).toHaveBeenCalledWith({ values: { "ui.glass_chats": false } }));
    await waitFor(() => expect(glass()).toBe("off"));
    await user.click(screen.getByRole("checkbox", { name: "Glass chats" }));
    await waitFor(() => expect(glass()).toBe("on"));
  });

  it("says why the chats are solid", async () => {
    state.reduceTransparency = true;
    await openAppearance();
    expect(await screen.findByText(/Reduce transparency is on/)).toBeInTheDocument();
  });

  it("puts every line of a thread on the glass pane", () => {
    const msgs = [
      message({ id: "a", author: "you", text: "Ship it?" }),
      message({ id: "b", author: "planner", text: "On it." }),
      message({ id: "c", author: "session", kind: "report", report_kind: "done", text: "PR is up" }),
      message({ id: "d", author: "planner", kind: "widget", widget_title: "Plan", widget_html: "<p>x</p>" }),
      message({ id: "e", author: "session", kind: "report", report_kind: "question", question_id: "q1", text: "?" }),
    ];
    const { container } = render(
      <ChatThread className="chat" workspaceId="w1" scope="planner:w1" onError={() => {}} messages={msgs} questions={[question()]} onAnswer={async () => {}} empty={null} />,
    );
    const wrap = container.querySelector(".glasswrap");
    const log = screen.getByRole("log", { name: "Conversation" });
    expect(wrap?.firstElementChild).toBe(log);
    for (const sel of [".b-me", ".b-pl", ".b-rep", ".widget", ".qbox"]) expect(log.querySelector(sel)).not.toBeNull();
  });

  it("the theme editor keeps a glass token's opacity", async () => {
    expect(alphaOf("#ffffff8c")).toBeCloseTo(0.549, 2);
    expect(alphaOf("#ffffff")).toBe(1);
    expect(withAlpha("#ABCDEF", 0.5)).toBe("#abcdef80");
    expect(withAlpha("#abcdef80", 1)).toBe("#abcdef");

    const user = await openAppearance();
    await user.click(await screen.findByRole("button", { name: "Duplicate Nucleus" }));
    const editor = screen.getByRole("group", { name: "Theme editor" });
    fireEvent.change(within(editor).getByLabelText("glass hex"), { target: { value: "#ffffff8c" } });
    expect(within(editor).getByLabelText("glass hex")).toHaveValue("#ffffff8c");
    fireEvent.change(within(editor).getByLabelText("glass opacity"), { target: { value: "30" } });
    expect(within(editor).getByLabelText("glass hex")).toHaveValue("#ffffff4d");
    fireEvent.change(within(editor).getByLabelText("glass colour"), { target: { value: "#102030" } });
    expect(within(editor).getByLabelText("glass hex")).toHaveValue("#1020304d");
    expect(within(editor).queryByLabelText("bg opacity")).toBeNull();
  });

  it("floats the header and composer over the chat only in the chat views", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    const planner = screen.getByRole("complementary", { name: "Planner" });
    expect(planner).toHaveAttribute("data-overlay");
    expect(planner.querySelector(":scope > .ghead")).not.toBeNull();
    expect(planner.querySelector(":scope > .composer")).not.toBeNull();

    await user.click(screen.getByRole("button", { name: /Open search-console/ }));
    const card = await screen.findByRole("complementary", { name: "Card search-console" });
    expect(card).toHaveAttribute("data-overlay");
    expect(card.querySelector(":scope > .ghead")).not.toBeNull();
    await user.click(within(card).getByRole("tab", { name: /Links/ }));
    await waitFor(() => expect(card).not.toHaveAttribute("data-overlay"));
    await user.click(within(card).getByRole("tab", { name: /Conversation/ }));
    await waitFor(() => expect(card).toHaveAttribute("data-overlay"));
  });
});
