import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { FieldEntry, Link, LinkItem, RemoteSession, ScriptView } from "../api";
import type { Board } from "../state/useBoard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Runs `load` now and again (debounced) on every `changed` event that lists one of `entities`. */
export function useReload(load: () => void, entities: string[], dep = "") {
  const ref = useRef(load);
  ref.current = load;
  const key = entities.join(",");
  useEffect(() => {
    ref.current();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = api.onChanged((e) => {
      if (!(e.entities ?? []).some((x) => key.split(",").includes(x))) return;
      clearTimeout(timer);
      timer = setTimeout(() => ref.current(), 30);
    });
    return () => {
      clearTimeout(timer);
      off();
    };
  }, [key, dep]);
}

/** The links of one card. Reloads when the card changes and on `changed` events for links or cards. */
export function useLinks(cardId: string, onError: (text: string) => void) {
  const [links, setLinks] = useState<Link[]>([]);
  const seq = useRef(0);
  const errRef = useRef(onError);
  errRef.current = onError;
  const load = useCallback(async () => {
    const mine = ++seq.current;
    if (!cardId) {
      setLinks([]);
      return;
    }
    try {
      const out = (await api.link_list({ card: cardId })) ?? [];
      if (mine === seq.current) setLinks(out);
    } catch (e) {
      if (mine === seq.current) errRef.current(errText(e));
    }
  }, [cardId]);
  useEffect(() => setLinks([]), [cardId]);
  useReload(() => void load(), ["links", "cards"], cardId);
  return { links, reload: load };
}

const fieldName = (e: FieldEntry) => e.path.slice(e.source.length + 1);
const show = (v: unknown) => (v === undefined || v === null || v === "" ? "–" : typeof v === "object" ? JSON.stringify(v) : String(v));

/** The card's Links tab: linked items with their fields, actions, cloud agent sessions and the link form. */
export function LinksTab({ board, links, reload }: { board: Board; links: Link[]; reload: () => Promise<void> }) {
  const card = board.selected;
  const { run } = board.actions;
  const [ints, setInts] = useState<ScriptView[]>([]);
  const [schema, setSchema] = useState<FieldEntry[]>([]);
  const [sessions, setSessions] = useState<RemoteSession[]>([]);
  const [loaded, setLoaded] = useState(false);
  const cardId = card?.id ?? "";

  const sessionInts = ints.filter((i) => i.status === "enabled" && i.used_here && i.sessions);
  const sessKey = sessionInts.map((i) => i.name).join(",");
  const loadSessions = useCallback(async () => {
    if (!cardId || !sessKey) {
      setSessions([]);
      return;
    }
    try {
      const all = await Promise.all(sessKey.split(",").map((name) => api.remote_session_list({ integration: name, card: cardId })));
      setSessions(all.flatMap((l) => l ?? []));
    } catch (e) {
      board.actions.toast(errText(e));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cardId, sessKey]);
  useEffect(() => void loadSessions(), [loadSessions]);

  useReload(() => {
    void (async () => {
      try {
        const [list, sc] = await Promise.all([api.integration_list(), api.field_schema_get()]);
        setInts(list ?? []);
        setSchema(sc?.fields ?? []);
        setLoaded(true);
      } catch (e) {
        board.actions.toast(errText(e));
      }
    })();
  }, ["scripts", "fields"]);

  if (!card) return null;
  const usable = ints.filter((i) => i.status === "enabled" && i.used_here && !i.sessions);
  const byName = (n: string) => ints.find((i) => i.name === n);
  const setField = (path: string, value: unknown) =>
    run(async () => {
      await api.card_field_set({ card: card.id, path, value });
      await reload();
    });

  return (
    <div className="cbody">
      {links.map((l) => (
        <LinkBox
          key={l.id}
          link={l}
          view={byName(l.integration)}
          fields={schema.filter((f) => f.source === l.integration)}
          onSet={setField}
          onUnlink={() => void run(async () => { await api.link_remove({ link_id: l.id }); await reload(); })}
          onRefresh={() => void run(async () => { await api.link_refresh({ link_id: l.id }); await reload(); })}
          onAction={(action) => {
            let input: unknown;
            if (action === "comment") {
              const text = window.prompt("Text");
              if (text === null) return;
              input = { text };
            }
            void run(() => api.link_action_run({ link_id: l.id, action, ...(input === undefined ? {} : { input }) }));
          }}
        />
      ))}
      {sessionInts.map((i) => (
        <div className="box" key={i.name} aria-label={`Cloud agent sessions, ${i.name}`}>
          <div className="boxh">
            <span className="src">{i.name}</span>
            <b>Cloud agent sessions</b>
            <div className="spacer" style={{ flex: 1 }} />
            <button className="btn sm" onClick={() => void run(async () => { await api.remote_session_start({ integration: i.name, card: card.id }); await loadSessions(); })}>Start one</button>
          </div>
          {sessions.filter((s) => s.integration === i.name).map((s) => (
            <div className="rrow" key={s.id}>
              <span className={`dot ${s.status === "running" ? "working pulse" : s.status === "failed" ? "blocked" : ""}`} />
              <span style={{ flex: 1, minWidth: 0 }}>{s.title || s.id}</span>
              <span className="hint">{s.status}</span>
              {s.url ? <a href={s.url} target="_blank" rel="noreferrer">Open</a> : null}
              {s.status === "running" ? (
                <button className="btn sm" onClick={() => void run(async () => { await api.remote_session_stop({ integration: i.name, id: s.id }); await loadSessions(); })}>Stop</button>
              ) : null}
            </div>
          ))}
          {!sessions.some((s) => s.integration === i.name) ? <div className="rrow hint">No sessions for this card.</div> : null}
        </div>
      ))}
      {loaded && !usable.length && !links.length && !sessionInts.length ? (
        <div className="empty">This workspace has no integrations yet. Ask the planner to build one, or open Settings, Integrations.</div>
      ) : null}
      {usable.length ? <LinkForm cardId={card.id} usable={usable} onAdd={(integration, item_id) => run(async () => { await api.link_add({ card: card.id, integration, item_id }); await reload(); })} onError={board.actions.toast} /> : null}
    </div>
  );
}

interface LinkBoxProps {
  link: Link;
  view?: ScriptView;
  fields: FieldEntry[];
  onSet: (path: string, value: unknown) => unknown;
  onUnlink: () => void;
  onRefresh: () => void;
  onAction: (action: string) => void;
}

function LinkBox({ link: l, view, fields, onSet, onUnlink, onRefresh, onAction }: LinkBoxProps) {
  const actions = (view?.ops ?? []).filter((o) => o.startsWith("actions.")).map((o) => o.slice("actions.".length));
  return (
    <div className="box" role="group" aria-label={`${l.integration} ${l.item_id}`}>
      <div className="boxh">
        <span className="src">{l.integration}</span>
        <b>
          <span className="mono" style={{ fontSize: 12 }}>{l.item_id}</span>
          {" · "}
          {l.url ? <a href={l.url} target="_blank" rel="noreferrer">{l.title || l.item_id}</a> : l.title}
        </b>
        <div style={{ flex: 1 }} />
        <button className="btn sm" onClick={onRefresh}>Refresh</button>
        <button className="btn sm danger" onClick={onUnlink}>Unlink</button>
      </div>
      {fields.length ? (
        <div className="fg">
          {fields.map((f) => {
            const name = fieldName(f);
            const cur = l.fields?.[name];
            return (
              <FieldRow key={f.path} entry={f} value={cur} onCommit={(v) => onSet(f.path, v)} />
            );
          })}
        </div>
      ) : null}
      {actions.length ? (
        <div className="rrow">
          {actions.map((a) => <button key={a} className="btn sm" onClick={() => onAction(a)}>{a}</button>)}
        </div>
      ) : null}
      <div className="rrow"><span className="hint">Edits go through the integration&apos;s update op, then rules run.</span></div>
    </div>
  );
}

function FieldRow({ entry, value, onCommit }: { entry: FieldEntry; value: unknown; onCommit: (v: string) => unknown }) {
  const cur = value === undefined || value === null ? "" : String(value);
  return (
    <>
      <label>{entry.label}</label>
      {!entry.editable ? (
        <span>{show(value)}</span>
      ) : entry.type === "enum" && entry.options?.length ? (
        <select key={cur} className="in" aria-label={entry.label} defaultValue={cur} onChange={(e) => { if (e.target.value !== cur) void onCommit(e.target.value); }}>
          {cur && !entry.options.includes(cur) ? <option value={cur}>{cur}</option> : null}
          {!cur ? <option value="" /> : null}
          {entry.options.map((o) => <option key={o} value={o}>{o}</option>)}
        </select>
      ) : (
        <input
          key={cur}
          className="in"
          aria-label={entry.label}
          defaultValue={cur}
          onBlur={(e) => { if (e.target.value !== cur) void onCommit(e.target.value); }}
          onKeyDown={(e) => { if (e.key === "Enter") e.currentTarget.blur(); }}
        />
      )}
    </>
  );
}

function LinkForm({ usable, onAdd, onError }: { cardId: string; usable: ScriptView[]; onAdd: (integration: string, itemId: string) => unknown; onError: (t: string) => void }) {
  const [integration, setIntegration] = useState(usable[0]?.name ?? "");
  const [q, setQ] = useState("");
  const [itemId, setItemId] = useState("");
  const [results, setResults] = useState<LinkItem[]>([]);
  const [busy, setBusy] = useState(false);
  const cur = usable.some((i) => i.name === integration) ? integration : (usable[0]?.name ?? "");
  const search = async () => {
    if (!q.trim()) return;
    setBusy(true);
    try {
      const out = await api.link_search({ integration: cur, q: q.trim() });
      setResults(out?.items ?? []);
    } catch (e) {
      onError(errText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="box">
      <div className="boxh"><b>Link an item</b></div>
      <div className="fg">
        <label htmlFor="link-int">Integration</label>
        <select id="link-int" className="in" value={cur} onChange={(e) => { setIntegration(e.target.value); setResults([]); }}>
          {usable.map((i) => <option key={i.name} value={i.name}>{i.name}</option>)}
        </select>
        <label htmlFor="link-q">Search</label>
        <div style={{ display: "flex", gap: 6 }}>
          <input id="link-q" className="in" style={{ flex: 1 }} value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") void search(); }} placeholder="Title or key" />
          <button className="btn sm" disabled={busy || !q.trim()} onClick={() => void search()}>Search</button>
        </div>
        <label htmlFor="link-item">Item id</label>
        <input id="link-item" className="in mono" value={itemId} onChange={(e) => setItemId(e.target.value)} placeholder="e.g. ACME-541" />
      </div>
      {results.map((r) => (
        <button key={r.item_id} className={"rrow" + (itemId === r.item_id ? " on" : "")} style={{ textAlign: "left", width: "100%", background: "none" }} onClick={() => setItemId(r.item_id)}>
          <span className="mono" style={{ fontSize: 12 }}>{r.item_id}</span>
          <span style={{ flex: 1, minWidth: 0 }}>{r.title}</span>
        </button>
      ))}
      <div className="rrow">
        <button className="btn sm pri" disabled={!itemId.trim() || !cur} onClick={async () => { await onAdd(cur, itemId.trim()); setItemId(""); setResults([]); }}>Link</button>
      </div>
    </div>
  );
}
