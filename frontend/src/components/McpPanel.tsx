// Settings → MCP server: where the server listens, the token, how many tools it serves, the live
// call log, and how to connect your own Claude Code.
import { useCallback, useEffect, useState } from "react";
import * as api from "../api";
import type { McpCall, McpInfo } from "../api";
import { clock } from "../lib";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

interface Props {
  /** Replaceable in tests. */
  client?: { info: () => Promise<McpInfo>; calls: () => Promise<McpCall[]> };
  on?: (cb: () => void) => () => void;
  copy?: (text: string) => Promise<void>;
}

const defaultClient = { info: api.mcp_info, calls: () => api.mcp_calls({ limit: 100 }) };
const defaultCopy = (text: string) => navigator.clipboard.writeText(text);

/** abcdef0123456789 shows as ••••••••6789. */
export const maskToken = (t: string) => (t ? "••••••••" + t.slice(-4) : "—");

/** The arguments of a call as one short line. */
export function argsText(args: unknown, max = 90): string {
  if (args === null || args === undefined) return "";
  const s = typeof args === "string" ? args : JSON.stringify(args);
  if (s === "{}" || s === "null") return "";
  return s.length > max ? s.slice(0, max - 1) + "…" : s;
}

const actorClass = (a: string) => (a === "you" ? "" : a === "planner" ? "acc" : a.startsWith("session:") ? "ok" : "warn");

export function CallRow({ call }: { call: McpCall }) {
  return (
    <tr data-testid="call-row">
      <td className="mono">{clock(call.created_at)}</td>
      <td><span className={`pill ${actorClass(call.actor)}`}>{call.actor}</span></td>
      <td className="mono">{call.tool}</td>
      <td className="mono args" title={argsText(call.args, 400)}>{argsText(call.args)}</td>
      <td>
        {call.ok ? <span className="pill ok">ok</span> : <span className="pill bad" title={call.error}>failed</span>}
        <span className="hint"> {call.duration_ms} ms</span>
      </td>
    </tr>
  );
}

export function McpPanel({ client = defaultClient, on = api.onChanged, copy = defaultCopy }: Props) {
  const [info, setInfo] = useState<McpInfo | null>(null);
  const [calls, setCalls] = useState<McpCall[]>([]);
  const [err, setErr] = useState("");
  const [copied, setCopied] = useState("");

  const load = useCallback(() => {
    client.info().then(setInfo).catch((e) => setErr(errText(e)));
    client.calls().then(setCalls).catch((e) => setErr(errText(e)));
  }, [client]);

  useEffect(() => {
    load();
    return on(load);
  }, [load, on]);

  const doCopy = async (what: string, text: string) => {
    try {
      await copy(text);
      setCopied(what);
      setTimeout(() => setCopied((c) => (c === what ? "" : c)), 1500);
    } catch (e) {
      setErr(errText(e));
    }
  };

  const snippet = info ? `claude mcp add --transport http hadron ${info.url} \\\n  --header "Authorization: Bearer <token>"` : "";

  return (
    <>
      <h4>MCP server</h4>
      <p className="sub">
        Claude Code sessions use this server to talk to Hadron, and so can your own Claude. Every Hadron action is one tool, and every call is logged below.
      </p>
      <div className="box">
        <div className="fg">
          <label>Status</label>
          <span>{info?.running ? <span className="pill ok">Listening</span> : <span className="pill bad">Not running</span>}</span>
          <label>Endpoint</label>
          <span className="mono" data-testid="mcp-url">{info?.url || "—"}</span>
          <label>Token</label>
          <span className="inline">
            <span className="mono" data-testid="mcp-token">{maskToken(info?.token ?? "")}</span>
            <button className="btn sm" type="button" disabled={!info?.token} onClick={() => void doCopy("token", info!.token)}>
              {copied === "token" ? "Copied" : "Copy"}
            </button>
          </span>
          <label>Tools</label>
          <span data-testid="mcp-tools">{info ? info.tool_count : "—"}</span>
        </div>
      </div>

      <h4 className="h5">Connect your own Claude</h4>
      <p className="sub">Run this once in a terminal. It has no session header, so Hadron logs your calls as <span className="mono">external</span>.</p>
      <pre className="code" aria-label="Connect command">{snippet}</pre>
      <div>
        <button className="btn" type="button" disabled={!info?.connect_command} onClick={() => void doCopy("command", info!.connect_command)}>
          {copied === "command" ? "Copied" : "Copy command with token"}
        </button>
      </div>

      <h4 className="h5">Live call log</h4>
      {err ? <p className="hint" style={{ color: "var(--bad)" }}>{err}</p> : null}
      {calls.length === 0 ? (
        <div className="empty">No calls yet. They appear here as sessions and clients use the tools.</div>
      ) : (
        <div className="scroll">
          <table className="table calls">
            <thead><tr><th>Time</th><th>Who</th><th>Tool</th><th>Arguments</th><th>Result</th></tr></thead>
            <tbody>{calls.map((c) => <CallRow key={c.id} call={c} />)}</tbody>
          </table>
        </div>
      )}
    </>
  );
}
