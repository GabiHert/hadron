import { useEffect, useState } from "react";
import * as api from "../api";
import type { BlockSource, BlockType, BlockView, FieldEntry } from "../api";
import { allISODates, buildCalendar, tooltip } from "../calendar";
import { hm, short } from "../lib";
import type { Board as BoardState } from "../state/useBoard";
import { useDash, useWidgetScripts } from "../state/useDash";
import { Icon } from "./ui";
import { Widget } from "./Widget";
import { Markdown } from "./Markdown";

const str = (v: unknown) => (v === null || v === undefined ? "" : typeof v === "object" ? JSON.stringify(v) : String(v));

const TYPE_LABEL: Partial<Record<BlockType, string>> = { custom: "html" };

const NEW_TYPES: { value: BlockType; label: string; title: string }[] = [
  { value: "bars", label: "Bars: count cards by a field", title: "Cards by" },
  { value: "stats", label: "Tiles: cards per column", title: "Cards by column" },
  { value: "table", label: "Table: waiting on you", title: "Waiting on you" },
  { value: "heatmap", label: "Heatmap: tool calls by hour", title: "Tool calls by hour" },
  { value: "prose", label: "Text", title: "Notes" },
  { value: "planner", label: "Summary the planner writes", title: "Planner's summary" },
  { value: "activity", label: "Activity: recent tool calls", title: "Recent tool calls" },
];

export function Dashboard({ board }: { board: BoardState }) {
  const { ws, actions } = board;
  const wsId = ws?.id ?? "";
  const dash = useDash(wsId, actions.toast);
  const scripts = useWidgetScripts();
  const [adding, setAdding] = useState(false);
  const [type, setType] = useState<BlockType>("bars");
  const [by, setBy] = useState("card.assignee");
  const [title, setTitle] = useState("");
  const [fields, setFields] = useState<FieldEntry[]>([]);

  useEffect(() => {
    if (!adding || !wsId) return;
    let alive = true;
    api.field_schema_get({ workspace_id: wsId }).then((s) => alive && setFields(s.fields.filter((f) => f.filterable && f.path !== "card.column"))).catch(() => {});
    return () => {
      alive = false;
    };
  }, [adding, wsId]);

  if (!ws) return null;
  const mutate = async <T,>(fn: () => Promise<T>) => {
    const out = await actions.run(fn);
    await dash.reload();
    return out;
  };

  const byLabel = by === "card.column" ? "column" : (fields.find((f) => f.path === by)?.label ?? by).toLowerCase();
  const defTitle = type === "bars" ? `Cards by ${byLabel}` : (NEW_TYPES.find((t) => t.value === type)?.title ?? "");

  const add = async () => {
    const t = title.trim() || defTitle;
    let spec: api.BlockSpec;
    switch (type) {
      case "bars": spec = { type, title: t, source: { query: "cards.count", args: { by } } }; break;
      case "stats": spec = { type, title: t, span: 2, source: { query: "cards.count", args: { by: "column" } } }; break;
      case "table": spec = { type, title: t, source: { query: "questions.open" } }; break;
      case "heatmap": spec = { type, title: t, span: 2, source: { query: "mcp.calls", args: { by: "hour" } } }; break;
      case "prose": spec = { type, title: t, text: "Edit this text with the planner." }; break;
      case "activity": spec = { type, title: t, span: 2, source: { query: "mcp.calls", args: { limit: 7 } } }; break;
      default: spec = { type: "planner", title: t };
    }
    await mutate(() => api.dash_block_add({ workspace_id: wsId, ...spec }));
    setAdding(false);
    setTitle("");
  };

  const blocks = dash.blocks;
  return (
    <div className="dash">
      <div className="inline" style={{ gridColumn: "1 / -1" }}>
        <span className="hint" style={{ flex: 1, minWidth: 200 }}>
          Each block reads the board through a named query. The planner edits this layout too: ask it to "add a chart of cards by assignee to the dashboard".
        </span>
        <button className="btn" type="button" data-dash-add="" title="Add block (n)" aria-expanded={adding} onClick={() => setAdding(!adding)}><Icon name="plus" size={13} />Add block</button>
      </div>
      {adding ? (
        <div className="blk s2" data-dash-form="" onKeyDown={(e) => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); setAdding(false); } }}>
          <div className="blkb">
            <div className="inline" style={{ alignItems: "flex-end" }}>
              <label className="field" style={{ flex: "1 1 200px" }}>
                <span>Block type</span>
                <select id="nb-type" className="in" value={type} onChange={(e) => setType(e.target.value as BlockType)}>
                  {NEW_TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </label>
              {type === "bars" ? (
                <label className="field" style={{ flex: "1 1 160px" }}>
                  <span>Count by</span>
                  <select id="nb-by" className="in" value={by} onChange={(e) => setBy(e.target.value)}>
                    <option value="column">Column</option>
                    {fields.map((f) => <option key={f.path} value={f.path}>{f.label}</option>)}
                  </select>
                </label>
              ) : null}
              <label className="field" style={{ flex: "2 1 200px" }}>
                <span>Title</span>
                <input id="nb-title" className="in" value={title} placeholder={defTitle} onChange={(e) => setTitle(e.target.value)} />
              </label>
              <button className="btn pri" type="button" onClick={() => void add()}>Add</button>
              <button className="btn" type="button" onClick={() => setAdding(false)}>Cancel</button>
            </div>
          </div>
        </div>
      ) : null}
      {dash.loaded && !blocks.length ? <div className="empty" style={{ gridColumn: "1 / -1" }}>The dashboard is empty. Add a block, or ask the planner to build one.</div> : null}
      {blocks.map((b, i) => (
        <section key={b.id} className={"blk" + (b.span === 2 ? " s2" : "")} aria-label={b.title} data-block={b.id} tabIndex={0}>
          <div className="blkh">
            <h3>{b.title}</h3>
            <span className="pill">{TYPE_LABEL[b.type] ?? b.type}</span>
            <button className="ibtn" style={{ width: 24, height: 24, fontSize: 12 }} type="button" aria-label={b.span === 2 ? "Half width" : "Full width"} title={b.span === 2 ? "Half width" : "Full width"}
              onClick={() => void mutate(() => api.dash_block_update({ block_id: b.id, span: b.span === 2 ? 1 : 2 }))}>{b.span === 2 ? "½" : "↔"}</button>
            <button className="ibtn" style={{ width: 24, height: 24 }} type="button" aria-label="Move earlier" disabled={i === 0}
              onClick={() => void mutate(() => api.dash_block_move({ block_id: b.id, position: i - 1 }))}>↑</button>
            <button className="ibtn" style={{ width: 24, height: 24 }} type="button" aria-label="Move later" disabled={i === blocks.length - 1}
              onClick={() => void mutate(() => api.dash_block_move({ block_id: b.id, position: i + 1 }))}>↓</button>
            <button className="ibtn" style={{ width: 24, height: 24 }} type="button" aria-label={`Remove ${b.title}`}
              onClick={() => void mutate(() => api.dash_block_remove({ block_id: b.id }))}><Icon name="close" size={13} /></button>
          </div>
          <div className="blkb">
            <BlockStatus b={b} board={board} scripts={scripts} onRewrite={async () => {
              const out = await mutate(() => api.dash_block_refresh({ block_id: b.id }));
              if (out) actions.toast("Asked the planner to write it");
            }} />
          </div>
        </section>
      ))}
    </div>
  );
}

const ellipsis = { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;

function ageText(s: number): string {
  return s >= 7200 ? `Updated ${Math.floor(s / 3600)} h ago` : `Updated ${Math.floor(s / 60)} min ago`;
}

function BlockStatus({ b, board, scripts, onRewrite }: { b: BlockView; board: BoardState; scripts: boolean; onRewrite: () => void }) {
  const d = b.data;
  // A calendar that was asked for but cannot be drawn (non-date keys) shows the error only.
  const unusable = b.type === "heatmap" && b.source?.layout === "calendar" && !allISODates(d?.groups ?? []);
  const hasData = !!d && !unusable && (d.groups.length > 0 || d.rows.length > 0);
  if (b.error && !hasData) return <div className="hint" role="note">{b.error}</div>;
  if (d?.pending && !hasData)
    return <div className="hint" role="status" aria-label="Block status">Fetching the data…</div>;
  return (
    <>
      {b.error ? <div className="hint" role="note" style={{ marginBottom: 6 }}>{b.error}</div> : null}
      <BlockBody b={b} board={board} scripts={scripts} onRewrite={onRewrite} />
      {d && d.age_s !== undefined && d.age_s >= 60 ? <div className="hint" style={{ marginTop: 6, fontSize: 11 }}>{ageText(d.age_s)}</div> : null}
      {d?.stale && !b.error ? <div className="hint" style={{ marginTop: 6, fontSize: 11 }} role="status" aria-label="Block status">Refreshing…</div> : null}
    </>
  );
}

function BlockBody({ b, board, scripts, onRewrite }: { b: BlockView; board: BoardState; scripts: boolean; onRewrite: () => void }) {
  const groups = b.data?.groups ?? [];
  const rows = b.data?.rows ?? [];
  switch (b.type) {
    case "stats":
      return (
        <div className="tiles">
          {groups.map((g) => <div key={g.key} className="tilev"><span className="cap">{g.label}</span><b>{g.count}</b></div>)}
        </div>
      );
    case "bars": {
      const max = Math.max(1, ...groups.map((g) => g.count));
      return (
        <div>
          {groups.map((g) => (
            <div key={g.key} className="barrow">
              <span style={ellipsis}>{g.label}</span>
              <span className="bar"><i style={{ width: `${(g.count / max) * 100}%` }} /></span>
              <span className="mono" style={{ textAlign: "right" }}>{g.count}</span>
            </div>
          ))}
        </div>
      );
    }
    case "heatmap": {
      const layout = b.source?.layout;
      if (layout === "calendar" || (layout !== "strip" && allISODates(groups))) return <CalendarHeatmap b={b} />;
      const max = Math.max(1, ...groups.map((g) => g.count));
      return (
        <div style={{ display: "flex", gap: 3 }} role="img" aria-label={b.title}>
          {groups.map((g) => (
            <span key={g.key} title={`${g.label}: ${g.count}`} style={{ flex: 1, height: 28, borderRadius: 3, background: "var(--acc)", opacity: g.count ? 0.15 + 0.85 * (g.count / max) : 0.06 }} />
          ))}
        </div>
      );
    }
    case "prose":
      return <Markdown text={b.text ?? ""} />;
    case "planner":
      return (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {b.text ? <Markdown className="md-serif" text={b.text} /> : <p style={{ margin: 0, font: "14px/1.55 var(--sans)" }}>The planner has not written this yet. Use Rewrite now.</p>}
          <div className="inline">
            <span className="pill">{b.schedule || "on demand"}</span>
            <span className="hint" style={{ flex: 1 }}>{b.text_at ? `Written by the planner at ${hm(b.text_at)}` : "Written by the planner"}</span>
            <button className="btn sm" type="button" onClick={onRewrite}>Rewrite now</button>
          </div>
        </div>
      );
    case "activity":
      return rows.length ? (
        <div>
          {rows.map((r, i) =>
            r.rule !== undefined ? (
              <div key={i} className="rrow" style={{ padding: "5px 0" }}>
                <span className="mono hint">{hm(str(r.at))}</span><span className="pill warn">rule</span>
                <span className="mono" style={{ fontSize: 12 }}>{str(r.rule)}</span>
                <span className="mono hint" style={{ flex: 1, minWidth: 0, ...ellipsis }}>{short(`${str(r.card)} ${str(r.text)}`, 70)}</span>
              </div>
            ) : (
              <div key={i} className="rrow" style={{ padding: "5px 0" }}>
                <span className="mono hint">{hm(str(r.at))}</span>
                <span className={"pill " + (r.actor === "planner" ? "acc" : r.actor === "session" ? "ok" : "")}>{str(r.actor)}</span>
                <span className="mono" style={{ fontSize: 12 }}>{str(r.tool)}</span>
                <span className="mono hint" style={{ flex: 1, minWidth: 0, ...ellipsis }}>{short(str(r.args), 60)}</span>
              </div>
            ),
          )}
        </div>
      ) : <div className="hint">Nothing to show.</div>;
    case "custom":
      return b.html === "live:diagram" ? <SessionMap rows={rows} /> : <Widget variant="block" title="" html={b.html} height={b.source?.height} data={b.data ?? undefined} scripts={scripts} />;
    default:
      return <TableBody b={b} board={board} />;
  }
}

const LEVEL_BG = [
  "color-mix(in srgb, var(--fg) 8%, transparent)",
  "color-mix(in srgb, var(--acc) 28%, transparent)",
  "color-mix(in srgb, var(--acc) 50%, transparent)",
  "color-mix(in srgb, var(--acc) 75%, transparent)",
  "color-mix(in srgb, var(--acc) 100%, transparent)",
];
const SQ = 12;
const GAP = 3;

function CalendarHeatmap({ b }: { b: BlockView }) {
  const unit = typeof b.source?.unit === "string" && b.source.unit ? b.source.unit : undefined;
  const cal = buildCalendar(b.data?.groups ?? [], { weeks: b.source?.weeks, now: new Date(), unit });
  return (
    <div style={{ maxWidth: "100%" }}>
      <div style={{ overflowX: "auto", maxWidth: "100%", paddingBottom: 4 }}>
        <div role="img" aria-label={`${b.title}: ${cal.totalText}`}
          style={{ display: "grid", gridTemplateColumns: `auto repeat(${cal.weeks}, ${SQ}px)`, gridTemplateRows: `14px repeat(7, ${SQ}px)`, gap: GAP, width: "max-content", fontSize: 10 }}>
          {cal.months.map((m) => (
            <span key={m.col} className="hint" style={{ gridRow: 1, gridColumn: m.col + 2, whiteSpace: "nowrap", overflow: "visible", lineHeight: "14px" }}>{m.label}</span>
          ))}
          {cal.weekdayLabels.map((w, r) => (
            <span key={r} className="hint" style={{ gridRow: r + 2, gridColumn: 1, lineHeight: `${SQ}px`, paddingRight: 4 }}>{w}</span>
          ))}
          {cal.columns.map((col, c) =>
            col.map((cell, r) =>
              cell.future ? null : (
                <span key={cell.date} data-level={cell.level} data-date={cell.date} title={tooltip(cell, unit)} aria-label={tooltip(cell, unit)}
                  style={{ gridRow: r + 2, gridColumn: c + 2, width: SQ, height: SQ, borderRadius: 2, background: LEVEL_BG[cell.level] }} />
              ),
            ),
          )}
        </div>
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 4, flexWrap: "wrap" }}>
        <span className="hint" style={{ flex: 1, minWidth: 0 }}>{cal.totalText}</span>
        <span className="hint" style={{ display: "inline-flex", alignItems: "center", gap: 3, fontSize: 10 }}>
          <span>Less</span>
          {LEVEL_BG.map((bg, i) => <span key={i} data-legend-level={i} style={{ width: 10, height: 10, borderRadius: 2, background: bg, display: "inline-block" }} />)}
          <span>More</span>
        </span>
      </div>
    </div>
  );
}

function TableBody({ b, board }: { b: BlockView; board: BoardState }) {
  const d = b.data;
  if (!d) return <div className="hint">Nothing to show.</div>;
  if (b.source?.query === "questions.open")
    return d.rows.length ? (
      <div>
        {d.rows.map((r) => (
          <div key={str(r.id)} className="rrow" style={{ padding: "6px 0" }}>
            <button className="btn sm" type="button" onClick={() => board.actions.open(str(r.card_id))}>{str(r.card)}</button>
            <span className="mono hint">{str(r.label)}</span>
            <span className={"pill " + (r.kind === "approval" ? "bad" : "warn")}>{str(r.kind)}</span>
            <span className="hint" style={{ flex: 1, minWidth: 0 }}>{short(str(r.text), 70)}</span>
          </div>
        ))}
      </div>
    ) : <div className="hint">Nothing waits on you.</div>;
  let cols: { key: string; label: string }[];
  let data: Record<string, unknown>[];
  if (d.shape === "groups") {
    cols = [{ key: "label", label: "Key" }, { key: "count", label: "Count" }];
    data = d.groups as unknown as Record<string, unknown>[];
  } else {
    const src: BlockSource = b.source ?? {};
    cols = src.columns?.length ? src.columns : Object.keys(d.rows[0] ?? {}).slice(0, 6).map((k) => ({ key: k, label: k }));
    data = d.rows;
  }
  if (!data.length) return <div className="hint">Nothing to show.</div>;
  return (
    <table className="tbl" style={{ width: "100%", borderCollapse: "collapse", fontSize: 12.5 }}>
      <thead><tr>{cols.map((c) => <th key={c.key} className="cap" style={{ textAlign: "left", padding: "3px 6px" }}>{c.label}</th>)}</tr></thead>
      <tbody>{data.map((r, i) => <tr key={i}>{cols.map((c) => <td key={c.key} style={{ padding: "3px 6px", borderTop: "1px solid var(--row)" }}>{short(str(r[c.key]), 80)}</td>)}</tr>)}</tbody>
    </table>
  );
}

/** The built-in "live:diagram" block: every card as a dot, coloured by liveness, under its column. */
function SessionMap({ rows }: { rows: Record<string, unknown>[] }) {
  const byCol = new Map<string, Record<string, unknown>[]>();
  for (const r of rows) byCol.set(str(r.column), [...(byCol.get(str(r.column)) ?? []), r]);
  if (!rows.length) return <div className="hint">Nothing to show.</div>;
  return (
    <div role="img" aria-label="Session map" style={{ display: "flex", gap: 14, flexWrap: "wrap" }}>
      {[...byCol.entries()].map(([name, cards]) => (
        <div key={name} style={{ minWidth: 120 }}>
          <span className="cap">{name}</span>
          {cards.map((c) => (
            <div key={str(c.id)} style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12.5 }}>
              <span className={`dot ${str(c.liveness)}`} title={str(c.liveness)} />{str(c.nick)}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
