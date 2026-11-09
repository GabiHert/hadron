import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { Widget, clampHeight, widgetSandbox, widgetSrcDoc } from "./components/Widget";
import { dashBlock, message, mocks, reset, state } from "./test/fakeApi";

vi.mock("./api", async () => (await import("./test/fakeApi")).apiMock);

beforeEach(() => reset());

const CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:";

describe("widget security", () => {
  it.each([false, true])("sandboxes the iframe (scripts=%s)", (scripts) => {
    const { container } = render(<Widget title="W" html="<p>hi</p>" scripts={scripts} data={{ a: 1 }} />);
    const f = container.querySelector("iframe")!;
    expect(f.hasAttribute("sandbox")).toBe(true);
    expect(f.getAttribute("sandbox")).not.toContain("allow-same-origin");
    expect(f.getAttribute("sandbox")).toBe(scripts ? "allow-scripts" : "");
    expect(f.hasAttribute("srcdoc")).toBe(true);
    expect(f.hasAttribute("src")).toBe(false);
    expect(f.getAttribute("title")).toBe("W");
    const doc = f.getAttribute("srcdoc")!;
    expect(doc.startsWith('<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy"')).toBe(true);
    expect(doc).toContain(`content="${scripts ? CSP + "; script-src 'unsafe-inline'" : CSP}"`);
    expect(doc.includes("script-src")).toBe(scripts);
    expect(doc).toContain("--fg:");
    expect(doc).toContain("--acc:");
    expect(doc).toContain("body{margin:0;font:13px/1.5 system-ui,sans-serif;color:var(--fg);background:transparent}");
    expect(doc.includes("hadronHeight")).toBe(scripts);
  });

  it("escapes data so it cannot close the data block", () => {
    const doc = widgetSrcDoc("<p>x</p>", { scripts: false, data: { t: "</script><img src=x onerror=alert(1)><!--" } });
    const start = doc.indexOf('id="hadron-data">') + 'id="hadron-data">'.length;
    const end = doc.indexOf("</script>", start);
    const json = doc.slice(start, end);
    expect(json).not.toContain("<");
    expect(JSON.parse(json).t).toBe("</script><img src=x onerror=alert(1)><!--");
  });

  it("has an empty sandbox helper and clamps heights to 80..1200", () => {
    expect(widgetSandbox(false)).toBe("");
    expect(widgetSandbox(true)).toBe("allow-scripts");
    expect(clampHeight(10)).toBe(80);
    expect(clampHeight(5000)).toBe(1200);
    expect(clampHeight(300)).toBe(300);
    const { container } = render(<Widget title="W" html="" height={9999} scripts={false} />);
    expect(container.querySelector("iframe")!.style.height).toBe("1200px");
    const d = render(<Widget title="D" html="" scripts={false} />);
    expect(d.container.querySelector("iframe")!.style.height).toBe("220px");
  });
});

async function openDashboard() {
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Acme" });
  await user.click(screen.getByRole("tab", { name: /Dashboard/ }));
  await screen.findByRole("region", { name: "Cards by column" });
  return user;
}

describe("dashboard", () => {
  beforeEach(() => {
    state.dash = [
      dashBlock("b1", { type: "stats", title: "Cards by column", span: 1, source: { query: "cards.count" },
        data: { query: "cards.count", shape: "groups", groups: [{ key: "n", label: "Needs you", count: 1 }, { key: "w", label: "Working", count: 3 }], rows: [] } }),
      dashBlock("b2", { type: "bars", title: "By assignee", position: 1, source: { query: "cards.count" },
        data: { query: "cards.count", shape: "groups", groups: [{ key: "you", label: "You", count: 3 }, { key: "n", label: "Ana", count: 1 }], rows: [] } }),
      dashBlock("b3", { type: "table", title: "Waiting on you", position: 2, source: { query: "questions.open" },
        data: { query: "questions.open", shape: "rows", groups: [], rows: [{ id: "q1", card_id: "c1", card: "search-console", key: "ACME-508", label: "Q-01", kind: "decision", text: "Round up?" }] } }),
      dashBlock("b4", { type: "planner", title: "Planner's summary", position: 3 }),
    ];
  });

  it("renders the block types", async () => {
    await openDashboard();
    const stats = screen.getByRole("region", { name: "Cards by column" });
    expect(within(stats).getByText("Needs you")).toBeInTheDocument();
    expect(within(stats).getByText("3")).toBeInTheDocument();
    const bars = screen.getByRole("region", { name: "By assignee" });
    expect(within(bars).getByText("Ana")).toBeInTheDocument();
    const table = screen.getByRole("region", { name: "Waiting on you" });
    expect(within(table).getByRole("button", { name: "search-console" })).toBeInTheDocument();
    expect(within(table).getByText("Q-01")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Planner's summary" })).getByText(/has not written this yet/)).toBeInTheDocument();
  });

  it("rewrites a planner block, toggles width, moves and removes", async () => {
    const user = await openDashboard();
    const planner = screen.getByRole("region", { name: "Planner's summary" });
    await user.click(within(planner).getByRole("button", { name: "Rewrite now" }));
    expect(mocks.dash_block_refresh).toHaveBeenCalledWith({ block_id: "b4" });
    expect(await screen.findByText("Asked the planner to write it")).toBeInTheDocument();

    const stats = screen.getByRole("region", { name: "Cards by column" });
    expect(within(stats).getByRole("button", { name: "Move earlier" })).toBeDisabled();
    await user.click(within(stats).getByRole("button", { name: "Full width" }));
    expect(mocks.dash_block_update).toHaveBeenCalledWith({ block_id: "b1", span: 2 });

    await user.click(within(screen.getByRole("region", { name: "By assignee" })).getByRole("button", { name: "Move later" }));
    expect(mocks.dash_block_move).toHaveBeenCalledWith({ block_id: "b2", position: 2 });

    await user.click(screen.getByRole("button", { name: "Remove By assignee" }));
    expect(mocks.dash_block_remove).toHaveBeenCalledWith({ block_id: "b2" });
    await waitFor(() => expect(screen.queryByRole("region", { name: "By assignee" })).toBeNull());
  });

  it("adds a bars block by assignee", async () => {
    const user = await openDashboard();
    await user.click(screen.getByRole("button", { name: "Add block" }));
    await screen.findByRole("option", { name: "Assignee" });
    await user.selectOptions(screen.getByLabelText("Count by"), "card.assignee");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(mocks.dash_block_add).toHaveBeenCalledWith(expect.objectContaining({ type: "bars", source: { query: "cards.count", args: { by: "card.assignee" } } }));
  });

  it("shows the empty state", async () => {
    state.dash = [];
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("tab", { name: /Dashboard/ }));
    expect(await screen.findByText(/The dashboard is empty/)).toBeInTheDocument();
  });
});

describe("chat widgets", () => {
  it("renders a sandboxed widget and pins it", async () => {
    state.messages = [message({ id: "m9", kind: "widget", widget_title: "Burndown", widget_html: "<p>x</p>" })];
    const user = userEvent.setup();
    const { container } = render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    const group = await screen.findByRole("group", { name: "Widget Burndown" });
    const f = group.querySelector("iframe")!;
    expect(f.getAttribute("sandbox")).not.toContain("allow-same-origin");
    expect(container.querySelectorAll("iframe[src]").length).toBe(0);
    await user.click(within(group).getByRole("button", { name: "Pin Burndown to the dashboard" }));
    expect(mocks.widget_pin).toHaveBeenCalledWith({ message_id: "m9" });
    expect(await within(group).findByRole("button", { name: /on the dashboard/i })).toBeDisabled();
  });
});
