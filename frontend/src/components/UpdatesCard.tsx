// The Settings → Updates card. Adapted from the release-updater skill's UpdatesCard.tsx
// (~/.claude/skills/release-updater/templates/frontend): same logic, Hadron's classes, and api.ts
// instead of injected bindings.
import { useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { UpdateStatus } from "../api";

const RUNNING = ["checking", "downloading", "verifying", "installing"];
const MAX_LINES = 5000;
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

interface Props {
  appName?: string;
  /** Replaceable in tests. */
  client?: {
    getStatus: () => Promise<UpdateStatus>;
    check: () => Promise<UpdateStatus>;
    start: () => Promise<unknown>;
    setToken: (token: string) => Promise<UpdateStatus>;
  };
  on?: (event: string, cb: (data: any) => void) => () => void;
  onWhatsNew?: () => void;
}

const defaultClient = {
  getStatus: api.get_update_status,
  check: api.check_for_update,
  start: api.start_update,
  setToken: (token: string) => api.set_update_token({ token }),
};

export function UpdatesCard({ appName = "Hadron", client = defaultClient, on = api.onEvent, onWhatsNew }: Props) {
  const [u, setU] = useState<UpdateStatus | null>(null);
  const [lines, setLines] = useState<string[]>([]);
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
  const [token, setToken] = useState("");
  const [tokenOpen, setTokenOpen] = useState(false);
  const lastSeq = useRef(0);
  const logRef = useRef<HTMLPreElement>(null);

  const load = (s: UpdateStatus) => {
    setU(s);
    if (s.seq >= lastSeq.current) {
      setLines(s.lines ?? []);
      lastSeq.current = s.seq;
    }
  };
  const refresh = () => client.getStatus().then(load).catch((e) => setErr(errText(e)));

  useEffect(() => {
    void refresh();
    const offOut = on("update:output", (e: { seq: number; line: string }) => {
      if (!e || e.seq <= lastSeq.current) return;
      lastSeq.current = e.seq;
      setLines((l) => (l.length >= MAX_LINES ? [...l.slice(l.length - MAX_LINES + 1), e.line] : [...l, e.line]));
    });
    const offState = on("update:state", (e: { state: string; message: string }) => {
      if (!e) return;
      if (e.state === "checking") setLines([]);
      setU((cur) => (cur ? { ...cur, state: e.state, message: e.message ?? "" } : cur));
      if (e.state === "idle" || e.state === "failed") void refresh();
    });
    const offAvail = on("update:available", () => void refresh());
    return () => {
      offOut();
      offState();
      offAvail();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines]);

  const act = async (what: string, f: () => Promise<unknown>) => {
    setBusy(what);
    setErr("");
    try {
      await f();
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy("");
    }
  };

  const state = u?.state ?? "idle";
  const running = RUNNING.includes(state);
  const latest = u?.latest;
  const translocated = !!u?.installedApp?.includes("/AppTranslocation/");
  const stateText: Record<string, string> = {
    checking: "Checking GitHub…",
    downloading: "Downloading…",
    verifying: "Checking the download and its signature…",
    installing: u?.message || `${appName} quits now and reopens when the new version is in place.`,
  };

  return (
    <>
      <h4>Updates</h4>
      <p className="sub">
        {appName} updates itself from GitHub Releases. It checks the SHA-256 and the code signature, swaps the app, and rolls back if the new version doesn&apos;t start.
      </p>
      <div className="box">
        <div className="fg">
          <label>Running</label>
          <span className="mono" data-testid="running-version">{u?.version ? u.version : "—"}</span>
          <label>Latest</label>
          <span className="mono">{latest ? latest.tag : "not checked yet"}</span>
          <label>Checks</label>
          <span>at launch + 1 min, then every 6 h</span>
        </div>
        <div className="inline" style={{ padding: "0 12px 12px" }}>
          <button className="btn" type="button" disabled={!!busy || running} onClick={() => void act("check", async () => load(await client.check()))}>
            {busy === "check" ? "Checking…" : "Check for updates"}
          </button>
          {latest && !u?.upToDate ? (
            <button
              className="btn pri"
              type="button"
              disabled={!!busy || running || !u?.installedApp || translocated}
              onClick={() =>
                void act("update", async () => {
                  await client.start();
                  await refresh();
                })
              }
            >
              {running ? "Updating…" : `Update to ${latest.tag}`}
            </button>
          ) : null}
          {onWhatsNew ? <button className="btn" type="button" onClick={onWhatsNew}>What&apos;s new</button> : null}
          {latest && u?.upToDate && !running && state !== "failed" ? <span className="pill ok">{appName} is up to date</span> : null}
        </div>
      </div>
      {translocated ? (
        <p className="hint" style={{ color: "var(--bad)" }}>
          macOS is running {appName} from a temporary read-only copy, so it can&apos;t update itself. Quit {appName}, drag {appName}.app into Applications in Finder, then open it from there.
        </p>
      ) : null}
      {u && !u.installedApp ? <p className="hint">{appName} is not running from an app bundle, so it cannot update itself.</p> : null}
      {running ? <p role="status">{stateText[state]}</p> : null}
      {state === "failed" && u?.message ? <p className="hint" style={{ color: "var(--bad)" }}>{u.message}</p> : null}
      {err ? <p className="hint" style={{ color: "var(--bad)" }}>{err}</p> : null}
      <p className="hint">
        Updates come from the releases of <span className="mono">{u?.repo}</span>.{" "}
        {u?.hasToken ? "A GitHub token is saved in your keychain." : "A private repo needs a fine-grained token with read-only Contents access."}{" "}
        <button className="btn sm quiet" type="button" onClick={() => setTokenOpen((o) => !o)} aria-expanded={tokenOpen}>
          {u?.hasToken ? "Change token" : "Add token"}
        </button>
      </p>
      {tokenOpen ? (
        <form
          className="inline"
          onSubmit={(e) => {
            e.preventDefault();
            void act("token", async () => {
              load(await client.setToken(token));
              setToken("");
              setTokenOpen(false);
            });
          }}
        >
          <input className="in" type="password" autoComplete="off" spellCheck={false} placeholder="github_pat_…" aria-label="GitHub token" value={token} onChange={(e) => setToken(e.target.value)} />
          <button className="btn pri" type="submit" disabled={!!busy || !token.trim()}>Save in keychain</button>
          {u?.hasToken ? (
            <button className="btn" type="button" disabled={!!busy} onClick={() => void act("token", async () => load(await client.setToken("")))}>Remove</button>
          ) : null}
        </form>
      ) : null}
      {lines.length > 0 ? (
        <details open={running || state === "failed"}>
          <summary>Output ({lines.length} lines)</summary>
          <pre className="code" ref={logRef} style={{ maxHeight: 240, overflow: "auto" }}>{lines.join("\n")}</pre>
        </details>
      ) : null}
    </>
  );
}

/** The header pill: shown when a newer release is known. Clicking it opens Settings → Updates. */
export function useUpdateAvailable(): UpdateStatus | null {
  const [s, setS] = useState<UpdateStatus | null>(null);
  useEffect(() => {
    let live = true;
    const refresh = () => api.get_update_status().then((v) => live && setS(v)).catch(() => undefined);
    void refresh();
    const offA = api.onEvent("update:available", refresh);
    const offS = api.onEvent("update:state", refresh);
    return () => {
      live = false;
      offA();
      offS();
    };
  }, []);
  return s && s.latest && !s.upToDate ? s : null;
}
