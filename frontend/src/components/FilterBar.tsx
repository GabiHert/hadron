import { useEffect, useRef, useState } from "react";
import type { FieldEntry } from "../api";
import { celOfFilter, optionsFor } from "../filters";
import type { Filters } from "../state/useFilters";
import type { Board } from "../state/useBoard";

const srcOf = (e: FieldEntry) => (e.source === "native" || e.source === "custom" ? "" : e.source);

/** The filter bar of the board: saved views, pinned field chips, search, Customize filters, Clear, Save as view and the CEL preview. */
export function FilterBar({ board, filters }: { board: Board; filters: Filters }) {
  const { cards, columns } = board;
  const { filter, schema, views, pinned, active, error } = filters;
  const [menu, setMenu] = useState(false);
  const [viewName, setViewName] = useState<string | null>(null);
  const [draft, setDraft] = useState(filter.expr);
  const [draftError, setDraftError] = useState("");
  const checkSeq = useRef(0);

  // The expression draft starts from the applied one when the menu opens.
  useEffect(() => {
    if (menu) setDraft(filter.expr);
  }, [menu, filter.expr]);

  // Compile the draft as the user types, so a mistake shows before Apply, with CEL's own words.
  useEffect(() => {
    if (!menu) return;
    const mine = ++checkSeq.current;
    if (!draft.trim()) {
      setDraftError("");
      return;
    }
    const t = setTimeout(() => {
      void filters.check(draft).then((msg) => {
        if (mine === checkSeq.current) setDraftError(msg);
      });
    }, 250);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft, menu]);

  const entry = (p: string) => schema.find((f) => f.path === p);
  const filterable = schema.filter((f) => f.filterable);

  return (
    <div className="filters" aria-label="Filters" style={{ position: "relative" }}>
      {views.map((v) => (
        <button key={v.id} className={"pill" + (filter.view === v.id ? " acc" : "")} onClick={() => filters.applyView(v)} aria-pressed={filter.view === v.id} title={v.name}>
          {v.name}
        </button>
      ))}
      {views.length ? <span style={{ width: 1, height: 18, background: "var(--border)" }} /> : null}
      {pinned.map((p) => {
        const d = entry(p);
        if (!d) return null;
        const options = optionsFor(d, cards, columns);
        const cur = filter.vals[p] ?? "";
        return (
          <label key={p} className={"pill" + (cur ? " acc" : "")} style={{ paddingRight: 2 }}>
            {srcOf(d) ? <span className="src">{srcOf(d)}</span> : null}
            {d.label}
            {options.length || !srcOf(d) ? (
              <select
                style={{ border: 0, background: "transparent", fontSize: "11.5px", fontWeight: 600, color: "inherit", padding: "0 2px" }}
                value={cur}
                onChange={(e) => filters.setVal(p, e.target.value)}
                aria-label={`Filter by ${d.label}`}
              >
                <option value="">any</option>
                {options.map((o) => (
                  <option key={o} value={o}>
                    {o}
                  </option>
                ))}
                {cur && !options.includes(cur) ? <option value={cur}>{cur}</option> : null}
              </select>
            ) : (
              <input
                style={{ border: 0, background: "transparent", fontSize: "11.5px", fontWeight: 600, color: "inherit", width: 70, padding: "0 2px" }}
                value={cur}
                placeholder="any"
                onChange={(e) => filters.setVal(p, e.target.value)}
                aria-label={`Filter by ${d.label}`}
              />
            )}
          </label>
        );
      })}
      <input
        className="in"
        style={{ padding: "2px 8px", fontSize: 12, width: 130 }}
        placeholder="Search cards"
        value={filter.q}
        onChange={(e) => filters.setQ(e.target.value)}
        aria-label="Search cards"
        id="card-search"
      />
      <button className="btn sm" onClick={() => setMenu(!menu)} aria-expanded={menu}>
        Customize filters
      </button>
      {active ? (
        <button className="btn quiet sm" onClick={filters.clear}>
          Clear
        </button>
      ) : null}
      {active ? (
        viewName === null ? (
          <button className="btn quiet sm" onClick={() => setViewName("")}>
            Save as view
          </button>
        ) : (
          <form
            className="inline"
            onSubmit={(e) => {
              e.preventDefault();
              const name = viewName.trim();
              if (name) {
                void filters.saveView(name);
                setViewName(null);
              }
            }}
          >
            <input className="in" id="view-name" autoFocus style={{ padding: "2px 8px", fontSize: 12, width: 130 }} placeholder="View name" value={viewName} onChange={(e) => setViewName(e.target.value)} aria-label="View name" />
            <button className="btn sm pri" type="submit">
              Save
            </button>
            <button className="btn sm quiet" type="button" onClick={() => setViewName(null)}>
              Cancel
            </button>
          </form>
        )
      ) : null}
      <span className="cel" aria-label="Filter as CEL">{celOfFilter(filter)}</span>
      {error && !menu ? (
        <span className="hint" role="alert" style={{ color: "var(--bad)", whiteSpace: "pre-wrap", fontFamily: "var(--mono)" }}>
          {error}
        </span>
      ) : null}
      {menu ? (
        <div className="menu" style={{ top: 30, left: 0, right: "auto", width: 360 }}>
          <span className="cap">Show in the filter bar</span>
          {filterable.map((d) => (
            <label key={d.path} style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
              <input type="checkbox" checked={pinned.includes(d.path)} onChange={() => void filters.pin(d.path)} aria-label={`Pin ${d.label}`} />
              {srcOf(d) ? <span className="src">{srcOf(d)}</span> : null}
              <span style={{ flex: 1 }}>{d.label}</span>
              <span className="mono hint">{d.path}</span>
            </label>
          ))}
          <span className="cap" style={{ marginTop: 4 }}>Expression (CEL)</span>
          <form
            className="inline"
            onSubmit={(e) => {
              e.preventDefault();
              if (!draftError) filters.setExpr(draft.trim());
            }}
          >
            <input
              className="in mono"
              id="filter-expr"
              style={{ flex: 1, fontSize: "11.5px" }}
              placeholder="card.assignee == 'You' && card.stage != ''"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              aria-label="CEL expression"
              aria-invalid={!!draftError}
            />
            <button className="btn sm pri" type="submit" disabled={!!draftError}>
              Apply
            </button>
          </form>
          {draftError ? (
            <span className="hint" role="alert" style={{ color: "var(--bad)", whiteSpace: "pre-wrap", fontFamily: "var(--mono)" }}>
              {draftError}
            </span>
          ) : (
            <span className="hint">Any field of any enabled integration. The planner can set filters and views through filter_eval and view_save.</span>
          )}
          {views.length ? (
            <>
              <span className="cap" style={{ marginTop: 4 }}>Saved views</span>
              {views.map((v) => (
                <div key={v.id} className="inline">
                  <span style={{ flex: 1 }}>{v.name}</span>
                  <button className="btn sm quiet" onClick={() => void filters.deleteView(v.id)} aria-label={`Delete view ${v.name}`}>
                    Delete
                  </button>
                </div>
              ))}
            </>
          ) : null}
          <div className="inline" style={{ justifyContent: "flex-end" }}>
            <button className="btn sm" onClick={() => setMenu(false)}>
              Done
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
