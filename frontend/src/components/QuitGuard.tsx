import { useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { RunningSessions } from "../api";
import { ModalHead, Scrim } from "./Modals";

/** Go's events (internal/quitguard). */
export const EVENT_HOLD = "quit:hold";
export const EVENT_CONFIRM = "quit:confirm";
/** The setting that turns the guard off; on unless false. */
export const SETTING_QUIT_GUARD = "app.quit_guard";

interface HoldEvent {
  state: "start" | "cancel" | "done";
  ms: number;
}

/** How long the "Hold ⌘Q to quit" hint stays after a tap. */
const TAP_HINT_MS = 1800;
const RING = 2 * Math.PI * 15;

/** What the confirmation says about the running sessions. */
export function quitSessionsText(run: RunningSessions | null): string {
  const n = run?.count ?? 0;
  if (n === 0) return "No sessions are running.";
  const are = n === 1 ? "1 session is running" : `${n} sessions are running`;
  return run?.restore_on_launch === false
    ? `${are} and will stop. Turn on Resume running sessions in Settings, General to bring them back next time.`
    : `${are}. ${n === 1 ? "It resumes" : "They resume"} when Hadron opens again.`;
}

/**
 * The quit guard's window side: the "Hold ⌘Q to quit" overlay with a ring that fills while ⌘Q is
 * held, and the confirmation that Quit from the app menu or the Dock asks for. Go decides when to
 * show each; this only shows them, and the confirmation's Quit calls app_quit.
 */
export function QuitGuard({ toast }: { toast: (msg: string) => void }) {
  const [hold, setHold] = useState<null | { phase: "holding" | "tap" | "done"; ms: number; n: number }>(null);
  const [asking, setAsking] = useState(false);
  const [run, setRun] = useState<RunningSessions | null>(null);
  const hide = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    const offHold = api.onEvent(EVENT_HOLD, (d) => {
      const e = d as HoldEvent;
      clearTimeout(hide.current);
      if (e.state === "start") {
        // n re-keys the ring so each hold fills from empty.
        setHold((h) => ({ phase: "holding", ms: e.ms || 1000, n: (h?.n ?? 0) + 1 }));
      } else if (e.state === "cancel") {
        setHold((h) => ({ phase: "tap", ms: e.ms || 1000, n: h?.n ?? 0 }));
        hide.current = setTimeout(() => setHold(null), TAP_HINT_MS);
      } else {
        setHold((h) => ({ phase: "done", ms: e.ms || 1000, n: h?.n ?? 0 }));
      }
    });
    const offConfirm = api.onEvent(EVENT_CONFIRM, () => {
      setRun(null);
      setAsking(true);
      api.sessions_running().then(setRun).catch(() => undefined);
    });
    return () => {
      offHold();
      offConfirm();
      clearTimeout(hide.current);
    };
  }, []);

  const quit = async () => {
    try {
      await api.app_quit();
    } catch (e) {
      setAsking(false);
      toast(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <>
      {hold ? (
        <div className={"quithold " + hold.phase} role="status" aria-live="polite" style={{ ["--hold" as string]: `${hold.ms}ms` }}>
          <svg className="quitring" viewBox="0 0 36 36" width="34" height="34" aria-hidden="true">
            <circle className="track" cx="18" cy="18" r="15" />
            <circle key={hold.n} className="fill" cx="18" cy="18" r="15" style={{ strokeDasharray: RING, strokeDashoffset: hold.phase === "tap" ? RING : undefined }} />
          </svg>
          <span>{hold.phase === "done" ? "Quitting Hadron…" : "Hold ⌘Q to quit"}</span>
        </div>
      ) : null}
      {asking ? (
        <Scrim onClose={() => setAsking(false)}>
          <div className="modal confirm" role="alertdialog" aria-modal="true" aria-labelledby="qg-t" aria-describedby="qg-d">
            <ModalHead id="qg-t" title="Quit Hadron?" onClose={() => setAsking(false)} />
            <div className="mb">
              <p id="qg-d" style={{ margin: 0 }}>{run ? quitSessionsText(run) : "Checking the running sessions…"}</p>
              <p className="hint" style={{ margin: "8px 0 0" }}>Holding ⌘Q for a second quits without asking. Settings, General turns this guard off.</p>
            </div>
            <div className="mf">
              <button className="btn" onClick={() => setAsking(false)}>Cancel</button>
              <button className="btn pri danger" autoFocus onClick={() => void quit()}>Quit</button>
            </div>
          </div>
        </Scrim>
      ) : null}
    </>
  );
}
