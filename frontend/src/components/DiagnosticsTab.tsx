import { useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { LogLine } from "../api";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";

const LEVELS: [string, string][] = [["", "All"], ["debug", "Debug"], ["info", "Info"], ["warn", "Warn"], ["error", "Error"]];

const clock = (t: string) => /T(\d\d:\d\d:\d\d)/.exec(t)?.[1] ?? t;
const val = (v: unknown) => (typeof v === "string" ? v : JSON.stringify(v));
const fieldsOf = (l: LogLine) => Object.entries(l.fields ?? {}).map(([k, v]) => `${k}=${val(v)}`).join(" ");

/** Settings → Diagnostics: the debug-logging switch, the log folder, a live tail and a copyable report. */
export function DiagnosticsTab({ toast }: { toast: (text: string) => void }) {
  const [on, setOn] = useState(false);
  const [err, setErr] = useState("");
  const [logDir, setLogDir] = useState("");
  const [lines, setLines] = useState<LogLine[]>([]);
  const [level, setLevel] = useState("");
  const [contains, setContains] = useState("");
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let alive = true;
    api.settings_get({ key: "debug.logging" }).then((s) => { if (alive) setOn(s["debug.logging"] === true); }).catch(() => undefined);
    api.diagnostics_get().then((d) => { if (alive) setLogDir(d.log_dir); }).catch(() => undefined);
    return () => { alive = false; };
  }, []);

  useEffect(() => {
    let alive = true;
    const load = () => {
      api.logs_tail({ lines: 200, level, contains }).then((l) => { if (alive) setLines(l ?? []); }).catch(() => undefined);
    };
    load();
    const id = setInterval(load, 2000);
    return () => { alive = false; clearInterval(id); };
  }, [level, contains]);

  useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines]);

  const toggle = async (v: boolean) => {
    setErr("");
    try {
      await api.debug_logging_set({ enabled: v });
      setOn(v);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  const copy = async () => {
    try {
      const d = await api.diagnostics_get();
      const text = [
        `Hadron ${d.version}`,
        `macOS: ${d.macos}`,
        `Arch: ${d.arch}`,
        `Debug logging: ${d.debug_logging ? "on" : "off"}`,
        `Home: ${d.home}`,
        `Settings: ${JSON.stringify(d.settings, null, 2)}`,
        "--- last log lines ---",
        ...(d.recent_log_lines ?? []),
      ].join("\n");
      await navigator.clipboard.writeText(text);
      toast("Diagnostics copied.");
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <>
      <h4>Diagnostics</h4>
      <div className="box">
        <div className="rrow">
          <button className={"switch" + (on ? " on" : "")} onClick={() => void toggle(!on)} aria-pressed={on} aria-label="Debug logging" />
          <span style={{ flex: 1 }}><b style={{ fontWeight: 500 }}>Debug logging</b><br /><span className="hint">Off, Hadron keeps only errors and crashes. On, it also records every action, session start and exit, chat delivery, rule firing, script run and update step, as JSON lines. Secrets, tokens and message text are never written. 7 days are kept, 50 MB at most.</span></span>
        </div>
      </div>
      {err ? <p className="hint" role="alert">{err}</p> : null}
      <div className="rrow" style={{ marginTop: 10, gap: 8 }}>
        <span className="mono hint" style={{ flex: 1 }} data-testid="log-dir">{logDir}</span>
        <button className="btn" disabled={!logDir} onClick={() => BrowserOpenURL(encodeURI("file://" + logDir))}>Show in Finder</button>
        <button className="btn" onClick={() => void copy()}>Copy diagnostics</button>
      </div>
      <div className="rrow" style={{ marginTop: 10, gap: 8 }}>
        <select aria-label="Level" value={level} onChange={(e) => setLevel(e.target.value)}>
          {LEVELS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
        </select>
        <input aria-label="Contains" placeholder="contains" value={contains} onChange={(e) => setContains(e.target.value)} style={{ flex: 1 }} />
      </div>
      <div className="code" ref={box} style={{ marginTop: 8, maxHeight: 320, overflow: "auto" }} aria-label="Log lines">
        {lines.length === 0 ? (
          <span className="hint">No log lines yet. Errors and crashes appear here; turn on debug logging to see everything.</span>
        ) : lines.map((l, i) => (
          <div key={i} className={l.level === "error" ? "bad" : ""}>
            {clock(l.time)} {l.level.toUpperCase().padEnd(5)} {l.msg}{fieldsOf(l) ? " " + fieldsOf(l) : ""}
          </div>
        ))}
      </div>
    </>
  );
}
