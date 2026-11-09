import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
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
  it("renders a sandboxed widget and pins it from its menu", async () => {
    state.messages = [message({ id: "m9", kind: "widget", widget_title: "Burndown", widget_html: "<p>x</p>" })];
    const user = userEvent.setup();
    const { container } = render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    const group = await screen.findByRole("group", { name: "Widget Burndown" });
    const f = group.querySelector("iframe")!;
    expect(f.getAttribute("sandbox")).not.toContain("allow-same-origin");
    expect(container.querySelectorAll("iframe[src]").length).toBe(0);
    expect(within(group).queryByRole("button", { name: /pin|dashboard/i })).toBeNull();
    await user.click(within(group).getByRole("button", { name: "More for Burndown" }));
    await user.click(within(group).getByRole("menuitem", { name: "Add to dashboard" }));
    expect(mocks.widget_pin).toHaveBeenCalledWith({ message_id: "m9" });
    await user.click(within(group).getByRole("button", { name: "More for Burndown" }));
    expect(await within(group).findByRole("menuitem", { name: "On the dashboard ✓" })).toBeDisabled();
  });
});

describe("heatmap calendar and cached-query status", () => {
  const heat = (over: Partial<Parameters<typeof dashBlock>[1]> & { groups?: { key: string; label: string; count: number }[] } = {}) => {
    const { groups, ...rest } = over;
    return dashBlock("h1", {
      type: "heatmap", title: "Deploys", span: 2, source: { query: "acme.deploys", unit: "deploys", weeks: 4 },
      data: { query: "acme.deploys", shape: "groups", groups: groups ?? [{ key: "2025-11-03", label: "3 Nov", count: 4 }, { key: "2025-11-04", label: "4 Nov", count: 1 }], rows: [] },
      ...rest,
    });
  };
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(2025, 10, 5, 12, 0, 0));
  });
  afterEach(() => vi.useRealTimers());

  async function show(b: ReturnType<typeof heat>) {
    state.dash = [b];
    const user = userEvent.setup({ advanceTimers: () => {} });
    render(<App />);
    await screen.findByRole("heading", { name: "Acme" });
    await user.click(screen.getByRole("tab", { name: /Dashboard/ }));
    return await screen.findByRole("region", { name: b.title });
  }

  it("draws a calendar for ISO-date groups with tooltips, levels, legend and total", async () => {
    const r = await show(heat());
    const img = within(r).getByRole("img");
    expect(img.getAttribute("aria-label")).toBe("Deploys: 5 deploys in the last 4 weeks");
    const mon = r.querySelector('[data-date="2025-11-03"]')!;
    expect(mon.getAttribute("title")).toBe("Mon 3 Nov · 4 deploys");
    expect(mon.getAttribute("aria-label")).toBe("Mon 3 Nov · 4 deploys");
    expect(mon.getAttribute("data-level")).toBe("4");
    expect(r.querySelector('[data-date="2025-11-04"]')!.getAttribute("data-level")).toBe("1");
    expect(r.querySelector('[data-date="2025-11-05"]')!.getAttribute("title")).toBe("Wed 5 Nov · 0 deploys");
    expect(r.querySelector('[data-date="2025-11-06"]')).toBeNull();
    expect(within(r).getByText("5 deploys in the last 4 weeks")).toBeInTheDocument();
    expect(within(r).getByText("Less")).toBeInTheDocument();
    expect(within(r).getByText("More")).toBeInTheDocument();
    expect(r.querySelectorAll("[data-legend-level]").length).toBe(5);
    expect(within(r).getByText("Mon")).toBeInTheDocument();
    expect(within(r).getByText("Nov")).toBeInTheDocument();
  });

  it("keeps the strip for hour keys", async () => {
    const r = await show(heat({ title: "Tool calls by hour", source: { query: "mcp.calls" }, groups: [{ key: "00", label: "00", count: 2 }, { key: "01", label: "01", count: 5 }] }));
    expect(r.querySelector("[data-date]")).toBeNull();
    expect(r.querySelector('[title="01: 5"]')).not.toBeNull();
  });

  it("honours layout strip over date keys", async () => {
    const r = await show(heat({ source: { query: "acme.deploys", layout: "strip" } }));
    expect(r.querySelector("[data-date]")).toBeNull();
    expect(r.querySelector('[title="3 Nov: 4"]')).not.toBeNull();
  });

  it("shows only the error when a calendar was asked for but keys are not dates", async () => {
    const r = await show(heat({ source: { query: "q", layout: "calendar" }, groups: [{ key: "00", label: "00", count: 1 }], error: "heatmap calendar needs YYYY-MM-DD keys" }));
    expect(within(r).getByRole("note")).toHaveTextContent("needs YYYY-MM-DD");
    expect(r.querySelector("[data-date]")).toBeNull();
    expect(r.querySelector('[title="00: 1"]')).toBeNull();
  });

  it("shows the fetching note instead of an empty chart", async () => {
    const r = await show(heat({ groups: [], data: { query: "q", shape: "groups", groups: [], rows: [], pending: true } }));
    expect(within(r).getByLabelText("Block status")).toHaveTextContent("Fetching the data…");
    expect(r.querySelector("[data-date]")).toBeNull();
  });

  it("shows the age in minutes and hours, and Refreshing when stale", async () => {
    const mk = (age_s: number, stale?: boolean) => heat({ data: { query: "q", shape: "groups", groups: [{ key: "2025-11-03", label: "x", count: 1 }], rows: [], age_s, stale } });
    let r = await show(mk(300, true));
    expect(within(r).getByText("Updated 5 min ago")).toBeInTheDocument();
    expect(within(r).getByText("Refreshing…")).toBeInTheDocument();
    cleanup();
    r = await show(mk(3 * 3600));
    expect(within(r).getByText("Updated 3 h ago")).toBeInTheDocument();
    expect(within(r).queryByText("Refreshing…")).toBeNull();
    cleanup();
    r = await show(mk(30));
    expect(within(r).queryByText(/Updated/)).toBeNull();
  });

  it("shows the data and the error note together, and the error alone without data", async () => {
    let r = await show(heat({ error: "the last refresh failed: timeout", data: { query: "q", shape: "groups", groups: [{ key: "2025-11-03", label: "x", count: 1 }], rows: [], stale: true } }));
    expect(within(r).getByRole("note")).toHaveTextContent("the last refresh failed: timeout");
    expect(r.querySelector('[data-date="2025-11-03"]')).not.toBeNull();
    expect(within(r).queryByText("Refreshing…")).toBeNull();
    cleanup();
    r = await show(heat({ error: "boom", data: null }));
    expect(within(r).getByRole("note")).toHaveTextContent("boom");
    expect(r.querySelector("[data-date]")).toBeNull();
  });
});
