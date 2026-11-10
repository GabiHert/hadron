import { useCallback, useState } from "react";
import * as api from "../api";
import type { Column, ScriptConfig, ScriptLog, ScriptView } from "../api";
import type { Board } from "../state/useBoard";
import { useReload } from "./LinksTab";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));
const list = (v: string[] | undefined) => (v && v.length ? v.join(", ") : "none");
const STATUS: Record<string, [string, string]> = {
  enabled: ["ok", "enabled"],
  pending: ["warn", "needs approval"],
  disabled: ["", "disabled"],
  rejected: ["bad", "rejected"],
};

/** Settings → Integrations: what is installed, what each one asks for, approval, config, secrets and activity. */
export function IntegrationsTab({ board }: { board: Board }) {
  const { ws, actions } = board;
  const [ints, setInts] = useState<ScriptView[]>([]);
  const [templates, setTemplates] = useState<{ name: string; description: string }[]>([]);
  const [logs, setLogs] = useState<ScriptLog[]>([]);
  const [name, setName] = useState("");
  const [tpl, setTpl] = useState("");

  const load = useCallback(async () => {
    try {
      const [l, t, g] = await Promise.all([api.integration_list(), api.integration_templates(), api.integration_logs({ limit: 20 })]);
      setInts(l ?? []);
      setTemplates(t ?? []);
      setLogs(g ?? []);
    } catch (e) {
      actions.toast(errText(e));
    }
  }, [actions]);
  useReload(() => void load(), ["scripts", "links", "settings"]);

  const run = async <T,>(fn: () => Promise<T>) => {
    const out = await actions.run(fn);
    await load();
    return out;
  };
  const askPlanner = () => {
    const n = window.prompt("Which service should the planner build an integration for? (for example Jira)");
    if (!n || !n.trim() || !ws) return;
    void run(() => api.chat_send({ workspace_id: ws.id, channel: "planner", text: `Build a ${n.trim()} integration for this workspace: scaffold it, write and test it, then ask me to approve it.` }));
  };

  const enabled = ints.filter((i) => i.status === "enabled");
  const others = ints.filter((i) => i.status !== "enabled");
  return (
    <>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <h4 style={{ flex: 1, margin: 0 }}>Integrations</h4>
        <button className="btn sm pri" onClick={askPlanner}>Ask the planner to build one</button>
      </div>
      <p className="sub">An integration is a small script that links cards to items in another tool. It runs in a sandbox and only gets the network, secrets and app access you approve.</p>
      {!ints.length ? <div className="empty">No integrations yet.</div> : null}
      {[...enabled, ...others].map((i) => (
        <IntegrationBox key={i.id} i={i} run={run} toast={actions.toast} columns={board.columns} />
      ))}
      <div className="box">
        <div className="boxh"><b>Scaffold a new integration</b></div>
        <div className="fg">
          <label htmlFor="int-name">Name</label>
          <input id="int-name" className="in" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. jira" />
          <label htmlFor="int-tpl">Template</label>
          <select id="int-tpl" className="in" value={tpl} onChange={(e) => setTpl(e.target.value)}>
            <option value="">Default</option>
            {templates.map((t) => <option key={t.name} value={t.name}>{t.name}{t.description ? `: ${t.description}` : ""}</option>)}
          </select>
        </div>
        <div className="rrow">
          <button className="btn sm pri" disabled={!name.trim()} onClick={async () => {
            const out = await run(() => api.integration_scaffold({ name: name.trim(), ...(tpl ? { template: tpl } : {}) }));
            if (out) setName("");
          }}>Create</button>
        </div>
      </div>
      <div className="box">
        <div className="boxh"><b>Recent activity</b></div>
        {logs.map((g) => (
          <div className="rrow" key={g.id}>
            <span className="mono" style={{ fontSize: 12 }}>{g.script_name}</span>
            <span className="mono hint">{g.op}</span>
            <span className={"pill " + (g.ok ? "ok" : "bad")}>{g.ok ? "ok" : "failed"}</span>
            <span className="hint">{g.duration_ms} ms</span>
            <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{(g.message ?? "").split("\n")[0]}</span>
          </div>
        ))}
        {!logs.length ? <div className="rrow hint">Nothing has run yet.</div> : null}
      </div>
    </>
  );
}

type Run = <T>(fn: () => Promise<T>) => Promise<T | undefined>;

function IntegrationBox({ i, run, toast, columns }: { i: ScriptView; run: Run; toast: (t: string) => void; columns: Column[] }) {
  const [files, setFiles] = useState<Record<string, string> | null>(null);
  const [open, setOpen] = useState(false);
  const [testing, setTesting] = useState<ScriptView["test_result"]>(null);
  const [st, label] = STATUS[i.status] ?? ["", i.status];
  const tr = testing ?? i.test_result;
  const total = tr ? tr.passed + tr.failed : i.tests;
  const toggleFiles = async () => {
    if (open) return setOpen(false);
    try {
      const [a, b] = await Promise.all([api.integration_read_file({ name: i.name, path: "index.js" }), api.integration_read_file({ name: i.name, path: "manifest.json" })]);
      setFiles({ "index.js": a.content, "manifest.json": b.content });
      setOpen(true);
    } catch (e) {
      toast(errText(e));
    }
  };
  return (
    <div className="box" role="group" aria-label={`Integration ${i.name}`}>
      <div className="boxh">
        <b>{i.name}</b>
        <span className="hint">{i.version}</span>
        <span className={"pill " + st}>{label}</span>
        <div style={{ flex: 1 }} />
        {i.status === "enabled" ? (
          <>
            <button className={"switch" + (i.used_here ? " on" : "")} role="switch" aria-checked={i.used_here} aria-label={`Use ${i.name} in this workspace`} title="Use in this workspace" onClick={() => void run(() => api.integration_use({ name: i.name, enabled: !i.used_here }))} />
            <button className="btn sm" onClick={() => void run(() => api.integration_disable({ name: i.name }))}>Disable</button>
          </>
        ) : null}
        {i.status === "pending" ? (
          <>
            <button className="btn sm pri" onClick={() => void run(() => api.integration_approve({ name: i.name }))}>Approve and enable</button>
            <button className="btn sm" onClick={() => void run(() => api.integration_disable({ name: i.name }))}>Reject</button>
          </>
        ) : null}
        {i.status === "disabled" || i.status === "rejected" ? (
          <button className="btn sm" onClick={() => void run(() => api.integration_approve({ name: i.name }))}>Approve and enable</button>
        ) : null}
      </div>
      {i.description ? <div className="rrow hint">{i.description}</div> : null}
      <div className="fg">
        <label>Network</label><span>{list(i.permissions?.network)}</span>
        <label>Secrets</label><span>{list(i.permissions?.secrets)}</span>
        <label>App API</label><span>{list(i.permissions?.api)}</span>
        {i.permissions?.exec?.length ? <><label>Commands</label><span>{list(i.permissions.exec)}</span></> : null}
        <label>Provides</label><span>{i.fields?.length ? i.fields.map((f) => f.label || f.name).join(", ") : "no fields"}</span>
        <label>Tests</label>
        <span>{tr ? `${tr.passed}/${total} passed` : total ? `${total} not run` : "none"}</span>
      </div>
      {i.new_domains?.length ? (
        <div className="rrow"><span className="pill warn">Asks for new access</span><span>{i.new_domains.join(", ")}</span></div>
      ) : null}
      {i.problems?.length ? <div className="rrow"><span className="pill bad">Problems</span><span>{i.problems.join("; ")}</span></div> : null}
      {tr && tr.failed > 0 ? (
        <details className="rrow" style={{ display: "block" }}>
          <summary>{tr.failed} failing</summary>
          {tr.cases.filter((c) => !c.ok).map((c) => <div key={c.name}><b>{c.name}</b> <span className="mono hint">{c.op}</span>: {c.error}</div>)}
        </details>
      ) : null}
      {i.status === "enabled" ? <ConfigForm i={i} run={run} /> : null}
      {i.status === "enabled" && i.used_here && i.discover ? <Discovery i={i} run={run} toast={toast} columns={columns} /> : null}
      {i.status === "enabled" && i.secrets?.length ? <Secrets i={i} run={run} /> : null}
      <div className="rrow">
        <button className="btn sm" onClick={async () => { const r = await run(() => api.integration_test({ name: i.name })); if (r) setTesting(r); }}>Run tests</button>
        <button className="btn sm quiet" aria-expanded={open} onClick={() => void toggleFiles()}>{open ? "Hide files" : "Files"}</button>
        <div style={{ flex: 1 }} />
        <button className="btn sm danger" onClick={() => { if (window.confirm(`Remove ${i.name}? Its links go with it.`)) void run(() => api.integration_remove({ name: i.name })); }}>Remove</button>
      </div>
      {open && files ? Object.entries(files).map(([p, c]) => <div key={p} className="rrow" style={{ display: "block" }}><div className="hint">{p}</div><pre className="mono" style={{ margin: 0, overflow: "auto", maxHeight: 220, fontSize: 11.5 }}>{c}</pre></div>) : null}
    </div>
  );
}

function ConfigForm({ i, run }: { i: ScriptView; run: Run }) {
  const [draft, setDraft] = useState<Record<string, unknown>>({});
  if (!i.config?.length) return null;
  const cur = (c: ScriptConfig) => (c.key in draft ? draft[c.key] : c.value);
  const set = (k: string, v: unknown) => setDraft((d) => ({ ...d, [k]: v }));
  return (
    <>
      <div className="fg">
        {i.config.map((c) => (
          <ConfigRow key={c.key} c={c} value={cur(c)} onChange={(v) => set(c.key, v)} />
        ))}
      </div>
      <div className="rrow">
        <button className="btn sm pri" disabled={!Object.keys(draft).length} onClick={async () => {
          const all: Record<string, unknown> = {};
          for (const c of i.config) all[c.key] = cur(c);
          const out = await run(() => api.integration_config_set({ name: i.name, config: all }));
          if (out) setDraft({});
        }}>Save settings</button>
      </div>
    </>
  );
}

function ConfigRow({ c, value, onChange }: { c: ScriptConfig; value: unknown; onChange: (v: unknown) => void }) {
  const label = c.label || c.key;
  return (
    <>
      <label htmlFor={`cfg-${c.key}`} title={c.description}>{label}</label>
      {c.type === "bool" ? (
        <input id={`cfg-${c.key}`} type="checkbox" checked={value === true} onChange={(e) => onChange(e.target.checked)} />
      ) : c.type === "number" ? (
        <input id={`cfg-${c.key}`} className="in" type="number" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? "" : Number(e.target.value))} />
      ) : (
        <input id={`cfg-${c.key}`} className="in" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value)} />
      )}
    </>
  );
}

function Secrets({ i, run }: { i: ScriptView; run: Run }) {
  const [vals, setVals] = useState<Record<string, string>>({});
  return (
    <div className="fg">
      {i.secrets.map((s) => (
        <SecretRow key={s.name} name={s.name} set={s.set} value={vals[s.name] ?? ""} onChange={(v) => setVals((p) => ({ ...p, [s.name]: v }))}
          onSave={async () => {
            const out = await run(() => api.integration_secret_set({ name: i.name, secret: s.name, value: vals[s.name] ?? "" }));
            setVals((p) => ({ ...p, [s.name]: "" }));
            return out;
          }}
          onClear={() => void run(() => api.integration_secret_clear({ name: i.name, secret: s.name }))} />
      ))}
    </div>
  );
}

function SecretRow({ name, set, value, onChange, onSave, onClear }: { name: string; set: boolean; value: string; onChange: (v: string) => void; onSave: () => unknown; onClear: () => void }) {
  return (
    <>
      <label htmlFor={`sec-${name}`}>{name}</label>
      <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
        {set ? <span className="pill ok">set</span> : <span className="pill warn">not set</span>}
        <input id={`sec-${name}`} className="in" style={{ flex: 1 }} type="password" autoComplete="off" aria-label={`Secret ${name}`} placeholder={set ? "Enter a new value to replace it" : "Value"} value={value} onChange={(e) => onChange(e.target.value)} />
        <button className="btn sm" disabled={!value} aria-label={`Save secret ${name}`} onClick={() => void onSave()}>Save</button>
        {set ? <button className="btn sm" aria-label={`Clear secret ${name}`} onClick={onClear}>Clear</button> : null}
      </div>
    </>
  );
}

/** Card discovery: new items of the integration become session-less cards in a column. */
function Discovery({ i, run, toast, columns }: { i: ScriptView; run: Run; toast: (t: string) => void; columns: Column[] }) {
  const d = i.discover!;
  const backlog = columns.find((c) => c.role === "backlog");
  const colValue = columns.find((c) => c.id === d.column || c.name.toLowerCase() === d.column.toLowerCase() || c.role === d.column)?.id ?? "";
  const set = (patch: { enabled?: boolean; column?: string; max_per_run?: number; forget?: boolean }) => run(() => api.integration_discover_set({ name: i.name, ...patch }));
  const when = d.last_run_at ? new Date(d.last_run_at).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) : "";
  return (
    <div className="rrow" role="group" aria-label={`Card discovery for ${i.name}`} style={{ display: "block" }}>
      <div className="inline">
        <button className={"switch" + (d.enabled ? " on" : "")} role="switch" aria-checked={d.enabled} aria-label={`Create cards from new ${i.name} items`} onClick={() => void set({ enabled: !d.enabled })} />
        <b style={{ fontWeight: 500 }}>Create cards from new items</b>
        <span className="hint">every {Math.round(i.poll_seconds / 60) || 1} min, without starting a session</span>
      </div>
      <div className="inline" style={{ marginTop: 6 }}>
        <span className="hint">into</span>
        <select className="in" value={colValue} aria-label="Column for new cards" onChange={(e) => void set({ column: e.target.value })}>
          <option value="">{backlog ? `${backlog.name} (backlog)` : "The first active column"}</option>
          {columns.filter((c) => c.id !== backlog?.id).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
        <span className="hint">at most</span>
        <input className="in" type="number" min={1} max={50} style={{ width: 64 }} defaultValue={d.max_per_run || 10} key={d.max_per_run} aria-label="Cards per run"
          onBlur={(e) => { const n = parseInt(e.target.value, 10); if (n >= 1 && n <= 50 && n !== (d.max_per_run || 10)) void set({ max_per_run: n }); }} />
        <span className="hint">cards a run</span>
        <div style={{ flex: 1 }} />
        <button className="btn sm" onClick={async () => {
          const r = await run(() => api.integration_discover_run({ name: i.name }));
          if (r) toast(r.created.length ? `${i.name} added ${r.created.length} card${r.created.length === 1 ? "" : "s"}${r.left ? `; ${r.left} more next run` : ""}.` : `Nothing new in ${i.name}.`);
        }}>Run now</button>
      </div>
      <div className="hint" data-testid={`discover-status-${i.name}`} style={{ marginTop: 6 }}>
        {d.discovered} item{d.discovered === 1 ? "" : "s"} on the board so far; archived cards do not come back.
        {when ? ` Last run ${when}: ${d.last_error ? `failed: ${d.last_error}` : `${d.last_created} new`}.` : ""}
        {d.discovered ? (
          <> <button className="btn sm quiet" onClick={() => { if (window.confirm(`Forget the ${d.discovered} items ${i.name} found? Items whose cards you deleted can then come back.`)) void set({ forget: true }); }}>Forget them</button></>
        ) : null}
      </div>
    </div>
  );
}
