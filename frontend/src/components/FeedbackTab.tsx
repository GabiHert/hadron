import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { Feedback, FeedbackKind, FeedbackSharing, FeedbackStatus, SharedFeedback, SharedFeedbackList } from "../api";
import { hm } from "../lib";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { ModalHead, Scrim } from "./Modals";

const STATUSES: [FeedbackStatus, string][] = [["new", "New"], ["planned", "Planned"], ["in_progress", "In progress"], ["done", "Done"], ["wontfix", "Won't fix"]];
const PILL: Record<FeedbackKind, string> = { bug: "bad", idea: "acc", annoyance: "warn", praise: "ok" };
/** How often the All users list refreshes while it is open; the cache in Go has the same age. */
export const SHARED_REFRESH_MS = 3 * 60 * 60 * 1000;

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

const day = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${hm(iso)}`;
};

/** The private words found in the text, ignoring case, as Go checks them before sending. */
export function privateHits(words: string[], ...text: string[]): string[] {
  const all = text.join("\n").toLowerCase();
  return words.filter((w, i) => w.trim() && all.includes(w.trim().toLowerCase()) && words.indexOf(w) === i);
}

/** Review and edit a note before it goes to GitHub, then send it. */
function SendReview({ f, words, onClose, toast }: { f: Feedback; words: string[]; onClose: () => void; toast: (t: string) => void }) {
  const [keep, setKeep] = useState(false);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [removed, setRemoved] = useState<string[]>([]);
  const [labels, setLabels] = useState<string[]>([]);
  const [via, setVia] = useState<"token" | "browser" | "">("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    let live = true;
    api.feedback_send({ id: f.id, preview: true, keep_details: keep }).then((p) => {
      if (!live) return;
      setTitle(p.draft.title);
      setBody(p.draft.body);
      setRemoved(p.draft.removed ?? []);
      setLabels(p.draft.labels);
      setVia(p.via);
    }).catch((e) => live && setErr(errText(e)));
    return () => { live = false; };
  }, [f.id, keep]);
  const hits = privateHits(words, title, body);
  const send = async () => {
    setBusy(true);
    setErr("");
    try {
      const out = await api.feedback_send({ id: f.id, title, body });
      toast(out.via === "token" ? "Sent to GitHub." : "Opened in your browser. Press Submit there to share it.");
      onClose();
    } catch (e) {
      setErr(errText(e));
      setBusy(false);
    }
  };
  return (
    <Scrim onClose={onClose}>
      <div className="modal fbsend" role="dialog" aria-modal="true" aria-labelledby="fbs-t">
        <ModalHead id="fbs-t" title="Send to GitHub" onClose={onClose} />
        <p className="hint" style={{ marginTop: 0 }}>
          This becomes a public issue on GabiHert/hadron{labels.length ? `, labelled ${labels.join(" and ")}` : ""}. Check what it says first.
        </p>
        <label className="hint" htmlFor="fbs-title">Title</label>
        <input id="fbs-title" value={title} onChange={(e) => setTitle(e.target.value)} style={{ width: "100%" }} />
        <label className="hint" htmlFor="fbs-body" style={{ display: "block", marginTop: 8 }}>Body</label>
        <textarea id="fbs-body" className="mono" rows={10} value={body} onChange={(e) => setBody(e.target.value)} style={{ width: "100%", resize: "vertical" }} />
        {removed.length > 0 && !keep && <p className="hint">Replaced to keep them private: {removed.join(", ")}.</p>}
        <label className="rrow" style={{ gap: 8, marginTop: 6 }}>
          <input type="checkbox" checked={keep} onChange={(e) => setKeep(e.target.checked)} />
          <span>Keep folder paths, my user name and host name</span>
        </label>
        {hits.length > 0 && (
          <p className="fbwarn" role="alert">It contains words you keep private: <b>{hits.join(", ")}</b>. Take them out to send it.</p>
        )}
        {err && <p className="fbwarn" role="alert">{err}</p>}
        <div className="rrow" style={{ gap: 8, marginTop: 12 }}>
          <span className="hint">{via === "token" ? "Sends with your saved token." : via === "browser" ? "Opens a filled-in issue in your browser; you press Submit there." : ""}</span>
          <span style={{ flex: 1 }} />
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn pri" disabled={busy || !via || !title.trim() || !body.trim() || hits.length > 0} onClick={() => void send()}>
            {via === "browser" ? "Open in browser" : "Send"}
          </button>
        </div>
      </div>
    </Scrim>
  );
}

function Item({ f, words, toast, reload }: { f: Feedback; words: string[]; toast: (t: string) => void; reload: () => void }) {
  const [note, setNote] = useState(f.note ?? "");
  const [url, setUrl] = useState(f.github_url ?? "");
  const [sending, setSending] = useState(false);
  useEffect(() => { setNote(f.note ?? ""); setUrl(f.github_url ?? ""); }, [f.note, f.github_url]);
  const snip = f.text.slice(0, 20);
  const upd = async (patch: { status?: FeedbackStatus; note?: string; github_url?: string }) => {
    try {
      await api.feedback_update({ id: f.id, ...patch });
      reload();
    } catch (e) {
      toast(errText(e));
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
        {f.github_url ? (
          <button className="btn" aria-label={`Open on GitHub: ${snip}`} onClick={() => BrowserOpenURL(f.github_url)}>On GitHub ↗</button>
        ) : (
          <button className="btn" aria-label={`Send to GitHub: ${snip}`} onClick={() => setSending(true)}>Send to GitHub…</button>
        )}
      </div>
      {sending && <SendReview f={f} words={words} toast={toast} onClose={() => { setSending(false); reload(); }} />}
    </div>
  );
}

/** The words to keep private and the sending token. */
function Sharing({ sharing, setSharing, toast }: { sharing: FeedbackSharing | null; setSharing: (s: FeedbackSharing) => void; toast: (t: string) => void }) {
  const saved = (sharing?.private_words ?? []).join("\n");
  const [words, setWords] = useState(saved);
  const [token, setToken] = useState("");
  useEffect(() => setWords(saved), [saved]);
  const saveWords = async () => {
    if (words === saved) return;
    try {
      setSharing(await api.feedback_sharing_set({ private_words: words.split("\n") }));
    } catch (e) {
      toast(errText(e));
    }
  };
  const saveToken = async (t: string) => {
    try {
      setSharing(await api.set_feedback_token({ token: t }));
      setToken("");
      toast(t ? "Token saved in your keychain." : "Token removed. Notes open in the browser now.");
    } catch (e) {
      toast(errText(e));
    }
  };
  return (
    <details className="box fbsharing" style={{ marginBottom: 10 }}>
      <summary>Sharing on GitHub</summary>
      <label className="hint" htmlFor="fb-words" style={{ display: "block", marginTop: 8 }}>
        Words to keep private, one per line. A note that contains one is not sent. They stay on this Mac.
      </label>
      <textarea id="fb-words" rows={4} value={words} placeholder={"Names of clients, projects or people"} onChange={(e) => setWords(e.target.value)} onBlur={() => void saveWords()} style={{ width: "100%", resize: "vertical" }} />
      <p className="hint" style={{ marginBottom: 4 }}>
        {sharing?.has_token
          ? "A token is saved: notes are sent as issues directly."
          : "No token: Send to GitHub opens a filled-in issue in your browser. A fine-grained token with Issues: write on GabiHert/hadron sends directly."}
      </p>
      <div className="rrow" style={{ gap: 8 }}>
        <input type="password" aria-label="Feedback token" placeholder="github_pat_…" value={token} onChange={(e) => setToken(e.target.value)} style={{ flex: 1 }} autoComplete="off" />
        <button className="btn" disabled={!token.trim()} onClick={() => void saveToken(token.trim())}>Save token</button>
        {sharing?.has_token && <button className="btn" onClick={() => void saveToken("")}>Remove</button>}
      </div>
    </details>
  );
}

function Mine({ toast, sharing, setSharing }: { toast: (t: string) => void; sharing: FeedbackSharing | null; setSharing: (s: FeedbackSharing) => void }) {
  const [status, setStatus] = useState("");
  const [items, setItems] = useState<Feedback[] | null>(null);
  const load = useCallback(() => {
    api.feedback_list(status ? { status: status as FeedbackStatus } : {}).then((l) => setItems(l ?? [])).catch((e) => toast(errText(e)));
  }, [status, toast]);
  useEffect(() => {
    load();
    return api.onChanged((e) => { if (!e.entities?.length || e.entities.includes("feedback")) load(); });
  }, [load]);
  const sorted = [...(items ?? [])].sort((a, b) => b.created_at.localeCompare(a.created_at));
  const words = sharing?.private_words ?? [];
  return (
    <>
      <Sharing sharing={sharing} setSharing={setSharing} toast={toast} />
      <div className="rrow" style={{ gap: 8, marginBottom: 10 }}>
        <select aria-label="Status" value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">All</option>
          {STATUSES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
        </select>
      </div>
      {items && sorted.length === 0 ? (
        <p className="hint">No feedback yet. Use the speech bubble in the header, or tell the planner "feedback: ...".</p>
      ) : sorted.map((f) => <Item key={f.id} f={f} words={words} toast={toast} reload={load} />)}
    </>
  );
}

function SharedItem({ s, toast, onImported }: { s: SharedFeedback; toast: (t: string) => void; onImported: () => void }) {
  const imp = async () => {
    try {
      await api.feedback_import_shared({ number: s.number });
      toast(`#${s.number} is in your inbox.`);
      onImported();
    } catch (e) {
      toast(errText(e));
    }
  };
  return (
    <div className="box" style={{ marginBottom: 8 }}>
      <div className="rrow" style={{ gap: 8 }}>
        {s.kind && <span className={"pill " + PILL[s.kind]}>{s.kind}</span>}
        <span className="hint mono">{s.version}</span>
        <span className="hint" style={{ marginLeft: "auto" }}>{day(s.created_at)}</span>
      </div>
      <p style={{ margin: "6px 0", fontWeight: 600 }}>{s.title}</p>
      <div className="rrow" style={{ gap: 8 }}>
        <button className="btn" aria-label={`Open #${s.number} on GitHub`} onClick={() => BrowserOpenURL(s.url)}>#{s.number} ↗</button>
        <span style={{ flex: 1 }} />
        {s.local_id ? <span className="hint">In your inbox</span> : <button className="btn" aria-label={`Import #${s.number}`} onClick={() => void imp()}>Import as a note</button>}
      </div>
    </div>
  );
}

/** Every user's shared feedback: open feedback issues, refreshed on open and every few hours. */
function AllUsers({ toast }: { toast: (t: string) => void }) {
  const [list, setList] = useState<SharedFeedbackList | null>(null);
  const [busy, setBusy] = useState(false);
  const load = useCallback(async (refresh: boolean) => {
    setBusy(true);
    try {
      setList(await api.feedback_list_shared({ refresh }));
    } catch (e) {
      toast(errText(e));
    } finally {
      setBusy(false);
    }
  }, [toast]);
  useEffect(() => {
    void load(true);
    const t = setInterval(() => void load(true), SHARED_REFRESH_MS);
    return () => clearInterval(t);
  }, [load]);
  const issues = list?.issues ?? [];
  return (
    <>
      <div className="rrow" style={{ gap: 8, marginBottom: 10 }}>
        <span className="hint">
          Open feedback issues on GabiHert/hadron{list?.fetched_at ? `, fetched ${day(list.fetched_at)}` : ""}.
        </span>
        <span style={{ flex: 1 }} />
        <button className="btn" disabled={busy} onClick={() => void load(true)}>{busy ? "Refreshing…" : "Refresh"}</button>
      </div>
      {list?.error && <p className="fbwarn" role="alert">{list.error}</p>}
      {list && issues.length === 0 && !list.error && <p className="hint">Nobody has shared feedback yet.</p>}
      {issues.map((s) => <SharedItem key={s.number} s={s} toast={toast} onImported={() => void load(false)} />)}
    </>
  );
}

/** Settings → Feedback: your inbox (with sharing on GitHub) and everyone's shared feedback. */
export function FeedbackTab({ toast }: { toast: (text: string) => void }) {
  const [view, setView] = useState<"mine" | "all">("mine");
  const [sharing, setSharing] = useState<FeedbackSharing | null>(null);
  useEffect(() => {
    api.feedback_sharing_get().then(setSharing).catch(() => {});
  }, []);
  return (
    <>
      <div className="rrow" style={{ gap: 10, marginBottom: 10 }}>
        <h4 style={{ margin: 0 }}>Feedback</h4>
        <div className="seg" role="tablist" aria-label="Feedback view">
          <button role="tab" aria-selected={view === "mine"} className={view === "mine" ? "on" : ""} onClick={() => setView("mine")}>Mine</button>
          <button role="tab" aria-selected={view === "all"} className={view === "all" ? "on" : ""} onClick={() => setView("all")}>All users</button>
        </div>
      </div>
      {view === "mine" ? <Mine toast={toast} sharing={sharing} setSharing={setSharing} /> : <AllUsers toast={toast} />}
    </>
  );
}
