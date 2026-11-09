import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { Feedback, FeedbackKind, FeedbackStatus } from "../api";
import { hm } from "../lib";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";

const STATUSES: [FeedbackStatus, string][] = [["new", "New"], ["planned", "Planned"], ["in_progress", "In progress"], ["done", "Done"], ["wontfix", "Won't fix"]];
const PILL: Record<FeedbackKind, string> = { bug: "bad", idea: "acc", annoyance: "warn", praise: "ok" };

const day = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${hm(iso)}`;
};

function Item({ f, toast, reload }: { f: Feedback; toast: (t: string) => void; reload: () => void }) {
  const [note, setNote] = useState(f.note ?? "");
  const [url, setUrl] = useState(f.github_url ?? "");
  useEffect(() => { setNote(f.note ?? ""); setUrl(f.github_url ?? ""); }, [f.note, f.github_url]);
  const snip = f.text.slice(0, 20);
  const upd = async (patch: { status?: FeedbackStatus; note?: string; github_url?: string }) => {
    try {
      await api.feedback_update({ id: f.id, ...patch });
      reload();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e));
    }
  };
  return (
    <div className="box" style={{ marginBottom: 8 }}>
      <div className="rrow" style={{ gap: 8 }}>
        <span className={"pill " + PILL[f.kind]}>{f.kind}</span>
        <span className="hint mono">{f.area}</span>
        <span className="hint mono">{f.version}</span>
        <span className="hint" style={{ marginLeft: "auto" }}>{day(f.created_at)}</span>
      </div>
      <p style={{ whiteSpace: "pre-wrap", margin: "6px 0" }}>{f.text}</p>
      <div className="rrow" style={{ gap: 8, flexWrap: "wrap" }}>
        <select aria-label={`Status of ${snip}`} value={f.status} onChange={(e) => void upd({ status: e.target.value as FeedbackStatus })}>
          {STATUSES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
        </select>
        <input aria-label={`Note for ${snip}`} placeholder="Note" value={note} onChange={(e) => setNote(e.target.value)} onBlur={() => { if (note !== (f.note ?? "")) void upd({ note }); }} style={{ flex: 1 }} />
      </div>
      <div className="rrow" style={{ gap: 8, marginTop: 6 }}>
        <input aria-label={`Issue link for ${snip}`} placeholder="Issue link" value={url} onChange={(e) => setUrl(e.target.value)} onBlur={() => { if (url !== (f.github_url ?? "")) void upd({ github_url: url }); }} style={{ flex: 1 }} />
        <button className="btn" aria-label={`Open as GitHub issue: ${snip}`} onClick={() => BrowserOpenURL(f.issue_url)}>Open as GitHub issue</button>
      </div>
    </div>
  );
}

/** Settings → Feedback: the inbox, newest first, with a status filter and per-item triage. */
export function FeedbackTab({ toast }: { toast: (text: string) => void }) {
  const [status, setStatus] = useState("");
  const [items, setItems] = useState<Feedback[] | null>(null);
  const load = useCallback(() => {
    api.feedback_list(status ? { status: status as FeedbackStatus } : {}).then((l) => setItems(l ?? [])).catch((e) => toast(e instanceof Error ? e.message : String(e)));
  }, [status, toast]);
  useEffect(() => {
    load();
    return api.onChanged((e) => { if (!e.entities?.length || e.entities.includes("feedback")) load(); });
  }, [load]);
  const sorted = [...(items ?? [])].sort((a, b) => b.created_at.localeCompare(a.created_at));
  return (
    <>
      <h4>Feedback</h4>
      <div className="rrow" style={{ gap: 8, marginBottom: 10 }}>
        <select aria-label="Status" value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">All</option>
          {STATUSES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
        </select>
      </div>
      {items && sorted.length === 0 ? (
        <p className="hint">No feedback yet. Use the speech bubble in the header, or tell the planner "feedback: ...".</p>
      ) : sorted.map((f) => <Item key={f.id} f={f} toast={toast} reload={load} />)}
    </>
  );
}
