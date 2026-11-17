import * as L from "./sessionsLayout";

const tabs = (l: L.Layout) => l.tabs.map((t) => L.panes(t.root).join("|"));

describe("the Sessions layout", () => {
  it("gives each new session a tab, closes the panes of gone ones, and keeps the focus valid", () => {
    let l = L.reconcile(L.EMPTY, ["a", "b"]);
    expect(tabs(l)).toEqual(["a", "b"]);
    expect(L.activePane(l)).toBe("b");
    l = L.reconcile(l, ["a"]);
    expect(tabs(l)).toEqual(["a"]);
    expect(L.activePane(l)).toBe("a");
    expect(L.reconcile(l, [])).toEqual({ tabs: [], active: "" });
  });

  it("puts a session it was asked to split beside the pane, and closing one gives the room back", () => {
    let l = L.reconcile(L.EMPTY, ["a"]);
    l = L.reconcile(l, ["a", "b"], new Map([["b", { split: { pane: "a", dir: "row" } }]]));
    l = L.reconcile(l, ["a", "b", "c"], new Map([["c", { split: { pane: "b", dir: "col" } }]]));
    expect(tabs(l)).toEqual(["a|b|c"]);
    expect(L.activePane(l)).toBe("c");
    expect(L.neighbor(l, "up")).toBe("b");
    expect(L.neighbor(l, "left")).toBe("a");
    expect(L.neighbor(l, "right")).toBe("");
    l = L.focus(l, "a");
    expect(L.neighbor(l, "right")).toBe("b"); // the nearer of b and c, the first found
    l = L.close(L.focus(l, "b"), "b");
    expect(tabs(l)).toEqual(["a|c"]);
    expect(L.activePane(l)).toBe("a");
    const root = l.tabs[0].root as Extract<L.Node, { split: L.Dir }>;
    expect(root.split).toBe("row");
  });

  it("steps, selects, moves and drops tabs", () => {
    let l = L.reconcile(L.EMPTY, ["a", "b", "c"]);
    l = L.selectTab(l, 0);
    expect(L.activePane(l)).toBe("a");
    l = L.stepTab(l, -1);
    expect(L.activePane(l)).toBe("c");
    l = L.moveTab(l, L.activeTab(l)!.id, -1);
    expect(tabs(l)).toEqual(["a", "c", "b"]);
    l = L.placeTab(l, l.tabs[2].id, 0);
    expect(tabs(l)).toEqual(["b", "a", "c"]);
    expect(L.selectTab(l, 20).active).toBe(l.tabs[2].id);
  });

  it("keeps split ratios between 10% and 90%, and survives a round trip through the setting", () => {
    let l = L.split(L.openTab(L.EMPTY, "a"), "a", "b", "row");
    const id = (l.tabs[0].root as Extract<L.Node, { split: L.Dir }>).id;
    l = L.setRatio(l, id, 0.99);
    expect((l.tabs[0].root as Extract<L.Node, { split: L.Dir }>).ratio).toBe(0.9);
    expect(L.parseLayout(JSON.parse(JSON.stringify(l)))).toEqual(l);
    expect(L.parseLayout({ tabs: [{ id: "t", root: { split: "row", id: "s", a: { pane: "x" }, b: 7 } }] })).toEqual({ tabs: [{ id: "t", root: { pane: "x" }, active: "x" }], active: "t" });
    expect(L.parseLayout("junk")).toEqual(L.EMPTY);
  });
});
