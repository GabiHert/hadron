import type { Card, Column, FieldEntry, FilterSpec } from "./api";

/** A filter as the bar holds it. `view` is the saved view it came from, if any. */
export interface Filter {
  vals: Record<string, string>;
  q: string;
  expr: string;
  view: string | null;
}

export const EMPTY_FILTER: Filter = { vals: {}, q: "", expr: "", view: null };

/** The pinned values that are set. */
export function setVals(f: Filter): Record<string, string> {
  return Object.fromEntries(Object.entries(f.vals).filter(([, v]) => v));
}

export function isActive(f: Filter): boolean {
  return Object.keys(setVals(f)).length > 0 || !!f.q || !!f.expr;
}

/** What view_save stores: only what is set. */
export function toSpec(f: Filter): FilterSpec {
  const vals = setVals(f);
  const spec: FilterSpec = {};
  if (Object.keys(vals).length) spec.vals = vals;
  if (f.q) spec.q = f.q;
  if (f.expr) spec.expr = f.expr;
  return spec;
}

export function fromSpec(spec: FilterSpec | undefined, view: string | null): Filter {
  return { vals: { ...(spec?.vals ?? {}) }, q: spec?.q ?? "", expr: spec?.expr ?? "", view };
}

/** True for a field the browser can read from a card: native and custom ones. */
export const isLocalPath = (path: string) => path.startsWith("card.") || path.startsWith("custom.");

/** An expression, or a value on an integration's field, needs the backend: the browser does not know those fields. */
export function needsServer(f: Filter): boolean {
  return !!f.expr || Object.keys(setVals(f)).some((p) => !isLocalPath(p));
}

/** The value of a native or custom field on a card, as the filter compares it. */
export function localValue(card: Card, columns: Column[], path: string): string {
  const [ns, name] = [path.split(".")[0], path.slice(path.indexOf(".") + 1)];
  if (ns === "card") {
    switch (name) {
      case "column":
        return columns.find((c) => c.id === card.column_id)?.name ?? "";
      case "assignee":
        return card.assignee;
      case "liveness":
        return card.liveness;
      case "key":
        return card.key;
      case "nick":
        return card.nick;
      case "stage":
        return card.stage;
    }
    return "";
  }
  const v = card.fields?.[name];
  return v === undefined || v === null ? "" : String(v);
}

/** The browser's mirror of the pinned values and the search: the same rule as the backend's filter_eval. */
export function localMatch(card: Card, columns: Column[], f: Filter): boolean {
  const q = f.q.trim().toLowerCase();
  if (q && !`${card.nick} ${card.key} ${card.title}`.toLowerCase().includes(q)) return false;
  return Object.entries(setVals(f)).every(([p, v]) => localValue(card, columns, p) === v);
}

/** The filter as CEL, for the preview line. */
export function celOfFilter(f: Filter): string {
  const parts: string[] = [];
  for (const [p, v] of Object.entries(setVals(f))) parts.push(`${p} == ${JSON.stringify(v)}`);
  if (f.q) parts.push(`card.nick.contains(${JSON.stringify(f.q)})`);
  if (f.expr) parts.push(`(${f.expr})`);
  return parts.length ? parts.join(" && ") : "true";
}

/** The options a chip offers: the field's own, else the distinct values on the board. */
export function optionsFor(entry: FieldEntry, cards: Card[], columns: Column[]): string[] {
  if (entry.options?.length) return entry.options;
  if (!isLocalPath(entry.path)) return [];
  const seen = new Set<string>();
  for (const c of cards) {
    const v = localValue(c, columns, entry.path);
    if (v) seen.add(v);
  }
  return [...seen].sort((a, b) => a.localeCompare(b));
}
