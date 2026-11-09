import { applyMove } from "./state/useBoard";
import { card } from "./test/fakeApi";
import { slug } from "./lib";

test("slug makes kebab-case nicknames", () => {
  expect(slug("  Billing Fix! ")).toBe("billing-fix");
});

test("applyMove inserts at the index and renumbers both columns", () => {
  const cards = [card("a", "w", "x", "a", 0), card("b", "w", "x", "b", 1), card("c", "w", "y", "c", 0)];
  const out = applyMove(cards, "a", "y", 0);
  const by = (col: string) => out.filter((c) => c.column_id === col).sort((p, q) => p.position - q.position).map((c) => c.id + c.position);
  expect(by("y")).toEqual(["a0", "c1"]);
  expect(by("x")).toEqual(["b0"]);
  expect(applyMove(cards, "a", "x", 99).filter((c) => c.column_id === "x").sort((p, q) => p.position - q.position).map((c) => c.id + c.position)).toEqual(["b0", "a1"]);
});
