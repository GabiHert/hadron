import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { DiffEntry, HistoryEntry } from "../api";
import { hm } from "../lib";
import type { Board } from "../state/useBoard";

const ACTORS = [
  ["", "Everyone"],
  ["you", "You"],
  ["planner", "Planner"],
  ["session", "Sessions"],
  ["rule", "Rules"],
  ["external", "Other clients"],
] as const;

const show = (v: unknown) => (v === undefined ? "" : typeof v === "string" ? v : JSON.stringify(v));

/** Settings, History: every config change, who made it, what it did, and a way to undo it. */
export function HistoryTab({ board }: { board: Board }) {
  const { ws, actions } = board;
  const [actor, setActor] = useState("");
  const [rows, setRows] = useState<HistoryEntry[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<{ id: string; diff: DiffEntry[] } | null>(null);
  const load = useCallback(async () => {
    try {
      setRows(await api.config_history({ workspace_id: ws?.id, limit: 100, ...(actor ? { actor } : {}) }));
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [ws?.id, actor]);
  useEffect(() => {
    void load();
    return api.onChanged((e) => {
      if (!e.entities?.length || e.entities.some((x) => x !== "mcp_calls")) void load();
    });
  }, [load]);

  const toggle = async (id: string) => {
    if (open?.id === id) return setOpen(null);
    try {
      setOpen({ id, diff: (await api.config_diff({ change_id: id })).diff });
    } catch (e) {
      actions.toast(e instanceof Error ? e.message : String(e));
    }
  };
  const revert = async (h: HistoryEntry) => {
    if (!window.confirm(`Undo this change?\n\n${h.summary}`)) return;
    const out = await actions.run(() => api.config_revert({ change_id: h.id }));
    if (out) actions.toast(out.summary.charAt(0).toUpperCase() + out.summary.slice(1) + ".");
    await load();
  };

  return (
    <>
      <h4>History</h4>
      <p className="sub">Every change to columns, rules, views, the dashboard, prompts, launch profiles, scripts and workspaces, with who made it. Undo puts the earlier state back, unless something changed it again since: then undo the newer change first.</p>
      <div className="inline">
        <label className="field" style={{ flex: "0 1 220px" }}>
          <span>Made by</span>
          <select className="in" value={actor} onChange={(e) => setActor(e.target.value)} aria-label="Made by">
            {ACTORS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
          </select>
        </label>
      </div>
      {error ? <div className="empty" role="alert">{error}</div> : null}
      {rows && !rows.length && !error ? <div className="empty">No changes yet. Rename a column or edit a rule and it shows here.</div> : null}
      <div className="box">
        {(rows ?? []).map((h) => (
          <div key={h.id}>
            <div className="rrow">
              <span className="mono hint">{hm(h.at)}</span>
              <span className={"pill " + (h.actor_kind === "planner" ? "acc" : h.actor_kind === "rule" ? "warn" : h.actor_kind === "session" ? "ok" : "")}>{h.actor}</span>
              <span style={{ flex: 1, minWidth: 0 }}>
                <span className="mono hint">{h.action}</span><br />
                <span>{h.summary}</span>
                {h.reverted ? <> <span className="pill">reverted</span></> : null}
              </span>
              <button className="btn sm" onClick={() => void toggle(h.id)} aria-expanded={open?.id === h.id} aria-label={`Show what ${h.action} changed`}>Diff</button>
              <button className="btn sm" disabled={!h.revertible} title={h.reason} onClick={() => void revert(h)} aria-label={`Revert ${h.action}`}>Revert</button>
            </div>
            {open?.id === h.id ? (
              <div className="code" role="group" aria-label="Diff">
                {open.diff.length ? open.diff.map((d) => <div key={d.field}><b>{d.field}</b>: <span className="bad">{show(d.before)}</span> → <span className="ok">{show(d.after)}</span></div>) : "Nothing visible changed."}
              </div>
            ) : null}
          </div>
        ))}
      </div>
    </>
  );
}
