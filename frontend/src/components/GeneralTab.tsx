import { useEffect, useState } from "react";
import { CHAPTERS } from "./Tutorial";
import * as api from "../api";
import { SETTING_QUIT_GUARD } from "./QuitGuard";

/** Whether restored sessions that were working are told to continue; on unless false. */
export const SETTING_RESTORE_NUDGE = "sessions.restore_nudge";

/** Idle minutes before a card's session sleeps; 0 is never. Unset is 30. */
export const SETTING_SLEEP_AFTER = "sessions.sleep_after_minutes";
/** Whether idle sessions sleep when macOS reports memory pressure; on unless false. */
export const SETTING_MEMORY_GUARD = "sessions.memory_guard";
export const SLEEP_CHOICES: { minutes: number; label: string }[] = [
  { minutes: 10, label: "after 10 minutes idle" },
  { minutes: 15, label: "after 15 minutes idle" },
  { minutes: 30, label: "after 30 minutes idle" },
  { minutes: 60, label: "after 1 hour idle" },
  { minutes: 120, label: "after 2 hours idle" },
  { minutes: 0, label: "never" },
];

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Settings → General: app-wide behaviour that is not about one workspace. */
export function GeneralTab({ toast, onTutorial }: { toast: (msg: string) => void; onTutorial?: (chapter?: string) => void }) {
  const [restore, setRestore] = useState(true);
  const [guard, setGuard] = useState(true);
  const [nudge, setNudge] = useState(true);
  const [sleepAfter, setSleepAfter] = useState(30);
  const [memGuard, setMemGuard] = useState(true);
  useEffect(() => {
    let alive = true;
    api
      .settings_get({ key: SETTING_SLEEP_AFTER })
      .then((v) => {
        const n = v[SETTING_SLEEP_AFTER];
        if (alive) setSleepAfter(typeof n === "number" ? Math.max(0, n) : n === "never" ? 0 : 30);
      })
      .catch(() => {});
    api
      .settings_get({ key: SETTING_MEMORY_GUARD })
      .then((v) => alive && setMemGuard(v[SETTING_MEMORY_GUARD] !== false))
      .catch(() => {});
    api
      .settings_get({ key: SETTING_RESTORE_NUDGE })
      .then((v) => alive && setNudge(v[SETTING_RESTORE_NUDGE] !== false))
      .catch(() => {});
    api
      .settings_get({ key: "sessions.restore_on_launch" })
      .then((v) => alive && setRestore(v["sessions.restore_on_launch"] !== false))
      .catch(() => {});
    api
      .settings_get({ key: SETTING_QUIT_GUARD })
      .then((v) => alive && setGuard(v[SETTING_QUIT_GUARD] !== false))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);
  const toggle = async (on: boolean) => {
    try {
      await api.settings_update({ values: { "sessions.restore_on_launch": on } });
      setRestore(on);
    } catch (e) {
      toast(errText(e));
    }
  };
  const toggleNudge = async (on: boolean) => {
    try {
      await api.settings_update({ values: { [SETTING_RESTORE_NUDGE]: on } });
      setNudge(on);
    } catch (e) {
      toast(errText(e));
    }
  };
  const saveSleep = async (minutes: number) => {
    try {
      await api.settings_update({ values: { [SETTING_SLEEP_AFTER]: minutes } });
      setSleepAfter(minutes);
    } catch (e) {
      toast(errText(e));
    }
  };
  const toggleMemGuard = async (on: boolean) => {
    try {
      await api.settings_update({ values: { [SETTING_MEMORY_GUARD]: on } });
      setMemGuard(on);
    } catch (e) {
      toast(errText(e));
    }
  };
  const sleepChoices = SLEEP_CHOICES.some((c) => c.minutes === sleepAfter) ? SLEEP_CHOICES : [...SLEEP_CHOICES, { minutes: sleepAfter, label: `after ${sleepAfter} minutes idle` }];
  const toggleGuard = async (on: boolean) => {
    try {
      await api.settings_update({ values: { [SETTING_QUIT_GUARD]: on } });
      setGuard(on);
    } catch (e) {
      toast(errText(e));
    }
  };
  return (
    <>
      <h4>General</h4>
      <label className="inline">
        <input type="checkbox" aria-label="Resume running sessions after Hadron restarts" checked={restore} onChange={(e) => void toggle(e.target.checked)} />
        <span>Resume running sessions after Hadron restarts or updates</span>
      </label>
      <p className="hint">
        Sessions that were running when Hadron quit, updated or crashed start again with their conversation. A session you stopped yourself, or that you ended with /exit, stays stopped. Turning this off also forgets what was running.
      </p>
      <label className="inline" style={{ marginLeft: 22 }}>
        <input type="checkbox" aria-label="Ask sessions that were working to continue" disabled={!restore} checked={restore && nudge} onChange={(e) => void toggleNudge(e.target.checked)} />
        <span>Ask sessions that were working to continue</span>
      </label>
      <p className="hint" style={{ marginLeft: 22 }}>
        A session that was busy on a task when Hadron went down gets “Hadron restarted while you were working. Continue where you left off.” once it is back. Idle sessions are left alone, and the planner hears which cards were asked.
      </p>
      <h5 style={{ margin: "16px 0 6px" }}>Memory</h5>
      <label className="inline">
        <span>Put idle card sessions to sleep</span>
        <select className="in" aria-label="Put idle card sessions to sleep" value={sleepAfter} onChange={(e) => void saveSleep(Number(e.target.value))}>
          {sleepChoices.map((c) => (
            <option key={c.minutes} value={c.minutes}>{c.label}</option>
          ))}
        </select>
      </label>
      <p className="hint">
        A sleeping session's claude process is stopped to give its memory back; the card says asleep. Any message to it (from the planner, an answer, a key in its Terminal) wakes it first by resuming the same conversation. A session that is working, has a question open, or has a test or build still running under it stays awake. The planner never sleeps.
      </p>
      <label className="inline">
        <input type="checkbox" aria-label="Sleep idle sessions when the Mac runs short of memory" checked={memGuard} onChange={(e) => void toggleMemGuard(e.target.checked)} />
        <span>Sleep idle sessions when the Mac runs short of memory</span>
      </label>
      <p className="hint">
        When macOS reports memory pressure, Hadron puts the longest-idle sessions to sleep, one at a time, until it eases, and says which in the planner chat. It never stops a working session: if every session is busy, it only warns.
      </p>
      {onTutorial ? (
        <>
          <h5 style={{ margin: "16px 0 6px" }}>Tutorial</h5>
          <div className="inline" style={{ flexWrap: "wrap", gap: 6 }}>
            <button className="btn" type="button" onClick={() => onTutorial()}>Replay tutorial</button>
            <span className="hint">or jump to</span>
            {CHAPTERS.map((c) => (
              <button key={c.id} className="btn sm" type="button" aria-label={`Tutorial chapter: ${c.title}`} onClick={() => onTutorial(c.id)}>{c.title}</button>
            ))}
          </div>
          <p className="hint">A short tour: the basics, working with the planner, automating with the dashboard, integrations and routines, and driving Hadron over MCP. Also in Help and ⌘K.</p>
        </>
      ) : null}
      <label className="inline" style={{ marginTop: 14 }}>
        <input type="checkbox" aria-label="Guard against quitting by accident" checked={guard} onChange={(e) => void toggleGuard(e.target.checked)} />
        <span>Guard against quitting by accident</span>
      </label>
      <p className="hint">
        Hold ⌘Q for a second to quit: a quick tap only shows a reminder. Quit from the Hadron menu or the Dock asks first and says how many sessions are running. Updates still quit and reopen on their own.
      </p>
    </>
  );
}
