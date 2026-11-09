import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { vi } from "vitest";
import { ChatLine, QuestionCard } from "./components/Chat";
import { Markdown } from "./components/Markdown";
import { message, question } from "./test/fakeApi";

const openURL = vi.fn();
vi.mock("../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: (...a: unknown[]) => openURL(...a), EventsOn: () => () => {} }));
vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => openURL.mockClear());

const TABLE = [
  "Both PRs are open.",
  "",
  "| Repo | PR | Checks |",
  "|---|---|---|",
  "| acme-web | [#12](https://github.com/acme/acme-web/pull/12) | [success](https://ci.example.com/runs/1) |",
  "| acme-api | `#7` | pending |",
].join("\n");

describe("markdown in chat", () => {
  it("draws a GFM table in a planner message, in a scrolling region", () => {
    render(<ChatLine m={message({ author: "planner", text: TABLE })} />);
    const table = screen.getByRole("table");
    expect(within(table).getAllByRole("columnheader").map((h) => h.textContent)).toEqual(["Repo", "PR", "Checks"]);
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    expect(screen.getByRole("region", { name: "Table" })).toHaveClass("md-table");
    expect(screen.queryByText(/\|---\|/)).toBeNull();
    expect(within(table).getByText("#7").tagName).toBe("CODE");
  });

  it("opens links in the system browser and never navigates the window", () => {
    render(<Markdown text="See [success](https://ci.example.com/runs/1) or <https://example.com/a/very/long/path>." />);
    const link = screen.getByRole("link", { name: "success" });
    const ev = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
    expect(openURL).toHaveBeenCalledWith("https://ci.example.com/runs/1");
    fireEvent.click(screen.getByRole("link", { name: "https://example.com/a/very/long/path" }));
    expect(openURL).toHaveBeenLastCalledWith("https://example.com/a/very/long/path");
  });

  it("does not make a link of javascript: or other schemes", () => {
    render(<Markdown text="[click](javascript:alert(1)) and [file](file:///etc/passwd)" />);
    expect(screen.queryAllByRole("link")).toHaveLength(0);
    expect(screen.getByText("click")).toHaveClass("md-link-off");
  });

  it("shows inline code, and fenced code with a Copy button", async () => {
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    const { container } = render(<Markdown text={"Run `make test`:\n\n```bash\nmake -C acme-web test\nmake build\n```"} />);
    expect(screen.getByText("make test").tagName).toBe("CODE");
    expect(container.querySelector(".md-pre pre")?.textContent).toBe("make -C acme-web test\nmake build");
    expect(container.querySelector(".md-pre-bar")?.textContent).toContain("bash");
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }));
    expect(writeText).toHaveBeenCalledWith("make -C acme-web test\nmake build");
    await waitFor(() => expect(screen.getByRole("button", { name: "Copy code" })).toHaveTextContent("Copied"));
  });

  it("never renders raw HTML or loads a remote image", () => {
    const { container } = render(
      <Markdown text={'Hi <script>window.pwned=1</script> <img src="https://evil.example/x.png" onerror="alert(1)"> <b onclick="x()">b</b>\n\n![chart](https://evil.example/chart.png)'} />,
    );
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("[onclick]")).toBeNull();
    expect((window as unknown as { pwned?: number }).pwned).toBeUndefined();
    expect(screen.getByText("[image: chart]")).toBeInTheDocument();
  });

  it("renders lists, headings, quotes, emphasis and task lists", () => {
    const { container } = render(<Markdown text={"## Plan\n\n- **bold** and *italic*\n- [x] done\n- [ ] todo\n\n1. one\n2. two\n\n> quoted"} />);
    expect(screen.getByRole("heading", { level: 2, name: "Plan" })).toBeInTheDocument();
    expect(screen.getByText("bold").tagName).toBe("STRONG");
    expect(screen.getByText("italic").tagName).toBe("EM");
    expect(screen.getByRole("checkbox", { name: "done" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "not done" })).not.toBeChecked();
    expect(container.querySelector("ol")?.children).toHaveLength(2);
    expect(container.querySelector("blockquote")?.textContent).toContain("quoted");
  });

  it("renders your messages, reports and questions as markdown too", () => {
    render(
      <>
        <ChatLine m={message({ id: "a", author: "you", text: "use **acme-web**" })} />
        <ChatLine m={message({ id: "b", author: "session", kind: "report", report_kind: "done", text: "Merged `ACME-12`" })} />
        <QuestionCard q={question({ text: "Ship **ACME-12**?", context: "See [the run](https://ci.example.com/runs/2)" })} onAnswer={() => {}} />
      </>,
    );
    expect(screen.getByText("acme-web").tagName).toBe("STRONG");
    expect(screen.getByText("ACME-12", { selector: "code" })).toBeInTheDocument();
    expect(screen.getByText("ACME-12", { selector: "strong" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "the run" })).toBeInTheDocument();
  });
});
