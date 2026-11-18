import { useEffect, useState } from "react";
import { CHAPTERS } from "./Tutorial";
import * as api from "../api";
import { SETTING_QUIT_GUARD } from "./QuitGuard";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Settings → General: app-wide behaviour that is not about one workspace. */
export function GeneralTab({ toast, onTutorial }: { toast: (msg: string) => void; onTutorial?: (chapter?: string) => void }) {
  const [restore, setRestore] = useState(true);
  const [guard, setGuard] = useState(true);
  useEffect(() => {
    let alive = true;
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
